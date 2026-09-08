package state

import (
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

func TestAccessGroupsAndRules(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	alice := createUserWithRole(t, s, "alice", "")
	bob := createUserWithRole(t, s, "bob", "")

	aliceNode := s.CreateRegisteredNodeForTest(alice, "alice-1")
	s.PutNodeInStoreForTest(*aliceNode)

	bobNode := s.CreateRegisteredNodeForTest(bob, "bob-1")
	s.PutNodeInStoreForTest(*bobNode)

	_, err := s.updatePolicyManagerNodes()
	require.NoError(t, err)

	// The builtin groups exist from the start and are fixed.
	model := s.AccessModel()
	require.Len(t, model.Groups, 2)
	all, self := model.Groups[0], model.Groups[1]
	assert.Equal(t, types.GroupAllName, all.Name)
	assert.True(t, all.IsBuiltin())
	assert.Equal(t, types.GroupSelfName, self.Name)
	assert.True(t, self.IsSelf())

	// A fresh database is seeded with the own-machines rule, enabled.
	require.Len(t, model.Rules, 1)
	assert.Equal(t, types.DefaultRuleName, model.Rules[0].Name)
	assert.True(t, model.Rules[0].Enabled)
	assert.Equal(t, []types.GroupID{all.ID}, model.Rules[0].SourceGroupIDs)
	assert.Equal(t, []types.GroupID{self.ID}, model.Rules[0].DestinationGroupIDs)

	// Self is a destination only, never a member set.
	_, _, err = s.CreateAccessRule(types.AccessRule{
		Name: "self as source", Enabled: true, Protocol: types.AccessProtocolAll,
		SourceGroupIDs: []types.GroupID{self.ID}, DestinationGroupIDs: []types.GroupID{all.ID},
	})
	require.ErrorIs(t, err, types.ErrRuleSelfSource)

	_, _, err = s.CreateAccessRule(types.AccessRule{
		Name: "self both ways", Enabled: true, Protocol: types.AccessProtocolAll, Bidirectional: true,
		SourceGroupIDs: []types.GroupID{all.ID}, DestinationGroupIDs: []types.GroupID{self.ID},
	})
	require.ErrorIs(t, err, types.ErrRuleSelfBoth)

	_, err = s.DeleteAccessRule(model.Rules[0].ID)
	require.NoError(t, err)

	_, _, err = s.UpdateGroup(all.ID, "Everyone", "", false)
	require.ErrorIs(t, err, types.ErrGroupBuiltin)

	_, err = s.DeleteGroup(all.ID)
	require.ErrorIs(t, err, types.ErrGroupBuiltin)

	_, _, err = s.AddGroupNode(all.ID, aliceNode.ID, nil)
	require.ErrorIs(t, err, types.ErrGroupBuiltin)

	// Names are validated and unique.
	_, _, err = s.CreateGroup("  ", "", false)
	require.ErrorIs(t, err, types.ErrGroupNameEmpty)

	_, _, err = s.CreateGroup("bad/name", "", false)
	require.ErrorIs(t, err, types.ErrGroupNameInvalid)

	eng, _, err := s.CreateGroup("Engineering", "", false)
	require.NoError(t, err)

	_, _, err = s.CreateGroup("Engineering", "", false)
	require.ErrorIs(t, err, types.ErrGroupNameTaken)

	servers, _, err := s.CreateGroup("Servers", "", false)
	require.NoError(t, err)

	// Membership by user and by node; unknown members are refused.
	_, _, err = s.AddGroupUser(eng.ID, types.UserID(999), nil)
	require.Error(t, err)

	_, _, err = s.AddGroupNode(servers.ID, types.NodeID(999), nil)
	require.ErrorIs(t, err, ErrNodeNotInNodeStore)

	group, _, err := s.AddGroupUser(eng.ID, types.UserID(alice.ID), nil)
	require.NoError(t, err)
	assert.Equal(t, []types.UserID{types.UserID(alice.ID)}, group.UserIDs)

	group, _, err = s.AddGroupNode(servers.ID, bobNode.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, []types.NodeID{bobNode.ID}, group.NodeIDs)

	// No rule yet: allow-all.
	filter, _ := s.Filter()
	assert.Equal(t, tailcfg.FilterAllowAll, filter)

	// A rule is validated against the model.
	_, _, err = s.CreateAccessRule(types.AccessRule{
		Name: "x", Enabled: true, Protocol: types.AccessProtocolTCP, Ports: "22",
		SourceGroupIDs: []types.GroupID{eng.ID}, DestinationGroupIDs: []types.GroupID{999},
	})
	require.ErrorIs(t, err, types.ErrGroupNotFound)

	_, _, err = s.CreateAccessRule(types.AccessRule{
		Name: "x", Enabled: true, Protocol: types.AccessProtocolICMP, Ports: "22",
		SourceGroupIDs: []types.GroupID{eng.ID}, DestinationGroupIDs: []types.GroupID{servers.ID},
	})
	require.ErrorIs(t, err, types.ErrRulePortsWithout)

	rule, c, err := s.CreateAccessRule(types.AccessRule{
		Name: "SSH", Enabled: true, Protocol: types.AccessProtocolTCP, Ports: " 22, 80-90 ",
		SourceGroupIDs: []types.GroupID{eng.ID}, DestinationGroupIDs: []types.GroupID{servers.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, "22,80-90", rule.Ports)
	assert.True(t, c.RequiresRuntimePeerComputation)

	filter, _ = s.Filter()
	require.Len(t, filter, 1)
	assert.Contains(t, filter[0].SrcIPs, aliceNode.IPv4.String())
	assert.NotContains(t, filter[0].SrcIPs, bobNode.IPv4.String())
	assert.Equal(t, bobNode.IPv4.String(), filter[0].DstPorts[0].IP)

	// The group is in use, so it cannot go.
	_, err = s.DeleteGroup(servers.ID)
	require.ErrorIs(t, err, types.ErrGroupInUse)

	// Disabling the rule restores allow-all; deleting frees the group.
	rule.Enabled = false
	_, _, err = s.UpdateAccessRule(rule)
	require.NoError(t, err)

	filter, _ = s.Filter()
	assert.Equal(t, tailcfg.FilterAllowAll, filter)

	_, err = s.DeleteAccessRule(rule.ID)
	require.NoError(t, err)

	_, err = s.DeleteAccessRule(rule.ID)
	require.ErrorIs(t, err, types.ErrRuleNotFound)

	_, err = s.DeleteGroup(servers.ID)
	require.NoError(t, err)

	// Deleting a user drops it from every group.
	group, _, err = s.AddGroupUser(eng.ID, types.UserID(bob.ID), nil)
	require.NoError(t, err)
	assert.Len(t, group.UserIDs, 2)

	_, err = s.DeleteNode(bobNode.View())
	require.NoError(t, err)

	_, err = s.DeleteUser(types.UserID(bob.ID))
	require.NoError(t, err)

	group, err = s.GetGroup(eng.ID)
	require.NoError(t, err)
	assert.Equal(t, []types.UserID{types.UserID(alice.ID)}, group.UserIDs)
}

func TestPreAuthKeyGroupsEnrolNode(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	alice := createUserWithRole(t, s, "alice", "")

	servers, _, err := s.CreateGroup("Servers", "", false)
	require.NoError(t, err)

	uid := types.UserID(alice.ID)

	key, err := s.CreatePreAuthKeyFromSpec(types.PreAuthKeySpec{
		UserID:        &uid,
		Reusable:      true,
		Preauthorized: true,
		Groups:        []types.GroupID{servers.ID, types.GroupID(999)},
	})
	require.NoError(t, err)
	assert.Equal(t, []types.GroupID{servers.ID, types.GroupID(999)}, key.Groups)

	node, err := registerWithKey(t, s, key.Key, "server-1")
	require.NoError(t, err)

	group, err := s.GetGroup(servers.ID)
	require.NoError(t, err)
	assert.Equal(t, []types.NodeID{node.ID()}, group.NodeIDs, "the missing group is skipped, the real one joined")
}
