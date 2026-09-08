package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestEphemeralLoginRequest covers a client that asks to be ephemeral in
// its register request rather than through an ephemeral pre-auth key: a
// tailscaled with mem: state, a tsnet server with Ephemeral, the wasm
// client. The node is ephemeral whichever way it registered, so it is
// deleted on logout and garbage collected after it drops off.
func TestEphemeralLoginRequest(t *testing.T) {
	t.Parallel()

	t.Run("interactive login is deleted on logout", func(t *testing.T) {
		t.Parallel()

		srv := servertest.NewServer(t)
		user := srv.CreateUser(t, "eph-login-user")
		apiKey := srv.CreateAPIKey(t, nil)
		httpClient := srv.HTTPClient(t)
		v1 := srv.URL + "/api/v1"

		login := servertest.NewPendingLogin(t, srv, "eph-login", servertest.WithEphemeralLogin())

		status, body := apiCall(t, httpClient, apiKey, http.MethodPost,
			v1+"/node/register?user="+user.Name+"&key="+login.AuthID.String(), nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "node", "ephemeral"), "the API reports the request's flag")
		assert.Nil(t, field(t, body, "node", "preAuthKey"), "no key was involved")

		client := login.Wait(t, 10*time.Second)
		client.WaitForUpdate(t, 10*time.Second)

		node := nodeByHostname(t, srv, "eph-login")
		assert.True(t, node.Ephemeral())
		assert.True(t, node.IsEphemeral())
		assert.Equal(t, 1, srv.State().ListEphemeralNodes().Len(), "the GC list includes it")

		require.NoError(t, client.LogoutAndDisconnect(t.Context()))

		_, found := srv.State().GetNodeByID(node.ID())
		assert.False(t, found, "an ephemeral node is deleted on logout")
	})

	t.Run("pre-auth key login is collected after it drops off", func(t *testing.T) {
		t.Parallel()

		srv := servertest.NewServer(t, servertest.WithEphemeralTimeout(3*time.Second))
		user := srv.CreateUser(t, "eph-key-user")

		regular := servertest.NewClient(t, srv, "eph-key-regular", servertest.WithUser(user))
		asked := servertest.NewClient(t, srv, "eph-key-asked",
			servertest.WithUser(user), servertest.WithEphemeralLogin())

		regular.WaitForPeers(t, 1, 10*time.Second)
		asked.WaitForPeers(t, 1, 10*time.Second)

		node := nodeByHostname(t, srv, "eph-key-asked")
		assert.True(t, node.AuthKey().Valid() && !node.AuthKey().Ephemeral(), "the key itself is not ephemeral")
		assert.True(t, node.IsEphemeral(), "the request made the node ephemeral")

		asked.Disconnect(t)

		// Grace period plus the ephemeral timeout, as in TestEphemeralNodes.
		regular.WaitForCondition(t, "ephemeral peer removed", 60*time.Second, func(nm *netmap.NetworkMap) bool {
			for _, p := range nm.Peers {
				if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == "eph-key-asked" {
					return false
				}
			}

			return true
		})

		_, found := srv.State().GetNodeByID(node.ID())
		assert.False(t, found)
	})
}
