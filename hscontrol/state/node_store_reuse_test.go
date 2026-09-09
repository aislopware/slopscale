package state

import (
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingPeers is a [PeersFunc] that makes every node a peer of every
// other and counts how often the store asked for it.
func countingPeers(calls *atomic.Int64) PeersFunc {
	return func(nodes []types.NodeView) map[types.NodeID][]types.NodeView {
		calls.Add(1)

		out := make(map[types.NodeID][]types.NodeView, len(nodes))

		for _, n := range nodes {
			for _, p := range nodes {
				if p.ID() != n.ID() {
					out[n.ID()] = append(out[n.ID()], p)
				}
			}
		}

		return out
	}
}

// TestNodeStoreKeepsPeerMapAcrossUnrelatedWrites pins that a write which
// changes no input of the peer relationship carries the previous peer map
// forward, with the peers' views pointing at the new copies, and that a
// write which does change one recomputes it.
func TestNodeStoreKeepsPeerMapAcrossUnrelatedWrites(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	nodes := benchNodes(3)
	initial := make(types.Nodes, 0, len(nodes))

	for _, n := range nodes {
		n.ApprovedAt = new(time.Now())
		initial = append(initial, new(n))
	}

	store := NewNodeStore(initial, countingPeers(&calls), 1, time.Millisecond)
	store.Start()

	defer store.Stop()

	require.Equal(t, int64(1), calls.Load(), "the initial snapshot builds the peer map")
	require.Equal(t, 2, store.ListPeers(1).Len())

	seen := time.Now().Add(-time.Minute)

	_, ok := store.UpdateNode(2, func(n *types.Node) { n.LastSeen = &seen })
	require.True(t, ok)
	assert.Equal(t, int64(1), calls.Load(), "a LastSeen update keeps the peer map")

	peers := store.ListPeers(1)
	require.Equal(t, 2, peers.Len())

	for _, p := range peers.All() {
		if p.ID() == 2 {
			require.True(t, p.LastSeen().Valid(), "the peer view must point at the updated copy")
			assert.WithinDuration(t, seen, p.LastSeen().Get(), 0)
		}
	}

	_, ok = store.UpdateNode(2, func(n *types.Node) { n.Tags = []string{"tag:server"} })
	require.True(t, ok)
	assert.Equal(t, int64(2), calls.Load(), "a tag change recomputes the peer map")

	_, ok = store.UpdateNode(3, func(n *types.Node) { n.SuspendedAt = new(time.Now()) })
	require.True(t, ok)
	assert.Equal(t, int64(3), calls.Load(), "a suspension recomputes the peer map")

	extra := benchNodes(4)[4]
	store.PutNode(extra)
	assert.Equal(t, int64(4), calls.Load(), "a new node recomputes the peer map")
	assert.Equal(t, 3, store.ListPeers(1).Len())

	// DeleteNode does not wait for its batch.
	store.DeleteNode(4)
	require.Eventually(t, func() bool { return calls.Load() == 5 }, time.Second, time.Millisecond,
		"a deleted node recomputes the peer map")
	assert.Equal(t, 2, store.ListPeers(1).Len())

	store.RebuildPeerMaps()
	assert.Equal(t, int64(6), calls.Load(), "an explicit rebuild recomputes the peer map")
}

// BenchmarkUpdateNodePeerMap compares a write that keeps the peer map
// with one that recomputes it, over a tailnet where every node peers
// with every other, so the recompute is the O(n²) scan production pays
// through the policy.
func BenchmarkUpdateNodePeerMap(b *testing.B) {
	var calls atomic.Int64

	nodes := benchNodes(benchNodeCount)
	initial := make(types.Nodes, 0, len(nodes))

	for _, n := range nodes {
		n.ApprovedAt = new(time.Now())
		initial = append(initial, new(n))
	}

	store := NewNodeStore(initial, countingPeers(&calls), 1, time.Millisecond)
	store.Start()

	defer store.Stop()

	b.Run("last_seen_only", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			seen := time.Now()

			store.UpdateNode(1, func(n *types.Node) { n.LastSeen = &seen })
		}
	})

	b.Run("tags_changed", func(b *testing.B) {
		b.ReportAllocs()

		i := 0
		for b.Loop() {
			i++
			tag := "tag:" + strconv.Itoa(i)

			store.UpdateNode(1, func(n *types.Node) { n.Tags = []string{tag} })
		}
	})
}
