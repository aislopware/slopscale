package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

const approvalWait = 10 * time.Second

func authorized(nm *netmap.NetworkMap) bool {
	return nm.SelfNode.Valid() && nm.SelfNode.MachineAuthorized()
}

// TestApprovalEndToEnd proves device and users approval through the HTTP
// surface and the clients' netmaps: with device approval on, a node
// registered with an ordinary key waits, sees no peers and is seen by
// none, and its client reports NeedsMachineAuth; approving it through v1
// (and withdrawing through v2) flips all of that live. A preauthorized
// key skips the wait. Users approval holds login-created users until an
// administrator acts, and switching either approval off admits everything
// that was waiting. The subtests build on one another, so they run in
// order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestApprovalEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"
	v2 := srv.URL + "/api/v2"

	owner := srv.CreateUser(t, "approval-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	setSettings := func(body map[string]bool) map[string]any {
		status, resp := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings", body)
		require.Equal(t, http.StatusOK, status, resp)

		return resp
	}

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	bob := servertest.NewClient(t, srv, "bob", servertest.WithUser(owner))
	alice.WaitForPeerCount(t, 1, approvalWait)
	bob.WaitForPeerCount(t, 1, approvalWait)

	t.Run("approval off admits at registration", func(t *testing.T) {
		require.True(t, authorized(alice.Netmap()))

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+alice.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, true, field(t, body, "node", "approved"))
		assert.NotNil(t, field(t, body, "node", "approvedAt"))

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/settings", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, false, body["devicesApprovalOn"])
		assert.Equal(t, false, body["usersApprovalOn"])
	})

	// Clients are created on the parent test: a client is torn down with the
	// testing.TB it was created for, so one made inside a subtest would be
	// gone before the next subtest looks at it.
	got := setSettings(map[string]bool{"devicesApprovalOn": true})
	assert.Equal(t, true, got["devicesApprovalOn"])

	plainKey := srv.CreatePreAuthKeyFromSpec(t, types.PreAuthKeySpec{
		UserID: owner.TypedID(), Reusable: true, Preauthorized: false,
	})
	carol := servertest.NewClient(t, srv, "carol", servertest.WithUser(owner), servertest.WithAuthKey(plainKey))

	t.Run("device approval on holds new nodes", func(t *testing.T) {
		carol.WaitForCondition(t, "self not authorized", approvalWait, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid() && !nm.SelfNode.MachineAuthorized()
		})
		assert.Empty(t, carol.Netmap().Peers, "a waiting node has no peers")
		assert.Len(t, alice.Netmap().Peers, 1, "a waiting node is nobody's peer")

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+carol.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, false, field(t, body, "node", "approved"))

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v2+"/device/"+carol.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, false, body["authorized"])
	})

	t.Run("approving through v1 admits the node live", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/node/"+carol.NodeIDString()+"/approve", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "node", "approved"))

		carol.WaitForCondition(t, "authorized with peers", approvalWait, func(nm *netmap.NetworkMap) bool {
			return authorized(nm) && len(nm.Peers) == 2
		})
		alice.WaitForPeerCount(t, 2, approvalWait)
		bob.WaitForPeerCount(t, 2, approvalWait)
	})

	t.Run("withdrawing through v2 isolates the node live", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodPost,
			v2+"/device/"+carol.NodeIDString()+"/authorized", map[string]bool{"authorized": false})
		require.Equal(t, http.StatusOK, status)

		carol.WaitForCondition(t, "unauthorized without peers", approvalWait, func(nm *netmap.NetworkMap) bool {
			return !authorized(nm) && len(nm.Peers) == 0
		})
		alice.WaitForPeerCount(t, 1, approvalWait)
		bob.WaitForPeerCount(t, 1, approvalWait)
	})

	// The server's own keys are preauthorized.
	dave := servertest.NewClient(t, srv, "dave", servertest.WithUser(owner))

	t.Run("a preauthorized key skips the wait", func(t *testing.T) {
		dave.WaitForCondition(t, "authorized at registration", approvalWait, func(nm *netmap.NetworkMap) bool {
			return authorized(nm) && len(nm.Peers) == 2
		})
		alice.WaitForPeerCount(t, 2, approvalWait)
	})

	var login *types.User

	t.Run("users approval on holds login-created users", func(t *testing.T) {
		setSettings(map[string]bool{"usersApprovalOn": true})

		var err error

		login, _, err = srv.State().CreateUserFromLogin(types.User{Name: "login-user"})
		require.NoError(t, err)
		require.Nil(t, login.ApprovedAt)

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/user?id="+userID(login), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, false, field(t, body, "users", "0", "approved"))

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v2+"/users/"+userID(login), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "needs-approval", body["status"])

		// An administrator-created user never waits.
		created := srv.CreateUser(t, "created-user")
		assert.NotNil(t, created.ApprovedAt)
	})

	t.Run("approving a user through v2", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodPost, v2+"/users/"+userID(login)+"/approve", nil)
		require.Equal(t, http.StatusOK, status)

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v2+"/users/"+userID(login), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "active", body["status"])
	})

	t.Run("withdrawing a user withdraws their nodes", func(t *testing.T) {
		disabled := false

		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(owner)+"/approve",
			map[string]*bool{"approved": &disabled})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "user", "approved"))

		alice.WaitForCondition(t, "unauthorized without peers", approvalWait, func(nm *netmap.NetworkMap) bool {
			return !authorized(nm) && len(nm.Peers) == 0
		})
		bob.WaitForCondition(t, "unauthorized without peers", approvalWait, func(nm *netmap.NetworkMap) bool {
			return !authorized(nm) && len(nm.Peers) == 0
		})
	})

	t.Run("switching approvals off admits everything waiting", func(t *testing.T) {
		got := setSettings(map[string]bool{"devicesApprovalOn": false, "usersApprovalOn": false})
		assert.Equal(t, false, got["devicesApprovalOn"])
		assert.Equal(t, false, got["usersApprovalOn"])

		// Owner is still withdrawn: user approval is explicit once withdrawn,
		// so re-approve and expect every node of theirs (alice, bob, carol,
		// dave) to come back with device approval off.
		status, _ := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(owner)+"/approve", nil)
		require.Equal(t, http.StatusOK, status)

		for _, c := range []*servertest.TestClient{alice, bob, carol, dave} {
			c.WaitForCondition(t, "authorized with 3 peers", approvalWait, func(nm *netmap.NetworkMap) bool {
				return authorized(nm) && len(nm.Peers) == 3
			})
		}
	})
}
