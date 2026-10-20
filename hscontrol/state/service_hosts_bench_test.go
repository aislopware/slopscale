package state

import (
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
)

// benchServiceCount is how many services the service benchmarks host,
// each on two online machines in different regions.
const benchServiceCount = 20

// benchServices makes the first 2*benchServiceCount of nodes hosts of
// benchServiceCount services, two per service in two regions, and
// returns the services table the election reads.
func benchServices(nodes map[types.NodeID]types.Node) map[tailcfg.ServiceName][]netip.Prefix {
	now := time.Now()
	online := true
	table := make(map[tailcfg.ServiceName][]netip.Prefix, benchServiceCount)

	for i := range benchServiceCount {
		name := tailcfg.ServiceName("svc:bench-" + strconv.Itoa(i))
		addr := netip.AddrFrom4([4]byte{100, 100, 0, byte(i + 1)})
		table[name] = []netip.Prefix{netip.PrefixFrom(addr, 32)}

		for j, region := range []tailcfg.DERPRegionID{900, 4} {
			id := types.NodeID(2*i + j + 1)
			n := nodes[id]
			n.IsOnline = &online
			n.ApprovedAt = &now
			n.ApprovedServices = []string{string(name)}
			n.Services = &types.NodeServices{Services: []tailcfg.VIPService{{Name: name, Active: true}}}
			n.Hostinfo = &tailcfg.Hostinfo{NetInfo: &tailcfg.NetInfo{PreferredDERP: region}}
			nodes[id] = n
		}
	}

	return table
}

// BenchmarkSnapshotFromNodesServices is [BenchmarkSnapshotFromNodes] with
// the services of [benchServices] elected on every rebuild.
func BenchmarkSnapshotFromNodesServices(b *testing.B) {
	nodes := benchNodes(benchNodeCount)
	in := routeInputs{services: benchServices(nodes)}

	first := buildSnapshot(nodes, peerPositionsOf(noPeersFunc), PrimaryRouteLedger{}, nil, in)
	prev := first.ledger()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		snap := buildSnapshot(nodes, peerPositionsOf(noPeersFunc), prev, nil, in)
		_ = snap.allNodes
	}
}

// BenchmarkServiceRoutesPerMap measures the service work of one full map
// response under regional routing: the service addresses of the self
// node and of every peer as the viewer, an Amsterdam laptop, sees them.
func BenchmarkServiceRoutesPerMap(b *testing.B) {
	nodes := benchNodes(benchNodeCount)
	in := routeInputs{services: benchServices(nodes)}

	all := make(types.Nodes, 0, len(nodes))
	for id := range nodes {
		n := nodes[id]
		all = append(all, &n)
	}

	store := NewNodeStore(all, noPeersFunc, TestBatchSize, TestBatchTimeout)
	store.Start()
	b.Cleanup(store.Stop)
	store.SetServices(in.services)

	viewerID := types.NodeID(benchNodeCount)
	viewer := viewerSiteFrom(viewerID, &tailcfg.NetInfo{
		PreferredDERP: 14,
		DERPLatency:   map[string]float64{"14-v4": 0.006, "4-v4": 0.011, "900-v4": 0.2},
	})

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = store.ServiceRoutesForNode(viewerID)

		byHost := store.ServiceRoutesByHost(viewer)
		for id := range types.NodeID(benchNodeCount) {
			_ = byHost[id+1]
		}
	}
}
