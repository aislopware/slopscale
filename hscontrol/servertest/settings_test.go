package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeyExpirySetting sets the tailnet's key expiry cap through the API
// and checks that the next login is bounded by it, that the server info
// endpoint reports the build and the config file's values, and that an
// out-of-range value is refused.
func TestKeyExpirySetting(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithNodeExpiry(90*24*time.Hour))
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "expiry-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/settings", nil)
	require.Equal(t, http.StatusOK, status)
	assert.InDelta(t, 0, body["keyExpiryDays"], 0)
	assert.InDelta(t, 90, body["defaultKeyExpiryDays"], 0)

	servertest.NewClient(t, srv, "before", servertest.WithUser(owner))
	beforeNode := srvNodeView(t, srv, findNodeID(t, srv, "before"))
	require.True(t, beforeNode.Expiry().Valid())
	assert.WithinDuration(t, time.Now().Add(90*24*time.Hour), beforeNode.Expiry().Get(), 5*time.Minute,
		"without a cap the file's default applies")

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings", map[string]any{"keyExpiryDays": 7})
	require.Equal(t, http.StatusOK, status, body)
	assert.InDelta(t, 7, body["keyExpiryDays"], 0)

	servertest.NewClient(t, srv, "after", servertest.WithUser(owner))
	afterNode := srvNodeView(t, srv, findNodeID(t, srv, "after"))
	require.True(t, afterNode.Expiry().Valid())
	assert.WithinDuration(t, time.Now().Add(7*24*time.Hour), afterNode.Expiry().Get(), 5*time.Minute,
		"a login after the cap is bounded by it")

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings", map[string]any{"keyExpiryDays": 400})
	require.Equal(t, http.StatusUnprocessableEntity, status, body)

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/server", nil)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, srv.URL, body["serverUrl"])
	assert.Equal(t, "sqlite", body["database"])
	assert.Equal(t, "2160h0m0s", body["nodeExpiry"])
	assert.NotEmpty(t, body["version"])
	assert.NotEmpty(t, body["ipv4Prefix"])
	assert.InDelta(t, 1, body["derpRegions"], 0)
}
