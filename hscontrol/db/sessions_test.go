package db

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessions(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	user, err := db.CreateUser(types.User{Name: "alice"})
	require.NoError(t, err)

	other, err := db.CreateUser(types.User{Name: "bob"})
	require.NoError(t, err)

	t.Run("token resolves to its user until expiry", func(t *testing.T) {
		t.Parallel()

		token, created, err := db.CreateSession(types.UserID(user.ID), time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.NotEmpty(t, token)
		assert.NotZero(t, created.ID)

		got, err := db.AuthenticateSession(token)
		require.NoError(t, err)
		assert.Equal(t, created.ID, got.ID)
		assert.Equal(t, types.UserID(user.ID), got.UserID)

		_, err = db.AuthenticateSession(token + "x")
		require.ErrorIs(t, err, ErrSessionNotFound)

		_, err = db.AuthenticateSession("")
		require.ErrorIs(t, err, ErrSessionNotFound)

		require.NoError(t, db.DeleteSession(created.ID))

		_, err = db.AuthenticateSession(token)
		require.ErrorIs(t, err, ErrSessionNotFound)

		require.NoError(t, db.DeleteSession(created.ID), "deleting twice is harmless")
	})

	t.Run("an expired session is refused and reaped", func(t *testing.T) {
		t.Parallel()

		token, _, err := db.CreateSession(types.UserID(user.ID), time.Now().Add(-time.Minute))
		require.NoError(t, err)

		_, err = db.AuthenticateSession(token)
		require.ErrorIs(t, err, ErrSessionExpired)

		reaped, err := db.DeleteExpiredSessions(time.Now())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, reaped, int64(1))

		_, err = db.AuthenticateSession(token)
		require.ErrorIs(t, err, ErrSessionNotFound)
	})

	t.Run("deleting the user or its sessions signs it out everywhere", func(t *testing.T) {
		t.Parallel()

		first, _, err := db.CreateSession(types.UserID(other.ID), time.Now().Add(time.Hour))
		require.NoError(t, err)

		second, _, err := db.CreateSession(types.UserID(other.ID), time.Now().Add(time.Hour))
		require.NoError(t, err)

		ended, err := db.DeleteUserSessions(types.UserID(other.ID))
		require.NoError(t, err)
		assert.Equal(t, int64(2), ended)

		for _, token := range []string{first, second} {
			_, err = db.AuthenticateSession(token)
			require.ErrorIs(t, err, ErrSessionNotFound)
		}

		third, _, err := db.CreateSession(types.UserID(other.ID), time.Now().Add(time.Hour))
		require.NoError(t, err)

		require.NoError(t, db.DestroyUser(types.UserID(other.ID)))

		_, err = db.AuthenticateSession(third)
		require.ErrorIs(t, err, ErrSessionNotFound, "the sessions row cascades with the user")
	})
}
