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

	require.NoError(t, FunnelConfig{ListenAddrs: []string{":443", "203.0.113.1:8443"}}.Validate())
	require.ErrorIs(t, FunnelConfig{ListenAddrs: []string{"443"}}.Validate(), ErrFunnelListenAddrInvalid)
	require.ErrorIs(t, FunnelConfig{Ports: []uint16{0}}.Validate(), ErrFunnelPortInvalid)
}

// TestSelfCapMapFunnel proves the self caps carry https once
// certificate assistance is on, and a node granted funnel learns its
// ports and is warned while HTTPS is off.
func TestSelfCapMapFunnel(t *testing.T) {
	t.Parallel()

	off := &Config{}
	caps := selfCapMap(off, tailcfg.NodeCapMap{nodecap.Funnel: nil})
	assert.NotContains(t, caps, nodecap.HTTPS)
	assert.Contains(t, caps, nodecap.Funnel)
	assert.Contains(t, caps, off.Funnel.FunnelPortsCap())
	assert.Contains(t, caps, nodecap.WarnFunnelNoHTTPS)

	on := &Config{BaseDomain: "example.com", HTTPSCerts: HTTPSCertsConfig{Enabled: true}}
	caps = selfCapMap(on, nil)
	assert.Contains(t, caps, nodecap.HTTPS)
	assert.NotContains(t, caps, nodecap.Funnel)
	assert.NotContains(t, caps, nodecap.WarnFunnelNoHTTPS)

	caps = selfCapMap(on, tailcfg.NodeCapMap{nodecap.Funnel: nil})
	assert.Contains(t, caps, on.Funnel.FunnelPortsCap())
	assert.NotContains(t, caps, nodecap.WarnFunnelNoHTTPS)

	// A node that reports a Funnel endpoint is a Funnel host.
	node := &Node{Hostinfo: &tailcfg.Hostinfo{IngressEnabled: true}}
	assert.True(t, node.View().FunnelEnabled())
	assert.False(t, (&Node{}).View().FunnelEnabled())
}
