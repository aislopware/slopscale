package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func lockFlags(_ *cobra.Command) {}

func TestLockCommands(t *testing.T) {
	enabledTime := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	disabledTime := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	onLock := clientv1.TailnetLock{
		Enabled:                     true,
		Head:                        "headhash123456",
		Keys:                        []clientv1.TailnetLockKey{{Id: "k1", Public: "tlpub:testkey", Votes: 1}},
		SignedNodeIds:               []string{"10", "11"},
		UnsignedNodeIds:             []string{"12"},
		SupportDisablementAvailable: true,
		EnabledAt:                   &enabledTime,
	}

	offLock := clientv1.TailnetLock{
		Enabled:                     false,
		Head:                        "",
		Keys:                        []clientv1.TailnetLockKey{},
		SignedNodeIds:               []string{},
		UnsignedNodeIds:             []string{},
		SupportDisablementAvailable: false,
		DisabledAt:                  &disabledTime,
	}

	cases := []commandCase{
		{
			name: "status on renders all details",
			src:  statusLockCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/tailnet-lock": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, onLock)
				},
			},
			wantIn: []string{
				"Tailnet lock: on",
				"Head: headhash123456",
				"Trusted keys:",
				"  tlpub:testkey (votes 1)",
				"Signed nodes: 2",
				"Unsigned nodes: 12",
				"Support disablement: available",
				"Enabled at: 2026-09-01 10:00:00",
			},
			wantNotIn: []string{
				"Disabled at:",
			},
		},
		{
			name: "status off renders off state",
			src:  statusLockCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/tailnet-lock": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, offLock)
				},
			},
			wantIn: []string{
				"Tailnet lock: off",
				"Signed nodes: 0",
				"Unsigned nodes: none",
				"Support disablement: not recorded",
				"Disabled at: 2026-09-02 12:00:00",
			},
			wantNotIn: []string{
				"Head:",
				"Trusted keys:",
				"Enabled at:",
			},
		},
		{
			name:  "status as json",
			src:   statusLockCmd,
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/tailnet-lock": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, onLock)
				},
			},
			wantIn: []string{`"enabled": true`, `"head": "headhash123456"`, `"supportDisablementAvailable": true`},
		},
		{
			name: "disable switches lock off and displays status",
			src:  disableLockCmd,
			routes: map[string]apiHandler{
				"POST /api/v1/tailnet-lock/disable": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, offLock)
				},
			},
			wantIn: []string{
				"Tailnet lock: off",
				"Signed nodes: 0",
				"Unsigned nodes: none",
				"Support disablement: not recorded",
				"Disabled at: 2026-09-02 12:00:00",
			},
		},
		{
			name: "disable returns error on failure",
			src:  disableLockCmd,
			routes: map[string]apiHandler{
				"POST /api/v1/tailnet-lock/disable": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusConflict, "no support disablement secret recorded")
				},
			},
			wantErr: "no support disablement secret recorded",
		},
	}

	runCommandCases(t, lockFlags, cases)
}

func TestLockCommandRegistration(t *testing.T) {
	assert.Equal(t, "lock", lockCmd.Use)
	assert.Equal(t, "Show or switch off tailnet lock", lockCmd.Short)
	assert.Contains(t, lockCmd.Long, "tailscale lock init")

	subcommands := lockCmd.Commands()

	names := make([]string, 0, len(subcommands))
	for _, c := range subcommands {
		names = append(names, c.Name())
	}

	assert.Contains(t, names, "status")
	assert.Contains(t, names, "disable")
	assert.Equal(t, []string{"show", "get"}, statusLockCmd.Aliases)
}
