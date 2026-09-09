package servertest_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/aislopware/slopscale/hscontrol"
	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signInConsole walks a fresh browser through a console sign-in and
// returns it.
func signInConsole(t *testing.T, srv *servertest.TestServer) *http.Client {
	t.Helper()

	browser := srv.HTTPClient(t)

	status, page := browse(t, browser, srv.URL+hscontrol.ConsoleLoginPath)
	require.Equal(t, http.StatusOK, status, "sign-in did not land in the console:\n%s", page)
	require.NotNil(t, sessionCookie(t, browser, srv), "the callback set no session cookie")

	return browser
}

// sessionList reads the sessions the browser may see, and the id of the
// one it is signed in with.
func sessionList(t *testing.T, browser *http.Client, srv *servertest.TestServer) ([]any, string) {
	t.Helper()

	status, body := consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/auth/sessions", nil, nil)
	require.Equal(t, http.StatusOK, status, "%v", body)

	sessions, ok := body["sessions"].([]any)
	require.True(t, ok, "no sessions array: %v", body)

	var current string

	for _, raw := range sessions {
		entry, ok := raw.(map[string]any)
		require.True(t, ok)

		if entry["current"] == true {
			require.Empty(t, current, "two sessions claim to be the current one")

			id, ok := entry["id"].(string)
			require.True(t, ok)

			current = id
		}
	}

	return sessions, current
}

// TestConsoleSessionManagement walks three browser sign-ins through the
// session list and the two ways of ending a session: the list flags the
// caller's own session, an administrator ends any session and the browser
// holding it is signed out on its next request, a member sees and ends
// only their own, and signing a user out everywhere ends all of theirs.
//
//nolint:tparallel // the subtests drive browser sessions opened in order.
func TestConsoleSessionManagement(t *testing.T) {
	t.Parallel()

	// alice signs in twice, so she holds two sessions; bob is a member.
	srv, _ := newOIDCServer(t, nil,
		oidcUser("alice", "alice@example.com", true),
		oidcUser("alice", "alice@example.com", true),
		oidcUser("bob", "bob@example.com", true),
	)

	alice := signInConsole(t, srv)
	aliceSecond := signInConsole(t, srv)
	bob := signInConsole(t, srv)

	users, err := srv.State().ListAllUsers()
	require.NoError(t, err)
	require.Len(t, users, 2, "alice signed in twice as the same user")

	var bobID string

	for i := range users {
		if users[i].Name == "bob" {
			bobID = strconv.FormatUint(uint64(users[i].ID), 10)
		}
	}

	require.NotEmpty(t, bobID)

	sessions, aliceSession := sessionList(t, alice, srv)
	require.Len(t, sessions, 3, "an administrator sees every session")
	require.NotEmpty(t, aliceSession, "the caller's own session is flagged")

	t.Run("a session records the browser it was opened from", func(t *testing.T) {
		entry, ok := sessions[0].(map[string]any)
		require.True(t, ok)

		assert.NotEmpty(t, entry["userAgent"], "the User-Agent is recorded")
		assert.NotEmpty(t, field(t, entry, "user", "name"), "the session names its user")
		assert.NotEmpty(t, entry["createdAt"])
		assert.NotEmpty(t, entry["expiresAt"])
		assert.NotEmpty(t, entry["lastSeenAt"])
	})

	t.Run("a member sees only their own session", func(t *testing.T) {
		mine, bobSession := sessionList(t, bob, srv)
		assert.Len(t, mine, 1)
		assert.NotEmpty(t, bobSession)
		assert.Equal(t, "bob", field(t, mine[0], "user", "name"))
	})

	t.Run("a member cannot end an administrator's session", func(t *testing.T) {
		status, body := consoleCall(t, bob, http.MethodDelete,
			srv.URL+"/api/v1/auth/sessions/"+aliceSession, nil, nil)
		assert.Equal(t, http.StatusForbidden, status, "%v", body)

		status, _ = consoleCall(t, alice, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
		assert.Equal(t, http.StatusOK, status, "the administrator is still signed in")
	})

	t.Run("an unknown session is not found", func(t *testing.T) {
		status, _ := consoleCall(t, alice, http.MethodDelete,
			srv.URL+"/api/v1/auth/sessions/999999", nil, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("ending another session invalidates its cookie", func(t *testing.T) {
		_, second := sessionList(t, aliceSecond, srv)
		require.NotEmpty(t, second)
		require.NotEqual(t, aliceSession, second)

		status, body := consoleCall(t, alice, http.MethodDelete,
			srv.URL+"/api/v1/auth/sessions/"+second, nil, nil)
		require.Equal(t, http.StatusNoContent, status, "%v", body)

		status, _ = consoleCall(t, aliceSecond, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
		assert.Equal(t, http.StatusUnauthorized, status, "the ended session's cookie no longer authenticates")

		status, _ = consoleCall(t, alice, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
		assert.Equal(t, http.StatusOK, status, "the session that did the ending is untouched")
	})

	t.Run("signing a user out everywhere ends every session of theirs", func(t *testing.T) {
		status, body := consoleCall(t, alice, http.MethodDelete,
			srv.URL+"/api/v1/user/"+bobID+"/sessions", nil, nil)
		require.Equal(t, http.StatusOK, status, "%v", body)
		assert.InDelta(t, 1, body["ended"], 0)

		status, _ = consoleCall(t, bob, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
		assert.Equal(t, http.StatusUnauthorized, status)

		sessions, _ := sessionList(t, alice, srv)
		assert.Len(t, sessions, 1, "only the administrator's own session is left")
	})

	t.Run("the session actions are audited", func(t *testing.T) {
		status, body := consoleCall(t, alice, http.MethodGet,
			srv.URL+"/api/v1/audit?action=session.end", nil, nil)
		require.Equal(t, http.StatusOK, status)
		// The refused and the missing attempt are recorded too, with the
		// status they were answered with.
		require.Len(t, body["events"], 3)
		assert.InDelta(t, http.StatusNoContent, field(t, body, "events", "0", "outcome"), 0)
		assert.InDelta(t, http.StatusNotFound, field(t, body, "events", "1", "outcome"), 0)
		assert.InDelta(t, http.StatusForbidden, field(t, body, "events", "2", "outcome"), 0)

		status, body = consoleCall(t, alice, http.MethodGet,
			srv.URL+"/api/v1/audit?action=user.sessions.end", nil, nil)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, body["events"], 1)
		assert.Equal(t, "bob", field(t, body, "events", "0", "targetName"))
	})
}
