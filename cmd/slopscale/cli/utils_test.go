package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errRenderFailed = errors.New("render failed")
	errBoom         = errors.New("boom")
)

func TestClientBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{
			name:    "bare host defaults to https",
			address: "slopscale.example.com:50443",
			want:    "https://slopscale.example.com:50443",
		},
		{
			name:    "explicit https scheme is kept",
			address: "https://slopscale.example.com",
			want:    "https://slopscale.example.com",
		},
		{
			name:    "explicit http scheme is kept",
			address: "http://localhost:8080",
			want:    "http://localhost:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientBaseURL(tt.address); got != tt.want {
				t.Errorf("clientBaseURL(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}

func TestFormatOutput(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	value := payload{Name: "alice", Count: 2}

	tests := []struct {
		name    string
		input   any
		format  string
		want    string
		wantErr bool
	}{
		{
			name:   "json is tab indented",
			input:  value,
			format: "json",
			want:   "{\n\t\"name\": \"alice\",\n\t\"count\": 2\n}",
		},
		{
			name:   "json-line is compact",
			input:  value,
			format: "json-line",
			want:   `{"name":"alice","count":2}`,
		},
		{
			name:   "yaml",
			input:  value,
			format: "yaml",
			want:   "name: alice\ncount: 2\n",
		},
		{
			name:   "empty format returns the human override",
			input:  value,
			format: "",
			want:   "human text",
		},
		{
			name:   "unknown format falls back to the human override",
			input:  value,
			format: "xml",
			want:   "human text",
		},
		{
			name:    "json rejects values it cannot marshal",
			input:   make(chan int),
			format:  "json",
			wantErr: true,
		},
		{
			name:    "json-line rejects values it cannot marshal",
			input:   make(chan int),
			format:  "json-line",
			wantErr: true,
		},
		// yaml.v3 panics rather than returning an error for an unmarshalable
		// value, so formatOutput's yaml error branch has no reachable test.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := formatOutput(tt.input, "human text", tt.format)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAPIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		problem *clientv1.ErrorModel
		want    string
	}{
		{
			name:   "nil problem falls back to the status text",
			status: http.StatusNotFound,
			want:   "unexpected response status: 404 Not Found",
		},
		{
			name:    "detail alone",
			status:  http.StatusBadRequest,
			problem: &clientv1.ErrorModel{Detail: new("renaming user")},
			want:    "unexpected response status: renaming user",
		},
		{
			name:   "detail and error messages are joined",
			status: http.StatusUnprocessableEntity,
			problem: &clientv1.ErrorModel{
				Detail: new("renaming user"),
				Errors: &[]clientv1.ErrorDetail{
					{Message: new("name is too long")},
					{Message: new("")},
					{Message: nil},
					{Message: new("name contains spaces")},
				},
			},
			want: "unexpected response status: renaming user: name is too long: name contains spaces",
		},
		{
			name:    "title is used when detail and errors are empty",
			status:  http.StatusForbidden,
			problem: &clientv1.ErrorModel{Title: new("Forbidden"), Detail: new("")},
			want:    "unexpected response status: Forbidden",
		},
		{
			name:    "empty problem falls back to the status text",
			status:  http.StatusInternalServerError,
			problem: &clientv1.ErrorModel{Title: new(""), Errors: &[]clientv1.ErrorDetail{}},
			want:    "unexpected response status: 500 Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := apiError(tt.status, tt.problem)
			require.ErrorIs(t, err, errResponseStatus)
			assert.EqualError(t, err, tt.want)
		})
	}
}

func TestExpirationFromFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flag     string
		wantFrom time.Duration
		wantErr  string
	}{
		{name: "prometheus days", flag: "90d", wantFrom: 90 * 24 * time.Hour},
		{name: "go hours", flag: "1h", wantFrom: time.Hour},
		{name: "not a duration", flag: "soon", wantErr: "parsing duration"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			cmd.Flags().String("expiration", tt.flag, "")

			got, err := expirationFromFlag(cmd)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.WithinDuration(t, time.Now().Add(tt.wantFrom), got, time.Minute)
			assert.Equal(t, time.UTC, got.Location())
		})
	}
}

func TestConfirmActionForce(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("force", true, "")

	assert.True(t, confirmAction(cmd, "really?"))
}

func TestMustMarkRequired(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("known", "", "")

	require.NotPanics(t, func() { mustMarkRequired(cmd, "known") })
	require.PanicsWithValue(t,
		`marking flag "missing" required on "x": no such flag -missing`,
		func() { mustMarkRequired(cmd, "missing") })
}

// The tests below touch process-wide state (stdout, stderr, os.Args, the conf store,
// pterm) and therefore run serially.

func TestPrintOutput(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	value := payload{Name: "alice"}

	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "default prints the human override", format: "", want: "human text\n"},
		{name: "json prints the value", format: "json", want: "{\n\t\"name\": \"alice\"\n}\n"},
		{name: "json-line prints the value", format: "json-line", want: "{\"name\":\"alice\"}\n"},
		{name: "yaml prints the value", format: "yaml", want: "name: alice\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("output", tt.format, "")

			out, err := captureStdout(t, func() error {
				return printOutput(cmd, value, "human text")
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
		})
	}

	t.Run("marshal errors are returned", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().String("output", "json", "")

		_, err := captureStdout(t, func() error {
			return printOutput(cmd, make(chan int), "")
		})
		require.Error(t, err)
	})
}

func TestPrintListOutput(t *testing.T) {
	data := []string{"a", "b"}

	t.Run("machine format serialises data and skips the table", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().String("output", "json-line", "")

		rendered := false

		out, err := captureStdout(t, func() error {
			return printListOutput(cmd, data, func() error {
				rendered = true

				return nil
			})
		})
		require.NoError(t, err)
		assert.False(t, rendered)
		assert.Equal(t, "[\"a\",\"b\"]\n", out)
	})

	t.Run("human format renders the table", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().String("output", "", "")

		out, err := captureStdout(t, func() error {
			return printListOutput(cmd, data, func() error {
				return renderTable([]string{"Letter"}, [][]string{{"a"}, {"b"}})
			})
		})
		require.NoError(t, err)
		assert.Contains(t, out, "Letter")
		assert.Contains(t, out, "a")
		assert.Contains(t, out, "b")
	})

	t.Run("table errors propagate", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().String("output", "", "")

		_, err := captureStdout(t, func() error {
			return printListOutput(cmd, data, func() error { return errRenderFailed })
		})
		require.ErrorIs(t, err, errRenderFailed)
	})
}

func TestPrintError(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "human", format: "", want: "Error: boom\n"},
		{name: "json", format: "json", want: "{\n\t\"error\": \"boom\"\n}\n"},
		{name: "json-line", format: "json-line", want: "{\"error\":\"boom\"}\n"},
		{name: "yaml", format: "yaml", want: "error: boom\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStderr(t, func() {
				printError(errBoom, tt.format)
			})
			assert.Equal(t, tt.want, out)
		})
	}
}

// captureStderr runs fn and returns what it wrote to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	origStderr := os.Stderr
	os.Stderr = writer

	defer func() { os.Stderr = origStderr }()

	var buf bytes.Buffer

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = io.Copy(&buf, reader)
	}()

	fn()

	require.NoError(t, writer.Close())
	<-done

	return buf.String()
}

func TestHasMachineOutputFlag(t *testing.T) {
	origArgs := os.Args

	t.Cleanup(func() { os.Args = origArgs })

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no output flag", args: []string{"slopscale", "nodes", "list"}, want: false},
		{name: "json", args: []string{"slopscale", "nodes", "list", "-o", "json"}, want: true},
		{name: "json-line", args: []string{"slopscale", "--output", "json-line", "nodes", "list"}, want: true},
		{name: "yaml", args: []string{"slopscale", "nodes", "list", "yaml"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args

			assert.Equal(t, tt.want, hasMachineOutputFlag())
		})
	}
}

func TestNewSlopscaleCLIWithConfig(t *testing.T) {
	listOneNode := map[string]apiHandler{
		"GET /api/v1/node": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			t.Helper()
			writeJSON(t, w, clientv1.ListNodesOutputBody{Nodes: []clientv1.Node{laptopNode()}})
		},
	}

	t.Run("remote address without an api key is rejected", func(t *testing.T) {
		pointCLIAt(t, "https://slopscale.example.com")
		conf.Set("cli.api_key", "")

		_, client, _, err := newSlopscaleCLIWithConfig()
		require.ErrorIs(t, err, errAPIKeyNotSet)
		assert.Nil(t, client)

		// The wrapper used by every command reports the same failure.
		cmd := newTestCommand(t, listNodesCmd, nodeFlags, nil)

		_, err = runCommand(t, cmd)
		require.ErrorIs(t, err, errAPIKeyNotSet)
		require.ErrorContains(t, err, "connecting to slopscale")
	})

	t.Run("remote address sends the bearer token", func(t *testing.T) {
		serveAPI(t, map[string]apiHandler{
			"GET /api/v1/node": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				t.Helper()
				assertBearer(t, r)
				writeJSON(t, w, clientv1.ListNodesOutputBody{})
			},
		})

		ctx, client, cancel, err := newSlopscaleCLIWithConfig()
		require.NoError(t, err)

		defer cancel()

		resp, err := client.ListNodesWithResponse(ctx, &clientv1.ListNodesParams{})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode())
	})

	t.Run("insecure skips certificate verification", func(t *testing.T) {
		mux := http.NewServeMux()
		for pattern, handler := range listOneNode {
			mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { handler(t, w, r) })
		}

		server := httptest.NewTLSServer(mux)
		t.Cleanup(server.Close)

		pointCLIAt(t, server.URL)
		conf.Set("cli.insecure", true)

		ctx, client, cancel, err := newSlopscaleCLIWithConfig()
		require.NoError(t, err)

		defer cancel()

		resp, err := client.ListNodesWithResponse(ctx, &clientv1.ListNodesParams{})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode())
		require.NotNil(t, resp.JSON200)
		assert.Len(t, resp.JSON200.Nodes, 1)
	})

	t.Run("self-signed certificate is rejected unless insecure", func(t *testing.T) {
		server := httptest.NewTLSServer(http.NotFoundHandler())
		t.Cleanup(server.Close)

		pointCLIAt(t, server.URL)

		ctx, client, cancel, err := newSlopscaleCLIWithConfig()
		require.NoError(t, err)

		defer cancel()

		_, err = client.ListNodesWithResponse(ctx, &clientv1.ListNodesParams{})
		require.Error(t, err)
	})

	t.Run("no address dials the unix socket without authentication", func(t *testing.T) {
		serveAPIOnSocket(t, map[string]apiHandler{
			"GET /api/v1/node": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				t.Helper()
				assert.Empty(t, r.Header.Get("Authorization"))
				writeJSON(t, w, clientv1.ListNodesOutputBody{Nodes: []clientv1.Node{laptopNode()}})
			},
		})

		cmd := newTestCommand(t, listNodesCmd, nodeFlags, map[string]string{"output": "json-line"})

		out, err := runCommand(t, cmd)
		require.NoError(t, err)
		assert.Contains(t, out, `"givenName":"laptop"`)
	})
}

func TestNewSlopscaleServerWithConfigRejectsEmptyConfig(t *testing.T) {
	conf.Reset()
	t.Cleanup(conf.Reset)

	app, err := newSlopscaleServerWithConfig()
	require.ErrorContains(t, err, "loading configuration")
	assert.Nil(t, app)
}
