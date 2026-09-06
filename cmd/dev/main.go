// cmd/dev starts a local headscale development server with a pre-created
// user and pre-auth key, ready for connecting tailscale nodes via mts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	// defaultPort is the headscale listen port when --port is not given.
	defaultPort = 8080
	// metricsPortOffset is added to --port to derive the metrics listen
	// port, so the default lands on 9090.
	metricsPortOffset = 1010
	// healthTimeout bounds how long we wait for the server to come up.
	healthTimeout = 30 * time.Second
)

var (
	port = flag.Int("port", defaultPort, "headscale listen port")
	keep = flag.Bool("keep", false, "keep state directory on exit")
)

var errHealthTimeout = errors.New("health check timed out")

var errEmptyAuthKey = errors.New("empty auth key in response")

// maxDevPort is the highest --port value that keeps the derived metrics
// port (port+metricsPortOffset) inside the valid 1..65535 TCP range.
const maxDevPort = 65535 - metricsPortOffset

const devConfig = `---
server_url: http://127.0.0.1:%d
listen_addr: 127.0.0.1:%d
metrics_listen_addr: 127.0.0.1:%d

noise:
  private_key_path: %s/noise_private.key

prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48
  allocation: sequential

database:
  type: sqlite
  sqlite:
    path: %s/db.sqlite
    write_ahead_log: true

derp:
  server:
    enabled: false
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  auto_update_enabled: false

dns:
  magic_dns: true
  base_domain: headscale.dev
  override_local_dns: false

log:
  level: debug
  format: text

policy:
  mode: database

unix_socket: %s/headscale.sock
unix_socket_permission: "0770"
`

func main() {
	flag.Parse()
	log.SetFlags(0)

	if *port < 1 || *port > maxDevPort {
		log.Fatalf(
			"--port must be in 1..%d (higher values overflow the derived metrics port); got %d",
			maxDevPort, *port,
		)
	}

	http.DefaultClient.Timeout = 2 * time.Second
	http.DefaultClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	err := run()
	if err != nil {
		log.Fatal(err)
	}
}

func run() error {
	metricsPort := *port + metricsPortOffset

	tmpDir, err := os.MkdirTemp("", "headscale-dev-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}

	if !*keep {
		defer os.RemoveAll(tmpDir)
	}

	// Write config.
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := fmt.Sprintf(
		devConfig,
		*port, *port, metricsPort,
		tmpDir, tmpDir, tmpDir,
	)

	err = os.WriteFile(configPath, []byte(configContent), 0o600)
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	// Build headscale.
	fmt.Println("Building headscale...")

	hsBin := filepath.Join(tmpDir, "headscale")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	build := exec.CommandContext(ctx, "go", "build", "-o", hsBin, "./cmd/headscale")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr

	err = build.Run()
	if err != nil {
		return fmt.Errorf("building headscale: %w", err)
	}

	// Start headscale serve.
	fmt.Println("Starting headscale server...")

	serve := exec.CommandContext(ctx, hsBin, "serve", "-c", configPath)
	serve.Stdout = os.Stdout
	serve.Stderr = os.Stderr

	err = serve.Start()
	if err != nil {
		return fmt.Errorf("starting headscale: %w", err)
	}

	// Wait for server to be ready.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", *port)

	err = waitForHealth(ctx, healthURL, healthTimeout)
	if err != nil {
		return fmt.Errorf("waiting for headscale: %w", err)
	}

	// Create user.
	fmt.Println("Creating user and pre-auth key...")

	userJSON, err := runHS(ctx, hsBin, configPath, "users", "create", "dev", "-o", "json")
	if err != nil {
		return fmt.Errorf("creating user: %w", err)
	}

	userID, err := extractUserID(userJSON)
	if err != nil {
		return fmt.Errorf("parsing user: %w", err)
	}

	// Create pre-auth key.
	keyJSON, err := runHS(
		ctx, hsBin, configPath,
		"preauthkeys", "create",
		"-u", strconv.FormatUint(userID, 10),
		"--reusable",
		"-e", "24h",
		"-o", "json",
	)
	if err != nil {
		return fmt.Errorf("creating pre-auth key: %w", err)
	}

	authKey, err := extractAuthKey(keyJSON)
	if err != nil {
		return fmt.Errorf("parsing pre-auth key: %w", err)
	}

	// Print banner.
	fmt.Printf(
		`
=== Headscale Dev Environment ===
  Server:  http://127.0.0.1:%d
  Metrics: http://127.0.0.1:%d
  Debug:   http://127.0.0.1:%d/debug/ping
  Config:  %s
  State:   %s

Pre-auth key: %s

Connect nodes with mts:
  go tool mts server run                  # start mts (once, another terminal)
  go tool mts server add node1            # create a node
  go tool mts node1 up --login-server=http://127.0.0.1:%d --authkey=%s
  go tool mts node1 status                # check connection

Manage headscale:
  %s -c %s nodes list
  %s -c %s users list

Press Ctrl+C to stop.
`,
		*port, metricsPort, metricsPort,
		configPath, tmpDir,
		authKey,
		*port, authKey,
		hsBin, configPath,
		hsBin, configPath,
	)

	// Wait for headscale to exit.
	err = serve.Wait()
	if err != nil {
		// Context cancellation is expected on Ctrl+C.
		if ctx.Err() != nil {
			fmt.Println("\nShutting down...")

			return nil
		}

		return fmt.Errorf("headscale exited: %w", err)
	}

	return nil
}

// waitForHealth polls the health endpoint until it returns 200 or the
// timeout expires.
func waitForHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		// Busy-wait is acceptable for a dev tool polling a local server.
		//nolint:forbidigo // busy-wait polling local server health endpoint
		time.Sleep(200 * time.Millisecond)
	}

	return errHealthTimeout
}

// runHS executes a headscale CLI command and returns its stdout.
func runHS(ctx context.Context, bin, config string, args ...string) ([]byte, error) {
	fullArgs := append([]string{"-c", config}, args...)
	cmd := exec.CommandContext(ctx, bin, fullArgs...)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running headscale CLI %s: %w", bin, err)
	}

	return out, nil
}

// extractUserID parses the JSON output of "users create" and returns the
// user ID.
func extractUserID(data []byte) (uint64, error) {
	var user struct {
		ID uint64 `json:"id"`
	}

	err := json.Unmarshal(data, &user)
	if err != nil {
		return 0, fmt.Errorf("unmarshalling user JSON: %w (raw: %s)", err, data)
	}

	return user.ID, nil
}

// extractAuthKey parses the JSON output of "preauthkeys create" and
// returns the key string.
func extractAuthKey(data []byte) (string, error) {
	var key struct {
		Key string `json:"key"`
	}

	err := json.Unmarshal(data, &key)
	if err != nil {
		return "", fmt.Errorf("unmarshalling key JSON: %w (raw: %s)", err, data)
	}

	if key.Key == "" {
		return "", errEmptyAuthKey
	}

	return key.Key, nil
}
