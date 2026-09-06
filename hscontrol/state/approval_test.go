package state

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

func registerWithKey(t *testing.T, s *State, authKey, hostname string) (types.NodeView, error) {
	t.Helper()

	nodeKey := key.NewNode()
	machineKey := key.NewMachine()

	node, _, err := s.HandleNodeFromPreAuthKey(tailcfg.RegisterRequest{
		Auth:     &tailcfg.RegisterResponseAuth{AuthKey: authKey},
		NodeKey:  nodeKey.Public(),
		Hostinfo: &tailcfg.Hostinfo{Hostname: hostname},
		Expiry:   time.Now().Add(24 * time.Hour),
	}, machineKey.Public())

	return node, err
}

func setSetting(t *testing.T, s *State, k types.SettingKey, on bool) {
	t.Helper()

	_, err := s.SetSetting(k, on)
	require.NoError(t, err)
}

func TestDeviceApprovalAtRegistration(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	user := createUserWithRole(t, s, "owner", "")

	plain := func() string {
		pak, err := s.CreatePreAuthKeyFromSpec(types.PreAuthKeySpec{UserID: user.TypedID()})
		require.NoError(t, err)

		return pak.Key
	}
	preauthorized := func() string {
		pak, err := s.CreatePreAuthKeyFromSpec(types.PreAuthKeySpec{UserID: user.TypedID(), Preauthorized: true})
		require.NoError(t, err)

		return pak.Key
	}

	off, err := registerWithKey(t, s, plain(), "approval-off")
	require.NoError(t, err)
	assert.True(t, off.IsApproved(), "with device approval off every node is approved")

	setSetting(t, s, types.SettingDevicesApprovalOn, true)

	waiting, err := registerWithKey(t, s, plain(), "waiting")
	require.NoError(t, err)
	assert.False(t, waiting.IsApproved(), "an ordinary key registers a waiting node")

	skipped, err := registerWithKey(t, s, preauthorized(), "preauthorized")
	require.NoError(t, err)
	assert.True(t, skipped.IsApproved(), "a preauthorized key skips the wait")

	// The waiting node has no peers and is nobody's peer; the approved
	// nodes see each other.
	assert.Empty(t, s.ListPeers(waiting.ID()).AsSlice())
	assert.Empty(t, s.ListPeers(waiting.ID(), off.ID(), skipped.ID()).AsSlice())
	assert.Len(t, s.ListPeers(off.ID()).AsSlice(), 1)
	assert.Empty(t, s.ListPeers(off.ID(), waiting.ID()).AsSlice())

	filter, err := s.FilterForNode(waiting)
	require.NoError(t, err)
	assert.Nil(t, filter)

	// Approving fans out as a policy recompute that carries every node's
	// self node, so the admitted client sees MachineAuthorized flip.
	approved, c, err := s.SetNodeApproval(waiting.ID(), true)
	require.NoError(t, err)
	assert.True(t, approved.IsApproved())
	assert.True(t, c.RequiresRuntimePeerComputation)
	assert.True(t, c.IncludeSelf)
	assert.Len(t, s.ListPeers(waiting.ID()).AsSlice(), 2)

	// The approval survives a reload from the database.
	fromDB, err := s.db.GetNodeByID(waiting.ID())
	require.NoError(t, err)
	assert.NotNil(t, fromDB.ApprovedAt)

	// Withdrawing isolates the node again.
	_, _, err = s.SetNodeApproval(waiting.ID(), false)
	require.NoError(t, err)
	assert.Empty(t, s.ListPeers(waiting.ID()).AsSlice())
	assert.Len(t, s.ListPeers(off.ID()).AsSlice(), 1)

	// Switching device approval off admits everything waiting.
	c, err = s.SetSetting(types.SettingDevicesApprovalOn, false)
	require.NoError(t, err)
	assert.True(t, c.IncludeSelf)
	assert.Len(t, s.ListPeers(waiting.ID()).AsSlice(), 2)
}

func TestUsersApproval(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	admin := createUserWithRole(t, s, "owner", "")
	assert.NotNil(t, admin.ApprovedAt, "an administrator-created user is approved")

	login, _, err := s.CreateUserFromLogin(types.User{Name: "login-off"})
	require.NoError(t, err)
	assert.NotNil(t, login.ApprovedAt, "with users approval off a login-created user is approved")

	setSetting(t, s, types.SettingUsersApprovalOn, true)

	pending, _, err := s.CreateUserFromLogin(types.User{Name: "login-on"})
	require.NoError(t, err)
	require.Nil(t, pending.ApprovedAt, "with users approval on a login-created user waits")

	pak, err := s.CreatePreAuthKey(pending.TypedID(), true, false, nil, nil)
	require.NoError(t, err)

	_, err = registerWithKey(t, s, pak.Key, "pending-node")
	require.ErrorIs(t, err, ErrUserNotApproved, "a waiting user cannot register nodes")

	// A tagged key created by the waiting user registers a tagged node,
	// which the user does not own.
	tagged, err := s.CreatePreAuthKey(pending.TypedID(), true, false, nil, []string{"tag:server"})
	require.NoError(t, err)

	_, err = registerWithKey(t, s, tagged.Key, "tagged-node")
	require.NoError(t, err)

	approved, _, err := s.SetUserApproval(types.UserID(pending.ID), true)
	require.NoError(t, err)
	assert.NotNil(t, approved.ApprovedAt)

	node, err := registerWithKey(t, s, pak.Key, "pending-node")
	require.NoError(t, err)
	assert.True(t, node.IsApproved())

	// Withdrawing the user withdraws the user's nodes.
	_, _, err = s.SetUserApproval(types.UserID(pending.ID), false)
	require.NoError(t, err)

	withdrawn, ok := s.GetNodeByID(node.ID())
	require.True(t, ok)
	assert.False(t, withdrawn.IsApproved())

	// Switching users approval off admits the user; the node comes back
	// with the user because device approval is off.
	_, err = s.SetSetting(types.SettingUsersApprovalOn, false)
	require.NoError(t, err)

	fromDB, err := s.GetUserByID(types.UserID(pending.ID))
	require.NoError(t, err)
	assert.NotNil(t, fromDB.ApprovedAt)

	_, _, err = s.SetUserApproval(types.UserID(pending.ID), true)
	require.NoError(t, err)

	restored, ok := s.GetNodeByID(node.ID())
	require.True(t, ok)
	assert.True(t, restored.IsApproved())
}

func TestSettingsPersist(t *testing.T) {
	t.Parallel()

	dbPath := t.TempDir() + "/headscale.db"

	s, err := NewState(persistTestConfig(dbPath))
	require.NoError(t, err)
	assert.Equal(t, types.Settings{}, s.Settings(), "a fresh database has every switch off")

	setSetting(t, s, types.SettingDevicesApprovalOn, true)
	setSetting(t, s, types.SettingUsersApprovalOn, true)
	setSetting(t, s, types.SettingUsersApprovalOn, false)
	require.NoError(t, s.Close())

	reopened, err := NewState(persistTestConfig(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })

	assert.Equal(t, types.Settings{DevicesApprovalOn: true}, reopened.Settings())

	_, err = reopened.SetSetting("bogus", true)
	assert.ErrorIs(t, err, ErrUnknownSetting)
}
