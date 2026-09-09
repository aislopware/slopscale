package state

import (
	"testing"

	hsdb "github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShareNode(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	alice := createUserWithRole(t, s, "alice", "")
	bob := createUserWithRole(t, s, "bob", "")
	carol := createUserWithRole(t, s, "carol", "")

	node := s.CreateRegisteredNodeForTest(alice, "alice-1")
	s.PutNodeInStoreForTest(*node)

	aliceID, bobID, carolID := types.UserID(alice.ID), types.UserID(bob.ID), types.UserID(carol.ID)

	_, _, err := s.ShareNode(node.ID, aliceID, nil)
	require.ErrorIs(t, err, ErrShareWithOwner)

	_, _, err = s.ShareNode(node.ID, types.UserID(999), nil)
	require.ErrorIs(t, err, hsdb.ErrUserNotFound)

	_, _, err = s.ShareNode(types.NodeID(999), bobID, nil)
	require.ErrorIs(t, err, ErrNodeNotInNodeStore)

	by := aliceID
	view, c, err := s.ShareNode(node.ID, bobID, &by)
	require.NoError(t, err)
	assert.Equal(t, []types.UserID{bobID}, view.SharedWith().AsSlice())
	assert.True(t, c.RequiresRuntimePeerComputation, "a share recomputes every node's peers")

	_, _, err = s.ShareNode(node.ID, bobID, &by)
	require.ErrorIs(t, err, hsdb.ErrNodeAlreadyShared)

	_, _, err = s.ShareNode(node.ID, carolID, nil)
	require.NoError(t, err)

	// The share is in the store and survives a reload from the database.
	stored, ok := s.GetNodeByID(node.ID)
	require.True(t, ok)
	assert.Equal(t, []types.UserID{bobID, carolID}, stored.SharedWith().AsSlice())

	fromDB, err := s.DB().GetNodeByID(node.ID)
	require.NoError(t, err)
	assert.Equal(t, []types.UserID{bobID, carolID}, fromDB.SharedWith)

	view, _, err = s.UnshareNode(node.ID, bobID)
	require.NoError(t, err)
	assert.Equal(t, []types.UserID{carolID}, view.SharedWith().AsSlice())

	_, _, err = s.UnshareNode(node.ID, bobID)
	require.ErrorIs(t, err, hsdb.ErrNodeNotShared)

	// Deleting a sharee drops the share everywhere.
	_, err = s.DeleteUser(carolID)
	require.NoError(t, err)

	stored, ok = s.GetNodeByID(node.ID)
	require.True(t, ok)
	assert.Empty(t, stored.SharedWith().AsSlice())

	fromDB, err = s.DB().GetNodeByID(node.ID)
	require.NoError(t, err)
	assert.Empty(t, fromDB.SharedWith)
}

// TestShareNodeRefusesWaitingUser covers a sharee still waiting for
// approval: the share would come alive silently once they are admitted.
func TestShareNodeRefusesWaitingUser(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	alice := createUserWithRole(t, s, "alice", "")

	node := s.CreateRegisteredNodeForTest(alice, "alice-1")
	s.PutNodeInStoreForTest(*node)

	setSetting(t, s, types.SettingUsersApprovalOn, true)

	pending, _, err := s.CreateUserFromLogin(types.User{Name: "waiting"})
	require.NoError(t, err)
	require.Nil(t, pending.ApprovedAt)

	_, _, err = s.ShareNode(node.ID, types.UserID(pending.ID), nil)
	require.ErrorIs(t, err, ErrUserNotApproved)

	stored, ok := s.GetNodeByID(node.ID)
	require.True(t, ok)
	assert.Empty(t, stored.SharedWith().AsSlice())
}
