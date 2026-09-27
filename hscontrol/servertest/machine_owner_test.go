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
)

// TestMachineOwnerBesideName proves that a response naming a machine by
// its given name also says whose it is: the user for a personal machine,
// the tags (and no user, though the node records who created it) for a
// tagged one, and the owner of a node an audit event is about.
func TestMachineOwnerBesideName(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	alice := srv.CreateUser(t, "owner-alice")
	aliceKey := srv.CreateAPIKey(t, alice)

	changed, err := srv.State().SetPolicy([]byte(`{
		"tagOwners": {"tag:gw": ["owner-alice@"]},
		"grants": [{"src": ["*"], "dst": ["*"], "ip": ["*"]}]
	}`))
	require.NoError(t, err)

	if changed {
		changes, reloadErr := srv.State().ReloadPolicy()
		require.NoError(t, reloadErr)
		srv.App.Change(changes...)
	}

	reporting := servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
		hi.NetInfo = &tailcfg.NetInfo{PreferredDERP: 1, DERPLatency: map[string]float64{"1-v4": 0.010}}
	})
	personal := servertest.NewClient(t, srv, "localhost", servertest.WithUser(alice), reporting)
	tagged := servertest.NewClient(t, srv, "localhost-gw",
		servertest.WithUser(alice), servertest.WithTags("tag:gw"), reporting)

	personal.WaitForPeerCount(t, 1, 10*time.Second)
	tagged.WaitForPeerCount(t, 1, 10*time.Second)

	status, body := apiCall(t, client, aliceKey, http.MethodGet, v1+"/derp/latency", nil)
	require.Equal(t, http.StatusOK, status, body)

	owners := map[string]any{}

	machines, _ := body["machines"].([]any)
	for _, m := range machines {
		machine, _ := m.(map[string]any)
		name, _ := machine["name"].(string)
		owners[name] = machine["owner"]
	}

	require.Len(t, owners, 2, body)
	assert.Equal(t, map[string]any{
		"tags":          []any{},
		"userId":        strconv.FormatUint(uint64(alice.ID), 10),
		"userName":      "owner-alice",
		"displayName":   "",
		"profilePicUrl": "",
	}, owners["localhost"])
	assert.Equal(t, map[string]any{
		"tags":          []any{"tag:gw"},
		"userId":        "",
		"userName":      "",
		"displayName":   "",
		"profilePicUrl": "",
	}, owners["localhost-gw"])

	status, body = apiCall(t, client, aliceKey, http.MethodPost,
		v1+"/node/"+personal.NodeIDString()+"/rename/alices-laptop", nil)
	require.Equal(t, http.StatusOK, status, body)

	status, body = apiCall(t, client, aliceKey, http.MethodGet, v1+"/audit?action=node.rename", nil)
	require.Equal(t, http.StatusOK, status, body)

	events, _ := body["events"].([]any)
	require.Len(t, events, 1, body)
	assert.Equal(t, "owner-alice", field(t, events, "0", "targetOwner", "userName"))
	assert.Nil(t, field(t, events, "0", "actorOwner"), "an API key is not a machine")
}
