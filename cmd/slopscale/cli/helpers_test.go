package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The command tests in this package drive the real RunE closures against a
// fake API. The CLI resolves its endpoint through viper (cli.address,
// cli.api_key) and prints through os.Stdout and pterm's default writer, all
// process-wide, so those tests run serially and never call t.Parallel. Tests
// of pure functions do.

const testAPIKey = "test-api-key"

// apiHandler is an http.HandlerFunc that can also assert on the request.
type apiHandler func(t *testing.T, w http.ResponseWriter, r *http.Request)

// serveAPI starts a fake slopscale API from Go 1.22 mux patterns such as
// "POST /api/v1/node/{id}/expire", points the CLI at it over HTTP, and closes
// it after the test. A request that matches no pattern gets a 404 or 405, so
// an unexpected call surfaces as a command error rather than passing quietly.
func serveAPI(t *testing.T, routes map[string]apiHandler) {
	t.Helper()

	mux := http.NewServeMux()
	for pattern, handler := range routes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			handler(t, w, r)
		})
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	pointCLIAt(t, server.URL)
}

// pointCLIAt sets the viper keys newSlopscaleCLIWithConfig and newV2Client
// read, so clientRunE and withClient build a remote client for baseURL. The
// keys already exist for operators, which keeps production free of test hooks.
func pointCLIAt(t *testing.T, baseURL string) {
	t.Helper()

	viper.Set("cli.address", baseURL)
	viper.Set("cli.api_key", testAPIKey)
	viper.Set("cli.timeout", "5s")
	viper.Set("cli.insecure", false)
	t.Cleanup(viper.Reset)
}

// serveAPIOnSocket is serveAPI over a unix socket, exercising the local-trust
// path the CLI takes when cli.address is unset.
func serveAPIOnSocket(t *testing.T, routes map[string]apiHandler) {
	t.Helper()

	mux := http.NewServeMux()
	for pattern, handler := range routes {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			handler(t, w, r)
		})
	}

	socketPath := shortSocketPath(t)

	var lc net.ListenConfig

	listener, err := lc.Listen(t.Context(), "unix", socketPath)
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(mux)
	require.NoError(t, server.Listener.Close())

	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	viper.Set("cli.address", "")
	viper.Set("cli.timeout", "5s")
	viper.Set("unix_socket", socketPath)
	t.Cleanup(viper.Reset)
}

// newTestCommand wraps src's RunE in a fresh cobra command so tests never
// touch the package-level flag state built in init(). define registers the
// flags the command reads, mirroring init(); output and force are persistent
// flags on the root command in production and are added here.
func newTestCommand(
	t *testing.T,
	src *cobra.Command,
	define func(*cobra.Command),
	flags map[string]string,
) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: src.Use, RunE: src.RunE}
	cmd.Flags().StringP("output", "o", "", "")
	cmd.Flags().Bool("force", false, "")

	if define != nil {
		define(cmd)
	}

	for name, value := range flags {
		require.NoError(t, cmd.Flags().Set(name, value), "setting flag --%s", name)
	}

	return cmd
}

// commandCase drives one command invocation against a fake API.
type commandCase struct {
	name      string
	src       *cobra.Command
	flags     map[string]string
	args      []string
	routes    map[string]apiHandler
	prompt    string   // answer fed to a confirmation prompt, when set
	want      string   // exact stdout; also asserted when no other stdout expectation is set
	wantIn    []string // substrings stdout must contain
	wantNotIn []string // substrings stdout must not contain
	wantErr   string   // substring the returned error must contain
}

// runCommandCases runs each case against its own fake API. define registers
// the flags the command family reads.
func runCommandCases(t *testing.T, define func(*cobra.Command), cases []commandCase) {
	t.Helper()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			serveAPI(t, tt.routes)

			if tt.prompt != "" {
				answerPrompt(t, tt.prompt)
			}

			cmd := newTestCommand(t, tt.src, define, tt.flags)

			out, err := runCommand(t, cmd, tt.args...)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)

			if tt.want != "" || (len(tt.wantIn) == 0 && len(tt.wantNotIn) == 0) {
				assert.Equal(t, tt.want, out)
			}

			for _, s := range tt.wantIn {
				assert.Contains(t, out, s)
			}

			for _, s := range tt.wantNotIn {
				assert.NotContains(t, out, s)
			}
		})
	}
}

// deleteOK answers a DELETE whose {id} path value must equal id with an
// empty JSON object, the v1 API's delete response.
func deleteOK(id string) apiHandler {
	return func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assert.Equal(t, id, r.PathValue("id"))
		writeJSON(t, w, map[string]any{})
	}
}

// runCommand runs cmd's RunE and returns what it wrote to stdout.
func runCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()

	return captureStdout(t, func() error {
		return cmd.RunE(cmd, args)
	})
}

// captureStdout runs fn and returns everything it wrote to stdout. pterm
// tables go through pterm's own default writer rather than os.Stdout, so both
// are redirected.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	origStdout := os.Stdout
	os.Stdout = writer

	pterm.SetDefaultOutput(writer)

	defer func() {
		os.Stdout = origStdout

		pterm.SetDefaultOutput(origStdout)
	}()

	var buf bytes.Buffer

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = io.Copy(&buf, reader)
	}()

	runErr := fn()

	require.NoError(t, writer.Close())
	<-done

	return buf.String(), runErr
}

// answerPrompt feeds answer to the next util.YesNo prompt through os.Stdin.
func answerPrompt(t *testing.T, answer string) {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	_, err = writer.WriteString(answer + "\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	origStdin := os.Stdin
	os.Stdin = reader

	t.Cleanup(func() {
		os.Stdin = origStdin

		reader.Close()
	})
}

// writeJSON serves v as a 200 application/json body.
func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	err := json.NewEncoder(w).Encode(v)
	assert.NoError(t, err)
}

// writeProblem serves an RFC 7807 problem with the given status and detail,
// the shape the v1 API uses for every error.
func writeProblem(t *testing.T, w http.ResponseWriter, status int, detail string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(map[string]any{
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
	assert.NoError(t, err)
}

// decodeBody reads the JSON request body into v.
func decodeBody(t *testing.T, r *http.Request, v any) {
	t.Helper()

	err := json.NewDecoder(r.Body).Decode(v)
	assert.NoError(t, err)
}

// assertBearer checks the request carries the API key the CLI was given.
func assertBearer(t *testing.T, r *http.Request) {
	t.Helper()

	assert.Equal(t, "Bearer "+testAPIKey, r.Header.Get("Authorization"))
}

// indentJSON is what printOutput emits for v under --output json.
func indentJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.MarshalIndent(v, "", "\t")
	require.NoError(t, err)

	return string(b) + "\n"
}

// shortSocketPath returns a unix socket path in a fresh temp directory that
// stays under the 104-byte macOS limit. t.TempDir embeds the full test name,
// which overflows it for long subtest names.
func shortSocketPath(t *testing.T) string {
	t.Helper()

	//nolint:usetesting // t.TempDir embeds the test name and overflows the socket path limit
	dir, err := os.MkdirTemp("", "hs")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	return filepath.Join(dir, "hs.sock")
}
