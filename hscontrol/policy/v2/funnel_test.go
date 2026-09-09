package v2

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tailcfg/peercap"
)

// TestFunnelGrants proves the policy opens every port from the ingress
// tag to the nodes granted Funnel, with the ingress capability, and adds
// nothing when no nodeAttrs grants Funnel.
func TestFunnelGrants(t *testing.T) {
	t.Parallel()

	var nilPol *Policy
	assert.Nil(t, nilPol.funnelGrants(), "no policy")

	pol := &Policy{NodeAttrs: []NodeAttrGrant{{Targets: Aliases{Wildcard}, Attrs: []nodecap.Cap{"https"}}}}
	assert.Nil(t, pol.funnelGrants(), "nothing grants funnel")

	pol.NodeAttrs = append(pol.NodeAttrs,
		NodeAttrGrant{Targets: Aliases{tp("tag:web")}, Attrs: []nodecap.Cap{"funnel"}},
		NodeAttrGrant{Targets: Aliases{up("alice@")}, Attrs: []nodecap.Cap{"https", "funnel"}},
	)

	grants := pol.funnelGrants()
	require.Len(t, grants, 1)

	assert.Equal(t, Aliases{tp(types.FunnelIngressTag)}, grants[0].Sources)
	assert.Equal(t, Aliases{tp("tag:web"), up("alice@")}, grants[0].Destinations)
	require.Len(t, grants[0].InternetProtocols, 1)
	assert.Equal(t, ProtocolNameWildcard, grants[0].InternetProtocols[0].Protocol)
	assert.Equal(t, []tailcfg.PortRange{tailcfg.PortRangeAny}, grants[0].InternetProtocols[0].Ports)
	assert.Contains(t, grants[0].App, peercap.Ingress)
}

// funnelFixture is an ingress node, a node granted Funnel and a
// bystander.
func funnelFixture() (types.Users, types.Nodes) {
	users := types.Users{{Name: "alice", ID: 1}}
	ingress := &types.Node{
		ID:       1,
		Hostname: "ingress",
		IPv4:     createAddr("100.64.0.1"),
		Tags:     []string{types.FunnelIngressTag},
	}
	web := node("web", "100.64.0.2", "fd7a:115c:a1e0::2", users[0])
	web.ID = 2
	web.Tags = []string{"tag:web"}
	web.UserID = nil
	web.User = nil
	other := node("other", "100.64.0.3", "fd7a:115c:a1e0::3", users[0])
	other.ID = 3

	return users, types.Nodes{ingress, web, other}
}

// capGrantsFor returns the CapGrant rules the ingress node holds in the
// filter.
func capGrantsFor(filter []tailcfg.FilterRule, src string) []tailcfg.CapGrant {
	var grants []tailcfg.CapGrant

	for _, rule := range filter {
		for _, s := range rule.SrcIPs {
			if s == src {
				grants = append(grants, rule.CapGrant...)
			}
		}
	}

	return grants
}

// TestFunnelFilterOpenTailnet proves a policy with nodeAttrs only, which
// does not enforce, stays allow-all and still carries the ingress
// capability from the ingress node to the funnel node.
func TestFunnelFilterOpenTailnet(t *testing.T) {
	t.Parallel()

	users, nodes := funnelFixture()

	pm, err := NewPolicyManager([]byte(`{
		"tagOwners": {"tag:web": ["alice@"]},
		"nodeAttrs": [{"target": ["tag:web"], "attr": ["funnel"]}]
	}`), users, nodes.ViewSlice())
	require.NoError(t, err)

	filter, _ := pm.Filter()
	require.GreaterOrEqual(t, len(filter), len(tailcfg.FilterAllowAll)+1)
	assert.Equal(t, tailcfg.FilterAllowAll, filter[:len(tailcfg.FilterAllowAll)], "the tailnet stays open")

	grants := capGrantsFor(filter, "100.64.0.1")
	require.Len(t, grants, 1)
	assert.Contains(t, grants[0].CapMap, peercap.Ingress)
	assert.Equal(t, "100.64.0.2/32", grants[0].Dsts[0].String())

	caps := pm.NodeCapMap(nodes[1].ID)
	assert.Contains(t, caps, nodecap.Funnel)
	assert.NotContains(t, pm.NodeCapMap(nodes[2].ID), nodecap.Funnel)
}

// TestFunnelFilterEnforcing proves an enforcing policy lets the ingress
// node reach the funnel node on every port with the capability, and
// nothing else it is not otherwise granted.
func TestFunnelFilterEnforcing(t *testing.T) {
	t.Parallel()

	users, nodes := funnelFixture()

	pm, err := NewPolicyManager([]byte(`{
		"tagOwners": {"tag:web": ["alice@"]},
		"acls": [{"action": "accept", "src": ["alice@"], "dst": ["tag:web:443"]}],
		"nodeAttrs": [{"target": ["tag:web"], "attr": ["funnel"]}]
	}`), users, nodes.ViewSlice())
	require.NoError(t, err)

	filter, _ := pm.Filter()

	var ingressPorts []tailcfg.NetPortRange

	for _, rule := range filter {
		for _, s := range rule.SrcIPs {
			if s == "100.64.0.1" {
				ingressPorts = append(ingressPorts, rule.DstPorts...)
			}
		}
	}

	require.NotEmpty(t, ingressPorts, "the ingress node reaches the funnel node")

	for _, p := range ingressPorts {
		assert.Equal(t, tailcfg.PortRangeAny, p.Ports)
		assert.NotContains(t, p.IP, "100.64.0.3", "the bystander is not opened")
	}

	grants := capGrantsFor(filter, "100.64.0.1")
	require.Len(t, grants, 1)
	assert.Contains(t, grants[0].CapMap, peercap.Ingress)

	// The ingress node and the funnel node see each other; the bystander
	// sees the funnel node only through alice's rule.
	peers := pm.BuildPeerMap(nodes.ViewSlice())
	assert.Len(t, peers[nodes[0].ID], 1)
	assert.Contains(t, peers[nodes[1].ID], nodes[0].View())
}
