package state

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateUserInviteRules pins what an invitation may ask for: a real
// role that is not owner, a lifetime inside the allowed range, groups that
// exist, and an address that belongs to neither a user nor another
// pending invitation.
func TestCreateUserInviteRules(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	group, _, err := s.CreateGroup("engineering", "", false)
	require.NoError(t, err)

	t.Run("an invitation cannot hand out ownership", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(nil, InviteSpec{Email: "owner@example.com", Role: types.RoleOwner})
		require.ErrorIs(t, err, types.ErrInviteOwnerRole)
	})

	t.Run("the role must exist", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(nil, InviteSpec{Email: "root@example.com", Role: types.Role("root")})
		require.ErrorIs(t, err, types.ErrInvalidRole)
	})

	t.Run("an address is needed", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(nil, InviteSpec{Email: "   ", Role: types.RoleMember})
		require.ErrorIs(t, err, types.ErrInviteEmailEmpty)
	})

	t.Run("the address must be a plain mailbox", func(t *testing.T) {
		t.Parallel()

		// The address is mailed a link and becomes the user's login, so a
		// display name, a list or a header is not an address here.
		for _, email := range []string{
			"not-an-address",
			"ops <ops@example.com>",
			"a@example.com, b@example.com",
			"a@@example.com",
			"ops@example.com\nbcc: attacker@example.com",
		} {
			_, _, err := s.CreateUserInvite(nil, InviteSpec{Email: email, Role: types.RoleMember})
			require.ErrorIs(t, err, types.ErrInviteEmailInvalid, "email %q", email)
		}

		_, _, err := s.CreateUserInvite(nil, InviteSpec{Email: "plain@example.com", Role: types.RoleMember})
		require.NoError(t, err)
	})

	t.Run("the lifetime is bounded", func(t *testing.T) {
		t.Parallel()

		for _, expiry := range []time.Duration{-time.Hour, types.InviteMaxExpiry + time.Hour} {
			_, _, err := s.CreateUserInvite(nil, InviteSpec{
				Email: "long@example.com", Role: types.RoleMember, Expiry: expiry,
			})
			require.ErrorIs(t, err, types.ErrInviteExpiryRange, "expiry %s", expiry)
		}
	})

	t.Run("the groups must exist", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(nil, InviteSpec{
			Email: "ghost@example.com", Role: types.RoleMember, GroupIDs: []types.GroupID{999_999},
		})
		require.ErrorIs(t, err, types.ErrGroupNotFound)
	})

	t.Run("the default lifetime is a week", func(t *testing.T) {
		t.Parallel()

		invite, token, err := s.CreateUserInvite(nil, InviteSpec{
			Email: "ada@example.com", Role: types.RoleAdmin, GroupIDs: []types.GroupID{group.ID},
		})
		require.NoError(t, err)
		require.NotEmpty(t, token)

		assert.WithinDuration(t, time.Now().Add(types.InviteDefaultExpiry), invite.ExpiresAt, time.Minute)
		assert.Equal(t, []types.GroupID{group.ID}, invite.GroupIDs)

		_, _, err = s.CreateUserInvite(nil, InviteSpec{Email: "Ada@Example.com", Role: types.RoleMember})
		require.ErrorIs(t, err, types.ErrInviteEmailTaken, "a pending invitation holds the address")

		resolved, err := s.PendingUserInviteByToken(token)
		require.NoError(t, err)
		assert.Equal(t, invite.ID, resolved.ID)
	})

	t.Run("an address that belongs to a user is refused", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUser(types.User{Name: "bea", Email: "Bea@Example.com"})
		require.NoError(t, err)

		_, _, err = s.CreateUserInvite(nil, InviteSpec{Email: "bea@example.com", Role: types.RoleMember})
		require.ErrorIs(t, err, types.ErrInviteEmailTaken)
	})
}

// TestCreateUserInviteRoleActor pins that an invitation cannot hand out a
// role its sender could not assign directly: an it-admin holds the users
// scope, so without the check it could invite an admin and accept the
// invitation with a second identity of its own.
func TestCreateUserInviteRoleActor(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	// The first user of an empty tailnet becomes its owner, so make one
	// before the actors under test.
	createUserWithRole(t, s, "owner", types.RoleOwner)
	itAdmin := createUserWithRole(t, s, "it", types.RoleITAdmin)
	admin := createUserWithRole(t, s, "boss", types.RoleAdmin)

	t.Run("an it-admin cannot invite an admin", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(actor(itAdmin), InviteSpec{
			Email: "climb@example.com", Role: types.RoleAdmin,
		})
		require.ErrorIs(t, err, ErrRoleChangeForbidden)
	})

	t.Run("an it-admin may still invite a member", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(actor(itAdmin), InviteSpec{
			Email: "member@example.com", Role: types.RoleMember,
		})
		require.NoError(t, err)
	})

	t.Run("an admin may invite an admin", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(actor(admin), InviteSpec{
			Email: "peer@example.com", Role: types.RoleAdmin,
		})
		require.NoError(t, err)
	})

	t.Run("the socket may invite an admin", func(t *testing.T) {
		t.Parallel()

		_, _, err := s.CreateUserInvite(nil, InviteSpec{
			Email: "cli@example.com", Role: types.RoleAdmin,
		})
		require.NoError(t, err)
	})
}

// TestAcceptUserInvite pins what accepting does: the user is created
// approved even while user approval is on, gets the invited role through
// the same path an administrator uses and joins the invited groups, and
// the invitation is consumed once.
func TestAcceptUserInvite(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	_, err := s.SetSetting(types.SettingUsersApprovalOn, true)
	require.NoError(t, err)

	// The tailnet already has an owner, so the invited user is not made one
	// by being first.
	createUserWithRole(t, s, "owner", types.RoleOwner)

	group, _, err := s.CreateGroup("engineering", "", false)
	require.NoError(t, err)

	invite, _, err := s.CreateUserInvite(nil, InviteSpec{
		Email:    "cleo@example.com",
		Role:     types.RoleITAdmin,
		GroupIDs: []types.GroupID{group.ID},
	})
	require.NoError(t, err)

	user, changes, err := s.AcceptUserInvite(invite, types.User{Name: "cleo", Email: "cleo@example.com"})
	require.NoError(t, err)
	require.NotEmpty(t, changes, "the new user and its role are a change")

	assert.Equal(t, types.RoleITAdmin, user.Role)
	require.NotNil(t, user.ApprovedAt, "an invited user is approved while approval is on")

	joined, err := s.GetGroup(group.ID)
	require.NoError(t, err)
	assert.Contains(t, joined.UserIDs, types.UserID(user.ID))

	consumed, err := s.GetUserInvite(invite.ID)
	require.NoError(t, err)
	assert.True(t, consumed.Accepted())
	assert.Equal(t, types.UserID(user.ID), consumed.AcceptedUserID)

	_, err = s.PendingUserInviteByEmail("cleo@example.com")
	require.ErrorIs(t, err, types.ErrInviteNotFound)
}

// TestResendUserInvite pins that re-sending rotates the token and that an
// invitation already accepted cannot be re-sent.
func TestResendUserInvite(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	invite, first, err := s.CreateUserInvite(nil, InviteSpec{Email: "dora@example.com", Role: types.RoleMember})
	require.NoError(t, err)

	rotated, second, err := s.ResendUserInvite(invite.ID, time.Hour)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	assert.WithinDuration(t, time.Now().Add(time.Hour), rotated.ExpiresAt, time.Minute)

	_, err = s.PendingUserInviteByToken(first)
	require.ErrorIs(t, err, types.ErrInviteNotFound)

	_, err = s.PendingUserInviteByToken(second)
	require.NoError(t, err)

	_, _, err = s.ResendUserInvite(invite.ID, 800*time.Hour)
	require.ErrorIs(t, err, types.ErrInviteExpiryRange)

	user, _, err := s.AcceptUserInvite(rotated, types.User{Name: "dora", Email: "dora@example.com"})
	require.NoError(t, err)
	assert.Equal(t, types.RoleOwner, user.Role, "the first user of an empty tailnet owns it, invite or not")

	_, _, err = s.ResendUserInvite(invite.ID, time.Hour)
	require.ErrorIs(t, err, types.ErrInviteAccepted)

	require.NoError(t, s.DeleteUserInvite(invite.ID))
	require.ErrorIs(t, s.DeleteUserInvite(invite.ID), types.ErrInviteNotFound)
}
