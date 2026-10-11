package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

// TestFunnelConfigPorts pins the ports cap to the hosted control plane's
// shape, with the default ports when the config names none and the
// config's own, sorted and deduplicated, otherwise.
func TestFunnelConfigPorts(t *testing.T) {
	t.Parallel()

	assert.Equal(t, nodecap.Cap("https://tailscale.com/cap/funnel-ports?ports=443,8443,10000"),
		FunnelConfig{}.FunnelPortsCap())
	assert.Equal(t, nodecap.Cap("https://tailscale.com/cap/funnel-ports?ports=443,9443"),
		FunnelConfig{Ports: []uint16{9443, 443, 9443}}.FunnelPortsCap())

	require.NoError(t, FunnelConfig{Ports: []uint16{443, 8443}}.Validate())
	require.ErrorIs(t, FunnelConfig{Ports: []uint16{0}}.Validate(), ErrFunnelPortInvalid)
}

// TestSelfCapMapFunnel proves the self caps carry https once
// certificate assistance is on, and a node granted funnel learns its
// ports and is warned while HTTPS is off.
func TestSelfCapMapFunnel(t *testing.T) {
	t.Parallel()

	off := &Config{}
	caps := selfCapMap(off, 0, tailcfg.NodeCapMap{nodecap.Funnel: nil})
	assert.NotContains(t, caps, nodecap.HTTPS)
	assert.Contains(t, caps, nodecap.Funnel)
	assert.Contains(t, caps, off.Funnel.FunnelPortsCap())
	assert.Contains(t, caps, nodecap.WarnFunnelNoHTTPS)

	on := &Config{BaseDomain: "example.com", HTTPSCerts: HTTPSCertsConfig{Enabled: true}}
	caps = selfCapMap(on, 0, nil)
	assert.Contains(t, caps, nodecap.HTTPS)
	assert.NotContains(t, caps, nodecap.Funnel)
	assert.NotContains(t, caps, nodecap.WarnFunnelNoHTTPS)

	caps = selfCapMap(on, 0, tailcfg.NodeCapMap{nodecap.Funnel: nil})
	assert.Contains(t, caps, on.Funnel.FunnelPortsCap())
	assert.NotContains(t, caps, nodecap.WarnFunnelNoHTTPS)

	// A node that reports a Funnel endpoint is a Funnel host.
	node := &Node{Hostinfo: &tailcfg.Hostinfo{IngressEnabled: true}}
	assert.True(t, node.View().FunnelEnabled())
	assert.False(t, (&Node{}).View().FunnelEnabled())
}

// TestSelfCapMapCacheNetworkMaps proves a client at capability version 149
// is told to cache its netmap unless the policy opts it out, and an older
// one only when the policy opts it in.
func TestSelfCapMapCacheNetworkMaps(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	assert.Contains(t, selfCapMap(cfg, cacheNetworkMapsByDefault, nil), nodecap.CacheNetworkMaps)
	assert.NotContains(t, selfCapMap(cfg, cacheNetworkMapsByDefault-1, nil), nodecap.CacheNetworkMaps)
	assert.Contains(t,
		selfCapMap(cfg, cacheNetworkMapsByDefault-1, tailcfg.NodeCapMap{nodecap.CacheNetworkMaps: nil}),
		nodecap.CacheNetworkMaps)

	optedOut := tailcfg.NodeCapMap{nodecap.DisableCacheNetworkMaps: nil}
	assert.NotContains(t, selfCapMap(cfg, cacheNetworkMapsByDefault, optedOut), nodecap.CacheNetworkMaps)

	optedOut[nodecap.CacheNetworkMaps] = nil
	assert.NotContains(t, selfCapMap(cfg, cacheNetworkMapsByDefault-1, optedOut), nodecap.CacheNetworkMaps)
}
