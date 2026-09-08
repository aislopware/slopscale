package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sessionFlags mirrors the flags init() registers on the sessions subcommands.
func sessionFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("user", "u", "", "")
}

// longUserAgent is longer than the Browser column, so the table shortens it.
const longUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0"

func consoleSessions() []clientv1.ConsoleSession {
	alice := clientv1.User{Id: "1", Name: "alice"}
	bob := clientv1.User{Id: "2", Name: "bob"}

	return []clientv1.ConsoleSession{
		{
			Id:         "7",
			User:       alice,
			CreatedAt:  time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
			ExpiresAt:  time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC),
			LastSeenAt: time.Date(2026, 9, 7, 15, 30, 0, 0, time.UTC),
			RemoteAddr: "100.64.0.1",
			UserAgent:  longUserAgent,
			Current:    true,
		},
		{
			Id:         "8",
			User:       bob,
			CreatedAt:  time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC),
			ExpiresAt:  time.Date(2026, 9, 9, 9, 0, 0, 0, time.UTC),
			LastSeenAt: time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC),
		},
	}
}

func TestSessionCommands(t *testing.T) {
	sessions := consoleSessions()

	listAll := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)
		writeJSON(t, w, clientv1.ListSessionsOutputBody{Sessions: sessions})
	}

	cases := []commandCase{
		{
			name:   "list renders the user, times, address, browser and the current marker",
			src:    listSessionsCmd,
			routes: map[string]apiHandler{"GET /api/v1/auth/sessions": listAll},
			wantIn: []string{
				"Last seen", "Browser", "Current",
				"alice", "2026-09-01 09:00:00", "2026-09-07 15:30:00", "100.64.0.1",
				"Mozilla/5.0 (X11; Linux x86_64) Apple...", "yes",
				"bob", "-",
			},
			wantNotIn: []string{longUserAgent},
		},
		{
			name:      "list filters by user id on the client",
			src:       listSessionsCmd,
			flags:     map[string]string{"user": "2"},
			routes:    map[string]apiHandler{"GET /api/v1/auth/sessions": listAll},
			wantIn:    []string{"bob"},
			wantNotIn: []string{"alice"},
		},
		{
			name:   "list prints json",
			src:    listSessionsCmd,
			flags:  map[string]string{"output": "json", "user": "1"},
			routes: map[string]apiHandler{"GET /api/v1/auth/sessions": listAll},
			want:   indentJSON(t, sessions[:1]),
		},
		{
			name: "list surfaces the api error",
			src:  listSessionsCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/auth/sessions": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "credential is missing the required scope")
				},
			},
			wantErr: "missing the required scope",
		},
		{
			name: "end addresses the session in the path and accepts a 204",
			src:  endSessionCmd,
			args: []string{"7"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/auth/sessions/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "7", r.PathValue("id"))
					w.WriteHeader(http.StatusNoContent)
				},
			},
			want: "Session ended\n",
		},
		{
			name:  "end prints json",
			src:   endSessionCmd,
			args:  []string{"8"},
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/auth/sessions/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					w.WriteHeader(http.StatusNoContent)
				},
			},
			want: indentJSON(t, map[string]string{colResult: "Session ended"}),
		},
		{
			name: "end surfaces the api error",
			src:  endSessionCmd,
			args: []string{"9"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/auth/sessions/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "session not found")
				},
			},
			wantErr: "session not found",
		},
	}

	runCommandCases(t, sessionFlags, cases)
}

func TestShortUserAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{name: "empty stays empty"},
		{name: "short is kept", userAgent: "curl/8.7.1", want: "curl/8.7.1"},
		{
			name:      "long is trimmed to the column width",
			userAgent: longUserAgent,
			want:      "Mozilla/5.0 (X11; Linux x86_64) Apple...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shortUserAgent(tt.userAgent)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, len(got), userAgentWidth)
		})
	}
}

func TestFilterSessionsByUser(t *testing.T) {
	t.Parallel()

	sessions := consoleSessions()

	assert.Len(t, filterSessionsByUser(sessions, ""), 2)
	assert.Empty(t, filterSessionsByUser(sessions, "3"))

	kept := filterSessionsByUser(sessions, "1")
	require.Len(t, kept, 1)
	assert.Equal(t, "7", kept[0].Id)
}
