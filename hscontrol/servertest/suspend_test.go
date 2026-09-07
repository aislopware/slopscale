package servertest_test

import (
	"net/http"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestSuspendNode proves that suspending a node cuts it off live, that
// the client learns why, that lifting the suspension restores everything
// without a login, and that both edges reach webhook subscribers. The
// subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestSuspendNode(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "suspend-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	receiver := newWebhookReceiver(t)

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
		"url":           receiver.URL,
		"subscriptions": []string{"nodeSuspended", "nodeUnsuspended"},
	})
	require.Equal(t, http.StatusOK, status, body)

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	bob := servertest.NewClient(t, srv, "bob", servertest.WithUser(owner))
	alice.WaitForPeerCount(t, 1, approvalWait)
	bob.WaitForPeerCount(t, 1, approvalWait)

	suspended := func(nm *netmap.NetworkMap) bool {
		_, told := nm.DisplayMessages["headscale-suspended"]

		return !authorized(nm) && len(nm.Peers) == 0 && told
	}

	t.Run("suspending isolates the node live", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/node/"+bob.NodeIDString()+"/suspend", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "node", "suspended"))
		assert.NotNil(t, field(t, body, "node", "suspendedAt"))
		assert.Equal(t, true, field(t, body, "node", "approved"), "suspension leaves the approval alone")

		bob.WaitForCondition(t, "unauthorized, alone and told why", approvalWait, suspended)
		alice.WaitForPeerCount(t, 0, approvalWait)

		delivery := receiver.waitFor(t, types.EventNodeSuspended)
		require.Len(t, delivery.events, 1)
		assert.Contains(t, delivery.events[0].Message, "suspended")
	})

	t.Run("the list and the CLI-facing fields show it", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+bob.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, true, field(t, body, "node", "suspended"))

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+alice.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, false, field(t, body, "node", "suspended"))
		assert.Nil(t, field(t, body, "node", "suspendedAt"))
	})

	t.Run("suspending twice changes nothing and fires no event", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/node/"+bob.NodeIDString()+"/suspend", nil)
		require.Equal(t, http.StatusOK, status, body)

		assert.Equal(t, []types.WebhookEventType{types.EventNodeSuspended}, receiver.types(t))
	})

	t.Run("lifting the suspension restores the node without a login", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+bob.NodeIDString()+"/suspend", map[string]bool{"suspended": false})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "node", "suspended"))
		assert.Nil(t, field(t, body, "node", "suspendedAt"))

		bob.WaitForCondition(
			t,
			"authorized with a peer and no message",
			approvalWait,
			func(nm *netmap.NetworkMap) bool {
				_, told := nm.DisplayMessages["headscale-suspended"]

				return authorized(nm) && len(nm.Peers) == 1 && !told
			},
		)
		alice.WaitForPeerCount(t, 1, approvalWait)

		receiver.waitFor(t, types.EventNodeUnsuspended)
	})

	t.Run("an unknown node is 404", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodPost, v1+"/node/9999/suspend", nil)
		assert.Equal(t, http.StatusNotFound, status)
	})
}
