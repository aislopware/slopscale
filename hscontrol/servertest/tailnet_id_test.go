package servertest_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tsclient "tailscale.com/client/tailscale/v2"
	"tailscale.com/types/netmap"
)

// TestTailnetIDReachesOnlyTheSelfNode pins [tailcfg.Node.StableTailnetID]
// to the hosted control plane's shape: set on MapResponse.Node, which the
// client shows as CurrentTailnet.StableID, and empty on every peer.
func TestTailnetIDReachesOnlyTheSelfNode(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	bob := servertest.NewClient(t, srv, "bob", servertest.WithUser(owner))

	alice.WaitForPeerCount(t, 1, 5*time.Second)
	bob.WaitForPeerCount(t, 1, 5*time.Second)

	id := srv.State().TailnetID()
	require.False(t, id.IsZero(), "a server always has a tailnet ID")

	for _, client := range []*servertest.TestClient{alice, bob} {
		client.WaitForCondition(t, "self node carries the tailnet ID", 5*time.Second,
			func(nm *netmap.NetworkMap) bool {
				return nm.StableTailnetID() == id
			},
		)

		for _, peer := range client.Peers() {
			assert.Empty(t, peer.StableTailnetID(), "%s sees the tailnet ID on peer %s", client, peer.Name())
		}
	}
}

// TestTailnetIDInTheAPIs pins where an operator finds the tailnet ID and
// that /api/v2 takes it in place of "-", while any other tailnet, even one
// that differs only in case, is the same 404 as before.
func TestTailnetIDInTheAPIs(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")
	apiKey := srv.CreateAPIKey(t, owner)
	id := string(srv.State().TailnetID())

	status, body := apiCall(t, srv.HTTPClient(t), apiKey, http.MethodGet, srv.URL+"/api/v1/server", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, id, field(t, body, "tailnetId"))

	base, err := url.Parse(srv.URL)
	require.NoError(t, err)

	client := func(tailnet string) *tsclient.Client {
		return &tsclient.Client{BaseURL: base, APIKey: apiKey, Tailnet: tailnet, HTTP: srv.HTTPClient(t)}
	}

	for _, tailnet := range []string{"-", id} {
		_, err := client(tailnet).Users().List(t.Context(), nil, nil)
		require.NoError(t, err, "tailnet %q", tailnet)
	}

	for _, tailnet := range []string{"example.com", strings.ToLower(id), id + "x", "T0000000000CNTRL"} {
		_, err := client(tailnet).Users().List(t.Context(), nil, nil)
		require.Error(t, err, "tailnet %q", tailnet)
		assert.True(t, tsclient.IsNotFound(err), "tailnet %q: %v", tailnet, err)

		status, body := apiCall(t, srv.HTTPClient(t), apiKey, http.MethodGet,
			srv.URL+"/api/v2/tailnet/"+url.PathEscape(tailnet)+"/settings", nil)
		assert.Equal(t, http.StatusNotFound, status, "tailnet %q", tailnet)
		assert.Equal(t, "tailnet not found", field(t, body, "message"), "tailnet %q", tailnet)
	}
}
