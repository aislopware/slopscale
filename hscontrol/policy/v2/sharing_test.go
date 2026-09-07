package v2

import (
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// sharingFixture is three users with one personal device each; alice's
// device is shared with bob.
func sharingFixture() (types.Users, types.Nodes) {
	users := types.Users{
		{ID: 1, Name: "alice"},
		{ID: 2, Name: "bob"},
		{ID: 3, Name: "carol"},
	}

	nodes := types.Nodes{
		{
			ID: 1, Hostname: "alice-1", User: new(users[0]), UserID: new(users[0].ID),
			IPv4: ap("100.64.0.1"), SharedWith: []types.UserID{2},
		},
		{ID: 2, Hostname: "bob-1", User: new(users[1]), UserID: new(users[1].ID), IPv4: ap("100.64.0.2")},
		{ID: 3, Hostname: "carol-1", User: new(users[2]), UserID: new(users[2].ID), IPv4: ap("100.64.0.3")},
	}

	return users, nodes
}

func TestAutogroupSharedFilter(t *testing.T) {
	t.Parallel()

	users, nodes := sharingFixture()

	pol, err := unmarshalPolicy([]byte(`{
		"acls": [{"action": "accept", "src": ["autogroup:shared"], "dst": ["autogroup:member:22"]}]
	}`))
	require.NoError(t, err)
	require.NoError(t, pol.validate())

	// The shared node gets a rule admitting the sharee's device, and only
	// on the granted port.
	rules := pol.compileFilterRulesForNode(users, nodes[0].View(), nodes.ViewSlice())
	require.Len(t, rules, 1)
	assert.Equal(t, []string{"100.64.0.2"}, rules[0].SrcIPs)
	assert.Equal(t, []tailcfg.NetPortRange{{IP: "100.64.0.1", Ports: tailcfg.PortRange{First: 22, Last: 22}}},
		rules[0].DstPorts)

	// Nodes that are not shared get nothing from the rule, even though
	// autogroup:member covers them as destinations.
	assert.Empty(t, pol.compileFilterRulesForNode(users, nodes[1].View(), nodes.ViewSlice()))
	assert.Empty(t, pol.compileFilterRulesForNode(users, nodes[2].View(), nodes.ViewSlice()))

	// The peer map is one way: bob's device and alice's device are peers,
	// carol's is nobody's.
	pm, err := NewPolicyManager([]byte(`{
		"acls": [{"action": "accept", "src": ["autogroup:shared"], "dst": ["autogroup:member:22"]}]
	}`), users, nodes.ViewSlice())
	require.NoError(t, err)

	peers := pm.BuildPeerMap(nodes.ViewSlice())
	assert.Len(t, peers[1], 1)
	assert.Equal(t, types.NodeID(2), peers[1][0].ID())
	assert.Len(t, peers[2], 1)
	assert.Equal(t, types.NodeID(1), peers[2][0].ID())
	assert.Empty(t, peers[3])

	// Only the shared node's filter has a rule, so the sharee's device
	// initiates and the shared node cannot.
	bobRules, err := pm.FilterForNode(nodes[1].View())
	require.NoError(t, err)
	assert.Empty(t, bobRules)
}

func TestAutogroupSharedWithOtherSources(t *testing.T) {
	t.Parallel()

	users, nodes := sharingFixture()

	// carol reaches every member by policy; bob reaches alice's device
	// only through the share.
	pol, err := unmarshalPolicy([]byte(`{
		"grants": [{"src": ["autogroup:shared", "carol@"], "dst": ["autogroup:member"], "ip": ["*"]}]
	}`))
	require.NoError(t, err)
	require.NoError(t, pol.validate())

	aliceRules := pol.compileFilterRulesForNode(users, nodes[0].View(), nodes.ViewSlice())

	var srcs []string
	for _, r := range aliceRules {
		srcs = append(srcs, r.SrcIPs...)
	}

	assert.ElementsMatch(t, []string{"100.64.0.2", "100.64.0.3"}, srcs)

	bobRules := pol.compileFilterRulesForNode(users, nodes[1].View(), nodes.ViewSlice())
	require.Len(t, bobRules, 1)
	assert.Equal(t, []string{"100.64.0.3"}, bobRules[0].SrcIPs)
}

func TestAutogroupSharedSSH(t *testing.T) {
	t.Parallel()

	users, nodes := sharingFixture()

	pol, err := unmarshalPolicy([]byte(`{
		"ssh": [{
			"action": "accept", "src": ["autogroup:shared"], "dst": ["autogroup:member"], "users": ["autogroup:nonroot"]
		}]
	}`))
	require.NoError(t, err)
	require.NoError(t, pol.validate())

	sshPol, err := pol.compileSSHPolicy(
		"https://headscale.test",
		users,
		nodes[0].View(),
		nodes.ViewSlice(),
		SSHRecording{},
	)
	require.NoError(t, err)
	require.NotNil(t, sshPol)
	require.Len(t, sshPol.Rules, 1)
	require.Len(t, sshPol.Rules[0].Principals, 1)
	assert.Equal(t, "100.64.0.2", sshPol.Rules[0].Principals[0].NodeIP)

	sshPol, err = pol.compileSSHPolicy(
		"https://headscale.test",
		users,
		nodes[1].View(),
		nodes.ViewSlice(),
		SSHRecording{},
	)
	require.NoError(t, err)
	assert.Empty(t, sshPol.Rules, "a node that is not shared gets no SSH rule from autogroup:shared")
}

func TestAutogroupSharedValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		policy string
		want   error
	}{
		{
			name:   "destination",
			policy: `{"acls": [{"action": "accept", "src": ["*"], "dst": ["autogroup:shared:*"]}]}`,
			want:   ErrAutogroupSharedDst,
		},
		{
			name:   "grant destination",
			policy: `{"grants": [{"src": ["*"], "dst": ["autogroup:shared"], "ip": ["*"]}]}`,
			want:   ErrAutogroupSharedDst,
		},
		{
			name:   "with autogroup:self",
			policy: `{"acls": [{"action": "accept", "src": ["autogroup:shared"], "dst": ["autogroup:self:*"]}]}`,
			want:   ErrAutogroupSharedSelf,
		},
		{
			name: "via grant",
			policy: `{
				"grants": [{"src": ["autogroup:shared"], "dst": ["10.0.0.0/24"], "ip": ["*"], "via": ["tag:router"]}],
				"tagOwners": {"tag:router": ["alice@"]}}`,
			want: ErrAutogroupSharedVia,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pol, err := unmarshalPolicy([]byte(tc.policy))
			if err == nil {
				err = pol.validate()
			}

			require.ErrorIs(t, err, tc.want)
		})
	}
}
