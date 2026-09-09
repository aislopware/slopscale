package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestAccessGraph proves the access graph endpoint: two users' nodes with an ACL
// granting alice to bob over tcp:22 show bob in alice's reachable list with the port,
// and alice in bob's reachedBy list; a node with no rules has empty lists;
// and asking for a nonexistent node returns 404.
func TestAccessGraph(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "graph-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	alice := srv.CreateUser(t, "alice")
	bob := srv.CreateUser(t, "bob")
	carol := srv.CreateUser(t, "carol")

	aliceNode := servertest.NewClient(t, srv, "alice-node", servertest.WithUser(alice))
	bobNode := servertest.NewClient(t, srv, "bob-node", servertest.WithUser(bob))
	carolNode := servertest.NewClient(t, srv, "carol-node", servertest.WithUser(carol))

	aliceNode.WaitForCondition(t, "self valid", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})
	bobNode.WaitForCondition(t, "self valid", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})
	carolNode.WaitForCondition(t, "self valid", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})

	// Policy granting alice -> bob tcp:22.
	changed, err := srv.State().SetPolicy([]byte(`{
		"acls": [
			{"action": "accept", "src": ["alice@"], "dst": ["bob@:22"]}
		]
	}`))
	require.NoError(t, err)
	require.True(t, changed)

	changes, err := srv.State().ReloadPolicy()
	require.NoError(t, err)
	srv.App.Change(changes...)

	// Alice can reach bob.
	aliceNode.WaitForCondition(t, "alice sees bob as peer", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		for _, p := range nm.Peers {
			if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == "bob-node" {
				return true
			}
		}

		return false
	})

	// GET /api/v1/access-graph?node=<alice> lists bob under reachable with the port.
	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/access-graph?node="+aliceNode.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)

	reachable, ok := field(t, body, "reachable").([]any)
	require.True(t, ok)
	require.Len(t, reachable, 1)
	assert.Equal(t, bobNode.NodeIDString(), field(t, reachable, "0", "dst"))
	assert.Equal(t, []any{"22"}, field(t, reachable, "0", "ports"))

	reachedBy, ok := field(t, body, "reachedBy").([]any)
	require.True(t, ok)
	assert.Empty(t, reachedBy)

	// Bob's graph lists alice under reachedBy.
	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/access-graph?node="+bobNode.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)

	reachedBy, ok = field(t, body, "reachedBy").([]any)
	require.True(t, ok)
	require.Len(t, reachedBy, 1)
	assert.Equal(t, aliceNode.NodeIDString(), field(t, reachedBy, "0", "src"))
	assert.Equal(t, []any{"22"}, field(t, reachedBy, "0", "ports"))

	reachable, ok = field(t, body, "reachable").([]any)
	require.True(t, ok)
	assert.Empty(t, reachable)

	// A node with no rules has empty lists.
	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/access-graph?node="+carolNode.NodeIDString(), nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Empty(t, field(t, body, "reachable"))
	assert.Empty(t, field(t, body, "reachedBy"))

	// A missing node is 404.
	status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/access-graph?node=999999", nil)
	assert.Equal(t, http.StatusNotFound, status)
}
