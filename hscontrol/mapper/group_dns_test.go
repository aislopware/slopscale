package mapper

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
)

// TestGenerateDNSConfigGroupRoutes proves the node's group split DNS
// lands in the map response next to the tailnet's: a new domain gets
// its own entry and a shared one keeps the global resolvers first.
func TestGenerateDNSConfigGroupRoutes(t *testing.T) {
	t.Parallel()

	corp := &dnstype.Resolver{Addr: "10.0.0.53"}
	lab := &dnstype.Resolver{Addr: "10.1.0.53"}
	global := &dnstype.Resolver{Addr: "10.9.0.53"}

	t.Run("adds routes to a tailnet without split DNS", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{TailcfgDNSConfig: &tailcfg.DNSConfig{Domains: []string{"example.ts.net"}}}
		node := (&types.Node{Hostname: "laptop"}).View()

		got := generateDNSConfig(cfg, node, nil, map[string][]*dnstype.Resolver{"corp.example.com": {corp}})
		require.NotNil(t, got)
		assert.Equal(t, map[string][]*dnstype.Resolver{"corp.example.com": {corp}}, got.Routes)

		// Nothing applies: the tailnet's own routes stay as they were.
		got = generateDNSConfig(cfg, node, nil, nil)
		require.NotNil(t, got)
		assert.Nil(t, got.Routes)
	})

	t.Run("appends to the tailnet's resolvers for the same domain", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{TailcfgDNSConfig: &tailcfg.DNSConfig{
			Routes: map[string][]*dnstype.Resolver{"corp.example.com": {global}},
		}}
		node := (&types.Node{Hostname: "laptop"}).View()

		got := generateDNSConfig(cfg, node, nil, map[string][]*dnstype.Resolver{
			"corp.example.com": {corp},
			"lab.example.com":  {lab},
		})
		require.NotNil(t, got)
		assert.Equal(t, []*dnstype.Resolver{global, corp}, got.Routes["corp.example.com"])
		assert.Equal(t, []*dnstype.Resolver{lab}, got.Routes["lab.example.com"])

		// The clone protects the config: a second node without the
		// group sees the tailnet's routes alone.
		got = generateDNSConfig(cfg, node, nil, nil)
		require.NotNil(t, got)
		assert.Equal(t, map[string][]*dnstype.Resolver{"corp.example.com": {global}}, got.Routes)
	})

	t.Run("leaves the zones the client answers itself alone", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{TailcfgDNSConfig: &tailcfg.DNSConfig{
			Proxied: true,
			Routes:  map[string][]*dnstype.Resolver{"64.100.in-addr.arpa": {}},
		}}
		node := (&types.Node{Hostname: "laptop"}).View()

		got := generateDNSConfig(cfg, node, nil, map[string][]*dnstype.Resolver{
			"64.100.in-addr.arpa": {corp},
			"lab.example.com":     {lab},
		})
		require.NotNil(t, got)
		assert.Empty(t, got.Routes["64.100.in-addr.arpa"], "the reverse zone still resolves locally")
		assert.NotNil(t, got.Routes["64.100.in-addr.arpa"], "and stays an empty list, not nil")
		assert.Equal(t, []*dnstype.Resolver{lab}, got.Routes["lab.example.com"])
	})
}
