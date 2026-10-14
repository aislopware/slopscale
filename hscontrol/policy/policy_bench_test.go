package policy

import (
	"net/netip"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/policy/matcher"
	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
)

// BenchmarkReduceRoutesAppConnector reduces an app connector's own routes
// for its self node, the shape that took minutes per map once a wildcard
// domain had taught the connector about four thousand /32s.
func BenchmarkReduceRoutesAppConnector(b *testing.B) {
	routes := make([]netip.Prefix, 0, 4000)
	for i := range 4000 {
		routes = append(routes, netip.PrefixFrom(netip.AddrFrom4([4]byte{52, 216, byte(i >> 8), byte(i)}), 32))
	}

	node := &types.Node{
		ID:             1,
		IPv4:           ap("100.64.0.1"),
		Hostinfo:       &tailcfg.Hostinfo{RoutableIPs: routes},
		ApprovedRoutes: routes,
	}

	matchers := matcher.MatchesFromFilterRules([]tailcfg.FilterRule{
		{
			SrcIPs:   []string{"100.64.0.2/32", "100.64.0.3/32"},
			DstPorts: []tailcfg.NetPortRange{{IP: "10.0.0.0/8", Ports: tailcfg.PortRangeAny}},
		},
		{
			SrcIPs:   []string{"100.64.0.0/10"},
			DstPorts: []tailcfg.NetPortRange{{IP: "52.216.0.0/16", Ports: tailcfg.PortRangeAny}},
		},
	})

	view := node.View()

	if got := ReduceRoutes(view, routes, matchers); len(got) != len(routes) {
		b.Fatalf("benchmark setup error: reduced to %d of %d routes", len(got), len(routes))
	}

	b.ReportAllocs()

	for b.Loop() {
		ReduceRoutes(view, routes, matchers)
	}
}
