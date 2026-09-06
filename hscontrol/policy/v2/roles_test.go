package v2

import (
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

func roleTestFixtures() (types.Users, types.Nodes) {
	users := types.Users{
		{ID: 1, Name: "owner", Role: types.RoleOwner},
		{ID: 2, Name: "admin", Role: types.RoleAdmin},
		{ID: 3, Name: "netadmin", Role: types.RoleNetworkAdmin},
		{ID: 4, Name: "member", Role: types.RoleMember},
	}

	nodes := types.Nodes{
		{ID: 1, User: &users[0], UserID: new(users[0].ID), IPv4: ap("100.64.0.1")},
		{ID: 2, User: &users[1], UserID: new(users[1].ID), IPv4: ap("100.64.0.2")},
		{ID: 3, User: &users[2], UserID: new(users[2].ID), IPv4: ap("100.64.0.3")},
		{ID: 4, User: &users[3], UserID: new(users[3].ID), IPv4: ap("100.64.0.4")},
		// A tagged node created by the owner carries no role.
		{ID: 5, User: &users[0], Tags: []string{"tag:server"}, IPv4: ap("100.64.0.5")},
	}

	return users, nodes
}

// TestRoleAutogroupsResolve pins that each role autogroup selects the
// personal devices of the users holding that role, read from the user list
// rather than the node's embedded user copy.
func TestRoleAutogroupsResolve(t *testing.T) {
	t.Parallel()

	users, nodes := roleTestFixtures()

	// The node's embedded copy is stale on purpose: the list is authoritative.
	stale := *nodes[3].User
	stale.Role = types.RoleOwner
	nodes[3].User = &stale

	tests := []struct {
		group AutoGroup
		want  []string
	}{
		{AutoGroupOwner, []string{"100.64.0.1/32"}},
		{AutoGroupAdmin, []string{"100.64.0.2/32"}},
		{AutoGroupNetworkAdmin, []string{"100.64.0.3/32"}},
		{AutoGroupITAdmin, nil},
		{AutoGroupAuditor, nil},
	}

	for _, tt := range tests {
		t.Run(string(tt.group), func(t *testing.T) {
			t.Parallel()

			group := tt.group

			ips, err := group.resolve(nil, users, nodes.ViewSlice())
			require.NoError(t, err)

			var got []string
			for _, p := range ips.Prefixes() {
				got = append(got, p.String())
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRoleAutogroupsInGrants(t *testing.T) {
	t.Parallel()

	users, nodes := roleTestFixtures()

	pol := `{
		"tagOwners": {"tag:server": ["owner@"]},
		"grants": [
			{"src": ["autogroup:admin"], "dst": ["tag:server"], "ip": ["22"]},
			{"src": ["autogroup:owner"], "dst": ["autogroup:self"], "ip": ["*"]}
		],
		"ssh": [
			{"action": "accept", "src": ["autogroup:owner"], "dst": ["autogroup:network-admin"], "users": ["root"]}
		],
		"nodeAttrs": [
			{"target": ["autogroup:it-admin"], "attr": ["randomize-client-port"]}
		]
	}`

	pm, err := NewPolicyManager([]byte(pol), users, nodes.ViewSlice())
	require.NoError(t, err)

	rules, err := pm.FilterForNode(nodes[4].View())
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, []string{"100.64.0.2"}, rules[0].SrcIPs, "only the admin reaches the server")

	sshPol, err := pm.SSHPolicy("https://headscale.test", nodes[2].View())
	require.NoError(t, err)
	require.Len(t, sshPol.Rules, 1)
	require.Len(t, sshPol.Rules[0].Principals, 1)
	assert.Equal(t, "100.64.0.1", sshPol.Rules[0].Principals[0].NodeIP, "the owner may SSH into the network admin")

	assert.Nil(t, pm.NodeCapMap(3)[nodecap.RandomizeClientPort], "no it-admin exists")
}

func TestRoleAutogroupsRejectTaggedSSHSource(t *testing.T) {
	t.Parallel()

	users, nodes := roleTestFixtures()

	pol := `{
		"tagOwners": {"tag:server": ["owner@"]},
		"ssh": [
			{"action": "accept", "src": ["tag:server"], "dst": ["autogroup:admin"], "users": ["root"]}
		]
	}`

	_, err := NewPolicyManager([]byte(pol), users, nodes.ViewSlice())
	require.ErrorIs(t, err, ErrSSHTagSourceToAutogroupMember)
}

func TestStampRoleCaps(t *testing.T) {
	t.Parallel()

	users, nodes := roleTestFixtures()

	capMaps := map[types.NodeID]tailcfg.NodeCapMap{
		2: {nodecap.RandomizeClientPort: nil},
	}

	stampRoleCaps(users, nodes.ViewSlice(), capMaps)

	assert.Equal(t, tailcfg.NodeCapMap{nodecap.Admin: nil, nodecap.Owner: nil}, capMaps[1], "owner")
	assert.Equal(
		t,
		tailcfg.NodeCapMap{nodecap.RandomizeClientPort: nil, nodecap.Admin: nil},
		capMaps[2],
		"admin keeps its attrs",
	)
	assert.NotContains(t, capMaps, types.NodeID(3), "network admin is not an admin")
	assert.NotContains(t, capMaps, types.NodeID(4), "member")
	assert.NotContains(t, capMaps, types.NodeID(5), "tagged nodes inherit no role")

	assert.True(t, usersHaveAdmin(users))
	assert.False(t, usersHaveAdmin(types.Users{{ID: 9, Role: types.RoleAuditor}}))
}

// TestPolicyManagerStampsRoleCapsWithoutPolicy pins that role caps reach
// NodeCapMap even when the tailnet has no policy or nodeAttrs, which is the
// fast path the manager otherwise skips.
func TestPolicyManagerStampsRoleCapsWithoutPolicy(t *testing.T) {
	t.Parallel()

	users, nodes := roleTestFixtures()

	pm, err := NewPolicyManager(nil, users, nodes.ViewSlice())
	require.NoError(t, err)

	assert.Contains(t, pm.NodeCapMap(1), nodecap.Owner)
	assert.Contains(t, pm.NodeCapMap(2), nodecap.Admin)
	assert.Nil(t, pm.NodeCapMap(4))

	// Demoting the admin and promoting the member flows through SetUsers.
	users[1].Role = types.RoleMember
	users[3].Role = types.RoleAdmin

	_, err = pm.SetUsers(users)
	require.NoError(t, err)

	assert.Nil(t, pm.NodeCapMap(2))
	assert.Contains(t, pm.NodeCapMap(4), nodecap.Admin)
	assert.Contains(t, pm.NodesWithChangedCapMap(), types.NodeID(4))
}
