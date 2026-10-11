package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creachadair/command"
	"github.com/creachadair/flax"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunArguments exercises the real flag parser: a flag the shell turned
// into a positional argument (a folded YAML continuation leaves " --postgres=1")
// must be refused, not ignored with the backend silently left on SQLite.
func TestRunArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		postgres bool
		wantErr  error
	}{
		{
			name:     "postgres before selector",
			args:     []string{"--postgres=1", "--timeout=15m", "^TestSlopscale$"},
			postgres: true,
		},
		{
			name:     "postgres after selector",
			args:     []string{"^TestSlopscale$", "--postgres", "--timeout=15m"},
			postgres: true,
		},
		{
			name: "sqlite",
			args: []string{"--postgres=0", "--timeout=15m", "--test=^TestSlopscale$"},
		},
		{
			name:    "folded shell continuations",
			args:    []string{"^TestSlopscale$", " --timeout=15m", " --postgres=1"},
			wantErr: ErrUnexpectedTestArguments,
		},
		{
			name:    "extra selector",
			args:    []string{"^TestSlopscale$", "TestNodeCommand"},
			wantErr: ErrUnexpectedTestArguments,
		},
		{
			name:    "positional selector with test flag",
			args:    []string{"--test=^TestSlopscale$", "TestNodeCommand"},
			wantErr: ErrUnexpectedTestArguments,
		},
		{
			name:    "missing selector",
			wantErr: ErrTestPatternRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var config RunConfig

			cmd := command.C{
				Name:     "run",
				SetFlags: command.Flags(flax.MustBind, &config),
				Run:      func(env *command.Env) error { return config.validate(env.Args) },
			}

			err := command.Run(cmd.NewEnv(nil).MergeFlags(true), tt.args)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.postgres, config.UsePostgres)
			assert.Equal(t, "^TestSlopscale$", config.TestPattern)
			assert.Equal(t, 15*time.Minute, config.Timeout)
		})
	}
}

// TestStreamAndWaitDrainsFinalOutput holds back the last log packet after
// Docker reported the exit code: streamAndWait must still print it, or a
// fast test exit loses its PASS/FAIL summary.
func TestStreamAndWaitDrainsFinalOutput(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "test-output")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, output.Close()) })

	stdout := os.Stdout
	os.Stdout = output

	t.Cleanup(func() { os.Stdout = stdout })

	waitSent := make(chan struct{})
	releaseLogs := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/logs"):
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			w.WriteHeader(http.StatusOK)
			_ = http.NewResponseController(w).Flush()

			select {
			case <-releaseLogs:
				_, _ = w.Write([]byte("--- PASS: TestExample (0.01s)\nPASS\n"))
			case <-r.Context().Done():
			}
		case strings.HasSuffix(r.URL.Path, "/wait"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
			_ = http.NewResponseController(w).Flush()

			close(waitSent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cli, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.44"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cli.Close()) })

	type result struct {
		exitCode int
		err      error
	}

	done := make(chan result, 1)

	go func() {
		code, waitErr := streamAndWait(t.Context(), cli, "test-runner")
		done <- result{code, waitErr}
	}()

	<-waitSent

	select {
	case <-done:
		close(releaseLogs)
		t.Fatal("returned before the final test output was delivered")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseLogs)

	got := <-done
	require.NoError(t, got.err)
	assert.Zero(t, got.exitCode)

	data, err := os.ReadFile(output.Name())
	require.NoError(t, err)
	assert.Equal(t, "--- PASS: TestExample (0.01s)\nPASS\n", string(data))
}
