package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestClientUpdateRollout proves the server asks a connected client that
// allows remote updates and runs an older release to update itself, and
// leaves alone the one whose owner did not allow it.
func TestClientUpdateRollout(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "rollout-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	behind := func(allows bool) func(*tailcfg.Hostinfo) {
		return func(hi *tailcfg.Hostinfo) {
			hi.IPNVersion = "1.102.4-t1234abcd"
			hi.AllowsUpdate = allows
		}
	}

	// Both would start an update if asked, so only the server's choice
	// tells them apart.
	allowed := servertest.NewClient(t, srv, "rollout-allowed", servertest.WithUser(owner),
		servertest.WithHostinfo(behind(true)),
		servertest.WithClientUpdate(tailcfg.C2NUpdateResponse{Enabled: true, Supported: true}))
	declined := servertest.NewClient(t, srv, "rollout-declined", servertest.WithUser(owner),
		servertest.WithHostinfo(behind(false)),
		servertest.WithClientUpdate(tailcfg.C2NUpdateResponse{Enabled: true, Supported: true}))

	allowed.WaitForPeerCount(t, 1, c2nOpsWait)
	declined.WaitForPeerCount(t, 1, c2nOpsWait)

	started := func(c *servertest.TestClient) any {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+c.NodeIDString()+"/client-update", nil)
		require.Equal(t, http.StatusOK, status, body)

		return body["started"]
	}

	// Nothing is asked before the latest release is known.
	now := time.Now()
	srv.App.RollOutClientUpdateForTest(t, now)
	assert.Equal(t, false, started(allowed))

	_, changed := srv.App.GetState().SetLatestClientVersion("1.104.5")
	require.True(t, changed)

	srv.App.RollOutClientUpdateForTest(t, now)
	srv.App.RollOutClientUpdateForTest(t, now.Add(2*time.Minute))

	assert.Equal(t, true, started(allowed), "the client that allows it is asked")
	assert.Equal(t, false, started(declined), "the one that does not is left alone")
}
