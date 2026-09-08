package servertest_test

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestEmptyFilterHidesNewPeers pins that a node whose policy admits
// nobody keeps seeing nobody when another node registers. The full map
// takes its peers from the NodeStore's peer map, which the policy built;
// the incremental NodeAdded path used to skip the policy whenever the
// node's own filter was empty, which is exactly the shape of a policy
// that only admits autogroup:shared before anything is shared, so the
// newcomer leaked into the netmap.
func TestEmptyFilterHidesNewPeers(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	alice := srv.CreateUser(t, "leak-alice")
	bob := srv.CreateUser(t, "leak-bob")

	changed, err := srv.State().SetPolicy([]byte(`{
		"acls": [{"action": "accept", "src": ["autogroup:shared"], "dst": ["autogroup:member:*"]}]
	}`))
	require.NoError(t, err)
	require.True(t, changed)

	aliceNode := servertest.NewClient(t, srv, "leak-alice-1", servertest.WithUser(alice))
	aliceNode.WaitForCondition(t, "a netmap", sharingWait, func(nm *netmap.NetworkMap) bool {
		return nm.SelfNode.Valid()
	})

	bobNode := servertest.NewClient(t, srv, "leak-bob-1", servertest.WithUser(bob))
	bobNode.WaitForCondition(t, "a netmap", sharingWait, func(nm *netmap.NetworkMap) bool {
		return nm.SelfNode.Valid()
	})

	// The leak arrived as an incremental update a few batches after the
	// newcomer's own map, so keep looking for a while.
	assert.Never(t, func() bool {
		return len(aliceNode.Peers()) > 0 || len(bobNode.Peers()) > 0
	}, 300*time.Millisecond, 10*time.Millisecond, "the newcomer must not reach a node the policy hides it from")
}
