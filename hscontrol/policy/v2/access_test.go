package v2

import (
	"net/netip"
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// accessFixture is three users with a device each and one tagged server:
// alice and bob are in the "eng" group by user, the server sits in
// "servers" directly, carol is in no group.
func accessFixture() (types.Users, types.Nodes, types.AccessModel) {
	users := types.Users{
		{ID: 1, Name: "alice"},
		{ID: 2, Name: "bob"},
		{ID: 3, Name: "carol"},
	}

	nodes := types.Nodes{
		{ID: 1, Hostname: "alice-1", User: new(users[0]), UserID: new(users[0].ID), IPv4: ap("100.64.0.1")},
		{ID: 2, Hostname: "bob-1", User: new(users[1]), UserID: new(users[1].ID), IPv4: ap("100.64.0.2")},
		{ID: 3, Hostname: "carol-1", User: new(users[2]), UserID: new(users[2].ID), IPv4: ap("100.64.0.3")},
		{ID: 4, Hostname: "server", Tags: []string{"tag:server"}, IPv4: ap("100.64.0.4")},
	}

	model := types.AccessModel{
		Groups: []types.AccessGroup{
			{ID: 1, Name: "All", Builtin: types.GroupBuiltinAll},
			{ID: 2, Name: "eng", UserIDs: []types.UserID{1, 2}},
			{ID: 3, Name: "servers", NodeIDs: []types.NodeID{4}},
		},
		Rules: []types.AccessRule{
			{
				ID: 1, Name: "ssh", Enabled: true, Protocol: types.AccessProtocolTCP, Ports: "22",
				SourceGroupIDs: []types.GroupID{2}, DestinationGroupIDs: []types.GroupID{3},
			},
		},
	}

	return users, nodes, model
}

func TestAccessRulesCompileWithoutPolicyFile(t *testing.T) {
	t.Parallel()

	users, nodes, model := accessFixture()

	pm, err := NewPolicyManager(nil, users, nodes.ViewSlice())
	require.NoError(t, err)

	filter, _ := pm.Filter()
	assert.Equal(t, tailcfg.FilterAllowAll, filter, "no file and no rules is allow-all")

	changed, err := pm.SetAccessModel(model)
	require.NoError(t, err)
	assert.True(t, changed)

	filter, _ = pm.Filter()
	require.Len(t, filter, 1)
	assert.Equal(t, []string{"100.64.0.1-100.64.0.2"}, filter[0].SrcIPs)
	assert.Equal(t, []tailcfg.NetPortRange{{IP: "100.64.0.4", Ports: tailcfg.PortRange{First: 22, Last: 22}}},
		filter[0].DstPorts)
	assert.Equal(t, []int{ProtocolTCP}, filter[0].IPProto)

	// The peer map follows the rule: eng devices see the server and the
	// server sees them; carol sees nobody.
	peers := pm.BuildPeerMap(nodes.ViewSlice())
	assert.Len(t, peers[1], 1)
	assert.Len(t, peers[2], 1)
	assert.Empty(t, peers[3])
	assert.Len(t, peers[4], 2)

	// Disabling the only rule goes back to allow-all.
	model.Rules[0].Enabled = false

	changed, err = pm.SetAccessModel(model)
	require.NoError(t, err)
	assert.True(t, changed)

	filter, _ = pm.Filter()
	assert.Equal(t, tailcfg.FilterAllowAll, filter)
}

func TestAccessRulesBidirectionalAndAll(t *testing.T) {
	t.Parallel()

	users, nodes, model := accessFixture()
	model.Rules = []types.AccessRule{
		{
			ID: 1, Name: "servers reach everyone", Enabled: true, Protocol: types.AccessProtocolAll,
			Bidirectional:  true,
			SourceGroupIDs: []types.GroupID{3}, DestinationGroupIDs: []types.GroupID{1},
		},
	}

	pm, err := NewPolicyManager(nil, users, nodes.ViewSlice())
	require.NoError(t, err)

	_, err = pm.SetAccessModel(model)
	require.NoError(t, err)

	filter, _ := pm.Filter()
	require.Len(t, filter, 2)

	// Forward: the server may reach every node on any port.
	assert.Equal(t, []string{"100.64.0.4"}, filter[0].SrcIPs)
	assert.Len(t, filter[0].DstPorts, 3, "four addresses, rendered as compact ranges")
	assert.Nil(t, filter[0].IPProto)

	// Reverse: every node may reach the server.
	assert.Equal(t, []string{"100.64.0.1-100.64.0.4"}, filter[1].SrcIPs)
	assert.Equal(t, []tailcfg.NetPortRange{{IP: "100.64.0.4", Ports: tailcfg.PortRangeAny}}, filter[1].DstPorts)
}

func TestAccessRulesMergeWithPolicyFile(t *testing.T) {
	t.Parallel()

	users, nodes, model := accessFixture()

	pm, err := NewPolicyManager([]byte(`{
		"acls": [{"action": "accept", "src": ["carol@"], "dst": ["carol@:*"]}]
	}`), users, nodes.ViewSlice())
	require.NoError(t, err)

	filter, _ := pm.Filter()
	require.Len(t, filter, 1)

	_, err = pm.SetAccessModel(model)
	require.NoError(t, err)

	filter, _ = pm.Filter()
	require.Len(t, filter, 2, "the file's rule and the model's rule compile together")

	// Reloading the file keeps the model's grants.
	_, err = pm.SetPolicy([]byte(`{
		"acls": [{"action": "accept", "src": ["carol@"], "dst": ["carol@:443"]}]
	}`))
	require.NoError(t, err)

	filter, _ = pm.Filter()
	require.Len(t, filter, 2)
}

func TestAccessProtocolPorts(t *testing.T) {
	t.Parallel()

	icmp := accessProtocolPorts(types.AccessRule{Protocol: types.AccessProtocolICMP})
	require.Len(t, icmp, 2)
	assert.Equal(t, ProtocolNameICMP, icmp[0].Protocol)
	assert.Equal(t, ProtocolNameIPv6ICMP, icmp[1].Protocol)

	udp := accessProtocolPorts(types.AccessRule{Protocol: types.AccessProtocolUDP, Ports: "53,5000-5100"})
	require.Len(t, udp, 1)
	assert.Equal(t, []tailcfg.PortRange{{First: 53, Last: 53}, {First: 5000, Last: 5100}}, udp[0].Ports)

	// A rule naming a missing group on one side compiles to nothing.
	grants := accessGrants(types.AccessModel{
		Groups: []types.AccessGroup{{ID: 1}},
		Rules: []types.AccessRule{{
			Enabled: true, Protocol: types.AccessProtocolAll,
			SourceGroupIDs: []types.GroupID{1}, DestinationGroupIDs: []types.GroupID{9},
		}},
	})
	assert.Empty(t, grants)
}

func TestNetworkGrants(t *testing.T) {
	t.Parallel()

	users, nodes, model := accessFixture()
	model.Networks = []types.Network{
		{
			ID: 1, Name: "office", Enabled: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.0/24")},
			GroupIDs: []types.GroupID{2},
		},
	}

	pm, err := NewPolicyManager(nil, users, nodes.ViewSlice())
	require.NoError(t, err)

	// A network with an enforcing rule: eng reaches the server on 22 and
	// the network's prefix on every port.
	_, err = pm.SetAccessModel(model)
	require.NoError(t, err)

	filter, _ := pm.Filter()
	require.Len(t, filter, 2)
	assert.Equal(t, []string{"100.64.0.1-100.64.0.2"}, filter[1].SrcIPs)
	assert.Equal(t, []tailcfg.NetPortRange{{IP: "10.10.0.0/24", Ports: tailcfg.PortRangeAny}}, filter[1].DstPorts)
	assert.Nil(t, filter[1].IPProto)

	// An exit network grants the Internet, not every address: the tailnet's
	// own range must stay out of the destinations.
	model.Networks[0].Prefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/0"),
		netip.MustParsePrefix("::/0"),
		netip.MustParsePrefix("10.10.0.0/24"),
	}

	_, err = pm.SetAccessModel(model)
	require.NoError(t, err)

	filter, _ = pm.Filter()
	require.Len(t, filter, 2)

	for _, dst := range filter[1].DstPorts {
		assert.NotEqual(t, "0.0.0.0/0", dst.IP, "an exit network must not grant the whole address space")
		assert.NotEqual(t, "::/0", dst.IP)
		assert.NotEqual(t, "*", dst.IP)
	}

	assert.Contains(t, filter[1].DstPorts, tailcfg.NetPortRange{IP: "10.10.0.0/24", Ports: tailcfg.PortRangeAny})
	assert.Contains(t, filter[1].DstPorts, tailcfg.NetPortRange{IP: "8.0.0.0/7", Ports: tailcfg.PortRangeAny},
		"autogroup:internet is expanded into the public ranges")

	model.Networks[0].Prefixes = []netip.Prefix{netip.MustParsePrefix("10.10.0.0/24")}

	// A disabled network contributes nothing.
	model.Networks[0].Enabled = false

	_, err = pm.SetAccessModel(model)
	require.NoError(t, err)

	filter, _ = pm.Filter()
	assert.Len(t, filter, 1)

	// A network alone never makes the tailnet enforce.
	model.Networks[0].Enabled = true
	model.Rules = nil

	_, err = pm.SetAccessModel(model)
	require.NoError(t, err)

	filter, _ = pm.Filter()
	assert.Equal(t, tailcfg.FilterAllowAll, filter)
	assert.False(t, hasAccessGrants(model))
}
