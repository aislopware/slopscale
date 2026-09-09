package state

import (
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/types/key"
)

// benchNodeCount is the tailnet size the snapshot benchmarks build. It is
// large enough that the per-node index work dominates the fixed cost of a
// rebuild, which is what the write path pays on every batch.
const benchNodeCount = 1000

// noPeersFunc is a stub [PeersFunc] that does no work, so a snapshot
// benchmark measures the index building rather than the policy.
func noPeersFunc(_ []types.NodeView) map[types.NodeID][]types.NodeView {
	return nil
}

// benchNodes builds n nodes spread over a handful of users, each with its
// own machine and node key, as a real tailnet has.
func benchNodes(n int) map[types.NodeID]types.Node {
	now := time.Now()
	nodes := make(map[types.NodeID]types.Node, n)

	for i := range n {
		id := types.NodeID(i + 1)
		machineKey := key.NewMachine()
		nodeKey := key.NewNode()
		userID := uint(i%10) + 1
		ipv4 := netip.AddrFrom4([4]byte{100, 64, byte(i / 256), byte(i % 256)})

		nodes[id] = types.Node{
			ID:             id,
			MachineKey:     machineKey.Public(),
			NodeKey:        nodeKey.Public(),
			Hostname:       "bench-node",
			GivenName:      "bench-node-" + strconv.Itoa(i),
			UserID:         &userID,
			User:           &types.User{Name: "bench-user"},
			RegisterMethod: "test",
			IPv4:           &ipv4,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	}

	return nodes
}

// BenchmarkSnapshotFromNodes measures the index rebuild the write path runs
// once per batch, with the policy stubbed out.
func BenchmarkSnapshotFromNodes(b *testing.B) {
	nodes := benchNodes(benchNodeCount)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		snap := snapshotFromNodes(nodes, peerPositionsOf(noPeersFunc), PrimaryRouteLedger{}, nil)
		_ = snap.allNodes
	}
}

// BenchmarkNodeStoreUpdateNode measures one end-to-end write, batch size 1,
// against a tailnet of [benchNodeCount] nodes.
func BenchmarkNodeStoreUpdateNode(b *testing.B) {
	nodes := benchNodes(benchNodeCount)

	all := make(types.Nodes, 0, len(nodes))
	for id := range nodes {
		n := nodes[id]
		all = append(all, &n)
	}

	store := NewNodeStore(all, noPeersFunc, 1, TestBatchTimeout)
	store.Start()

	defer store.Stop()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		store.UpdateNode(types.NodeID(i%benchNodeCount+1), func(n *types.Node) {
			n.Hostname = "bench-updated"
		})
	}
}

// BenchmarkApplyBatchPuts measures one full write batch of puts against a
// tailnet of [benchNodeCount] nodes. Puts are the ops that resolve a given
// name, so this is the path that used to rebuild the name index per
// operation rather than once per batch.
func BenchmarkApplyBatchPuts(b *testing.B) {
	const putsPerBatch = 5

	nodes := benchNodes(benchNodeCount)

	all := make(types.Nodes, 0, len(nodes))
	for id := range nodes {
		n := nodes[id]
		all = append(all, &n)
	}

	store := NewNodeStore(all, noPeersFunc, putsPerBatch, TestBatchTimeout)

	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()

		batch := make([]work, putsPerBatch)

		for i := range batch {
			n := nodes[types.NodeID(i+1)]
			n.Hostname = "batched"
			batch[i] = work{
				op:         put,
				nodeID:     n.ID,
				node:       n,
				result:     make(chan struct{}),
				nodeResult: make(chan types.NodeView, 1),
			}
		}

		b.StartTimer()
		store.applyBatch(batch)
	}
}
