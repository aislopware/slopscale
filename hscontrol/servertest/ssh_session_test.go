package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

// TestSSHSession proves the browser SSH session endpoint end to end: a user's
// API key mints a one-time key for an ephemeral node of its own, the response
// describes the target the console dials, a credential without a user and a
// node that does not exist are refused, and the minted key really joins the
// tailnet as an ephemeral node owned by the caller that sees the target.
//
// The handler reports the target's state rather than refusing it, so an
// offline target and one that does not run Tailscale SSH are still 201, with
// online and sshServer false; the console decides what to do with that. A
// caller without the device read scope only learns about nodes a machine
// of theirs can see.
func TestSSHSession(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	user := srv.CreateUser(t, "ssh-operator")
	userKey := srv.CreateAPIKey(t, user)

	target := servertest.NewClient(
		t,
		srv,
		"ssh-target",
		servertest.WithUser(user),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.SSH_HostKeys = []string{"ssh-ed25519 AAAA"}
		}),
	)

	// A live node that never ran tailscale set --ssh.
	noSSH := servertest.NewClient(t, srv, "no-ssh", servertest.WithUser(user))

	target.WaitForPeerCount(t, 1, 10*time.Second)
	noSSH.WaitForPeerCount(t, 1, 10*time.Second)
	servertest.AssertPeerOnline(t, noSSH, "ssh-target")

	// A user's API key opens a session to an online target running Tailscale SSH.
	status, body := apiCall(t, client, userKey, http.MethodPost, v1+"/ssh-session", map[string]any{
		"nodeId": target.NodeIDString(),
	})
	require.Equal(t, http.StatusCreated, status, body)

	authKey, ok := field(t, body, "authKey").(string)
	require.True(t, ok, "authKey missing in %v", body)
	require.NotEmpty(t, authKey)

	assert.Equal(t, srv.URL, field(t, body, "controlUrl"))
	assert.Equal(t, "browser", field(t, body, "hostname"))
	assert.Equal(t, target.NodeIDString(), field(t, body, "target", "nodeId"))
	assert.Equal(t, "ssh-target", field(t, body, "target", "name"))
	assert.Equal(t, true, field(t, body, "target", "sshServer"))
	assert.Equal(t, true, field(t, body, "target", "online"))
	assert.Equal(t, "ssh-operator", field(t, body, "target", "username"))
	assert.NotEmpty(t, field(t, body, "target", "addresses"))

	// The key stops working in five minutes.
	expiresAt, ok := field(t, body, "expiresAt").(string)
	require.True(t, ok, "expiresAt missing in %v", body)

	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), expires, 30*time.Second)

	// The node record says the same, so the console greys the button out
	// before asking.
	status, body = apiCall(t, client, userKey, http.MethodGet, v1+"/node/"+target.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, true, field(t, body, "node", "sshServer"))

	status, body = apiCall(t, client, userKey, http.MethodGet, v1+"/node/"+noSSH.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, false, field(t, body, "node", "sshServer"))

	// A target that does not run Tailscale SSH is described, not refused.
	status, body = apiCall(t, client, userKey, http.MethodPost, v1+"/ssh-session", map[string]any{
		"nodeId": noSSH.NodeIDString(),
	})
	require.Equal(t, http.StatusCreated, status, body)
	assert.Equal(t, false, field(t, body, "target", "sshServer"))
	assert.Equal(t, true, field(t, body, "target", "online"))

	// So is a target that has never connected.
	offline := srv.CreateRegisteredNode(t, user, "offline-target")

	status, body = apiCall(t, client, userKey, http.MethodPost, v1+"/ssh-session", map[string]any{
		"nodeId": offline.StringID(),
	})
	require.Equal(t, http.StatusCreated, status, body)
	assert.Equal(t, false, field(t, body, "target", "online"))
	assert.Equal(t, false, field(t, body, "target", "sshServer"))

	// A credential with no user has no session of its own to open.
	socketKey := srv.CreateAPIKey(t, nil)

	status, body = apiCall(t, client, socketKey, http.MethodPost, v1+"/ssh-session", map[string]any{
		"nodeId": target.NodeIDString(),
	})
	assert.Equal(t, http.StatusForbidden, status, body)

	// A node that does not exist is 404.
	status, body = apiCall(t, client, userKey, http.MethodPost, v1+"/ssh-session", map[string]any{
		"nodeId": "999999",
	})
	assert.Equal(t, http.StatusNotFound, status, body)

	// A member (the default role, no API scopes) is not told about a node
	// none of their machines can see: a member with no machines gets 404
	// for the target, and 201 once a machine of theirs joins and, under the
	// open test policy, sees it.
	member := srv.CreateUser(t, "ssh-member")
	memberKey := srv.CreateAPIKey(t, member)

	status, body = apiCall(t, client, memberKey, http.MethodPost, v1+"/ssh-session", map[string]any{
		"nodeId": target.NodeIDString(),
	})
	assert.Equal(t, http.StatusNotFound, status, body)

	memberNode := servertest.NewClient(t, srv, "member-laptop", servertest.WithUser(member))
	memberNode.WaitForPeerCount(t, 3, 10*time.Second)

	status, body = apiCall(t, client, memberKey, http.MethodPost, v1+"/ssh-session", map[string]any{
		"nodeId": target.NodeIDString(),
	})
	require.Equal(t, http.StatusCreated, status, body)
	assert.Equal(t, "ssh-member", field(t, body, "target", "username"))

	// The key from the first session joins the tailnet as the caller's own
	// ephemeral node and sees the target.
	browser := servertest.NewClient(t, srv, "browser", servertest.WithUser(user), servertest.WithAuthKey(authKey))

	browser.WaitForCondition(t, "browser sees the target", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		if nm == nil {
			return false
		}

		for _, p := range nm.Peers {
			if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == "ssh-target" {
				return true
			}
		}

		return false
	})

	status, body = apiCall(t, client, userKey, http.MethodGet, v1+"/node/"+browser.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, true, field(t, body, "node", "ephemeral"))
	assert.Equal(t, userID(user), field(t, body, "node", "user", "id"))
	assert.Empty(t, field(t, body, "node", "tags"))
}
