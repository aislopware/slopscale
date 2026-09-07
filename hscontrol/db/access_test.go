package db

import (
	"testing"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessGroupsAndRules(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	alice, err := db.CreateUser(types.User{Name: "alice"})
	require.NoError(t, err)

	node := db.CreateNodeForTest(alice, "laptop")

	all, err := db.EnsureAllGroup()
	require.NoError(t, err)
	assert.Equal(t, types.GroupBuiltinAll, all.Builtin)

	again, err := db.EnsureAllGroup()
	require.NoError(t, err)
	assert.Equal(t, all.ID, again.ID, "the builtin group is created once")

	eng, err := db.CreateGroup("Engineering", "The engineers", false)
	require.NoError(t, err)
	assert.NotZero(t, eng.ID)

	_, err = db.CreateGroup("Engineering", "", false)
	require.ErrorIs(t, err, types.ErrGroupNameTaken)

	require.NoError(t, db.AddGroupNode(eng.ID, node.ID, nil))
	require.ErrorIs(t, db.AddGroupNode(eng.ID, node.ID, nil), types.ErrGroupMemberExists)
	require.NoError(t, db.AddGroupUser(eng.ID, types.UserID(alice.ID), nil))

	got, err := db.GetGroup(eng.ID)
	require.NoError(t, err)
	assert.Equal(t, []types.NodeID{node.ID}, got.NodeIDs)
	assert.Equal(t, []types.UserID{types.UserID(alice.ID)}, got.UserIDs)

	servers, err := db.CreateGroup("Servers", "", false)
	require.NoError(t, err)

	rule, err := db.CreateAccessRule(types.AccessRule{
		Name:                "SSH to servers",
		Enabled:             true,
		Protocol:            types.AccessProtocolTCP,
		Ports:               "22",
		SourceGroupIDs:      []types.GroupID{eng.ID},
		DestinationGroupIDs: []types.GroupID{servers.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, []types.GroupID{eng.ID}, rule.SourceGroupIDs)
	assert.Equal(t, []types.GroupID{servers.ID}, rule.DestinationGroupIDs)

	model, err := db.LoadAccessModel()
	require.NoError(t, err)
	require.Len(t, model.Groups, 3)
	require.Len(t, model.Rules, 1)
	assert.Len(t, model.RulesUsingGroup(servers.ID), 1)

	engFromModel, ok := model.Group(eng.ID)
	require.True(t, ok)
	assert.Equal(t, []types.NodeID{node.ID}, engFromModel.NodeIDs)

	rule.Bidirectional = true
	rule.SourceGroupIDs = []types.GroupID{eng.ID, all.ID}

	updated, err := db.UpdateAccessRule(rule)
	require.NoError(t, err)
	assert.True(t, updated.Bidirectional)
	assert.ElementsMatch(t, []types.GroupID{eng.ID, all.ID}, updated.SourceGroupIDs)

	replaced, err := db.SetGroupMembers(eng.ID, nil, []types.UserID{types.UserID(alice.ID)})
	require.NoError(t, err)
	assert.Empty(t, replaced.NodeIDs)
	assert.Len(t, replaced.UserIDs, 1)

	require.NoError(t, db.RemoveGroupUser(eng.ID, types.UserID(alice.ID)))
	require.ErrorIs(t, db.RemoveGroupUser(eng.ID, types.UserID(alice.ID)), types.ErrGroupMemberMissing)

	require.NoError(t, db.DeleteAccessRule(rule.ID))
	require.ErrorIs(t, db.DeleteAccessRule(rule.ID), types.ErrRuleNotFound)

	require.NoError(t, db.DeleteGroup(servers.ID))
	_, err = db.GetGroup(servers.ID)
	require.ErrorIs(t, err, types.ErrGroupNotFound)

	// Deleting a node drops its memberships.
	require.NoError(t, db.AddGroupNode(eng.ID, node.ID, nil))
	require.NoError(t, db.DeleteNode(node))

	got, err = db.GetGroup(eng.ID)
	require.NoError(t, err)
	assert.Empty(t, got.NodeIDs)
}
