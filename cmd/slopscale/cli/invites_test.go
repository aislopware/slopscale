package cli

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// inviteFlags mirrors the flags init() registers on "invites create", where
// --expiry carries the default window.
func inviteFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("email", "e", "", "")
	cmd.Flags().StringP("role", "r", "", "")
	cmd.Flags().StringSliceP("group", "g", []string{}, "")
	cmd.Flags().String("expiry", defaultInviteExpiry, "")
}

// inviteResendFlags mirrors "invites resend", where --expiry is unset by
// default so the body stays empty.
func inviteResendFlags(cmd *cobra.Command) {
	cmd.Flags().String("expiry", "", "")
}

const inviteURL = "https://slopscale.example.com/admin/login?invite=deadbeef"

func inviteFixture() clientv1.Invite {
	createdBy := "1"

	return clientv1.Invite{
		Id:        "3",
		Email:     "ada@example.com",
		Role:      "admin",
		GroupIds:  []string{"5", "6"},
		CreatedAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy: &createdBy,
	}
}

// writeCreated serves v as a 201, the status a created invitation answers with.
func writeCreated(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err := json.NewEncoder(w).Encode(v)
	assert.NoError(t, err)
}

func TestInviteCommands(t *testing.T) {
	invite := inviteFixture()
	created := clientv1.InviteOutputBody{Invite: invite, Url: inviteURL, EmailSent: true}

	accepted := clientv1.Invite{
		Id: "4", Email: "bob@example.com", Role: "member", GroupIds: []string{},
		CreatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC),
		Accepted:  true,
	}
	expired := clientv1.Invite{
		Id: "5", Email: "eve@example.com", Role: "member", GroupIds: []string{},
		CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 7, 8, 9, 0, 0, 0, time.UTC),
		Expired:   true,
	}
	list := []clientv1.Invite{invite, accepted, expired}

	listAll := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)
		writeJSON(t, w, clientv1.ListInvitesOutputBody{Invites: list})
	}

	cases := []commandCase{
		{
			name:  "create sends the email, role, groups and expiry",
			src:   createInviteCmd,
			flags: map[string]string{"email": "ada@example.com", "role": "admin", "group": "5,6", "expiry": "72h"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)

					var body clientv1.CreateInviteRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "ada@example.com", body.Email)
					assert.Equal(t, "admin", ptrStr(body.Role))
					assert.Equal(t, "72h", ptrStr(body.Expiry))

					if assert.NotNil(t, body.GroupIds) {
						assert.Equal(t, []string{"5", "6"}, *body.GroupIds)
					}

					writeCreated(t, w, created)
				},
			},
			want: "Invitation for ada@example.com expires 2100-01-01 00:00:00\n" +
				inviteURL + "\nMailed to ada@example.com\n",
		},
		{
			name:  "create defaults the expiry and omits the role and groups",
			src:   createInviteCmd,
			flags: map[string]string{"email": "ada@example.com"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreateInviteRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, defaultInviteExpiry, ptrStr(body.Expiry))
					assert.Nil(t, body.Role)
					assert.Nil(t, body.GroupIds)

					writeCreated(t, w, created)
				},
			},
			wantIn: []string{inviteURL},
		},
		{
			name:  "create reports a mail that could not be sent",
			src:   createInviteCmd,
			flags: map[string]string{"email": "ada@example.com"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()

					failed := "dial tcp 127.0.0.1:25: connection refused"
					out := clientv1.InviteOutputBody{Invite: invite, Url: inviteURL, EmailError: &failed}

					writeCreated(t, w, out)
				},
			},
			wantIn: []string{inviteURL, "Not mailed: dial tcp 127.0.0.1:25: connection refused"},
		},
		{
			name:  "create says so when there is no mail configured",
			src:   createInviteCmd,
			flags: map[string]string{"email": "ada@example.com"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeCreated(t, w, clientv1.InviteOutputBody{Invite: invite, Url: inviteURL})
				},
			},
			wantIn: []string{inviteURL, "Not mailed; send the link yourself"},
		},
		{
			name:  "create prints json",
			src:   createInviteCmd,
			flags: map[string]string{"email": "ada@example.com", "output": "json"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeCreated(t, w, created)
				},
			},
			want: indentJSON(t, created),
		},
		{
			name:  "create surfaces the api error",
			src:   createInviteCmd,
			flags: map[string]string{"email": "ada@example.com"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusConflict, "that address is already invited")
				},
			},
			wantErr: "that address is already invited",
		},
		{
			name:   "list renders the groups, expiry, status and creator",
			src:    listInvitesCmd,
			routes: map[string]apiHandler{"GET /api/v1/invite": listAll},
			wantIn: []string{
				"Email", "Groups", "Status", "Created by",
				"ada@example.com", "admin", "5, 6", "pending", "1",
				"bob@example.com", "accepted",
				"eve@example.com", "expired",
				"-",
			},
		},
		{
			name:   "list prints json",
			src:    listInvitesCmd,
			flags:  map[string]string{"output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/invite": listAll},
			want:   indentJSON(t, list),
		},
		{
			name: "list surfaces the api error",
			src:  listInvitesCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/invite": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "credential is missing the required scope")
				},
			},
			wantErr: "missing the required scope",
		},
		{
			name:   "delete addresses the invitation in the path",
			src:    deleteInviteCmd,
			args:   []string{"3"},
			routes: map[string]apiHandler{"DELETE /api/v1/invite/{id}": deleteOK("3")},
			want:   "Invitation deleted\n",
		},
		{
			name: "delete surfaces the api error",
			src:  deleteInviteCmd,
			args: []string{"9"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/invite/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "invitation not found")
				},
			},
			wantErr: "invitation not found",
		},
	}

	runCommandCases(t, inviteFlags, cases)
}

func TestInviteResendCommand(t *testing.T) {
	invite := inviteFixture()
	resent := clientv1.InviteOutputBody{Invite: invite, Url: inviteURL, EmailSent: true}

	cases := []commandCase{
		{
			name: "resend sends an empty body when no expiry is given",
			src:  resendInviteCmd,
			args: []string{"3"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite/{id}/resend": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)
					assert.Equal(t, "3", r.PathValue("id"))

					var body map[string]any

					decodeBody(t, r, &body)
					assert.Empty(t, body)

					writeJSON(t, w, resent)
				},
			},
			want: "Invitation for ada@example.com expires 2100-01-01 00:00:00\n" +
				inviteURL + "\nMailed to ada@example.com\n",
		},
		{
			name:  "resend passes a new expiry",
			src:   resendInviteCmd,
			args:  []string{"3"},
			flags: map[string]string{"expiry": "24h"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite/{id}/resend": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.ResendInviteRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "24h", ptrStr(body.Expiry))

					writeJSON(t, w, resent)
				},
			},
			wantIn: []string{inviteURL},
		},
		{
			name: "resend surfaces the api error",
			src:  resendInviteCmd,
			args: []string{"4"},
			routes: map[string]apiHandler{
				"POST /api/v1/invite/{id}/resend": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusConflict, "an accepted invitation cannot be resent")
				},
			},
			wantErr: "an accepted invitation cannot be resent",
		},
	}

	runCommandCases(t, inviteResendFlags, cases)
}

func TestInviteStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "pending", inviteStatus(clientv1.Invite{}))
	assert.Equal(t, "expired", inviteStatus(clientv1.Invite{Expired: true}))
	assert.Equal(t, "accepted", inviteStatus(clientv1.Invite{Accepted: true}))
	assert.Equal(t, "accepted", inviteStatus(clientv1.Invite{Accepted: true, Expired: true}),
		"an invitation that was accepted stays accepted once its window passes")
}
