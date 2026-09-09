package db

import (
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
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

		token, created, err := db.CreateSession(types.UserID(user.ID), time.Now().Add(time.Hour),
			SessionClient{RemoteAddr: "192.0.2.10", UserAgent: "Mozilla/5.0"})
		require.NoError(t, err)
		require.NotEmpty(t, token)
		assert.NotZero(t, created.ID)
		assert.Equal(t, "192.0.2.10", created.RemoteAddr)
		assert.Equal(t, "Mozilla/5.0", created.UserAgent)

		read, err := db.GetSession(created.ID)
		require.NoError(t, err)
		assert.Equal(t, "192.0.2.10", read.RemoteAddr, "the client is stored, not only returned")
		assert.Equal(t, "Mozilla/5.0", read.UserAgent)

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

		token, _, err := db.CreateSession(types.UserID(user.ID), time.Now().Add(-time.Minute), SessionClient{})
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

		first, _, err := db.CreateSession(types.UserID(other.ID), time.Now().Add(time.Hour), SessionClient{})
		require.NoError(t, err)

		second, _, err := db.CreateSession(types.UserID(other.ID), time.Now().Add(time.Hour), SessionClient{})
		require.NoError(t, err)

		ended, err := db.DeleteUserSessions(types.UserID(other.ID))
		require.NoError(t, err)
		assert.Equal(t, int64(2), ended)

		for _, token := range []string{first, second} {
			_, err = db.AuthenticateSession(token)
			require.ErrorIs(t, err, ErrSessionNotFound)
		}

		third, _, err := db.CreateSession(types.UserID(other.ID), time.Now().Add(time.Hour), SessionClient{})
		require.NoError(t, err)

		require.NoError(t, db.DestroyUser(types.UserID(other.ID)))

		_, err = db.AuthenticateSession(third)
		require.ErrorIs(t, err, ErrSessionNotFound, "the sessions row cascades with the user")
	})
}

// TestSessionListing pins that listing shows only sessions that have not
// expired, narrows to one user when asked, and that a long User-Agent is
// cut rather than stored whole.
func TestSessionListing(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	alice, err := db.CreateUser(types.User{Name: "alice"})
	require.NoError(t, err)

	bob, err := db.CreateUser(types.User{Name: "bob"})
	require.NoError(t, err)

	_, live, err := db.CreateSession(types.UserID(alice.ID), time.Now().Add(time.Hour),
		SessionClient{RemoteAddr: "192.0.2.1", UserAgent: strings.Repeat("a", maxUserAgentLength+10)})
	require.NoError(t, err)
	assert.Len(t, live.UserAgent, maxUserAgentLength, "a long User-Agent is cut")

	_, _, err = db.CreateSession(types.UserID(alice.ID), time.Now().Add(-time.Hour), SessionClient{})
	require.NoError(t, err)

	_, bobs, err := db.CreateSession(types.UserID(bob.ID), time.Now().Add(time.Hour), SessionClient{})
	require.NoError(t, err)

	all, err := db.ListSessions(0, time.Now())
	require.NoError(t, err)
	require.Len(t, all, 2, "the expired session is left out")

	ids := []uint64{all[0].ID, all[1].ID}
	assert.Contains(t, ids, live.ID)
	assert.Contains(t, ids, bobs.ID)

	mine, err := db.ListSessions(types.UserID(alice.ID), time.Now())
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, live.ID, mine[0].ID)

	_, err = db.GetSession(live.ID + bobs.ID + 1)
	require.ErrorIs(t, err, ErrSessionNotFound)
}
