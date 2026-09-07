package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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
