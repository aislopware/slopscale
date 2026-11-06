package mapper

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
)

// TestGenerateDNSConfigExtraRecordRoutes proves every extra record no
// route covers gets an empty route, so a client older than 1.104 sends
// its queries to its own resolver without override_local_dns.
func TestGenerateDNSConfigExtraRecordRoutes(t *testing.T) {
	t.Parallel()

	node := (&types.Node{Hostname: "laptop"}).View()
	connector := &dnstype.Resolver{Addr: "http://100.64.0.9:41641/dns-query"}
	corp := &dnstype.Resolver{Addr: "10.0.0.53"}

	record := func(name string) tailcfg.DNSRecord {
		return tailcfg.DNSRecord{Name: name, Type: "A", Value: "100.64.0.28"}
	}

	t.Run("routes a record outside every route to the client", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{TailcfgDNSConfig: &tailcfg.DNSConfig{
			ExtraRecords: []tailcfg.DNSRecord{record("Wiki.Example.com."), record("wiki.example.com")},
		}}

		got := generateDNSConfig(cfg, node, nil, nil, nil)
		require.NotNil(t, got)
		assert.Equal(t, map[string][]*dnstype.Resolver{"wiki.example.com": {}}, got.Routes)
		assert.NotNil(t, got.Routes["wiki.example.com"], "an empty list, which Clone keeps, not nil")
		assert.NotNil(t, got.Clone().Routes["wiki.example.com"], "and it survives a clone")

		// The routes are added to the copy, not to the tailnet's config.
		assert.Nil(t, cfg.TailcfgDNSConfig.Routes)
	})

	t.Run("leaves a record a route covers to that route", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{TailcfgDNSConfig: &tailcfg.DNSConfig{
			Routes: map[string][]*dnstype.Resolver{"corp.example.com": {corp}},
			ExtraRecords: []tailcfg.DNSRecord{
				record("corp.example.com"),
				record("git.corp.example.com"),
				record("app.example.net"),
			},
		}}

		got := generateDNSConfig(cfg, node, nil, map[string][]*dnstype.Resolver{"example.net": {connector}}, nil)
		require.NotNil(t, got)
		assert.Equal(t, map[string][]*dnstype.Resolver{
			"corp.example.com": {corp},
			"example.net":      {connector},
		}, got.Routes)
	})

	t.Run("leaves names under the base domain to MagicDNS", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{
			BaseDomain: "ts.example.com",
			TailcfgDNSConfig: &tailcfg.DNSConfig{
				Proxied:      true,
				ExtraRecords: []tailcfg.DNSRecord{record("grafana.ts.example.com")},
			},
		}

		got := generateDNSConfig(cfg, node, nil, nil, []tailcfg.DNSRecord{record("svc.ts.example.com")})
		require.NotNil(t, got)
		assert.Nil(t, got.Routes)

		// Without MagicDNS the client does not route the base domain,
		// so the record needs a route of its own.
		cfg = &types.Config{
			BaseDomain: "ts.example.com",
			TailcfgDNSConfig: &tailcfg.DNSConfig{
				ExtraRecords: []tailcfg.DNSRecord{record("grafana.ts.example.com")},
			},
		}

		got = generateDNSConfig(cfg, node, nil, nil, nil)
		require.NotNil(t, got)
		assert.Equal(t, map[string][]*dnstype.Resolver{"grafana.ts.example.com": {}}, got.Routes)
	})
}
