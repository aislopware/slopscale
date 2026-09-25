package types

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
)

// TestCloneTailcfgDNSConfigFor checks that only a node using a resolver's
// gateway as its exit node gets that resolver, that every other node gets
// the tailnet's configuration unchanged, and that the gateway itself gets
// none.
func TestCloneTailcfgDNSConfigFor(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		DNSConfig: DNSConfig{
			OverrideLocalDNS: true,
			Nameservers: Nameservers{
				Global:          []string{"1.1.1.1", "9.9.9.9"},
				UseWithExitNode: []string{"9.9.9.9"},
				Split:           map[string][]string{"corp.example": {"10.0.0.53"}},
			},
		},
		TailcfgDNSConfig: &tailcfg.DNSConfig{},
	}
	cfg.SetTrafficResolvers(nil)
	own := cfg.CloneTailcfgDNSConfig()
	require.Len(t, own.Resolvers, 2)

	gw1 := TrafficResolver{
		Node:   1,
		Stable: "gw1",
		Addr:   netip.MustParseAddr("100.64.0.1"),
		DoH:    "http://100.64.0.1:40000/dns-query",
	}
	gw2 := TrafficResolver{Node: 2, Stable: "gw2", Addr: netip.MustParseAddr("100.64.0.2")}
	cfg.SetTrafficResolvers([]TrafficResolver{gw1, gw2})

	addrs := func(d *tailcfg.DNSConfig) []string {
		out := make([]string, 0, len(d.Resolvers))
		for _, r := range d.Resolvers {
			require.True(t, r.UseWithExitNode, "resolver %s is not kept with an exit node", r.Addr)
			out = append(out, r.Addr)
		}

		return out
	}

	got := cfg.CloneTailcfgDNSConfigFor(10, "gw1")
	assert.Equal(t, []string{"100.64.0.1", "http://100.64.0.1:40000/dns-query", "9.9.9.9"}, addrs(got))
	assert.Equal(t, own.Routes, got.Routes, "split DNS is untouched")

	got = cfg.CloneTailcfgDNSConfigFor(10, "gw2")
	assert.Equal(t, []string{"100.64.0.2", "9.9.9.9"}, addrs(got), "no peer API port, no fallback")

	for name, exit := range map[string]tailcfg.StableNodeID{"no exit node": "", "another exit node": "other"} {
		assert.Equal(t, own, cfg.CloneTailcfgDNSConfigFor(10, exit), name)
	}

	assert.Equal(t, own, cfg.CloneTailcfgDNSConfigFor(1, "gw1"), "a gateway never gets its own resolver")

	cfg.SetTrafficResolvers(nil)
	assert.Equal(t, own, cfg.CloneTailcfgDNSConfigFor(10, "gw1"), "switched off, every node gets the tailnet's DNS")
}

// TestWithTrafficResolverDedup checks that an operator resolver at the
// gateway resolver's address is not listed twice.
func TestWithTrafficResolverDedup(t *testing.T) {
	t.Parallel()

	own := &tailcfg.DNSConfig{Resolvers: []*dnstype.Resolver{{Addr: "100.64.0.1", UseWithExitNode: true}}}
	got := withTrafficResolver(own, TrafficResolver{Node: 1, Stable: "gw", Addr: netip.MustParseAddr("100.64.0.1")})

	assert.Equal(t, []*dnstype.Resolver{{Addr: "100.64.0.1", UseWithExitNode: true}}, got.Resolvers)
	assert.Len(t, own.Resolvers, 1, "the tailnet's configuration is not modified")
}
