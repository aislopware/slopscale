package cli

import (
	"net/http"
	"testing"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// settingsFlags mirrors the flags init() registers on the settings
// subcommands.
func settingsFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("devices-approval", false, "")
	cmd.Flags().Bool("users-approval", false, "")
	cmd.Flags().Int64("key-expiry-days", 0, "")
}

func TestSettingsCommands(t *testing.T) {
	current := clientv1.Settings{DevicesApprovalOn: true}

	cases := []commandCase{
		{
			name: "get renders both switches",
			src:  getSettingsCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/settings": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
			},
			wantIn: []string{"Device approval", "on", "Users approval", "off"},
		},
		{
			name:  "get as json",
			src:   getSettingsCmd,
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/settings": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, current)
				},
			},
			wantIn: []string{`"devicesApprovalOn": true`, `"usersApprovalOn": false`},
		},
		{
			name:  "set sends only the given switches",
			src:   setSettingsCmd,
			flags: map[string]string{"users-approval": "true"},
			routes: map[string]apiHandler{
				"POST /api/v1/settings": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.UpdateSettingsRequestBody

					decodeBody(t, r, &body)
					assert.Nil(t, body.DevicesApprovalOn)

					if assert.NotNil(t, body.UsersApprovalOn) {
						assert.True(t, *body.UsersApprovalOn)
					}

					writeJSON(t, w, clientv1.Settings{DevicesApprovalOn: true, UsersApprovalOn: true})
				},
			},
			wantIn: []string{"Users approval", "on"},
		},
		{
			name:  "set with an explicit off sends false",
			src:   setSettingsCmd,
			flags: map[string]string{"devices-approval": "false"},
			routes: map[string]apiHandler{
				"POST /api/v1/settings": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.UpdateSettingsRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.DevicesApprovalOn) {
						assert.False(t, *body.DevicesApprovalOn)
					}

					writeJSON(t, w, clientv1.Settings{})
				},
			},
			wantIn: []string{"Device approval", "off"},
		},
		{
			name:  "set sends the key expiry cap",
			src:   setSettingsCmd,
			flags: map[string]string{"key-expiry-days": "30"},
			routes: map[string]apiHandler{
				"POST /api/v1/settings": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.UpdateSettingsRequestBody

					decodeBody(t, r, &body)
					assert.Nil(t, body.DevicesApprovalOn)

					if assert.NotNil(t, body.KeyExpiryDays) {
						assert.Equal(t, int64(30), *body.KeyExpiryDays)
					}

					writeJSON(t, w, clientv1.Settings{KeyExpiryDays: 30, DefaultKeyExpiryDays: 180})
				},
			},
			wantIn: []string{"Key expiry", "30 days"},
		},
		{
			name: "get names the config file default when there is no cap",
			src:  getSettingsCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/settings": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.Settings{DefaultKeyExpiryDays: 180})
				},
			},
			wantIn: []string{"Key expiry", "180 days by default"},
		},
		{
			name:    "set without a switch is an error",
			src:     setSettingsCmd,
			wantErr: "at least one of",
		},
		{
			name:  "set surfaces the api error",
			src:   setSettingsCmd,
			flags: map[string]string{"devices-approval": "true"},
			routes: map[string]apiHandler{
				"POST /api/v1/settings": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "credential is missing the required scope")
				},
			},
			wantErr: "missing the required scope",
		},
	}

	runCommandCases(t, settingsFlags, cases)
}
