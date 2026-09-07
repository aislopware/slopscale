package servertest_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

const accessWait = 10 * time.Second

// TestAccessRulesEndToEnd proves groups and access rules through the v1
// API and the clients' netmaps. Without a policy file everyone is a peer;
// the first rule turns the tailnet default-deny and admits only what it
// names; a bidirectional rule opens both ways; deleting the last rule goes
// back to allow-all. A pre-auth key with groups enrols the node it
// registers. The subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestAccessRulesEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "access-owner")
	alice := srv.CreateUser(t, "alice")
	bob := srv.CreateUser(t, "bob")
	carol := srv.CreateUser(t, "carol")
	ownerKey := srv.CreateAPIKey(t, owner)

	aliceNode := servertest.NewClient(t, srv, "alice-1", servertest.WithUser(alice))
	bobNode := servertest.NewClient(t, srv, "bob-1", servertest.WithUser(bob))
	carolNode := servertest.NewClient(t, srv, "carol-1", servertest.WithUser(carol))

	for _, c := range []*servertest.TestClient{aliceNode, bobNode, carolNode} {
		c.WaitForCondition(t, "netmap with every peer", accessWait, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid() && len(nm.Peers) == 2
		})
	}

	var engID, serversID string

	t.Run("the builtin group exists and cannot change", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/group", nil)
		require.Equal(t, http.StatusOK, status, body)

		groups, ok := body["groups"].([]any)
		require.True(t, ok)
		require.Len(t, groups, 1)

		all, ok := groups[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "All", all["name"])
		assert.Equal(t, types.GroupBuiltinAll, all["builtin"])

		allID, ok := all["id"].(string)
		require.True(t, ok)

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/group/"+allID, nil)
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/group/"+allID,
			map[string]any{"name": "Everyone"})
		assert.Equal(t, http.StatusBadRequest, status, body)
	})

	t.Run("groups take users and machines", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{
			"name":    "Engineering",
			"userIds": []string{strconv.FormatUint(uint64(alice.ID), 10)},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "group", "id").(string)
		require.True(t, ok)

		engID = id

		assert.Equal(t, []any{strconv.FormatUint(uint64(alice.ID), 10)}, field(t, body, "group", "userIds"))

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{"name": "Engineering"})
		assert.Equal(t, http.StatusConflict, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{"name": "Servers"})
		require.Equal(t, http.StatusOK, status, body)

		id, ok = field(t, body, "group", "id").(string)
		require.True(t, ok)

		serversID = id

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+serversID+"/member",
			map[string]any{"nodeId": bobNode.NodeIDString()})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{bobNode.NodeIDString()}, field(t, body, "group", "nodeIds"))

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+serversID+"/member",
			map[string]any{"nodeId": bobNode.NodeIDString()})
		assert.Equal(t, http.StatusConflict, status, body)
	})

	var ruleID string

	t.Run("the first rule makes the tailnet default-deny", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-rule", map[string]any{
			"name":                "SSH to servers",
			"protocol":            "tcp",
			"ports":               "22",
			"sourceGroupIds":      []string{engID},
			"destinationGroupIds": []string{serversID},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "rule", "id").(string)
		require.True(t, ok)

		ruleID = id

		assert.Equal(t, true, field(t, body, "rule", "enabled"))

		// Alice reaches bob's machine on port 22; bob's machine admits
		// only her; carol is nobody's peer any more.
		aliceNode.WaitForCondition(t, "one peer", accessWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1 && nm.Peers[0].ID() == bobNode.Netmap().SelfNode.ID()
		})
		bobNode.WaitForCondition(t, "filter admitting alice", accessWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1 && len(nm.PacketFilter) == 1
		})
		carolNode.WaitForCondition(t, "no peers", accessWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})

		filter := bobNode.Netmap().PacketFilter[0]
		assert.Equal(t, aliceNode.Netmap().SelfNode.Addresses().AsSlice(), filter.Srcs)
		require.Len(t, filter.Dsts, 2, "the v4 and v6 address of bob's machine")

		for _, dst := range filter.Dsts {
			assert.Equal(t, uint16(22), dst.Ports.First)
			assert.Equal(t, uint16(22), dst.Ports.Last)
		}

		assert.Empty(t, aliceNode.Netmap().PacketFilter, "the rule is one way")

		// A group a rule names cannot be deleted.
		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/group/"+serversID, nil)
		assert.Equal(t, http.StatusConflict, status, body)
	})

	t.Run("a bidirectional rule opens both ways", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1+"/access-rule/"+ruleID, map[string]any{
			"name":                "Engineering and servers",
			"protocol":            "all",
			"bidirectional":       true,
			"sourceGroupIds":      []string{engID},
			"destinationGroupIds": []string{serversID},
		})
		require.Equal(t, http.StatusOK, status, body)

		aliceNode.WaitForCondition(t, "filter admitting bob", accessWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.PacketFilter) == 1
		})
		assert.Equal(t, bobNode.Netmap().SelfNode.Addresses().AsSlice(), aliceNode.Netmap().PacketFilter[0].Srcs)
	})

	t.Run("disabling the last rule restores allow-all", func(t *testing.T) {
		// Without a policy file the rules are all that restricts the
		// tailnet, and the list says so for the console's warnings.
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/access-rule", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "policyFileEnforces"))

		// PATCH flips the switch and keeps every other field.
		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/access-rule/"+ruleID, map[string]any{
			"enabled": false,
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "rule", "enabled"))
		assert.Equal(t, "Engineering and servers", field(t, body, "rule", "name"))
		assert.Equal(t, true, field(t, body, "rule", "bidirectional"))

		carolNode.WaitForCondition(t, "every peer again", accessWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 2
		})

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/access-rule/"+ruleID, nil)
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/access-rule/"+ruleID, nil)
		assert.Equal(t, http.StatusNotFound, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/group/"+serversID, nil)
		assert.Equal(t, http.StatusOK, status, body)
	})

	t.Run("a pre-auth key enrols the node in its groups", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/preauthkey", map[string]any{
			"user":     strconv.FormatUint(uint64(carol.ID), 10),
			"reusable": true,
			"groupIds": []string{engID},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{engID}, field(t, body, "preAuthKey", "groupIds"))

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/preauthkey", map[string]any{
			"user":     strconv.FormatUint(uint64(carol.ID), 10),
			"groupIds": []string{"999"},
		})
		assert.Equal(t, http.StatusNotFound, status, body)
	})
}

// TestPreAuthKeyGroupsEnrolNode registers a node with a key that carries a
// group and finds the node in it.
func TestPreAuthKeyGroupsEnrolNode(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "key-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	group, _, err := srv.State().CreateGroup("Servers", "")
	require.NoError(t, err)

	authKey := srv.CreatePreAuthKeyFromSpec(t, types.PreAuthKeySpec{
		UserID: owner.TypedID(), Reusable: true, Preauthorized: true, Groups: []types.GroupID{group.ID},
	})
	node := servertest.NewClient(t, srv, "server-1", servertest.WithUser(owner), servertest.WithAuthKey(authKey))

	node.WaitForCondition(t, "self", accessWait, func(nm *netmap.NetworkMap) bool { return nm.SelfNode.Valid() })

	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/group/"+group.ID.String(), nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []any{node.NodeIDString()}, field(t, body, "group", "nodeIds"))
}
