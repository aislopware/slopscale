package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// auditFlags mirrors the flags init() registers on "audit list".
func auditFlags(cmd *cobra.Command) {
	cmd.Flags().Int64P("limit", "l", defaultAuditLimit, "")
	cmd.Flags().StringP("user", "u", "", "")
	cmd.Flags().StringP("action", "a", "", "")
	cmd.Flags().String("target-kind", "", "")
	cmd.Flags().String("target-id", "", "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().String("before", "", "")
}

func TestAuditListCommand(t *testing.T) {
	events := clientv1.ListAuditOutputBody{
		Events: []clientv1.AuditEvent{
			{
				Id:         "7",
				CreatedAt:  time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC),
				ActorKind:  "session",
				ActorName:  "alice",
				Action:     "node.delete",
				TargetKind: "node",
				TargetName: "laptop",
				Outcome:    http.StatusNoContent,
				Detail:     map[string]any{},
			},
			{
				Id:        "6",
				CreatedAt: time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC),
				ActorKind: "system",
				Action:    "user.role.set",
				Outcome:   http.StatusOK,
				Detail:    map[string]any{"role": "admin"},
			},
		},
		NextBefore: "6",
	}

	cases := []commandCase{
		{
			name:  "renders actor, action, target, result and detail",
			src:   listAuditCmd,
			flags: map[string]string{"action": "node.", "limit": "2", "since": "24h", "user": "3"},
			routes: map[string]apiHandler{
				"GET /api/v1/audit": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					q := r.URL.Query()
					assert.Equal(t, "node.", q.Get("action"))
					assert.Equal(t, "2", q.Get("limit"))
					assert.Equal(t, "3", q.Get("actorUserId"))
					assert.NotEmpty(t, q.Get("since"), "a duration --since becomes an absolute time")
					assert.Empty(t, q.Get("before"))

					writeJSON(t, w, events)
				},
			},
			wantIn: []string{"alice (session)", "node.delete", "node laptop", "204", "system", `{"role":"admin"}`},
		},
		{
			name:  "json output carries the page cursor",
			src:   listAuditCmd,
			flags: map[string]string{"output": "json", "before": "8"},
			routes: map[string]apiHandler{
				"GET /api/v1/audit": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "8", r.URL.Query().Get("before"))
					writeJSON(t, w, events)
				},
			},
			wantIn: []string{`"nextBefore": "6"`, `"action": "node.delete"`},
		},
		{
			name:    "rejects a --since that is neither a time nor a duration",
			src:     listAuditCmd,
			flags:   map[string]string{"since": "yesterday"},
			wantErr: "neither an RFC 3339 time nor a duration",
		},
		{
			name: "surfaces the api error",
			src:  listAuditCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/audit": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "credential is missing the required scope")
				},
			},
			wantErr: "missing the required scope",
		},
	}

	runCommandCases(t, auditFlags, cases)
}

// auditExportFlags mirrors the flags init() registers on "audit export".
// --output is left out: newTestCommand already registers it, the same way the
// root command's persistent flag is shadowed in production.
func auditExportFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("user", "u", "", "")
	cmd.Flags().StringP("action", "a", "", "")
	cmd.Flags().String("target-kind", "", "")
	cmd.Flags().String("target-id", "", "")
	cmd.Flags().String("since", "", "")
	cmd.Flags().String("before", "", "")
	cmd.Flags().String("until", "", "")
	cmd.Flags().String("format", "csv", "")
}

const auditCSV = "id,time,action\n7,2026-09-07T10:00:00Z,node.delete\n"

func TestAuditExportCommand(t *testing.T) {
	cases := []commandCase{
		{
			name: "writes the csv to stdout and passes every filter",
			src:  exportAuditCmd,
			flags: map[string]string{
				"action": "node.", "user": "3", "target-kind": "node", "target-id": "9",
				"since": "24h", "until": "2026-10-01T00:00:00Z", "before": "8",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/audit/export": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)

					q := r.URL.Query()
					assert.Equal(t, "csv", q.Get("format"))
					assert.Equal(t, "node.", q.Get("action"))
					assert.Equal(t, "3", q.Get("actorUserId"))
					assert.Equal(t, "node", q.Get("targetKind"))
					assert.Equal(t, "9", q.Get("targetId"))
					assert.Equal(t, "8", q.Get("before"))
					assert.NotEmpty(t, q.Get("since"), "a duration --since becomes an absolute time")
					assert.Equal(t, "2026-10-01T00:00:00Z", q.Get("until"))

					w.Header().Set("Content-Type", "text/csv; charset=utf-8")

					_, err := w.Write([]byte(auditCSV))
					assert.NoError(t, err)
				},
			},
			want: auditCSV,
		},
		{
			name:  "a dash output also writes to stdout",
			src:   exportAuditCmd,
			flags: map[string]string{"output": "-"},
			routes: map[string]apiHandler{
				"GET /api/v1/audit/export": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					w.Header().Set("Content-Type", "text/csv; charset=utf-8")

					_, err := w.Write([]byte(auditCSV))
					assert.NoError(t, err)
				},
			},
			want: auditCSV,
		},
		{
			name:    "rejects an unknown format before calling the api",
			src:     exportAuditCmd,
			flags:   map[string]string{"format": "pdf"},
			wantErr: `--format must be csv or json, not "pdf"`,
		},
		{
			name:    "rejects an --until that is neither a time nor a duration",
			src:     exportAuditCmd,
			flags:   map[string]string{"until": "tomorrow"},
			wantErr: `--until "tomorrow" is neither an RFC 3339 time nor a duration`,
		},
		{
			name: "surfaces the api error",
			src:  exportAuditCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/audit/export": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "credential is missing the required scope")
				},
			},
			wantErr: "missing the required scope",
		},
	}

	runCommandCases(t, auditExportFlags, cases)
}

// TestAuditExportWritesFile covers --output and the JSON format, which the
// generated typed client cannot parse: the export is a file download, so the
// command reads the raw body.
func TestAuditExportWritesFile(t *testing.T) {
	const body = `[{"id":"7","action":"node.delete"}]`

	path := filepath.Join(t.TempDir(), "audit.json")

	serveAPI(t, map[string]apiHandler{
		"GET /api/v1/audit/export": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			t.Helper()
			assert.Equal(t, "json", r.URL.Query().Get("format"))
			w.Header().Set("Content-Type", "application/json")

			_, err := w.Write([]byte(body))
			assert.NoError(t, err)
		},
	})

	cmd := newTestCommand(t, exportAuditCmd, auditExportFlags, map[string]string{
		"output": path,
		"format": "json",
	})

	out, err := runCommand(t, cmd)
	require.NoError(t, err)
	assert.Equal(t, "Audit log written to "+path+"\n", out)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, body, string(written))
}
