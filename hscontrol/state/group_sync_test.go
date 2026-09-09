package state

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSyncUserGroups covers the mirror of an identity provider's groups
// claim: groups are created on first sight, a user follows the claim in
// and out of them, an operator-made group is never taken over by name,
// and the users of a synced group cannot be edited by hand.
func TestSyncUserGroups(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	alice := createUserWithRole(t, s, "alice", "")
	bob := createUserWithRole(t, s, "bob", "")
	aliceID, bobID := types.UserID(alice.ID), types.UserID(bob.ID)

	manual, _, err := s.CreateGroup("ops", "made by hand", false)
	require.NoError(t, err)

	c, err := s.SyncUserGroups(aliceID, []string{"eng", "ops", "sre"})
	require.NoError(t, err)
	assert.False(t, c.IsEmpty(), "memberships moved")

	groupByName := func(name string) types.AccessGroup {
		for _, g := range s.AccessModel().Groups {
			if g.Name == name {
				return g
			}
		}

		t.Fatalf("no group %q", name)

		return types.AccessGroup{}
	}

	eng, sre, ops := groupByName("eng"), groupByName("sre"), groupByName("ops")
	assert.Equal(t, types.GroupSourceOIDC, eng.Source)
	assert.Equal(t, []types.UserID{aliceID}, eng.UserIDs)
	assert.Equal(t, []types.UserID{aliceID}, sre.UserIDs)
	assert.Equal(t, manual.ID, ops.ID)
	assert.Empty(t, ops.Source, "an operator-made group keeps its source")
	assert.Empty(t, ops.UserIDs, "and is not joined by name")

	// The same claim again changes nothing.
	c, err = s.SyncUserGroups(aliceID, []string{"eng", "sre"})
	require.NoError(t, err)
	assert.True(t, c.IsEmpty())

	// Bob joins eng; Alice leaves sre when her claim drops it. The empty
	// synced group stays for the next member.
	_, err = s.SyncUserGroups(bobID, []string{"eng"})
	require.NoError(t, err)
	_, err = s.SyncUserGroups(aliceID, []string{"eng"})
	require.NoError(t, err)

	eng, sre = groupByName("eng"), groupByName("sre")
	assert.ElementsMatch(t, []types.UserID{aliceID, bobID}, eng.UserIDs)
	assert.Empty(t, sre.UserIDs)

	// The users of a synced group are the claim's business.
	_, _, err = s.AddGroupUser(sre.ID, bobID, nil)
	require.ErrorIs(t, err, types.ErrGroupSyncedUsers)
	_, _, err = s.RemoveGroupUser(eng.ID, bobID)
	require.ErrorIs(t, err, types.ErrGroupSyncedUsers)
	_, _, err = s.SetGroupMembers(eng.ID, nil, []types.UserID{aliceID})
	require.ErrorIs(t, err, types.ErrGroupSyncedUsers)

	// Nodes can still be added by hand, and the same user set is accepted.
	node := s.CreateRegisteredNodeForTest(alice, "alice-1")
	s.PutNodeInStoreForTest(*node)
	_, _, err = s.SetGroupMembers(eng.ID, []types.NodeID{node.ID}, []types.UserID{bobID, aliceID})
	require.NoError(t, err)
	_, _, err = s.AddGroupNode(sre.ID, node.ID, nil)
	require.NoError(t, err)

	// A synced group can be described and deleted like any other, but it
	// keeps its name: the sync finds it by the claim's name.
	_, _, err = s.UpdateGroup(sre.ID, "sre-team", "on call", true)
	require.ErrorIs(t, err, types.ErrGroupSyncedName)
	_, _, err = s.UpdateGroup(sre.ID, "sre", "on call", true)
	require.NoError(t, err)
	_, err = s.DeleteGroup(sre.ID)
	require.NoError(t, err)

	// Deleting a user takes them out of the synced groups with the rest.
	_, err = s.DeleteUser(bobID)
	require.NoError(t, err)
	assert.Equal(t, []types.UserID{aliceID}, groupByName("eng").UserIDs)
}

func TestOIDCGroupsConfigSyncedNames(t *testing.T) {
	t.Parallel()

	off := types.OIDCGroupsConfig{}
	assert.Nil(t, off.SyncedNames([]string{"eng"}))

	on := types.OIDCGroupsConfig{Sync: true}
	assert.Equal(t, []string{"eng", "sre"}, on.SyncedNames([]string{"eng", " sre ", "eng", "", "bad/name"}))

	prefixed := types.OIDCGroupsConfig{Sync: true, Prefix: "hs-"}
	assert.Equal(t, []string{"eng"}, prefixed.SyncedNames([]string{"hs-eng", "sre", "hs-"}))
}
