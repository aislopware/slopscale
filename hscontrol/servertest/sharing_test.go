package servertest_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

const sharingWait = 10 * time.Second

// sharedPeer returns the peer of nm shared by sharer, if any.
func sharedPeer(nm *netmap.NetworkMap, sharer tailcfg.UserID) (tailcfg.NodeView, bool) {
	for _, p := range nm.Peers {
		if p.Sharer() == sharer {
			return p, true
		}
	}

	return tailcfg.NodeView{}, false
}

// TestNodeSharingEndToEnd proves node sharing through the v1 API and the
// clients' netmaps: under a policy that only admits autogroup:shared,
// nobody is anybody's peer until alice shares her node with bob, after
// which bob's device sees it as a peer marked with alice as sharer and
// gets a packet filter for it, while carol's device still sees nothing.
// A member may share only the nodes they own, and unsharing takes it all
// back live. The subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestNodeSharingEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "sharing-owner")
	alice := srv.CreateUser(t, "alice")
	bob := srv.CreateUser(t, "bob")
	carol := srv.CreateUser(t, "carol")
	ownerKey := srv.CreateAPIKey(t, owner)
	aliceKey := srv.CreateAPIKey(t, alice)

	changed, err := srv.State().SetPolicy([]byte(`{
		"acls": [{"action": "accept", "src": ["autogroup:shared"], "dst": ["autogroup:member:*"]}]
	}`))
	require.NoError(t, err)
	require.True(t, changed)

	aliceNode := servertest.NewClient(t, srv, "alice-1", servertest.WithUser(alice))
	bobNode := servertest.NewClient(t, srv, "bob-1", servertest.WithUser(bob))
	carolNode := servertest.NewClient(t, srv, "carol-1", servertest.WithUser(carol))

	for _, c := range []*servertest.TestClient{aliceNode, bobNode, carolNode} {
		c.WaitForCondition(t, "netmap without peers", sharingWait, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid() && len(nm.Peers) == 0
		})
	}

	aliceUserID := tailcfg.UserID(alice.ID)
	bobID := strconv.FormatUint(uint64(bob.ID), 10)
	shareURL := v1 + "/node/" + aliceNode.NodeIDString() + "/share"

	t.Run("a member shares only their own node", func(t *testing.T) {
		status, body := apiCall(t, client, aliceKey, http.MethodPost,
			v1+"/node/"+bobNode.NodeIDString()+"/share", map[string]string{"userId": bobID})
		assert.Equal(t, http.StatusForbidden, status, body)

		status, body = apiCall(t, client, aliceKey, http.MethodPost, shareURL,
			map[string]string{"userId": strconv.FormatUint(uint64(alice.ID), 10)})
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, aliceKey, http.MethodPost, shareURL, map[string]string{"userId": bobID})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{bobID}, field(t, body, "node", "sharedWith"))

		status, body = apiCall(t, client, aliceKey, http.MethodPost, shareURL, map[string]string{"userId": bobID})
		assert.Equal(t, http.StatusConflict, status, body)
	})

	t.Run("the sharee sees the node as shared and reaches it", func(t *testing.T) {
		bobNode.WaitForCondition(t, "shared peer", sharingWait, func(nm *netmap.NetworkMap) bool {
			_, ok := sharedPeer(nm, aliceUserID)

			return ok
		})

		nm := bobNode.Netmap()
		peer, _ := sharedPeer(nm, aliceUserID)
		assert.Equal(t, aliceUserID, peer.User(), "the owner stays the node's user")

		_, ok := nm.UserProfiles[aliceUserID]
		assert.True(t, ok, "the sharer's profile travels with the shared node")

		// A packet filter lists what may reach the node, so the shared
		// node admits the sharee's device and the sharee's device admits
		// nothing: the share is one way.
		aliceNode.WaitForCondition(t, "filter admitting the sharee", sharingWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1 && len(nm.PacketFilter) == 1
		})
		assert.Equal(t, bobNode.Netmap().SelfNode.Addresses().AsSlice(),
			aliceNode.Netmap().PacketFilter[0].Srcs, "only the sharee's device is admitted")
		assert.Empty(t, nm.PacketFilter, "the shared node cannot initiate to the sharee's device")
		assert.Empty(t, carolNode.Peers(), "a user the node is not shared with sees nothing")

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+aliceNode.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, []any{bobID}, field(t, body, "node", "sharedWith"))
	})

	t.Run("unsharing takes the access back live", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, shareURL+"/"+bobID, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{}, field(t, body, "node", "sharedWith"))

		bobNode.WaitForCondition(t, "no peers", sharingWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})
		aliceNode.WaitForCondition(t, "no peers", sharingWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, shareURL+"/"+bobID, nil)
		assert.Equal(t, http.StatusBadRequest, status, body)
	})
}
