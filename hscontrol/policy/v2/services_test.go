package v2

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

func tcpPort(p uint16) tailcfg.ProtoPortRange {
	return tailcfg.ProtoPortRange{Proto: 6, Ports: tailcfg.PortRange{First: p, Last: p}}
}

// serviceFixture is a service with addresses, a tagged node that reports
// and is approved for it, a tagged node that only reports it and a
// user-owned bystander.
func serviceFixture() ([]types.VIPService, types.Users, types.Nodes) {
	users := types.Users{{ID: 1, Name: "alice"}}
	web := types.VIPService{
		ID:          1,
		Name:        "svc:web",
		DisplayName: "Web",
		IPv4:        ap("100.64.0.100"),
		IPv6:        ap("fd7a:115c:a1e0::100"),
	}
	announced := &types.NodeServices{
		Hash:     "h",
		Services: []tailcfg.VIPService{{Name: "svc:web", Ports: []tailcfg.ProtoPortRange{tcpPort(443)}, Active: true}},
	}

	approved := time.Now()
	host := &types.Node{
		ID: 1, Hostname: "web1", IPv4: ap("100.64.0.1"), IPv6: ap("fd7a:115c:a1e0::1"), ApprovedAt: &approved,
		Tags: []string{"tag:web"}, Services: announced, ApprovedServices: []string{"svc:web"},
	}
	pending := &types.Node{
		ID: 2, Hostname: "web2", IPv4: ap("100.64.0.2"), IPv6: ap("fd7a:115c:a1e0::2"), ApprovedAt: &approved,
		Tags: []string{"tag:web"}, Services: announced,
	}
	laptop := node("laptop", "100.64.0.3", "fd7a:115c:a1e0::3", users[0])
	laptop.ID = 3
	laptop.ApprovedAt = &approved

	return []types.VIPService{web}, users, types.Nodes{host, pending, laptop}
}

// TestServiceAliasResolvesToServiceAddresses pins that a svc: destination
// opens the service's own addresses once the service exists, and nothing
// before, and that a service is never a source.
func TestServiceAliasResolvesToServiceAddresses(t *testing.T) {
	t.Parallel()

	services, users, nodes := serviceFixture()

	pol := `{"grants": [{"src": ["*"], "dst": ["svc:web"], "ip": ["tcp:443"]}]}`

	pm, err := NewPolicyManager([]byte(pol), users, nodes.ViewSlice())
	require.NoError(t, err)

	rules, _ := pm.Filter()
	assert.Empty(t, rules, "an unknown service opens nothing")

	changed, err := pm.SetVIPServices(services)
	require.NoError(t, err)
	assert.True(t, changed)

	rules, _ = pm.Filter()
	require.Len(t, rules, 1)

	dsts := make([]string, 0, len(rules[0].DstPorts))
	for _, d := range rules[0].DstPorts {
		dsts = append(dsts, d.IP)
	}

	assert.ElementsMatch(t, []string{"100.64.0.100", "fd7a:115c:a1e0::100"}, dsts)

	_, err = NewPolicyManager(
		[]byte(`{"grants": [{"src": ["svc:web"], "dst": ["*"], "ip": ["*"]}]}`),
		users, nodes.ViewSlice(),
	)
	require.ErrorIs(t, err, ErrServiceAsSource)

	_, err = NewPolicyManager(
		[]byte(`{"acls": [{"action": "accept", "src": ["svc:web"], "dst": ["*:*"]}]}`),
		users, nodes.ViewSlice(),
	)
	require.ErrorIs(t, err, ErrServiceAsSource)

	_, err = NewPolicyManager(
		[]byte(`{"grants": [{"src": ["*"], "dst": ["svc:Bad_Name"], "ip": ["*"]}]}`),
		users, nodes.ViewSlice(),
	)
	require.ErrorIs(t, err, ErrInvalidServiceFormat)
}

// TestServiceAutoApprovers pins autoApprovers.services: the named tags,
// users and groups may host the service without an operator.
func TestServiceAutoApprovers(t *testing.T) {
	t.Parallel()

	services, users, nodes := serviceFixture()

	pol := `{
		"groups": {"group:ops": ["alice@"]},
		"tagOwners": {"tag:web": ["alice@"]},
		"autoApprovers": {"services": {"svc:web": ["tag:web"], "svc:db": ["group:ops"]}}
	}`

	pm, err := NewPolicyManager([]byte(pol), users, nodes.ViewSlice())
	require.NoError(t, err)

	_, err = pm.SetVIPServices(services)
	require.NoError(t, err)

	assert.True(t, pm.NodeCanApproveService(nodes[0].View(), "svc:web"), "tag:web host")
	assert.True(t, pm.NodeCanApproveService(nodes[1].View(), "svc:web"), "tag:web pending host")
	assert.False(t, pm.NodeCanApproveService(nodes[2].View(), "svc:web"), "laptop has no tag")
	assert.True(t, pm.NodeCanApproveService(nodes[2].View(), "svc:db"), "group member")
	assert.False(t, pm.NodeCanApproveService(nodes[0].View(), "svc:db"), "tagged node is in no group")
	assert.False(t, pm.NodeCanApproveService(nodes[0].View(), "svc:none"), "unknown service")

	_, err = NewPolicyManager(
		[]byte(`{"autoApprovers": {"services": {"svc:web": ["tag:nope"]}}}`),
		users, nodes.ViewSlice(),
	)
	require.Error(t, err, "an approver tag must be owned")

	// The section round-trips through the policy's JSON form.
	var parsed Policy

	require.NoError(t, json.Unmarshal([]byte(pol), &parsed))

	out, err := json.Marshal(&parsed)
	require.NoError(t, err)

	var again Policy

	require.NoError(t, json.Unmarshal(out, &again))
	assert.Len(t, again.AutoApprovers.Services, 2)
	assert.Equal(t, parsed.AutoApprovers.Services["svc:web"], again.AutoApprovers.Services["svc:web"])
}

// TestStampServiceCaps pins what the self cap map carries: the desktop
// clients cap on everyone, the host mapping on approved hosts only, and
// the services/<name> entry on the nodes that can reach a served service.
func TestStampServiceCaps(t *testing.T) {
	t.Parallel()

	services, _, nodes := serviceFixture()

	capMaps := map[types.NodeID]tailcfg.NodeCapMap{}
	stampServiceCaps(nil, nodes.ViewSlice(), nil, capMaps)
	assert.Empty(t, capMaps, "no services, no caps")

	reachable := func(node types.NodeView, _ types.VIPService) bool { return node.ID() != 3 }
	stampServiceCaps(services, nodes.ViewSlice(), reachable, capMaps)

	for id := types.NodeID(1); id <= 3; id++ {
		assert.Contains(t, capMaps[id], nodecap.ServicesInDesktopClients, "node %d", id)
	}

	mappings, err := tailcfg.UnmarshalNodeCapJSON[tailcfg.ServiceIPMappings](capMaps[1], nodecap.ServiceHost)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, []netip.Addr{*services[0].IPv4, *services[0].IPv6}, mappings[0]["svc:web"])

	assert.NotContains(t, capMaps[2], nodecap.ServiceHost, "announced but not approved")
	assert.NotContains(t, capMaps[3], nodecap.ServiceHost, "not a host")

	webCap := nodecap.Cap(string(nodecap.ServicesPrefix) + "web")

	details, err := tailcfg.UnmarshalNodeCapJSON[tailcfg.ServiceDetails](capMaps[1], webCap)
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, tailcfg.ServiceName("svc:web"), details[0].Name)
	assert.Equal(t, "Web", details[0].DisplayName)
	assert.Equal(
		t,
		[]tailcfg.ProtoPortRange{tcpPort(443)},
		details[0].Ports,
		"ports come from the hosts when the service names none",
	)
	assert.Contains(t, capMaps[2], webCap)
	assert.NotContains(t, capMaps[3], webCap, "the policy does not let the laptop reach it")

	// A service's own ports win over what the hosts serve.
	services[0].Ports = []tailcfg.ProtoPortRange{tcpPort(8443)}
	capMaps = map[types.NodeID]tailcfg.NodeCapMap{}
	stampServiceCaps(services, nodes.ViewSlice(), reachable, capMaps)

	details, err = tailcfg.UnmarshalNodeCapJSON[tailcfg.ServiceDetails](capMaps[1], webCap)
	require.NoError(t, err)
	assert.Equal(t, []tailcfg.ProtoPortRange{tcpPort(8443)}, details[0].Ports)

	// Nobody approved and advertising: no services/<name> entry anywhere.
	nodes[0].ApprovedServices = nil
	capMaps = map[types.NodeID]tailcfg.NodeCapMap{}
	stampServiceCaps(services, nodes.ViewSlice(), reachable, capMaps)
	assert.NotContains(t, capMaps[1], webCap)
	assert.NotContains(t, capMaps[1], nodecap.ServiceHost)
}

// TestPolicyManagerStampsServiceCapsWithoutPolicy pins that the caps reach
// NodeCapMap on a tailnet without a policy file, and that a host counts as
// a node whose peers must be recomputed.
func TestPolicyManagerStampsServiceCapsWithoutPolicy(t *testing.T) {
	t.Parallel()

	services, users, nodes := serviceFixture()

	pm, err := NewPolicyManager(nil, users, nodes.ViewSlice())
	require.NoError(t, err)
	assert.Nil(t, pm.NodeCapMap(1))

	_, err = pm.SetVIPServices(services)
	require.NoError(t, err)

	assert.Contains(t, pm.NodeCapMap(1), nodecap.ServiceHost)
	assert.Contains(t, pm.NodeCapMap(3), nodecap.ServicesInDesktopClients)
	assert.Contains(
		t,
		pm.NodeCapMap(3),
		nodecap.Cap(string(nodecap.ServicesPrefix)+"web"),
		"an open tailnet reaches everything",
	)

	assert.True(t, pm.NodeNeedsPeerRecompute(nodes[0].View()), "host")
	assert.False(t, pm.NodeNeedsPeerRecompute(nodes[2].View()), "laptop")

	_, err = pm.SetVIPServices(nil)
	require.NoError(t, err)
	assert.Nil(t, pm.NodeCapMap(1), "deleting the service takes the caps back")
}

// TestServicePrefixes pins the address list a host's AllowedIPs carry.
func TestServicePrefixes(t *testing.T) {
	t.Parallel()

	services, _, _ := serviceFixture()

	assert.Equal(t,
		[]netip.Prefix{netip.MustParsePrefix("100.64.0.100/32"), netip.MustParsePrefix("fd7a:115c:a1e0::100/128")},
		ServicePrefixes(services, []tailcfg.ServiceName{"svc:web", "svc:none"}),
	)
	assert.Nil(t, ServicePrefixes(services, nil))
}
