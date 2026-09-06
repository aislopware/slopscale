package cli

import (
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	validPolicy = `{
	// Everyone may talk to everyone.
	"acls": [
		{"action": "accept", "src": ["*"], "dst": ["*:*"]},
	],
}
`
	brokenPolicy = `{"acls": [`

	// strictPolicy carries no comments or trailing commas. The bypass path
	// runs the file through hujson.Standardize, which blanks those in place
	// before the policy is stored, so only strict JSON round-trips verbatim.
	strictPolicy = `{"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]}
`
)

// policyFlags mirrors the flags init() registers on the policy subcommands.
func policyFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("file", "f", "", "")
	cmd.Flags().Bool(bypassFlag, false, "")
}

// writePolicyFile writes content to a file in a fresh temp dir and returns
// its path.
func writePolicyFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "policy.hujson")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestPolicyCommands(t *testing.T) {
	policyFile := writePolicyFile(t, validPolicy)
	updatedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	cases := []commandCase{
		{
			name: "get prints the policy verbatim",
			src:  getPolicy,
			routes: map[string]apiHandler{
				"GET /api/v1/policy": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)
					writeJSON(t, w, clientv1.PolicyResponseBody{Policy: validPolicy, UpdatedAt: updatedAt})
				},
			},
			want: validPolicy + "\n",
		},
		{
			name: "get surfaces the api error",
			src:  getPolicy,
			routes: map[string]apiHandler{
				"GET /api/v1/policy": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "acl policy not found")
				},
			},
			wantErr: "acl policy not found",
		},
		{
			name:  "set uploads the file contents",
			src:   setPolicy,
			flags: map[string]string{"file": policyFile},
			routes: map[string]apiHandler{
				"PUT /api/v1/policy": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.PolicyRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Policy) {
						assert.Equal(t, validPolicy, *body.Policy)
					}

					writeJSON(t, w, clientv1.PolicyResponseBody{Policy: validPolicy, UpdatedAt: updatedAt})
				},
			},
			want: "Policy updated.\n",
		},
		{
			name:    "set fails when the file is missing",
			src:     setPolicy,
			flags:   map[string]string{"file": filepath.Join(t.TempDir(), "missing.hujson")},
			wantErr: "reading policy file",
		},
		{
			name:  "set surfaces the api error",
			src:   setPolicy,
			flags: map[string]string{"file": policyFile},
			routes: map[string]apiHandler{
				"PUT /api/v1/policy": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "policy mode is file")
				},
			},
			wantErr: "policy mode is file",
		},
		{
			name:  "check posts the file for validation",
			src:   checkPolicy,
			flags: map[string]string{"file": policyFile},
			routes: map[string]apiHandler{
				"POST /api/v1/policy/check": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.PolicyRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Policy) {
						assert.Equal(t, validPolicy, *body.Policy)
					}

					writeJSON(t, w, map[string]any{})
				},
			},
			want: "Policy is valid\n",
		},
		{
			name:    "check fails when the file is missing",
			src:     checkPolicy,
			flags:   map[string]string{"file": filepath.Join(t.TempDir(), "missing.hujson")},
			wantErr: "reading policy file",
		},
		{
			name:  "check surfaces the validation error",
			src:   checkPolicy,
			flags: map[string]string{"file": policyFile},
			routes: map[string]apiHandler{
				"POST /api/v1/policy/check": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "user \"nobody\" not found")
				},
			},
			wantErr: "user \"nobody\" not found",
		},
	}

	runCommandCases(t, policyFlags, cases)
}

// loadBypassConfig points viper at a server config whose database is a fresh
// SQLite file, so the --bypass-server-and-access-database-directly path can
// open a real database. It returns the directory holding that database.
func loadBypassConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	config := `
server_url: http://127.0.0.1:8080
listen_addr: 127.0.0.1:0
noise:
  private_key_path: ` + filepath.Join(dir, "noise_private.key") + `
prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48
  allocation: sequential
database:
  type: sqlite
  sqlite:
    path: ` + filepath.Join(dir, "headscale.sqlite") + `
policy:
  mode: db
dns:
  magic_dns: false
  override_local_dns: false
`

	configPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

	viper.Reset()
	require.NoError(t, types.LoadConfig(configPath, true))
	t.Cleanup(viper.Reset)

	return dir
}

// TestPolicyBypassCommands runs the bypass path against a real SQLite
// database. The steps share that database, so they run in order.
func TestPolicyBypassCommands(t *testing.T) {
	loadBypassConfig(t)

	validFile := writePolicyFile(t, strictPolicy)
	brokenFile := writePolicyFile(t, brokenPolicy)

	bypass := map[string]string{bypassFlag: "true", "force": "true"}

	withFile := func(path string) map[string]string {
		flags := map[string]string{"file": path}
		maps.Copy(flags, bypass)

		return flags
	}

	t.Run("get on an empty database reports no policy", func(t *testing.T) {
		cmd := newTestCommand(t, getPolicy, policyFlags, bypass)

		_, err := runCommand(t, cmd)
		require.ErrorIs(t, err, types.ErrPolicyNotFound)
	})

	t.Run("set rejects a policy that does not parse", func(t *testing.T) {
		cmd := newTestCommand(t, setPolicy, policyFlags, withFile(brokenFile))

		_, err := runCommand(t, cmd)
		require.ErrorContains(t, err, "parsing policy file")
	})

	t.Run("set stores a valid policy", func(t *testing.T) {
		cmd := newTestCommand(t, setPolicy, policyFlags, withFile(validFile))

		out, err := runCommand(t, cmd)
		require.NoError(t, err)
		assert.Equal(t, "Policy updated.\n", out)
	})

	t.Run("get prints the stored policy", func(t *testing.T) {
		cmd := newTestCommand(t, getPolicy, policyFlags, bypass)

		out, err := runCommand(t, cmd)
		require.NoError(t, err)
		// Byte-exact on purpose: JSONEq would hide whitespace the bypass
		// path rewrites before storing.
		assert.Equal(t, strictPolicy+"\n", out) //nolint:testifylint
	})

	t.Run("check accepts a valid policy", func(t *testing.T) {
		cmd := newTestCommand(t, checkPolicy, policyFlags, withFile(validFile))

		out, err := runCommand(t, cmd)
		require.NoError(t, err)
		assert.Equal(t, "Policy is valid\n", out)
	})

	t.Run("check rejects a policy that does not parse", func(t *testing.T) {
		cmd := newTestCommand(t, checkPolicy, policyFlags, withFile(brokenFile))

		_, err := runCommand(t, cmd)
		require.ErrorContains(t, err, "parsing policy file")
	})

	t.Run("declining the prompt aborts before opening the database", func(t *testing.T) {
		answerPrompt(t, "n")

		cmd := newTestCommand(t, getPolicy, policyFlags, map[string]string{bypassFlag: "true"})

		_, err := runCommand(t, cmd)
		require.ErrorIs(t, err, errAborted)
	})
}

func TestPolicyBypassWithoutServerConfig(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := newTestCommand(t, getPolicy, policyFlags, map[string]string{bypassFlag: "true", "force": "true"})

	_, err := runCommand(t, cmd)
	require.ErrorContains(t, err, "loading config")
}
