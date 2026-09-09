// cmd/dev starts a local slopscale development server with a pre-created
// user and pre-auth key, ready for connecting tailscale nodes via mts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/oauth2-proxy/mockoidc"
)

const (
	// defaultPort is the slopscale listen port when --port is not given.
	defaultPort = 8080
	// metricsPortOffset is added to --port to derive the metrics listen
	// port, so the default lands on 9090.
	metricsPortOffset = 1010
	// oidcPortOffset is added to --port to derive the mock identity
	// provider's port, so the default lands on 9100.
	oidcPortOffset = 1020
	// oidcUser is the identity the mock provider signs everyone in as:
	// mockoidc's default user. It is listed in oidc.admin_users, so the
	// console opens as an admin.
	oidcUser = "jane.doe@example.com"
	// healthTimeout bounds how long we wait for the server to come up.
	healthTimeout = 30 * time.Second
)

var (
	port      = flag.Int("port", defaultPort, "slopscale listen port")
	keep      = flag.Bool("keep", false, "keep state directory on exit")
	serverURL = flag.String("server-url", "",
		"public URL of the server (default http://127.0.0.1:<port>); "+
			"point it at the Vite dev server (http://localhost:5173) to sign in to the console from `bun run dev`")
)

var errHealthTimeout = errors.New("health check timed out")

var errEmptyAuthKey = errors.New("empty auth key in response")

// maxDevPort is the highest --port value that keeps the derived metrics
// and OIDC ports inside the valid 1..65535 TCP range.
const maxDevPort = 65535 - oidcPortOffset

const devConfig = `---
server_url: %s
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
    enabled: true
    stun_listen_addr: 127.0.0.1:3478
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  auto_update_enabled: false

dns:
  magic_dns: true
  base_domain: slopscale.dev
  override_local_dns: false
  nameservers:
    global:
      - 1.1.1.1
      - 1.0.0.1
  search_domains:
    - corp.slopscale.dev

log:
  level: debug
  format: text

policy:
  mode: database

# The dev server's webhook, log stream and DERP receivers all run on this
# host, and the seed script mints nodes through the debug endpoint.
egress:
  allow_loopback_targets: true

debug:
  node_api_enabled: true

unix_socket: %s/slopscale.sock
unix_socket_permission: "0770"

# Recordings land under the state directory so the console can serve them.
ssh_recording:
  dir: %s/recordings

# A mock identity provider runs inside cmd/dev; the console signs in
# through it and the user below comes out an admin.
oidc:
  issuer: %s
  client_id: %s
  client_secret: %s
  admin_users:
    - %s
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

	publicURL := *serverURL
	if publicURL == "" {
		publicURL = fmt.Sprintf("http://127.0.0.1:%d", *port)
	}

	tmpDir, err := os.MkdirTemp("", "slopscale-dev-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}

	if !*keep {
		defer os.RemoveAll(tmpDir)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start the mock identity provider first: its issuer goes in the config.
	provider, err := startMockOIDC(ctx, *port+oidcPortOffset)
	if err != nil {
		return err
	}

	defer provider.Shutdown() //nolint:errcheck // best effort on the way out

	// Write config.
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := fmt.Sprintf(
		devConfig,
		publicURL, *port, metricsPort,
		tmpDir, tmpDir, tmpDir, tmpDir,
		provider.Issuer(), provider.ClientID, provider.ClientSecret, oidcUser,
	)

	err = os.WriteFile(configPath, []byte(configContent), 0o600)
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	// Build slopscale.
	fmt.Println("Building slopscale...")

	hsBin := filepath.Join(tmpDir, "slopscale")

	build := exec.CommandContext(ctx, "go", "build", "-o", hsBin, "./cmd/slopscale")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr

	err = build.Run()
	if err != nil {
		return fmt.Errorf("building slopscale: %w", err)
	}

	// Start slopscale serve.
	fmt.Println("Starting slopscale server...")

	serve := exec.CommandContext(ctx, hsBin, "serve", "-c", configPath)
	serve.Stdout = os.Stdout
	serve.Stderr = os.Stderr

	err = serve.Start()
	if err != nil {
		return fmt.Errorf("starting slopscale: %w", err)
	}

	// Wait for server to be ready.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/health", *port)

	err = waitForHealth(ctx, healthURL, healthTimeout)
	if err != nil {
		return fmt.Errorf("waiting for slopscale: %w", err)
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
=== Slopscale Dev Environment ===
  Server:  http://127.0.0.1:%d
  Console: %s/admin/  (sign in through the mock provider as %s, an admin)
  OIDC:    %s
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

Manage slopscale:
  %s -c %s nodes list
  %s -c %s users list

Press Ctrl+C to stop.
`,
		*port, publicURL, oidcUser, provider.Issuer(), metricsPort, metricsPort,
		configPath, tmpDir,
		authKey,
		*port, authKey,
		hsBin, configPath,
		hsBin, configPath,
	)

	// Wait for slopscale to exit.
	err = serve.Wait()
	if err != nil {
		// Context cancellation is expected on Ctrl+C.
		if ctx.Err() != nil {
			fmt.Println("\nShutting down...")

			return nil
		}

		return fmt.Errorf("slopscale exited: %w", err)
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

// runHS executes a slopscale CLI command and returns its stdout.
func runHS(ctx context.Context, bin, config string, args ...string) ([]byte, error) {
	fullArgs := append([]string{"-c", config}, args...)
	cmd := exec.CommandContext(ctx, bin, fullArgs...)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("running slopscale CLI %s: %w", bin, err)
	}

	return out, nil
}

// extractUserID parses the JSON output of "users create" and returns the
// user ID. The API renders uint64 identifiers as strings.
func extractUserID(data []byte) (uint64, error) {
	var user struct {
		ID string `json:"id"`
	}

	err := json.Unmarshal(data, &user)
	if err != nil {
		return 0, fmt.Errorf("unmarshalling user JSON: %w (raw: %s)", err, data)
	}

	id, err := strconv.ParseUint(user.ID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing user id %q: %w", user.ID, err)
	}

	return id, nil
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

// startMockOIDC serves a mock OpenID Connect provider on the loopback port.
// Every authorization request signs in as oidcUser without a login page,
// which is what the console's e2e test and local development want.
func startMockOIDC(ctx context.Context, port int) (*mockoidc.MockOIDC, error) {
	provider, err := mockoidc.NewServer(nil)
	if err != nil {
		return nil, fmt.Errorf("creating mock OIDC provider: %w", err)
	}

	// With nothing queued every login is mockoidc.DefaultUser, whose
	// email is oidcUser.
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("listening for the mock OIDC provider: %w", err)
	}

	err = provider.Start(listener, nil)
	if err != nil {
		return nil, fmt.Errorf("starting the mock OIDC provider: %w", err)
	}

	return provider, nil
}
