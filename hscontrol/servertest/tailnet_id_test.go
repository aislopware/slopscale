package servertest_test

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
