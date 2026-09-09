package db

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newInvite stores an invite for email and returns it with its token.
func newInvite(t *testing.T, db *HSDatabase, invite types.UserInvite) (types.UserInvite, string) {
	t.Helper()

	token, hash, err := NewInviteToken()
	require.NoError(t, err)

	if invite.ExpiresAt.IsZero() {
		invite.ExpiresAt = time.Now().Add(types.InviteDefaultExpiry)
	}

	if invite.Role == "" {
		invite.Role = types.RoleMember
	}

	stored, err := db.CreateUserInvite(invite, hash)
	require.NoError(t, err)

	return stored, token
}

func TestUserInvites(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Run("a token resolves to its invite and the email is normalised", func(t *testing.T) {
		t.Parallel()

		stored, token := newInvite(t, db, types.UserInvite{
			Email:    "  Ada@Example.com ",
			Role:     types.RoleAdmin,
			GroupIDs: []types.GroupID{3, 7},
		})

		assert.Equal(t, "ada@example.com", stored.Email, "the address is stored lowercased and trimmed")
		assert.Equal(t, types.RoleAdmin, stored.Role)
		assert.Equal(t, []types.GroupID{3, 7}, stored.GroupIDs)
		assert.False(t, stored.Accepted())

		got, err := db.GetUserInviteByToken(token)
		require.NoError(t, err)
		assert.Equal(t, stored.ID, got.ID)

		_, err = db.GetUserInviteByToken(token + "x")
		require.ErrorIs(t, err, types.ErrInviteNotFound)

		_, err = db.GetUserInviteByToken("")
		require.ErrorIs(t, err, types.ErrInviteNotFound)

		pending, err := db.GetPendingUserInviteByEmail("ADA@example.com", time.Now())
		require.NoError(t, err)
		assert.Equal(t, stored.ID, pending.ID, "the email lookup ignores case")
	})

	t.Run("an expired or accepted invite is not pending", func(t *testing.T) {
		t.Parallel()

		expired, _ := newInvite(t, db, types.UserInvite{
			Email:     "gone@example.com",
			ExpiresAt: time.Now().Add(-time.Minute),
		})
		assert.True(t, expired.Expired(time.Now()))

		_, err := db.GetPendingUserInviteByEmail("gone@example.com", time.Now())
		require.ErrorIs(t, err, types.ErrInviteNotFound)

		user, err := db.CreateUser(types.User{Name: "bea"})
		require.NoError(t, err)

		used, _ := newInvite(t, db, types.UserInvite{Email: "bea@example.com"})

		require.NoError(t, db.AcceptUserInvite(used.ID, types.UserID(user.ID), time.Now()))

		read, err := db.GetUserInvite(used.ID)
		require.NoError(t, err)
		assert.True(t, read.Accepted())
		assert.Equal(t, types.UserID(user.ID), read.AcceptedUserID)

		require.ErrorIs(t, db.AcceptUserInvite(used.ID, types.UserID(user.ID), time.Now()),
			types.ErrInviteAccepted, "an invite is consumed once")

		_, err = db.GetPendingUserInviteByEmail("bea@example.com", time.Now())
		require.ErrorIs(t, err, types.ErrInviteNotFound)
	})

	t.Run("rotating gives a new token and retires the old one", func(t *testing.T) {
		t.Parallel()

		stored, token := newInvite(t, db, types.UserInvite{Email: "cleo@example.com"})

		fresh, hash, err := NewInviteToken()
		require.NoError(t, err)

		rotated, err := db.RotateUserInviteToken(stored.ID, hash, time.Now().Add(time.Hour))
		require.NoError(t, err)
		assert.Equal(t, stored.ID, rotated.ID)

		_, err = db.GetUserInviteByToken(token)
		require.ErrorIs(t, err, types.ErrInviteNotFound, "the link in the previous mail stops working")

		got, err := db.GetUserInviteByToken(fresh)
		require.NoError(t, err)
		assert.Equal(t, stored.ID, got.ID)
	})

	t.Run("an unknown invite is not found", func(t *testing.T) {
		t.Parallel()

		_, err := db.GetUserInvite(999_999)
		require.ErrorIs(t, err, types.ErrInviteNotFound)

		require.ErrorIs(t, db.DeleteUserInvite(999_999), types.ErrInviteNotFound)

		_, err = db.RotateUserInviteToken(999_999, []byte("nope"), time.Now().Add(time.Hour))
		require.ErrorIs(t, err, types.ErrInviteNotFound)
	})

	t.Run("revoking removes the invite", func(t *testing.T) {
		t.Parallel()

		stored, token := newInvite(t, db, types.UserInvite{Email: "dee@example.com"})

		require.NoError(t, db.DeleteUserInvite(stored.ID))

		_, err := db.GetUserInviteByToken(token)
		require.ErrorIs(t, err, types.ErrInviteNotFound)
	})

	t.Run("listing shows every invite newest first", func(t *testing.T) {
		t.Parallel()

		newInvite(t, db, types.UserInvite{Email: "eve@example.com"})

		invites, err := db.ListUserInvites()
		require.NoError(t, err)
		require.NotEmpty(t, invites)

		for i := 1; i < len(invites); i++ {
			assert.Greater(t, invites[i-1].ID, invites[i].ID, "newest first")
		}
	})
}
