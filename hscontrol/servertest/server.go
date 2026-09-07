// Package servertest provides an in-process test harness for Headscale's
// control plane. It wires a real Headscale server to real Tailscale
// [controlclient.Direct] instances, enabling fast, deterministic tests
// of the full control protocol without Docker or separate processes.
package servertest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"testing"
	"time"

	hscontrol "github.com/juanfont/headscale/hscontrol"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"tailscale.com/net/memnet"
	"tailscale.com/tailcfg"
)

// TestServer is an in-process Headscale control server suitable for
// use with Tailscale's [controlclient.Direct].
//
// Networking uses tailscale.com/net/memnet so that all TCP
// connections stay in-process — no real sockets are opened.
type TestServer struct {
	App *hscontrol.Headscale
	URL string

	memNet     *memnet.Network
	ln         net.Listener
	httpServer *http.Server
	st         *state.State
	tb         testing.TB
	serveErr   chan error
}

// ServerOption configures a [TestServer].
type ServerOption func(*serverConfig)

type serverConfig struct {
	batchDelay       time.Duration
	bufferedChanSize int
	ephemeralTimeout time.Duration
	nodeExpiry       time.Duration
	batcherWorkers   int
	taildropEnabled  bool
	realListener     bool
	nodeStoreBatch   time.Duration
	oidc             *types.OIDCConfig
	dns              *types.DNSConfig
}

func defaultServerConfig() *serverConfig {
	return &serverConfig{
		batchDelay:       50 * time.Millisecond,
		bufferedChanSize: 30,
		batcherWorkers:   1,
		ephemeralTimeout: 30 * time.Second,
		taildropEnabled:  true,
		// Production coalesces NodeStore writes for up to 500ms. Every
		// Connect, endpoint update, or route approval in a test would pay
		// that wait, so use the same short window the state tests use.
		nodeStoreBatch: state.TestBatchTimeout,
	}
}

// WithBatchDelay sets the batcher's change coalescing delay.
func WithBatchDelay(d time.Duration) ServerOption {
	return func(c *serverConfig) { c.batchDelay = d }
}

// WithBufferedChanSize sets the per-node map session channel buffer.
func WithBufferedChanSize(n int) ServerOption {
	return func(c *serverConfig) { c.bufferedChanSize = n }
}

// WithBatcherWorkers sets the number of batcher worker goroutines.
// Defaults to 1 for deterministic tests; pass
// [types.DefaultBatcherWorkers] to match production concurrency.
func WithBatcherWorkers(n int) ServerOption {
	return func(c *serverConfig) { c.batcherWorkers = n }
}

// WithEphemeralTimeout sets the ephemeral node inactivity timeout.
func WithEphemeralTimeout(d time.Duration) ServerOption {
	return func(c *serverConfig) { c.ephemeralTimeout = d }
}

// WithNodeExpiry sets the default node key expiry duration.
func WithNodeExpiry(d time.Duration) ServerOption {
	return func(c *serverConfig) { c.nodeExpiry = d }
}

// WithRealListener binds the HTTP API to a real loopback TCP port instead of
// the in-process memnet, so external processes — the Tailscale SDK over a real
// socket, tscli, OpenTofu — can reach the API. The Noise/[TestClient] control
// path still uses memnet, so this is for REST/API tests only.
func WithRealListener() ServerOption {
	return func(c *serverConfig) { c.realListener = true }
}

// WithTaildropEnabled toggles the Taildrop file-sharing feature.
// Defaults to true to match production. Pass false to verify
// behaviour when an operator has switched the toggle off — e.g.
// that [tailcfg.CapabilityFileSharing] is withheld from the
// always-on baseline.
func WithTaildropEnabled(enabled bool) ServerOption {
	return func(c *serverConfig) { c.taildropEnabled = enabled }
}

// WithOIDC makes the server authenticate interactive logins against the
// given OpenID Connect provider instead of the CLI/web flow. The issuer must
// already serve its discovery document when [NewServer] runs, because
// [hscontrol.NewHeadscale] discovers the provider at construction time; a
// failed discovery fails the test rather than falling back to web auth.
// Scope is passed through unchanged, so include at least "openid".
func WithOIDC(cfg types.OIDCConfig) ServerOption {
	return func(c *serverConfig) { c.oidc = &cfg }
}

// WithDNS gives the server a dns section, as the config file would.
// Without it the server sends clients no DNS configuration at all.
func WithDNS(cfg types.DNSConfig) ServerOption {
	return func(c *serverConfig) { c.dns = &cfg }
}

// NewServer creates and starts a Headscale test server.
// The server is fully functional and accepts real Tailscale control
// protocol connections over Noise.
func NewServer(tb testing.TB, opts ...ServerOption) *TestServer {
	tb.Helper()

	sc := defaultServerConfig()
	for _, o := range opts {
		o(sc)
	}

	tmpDir := tb.TempDir()

	prefixV4 := netip.MustParsePrefix("100.64.0.0/10")
	prefixV6 := netip.MustParsePrefix("fd7a:115c:a1e0::/48")

	cfg := types.Config{
		// Placeholder; updated below once the in-memory server starts.
		ServerURL:           "http://localhost:0",
		NoisePrivateKeyPath: tmpDir + "/noise_private.key",
		Node: types.NodeConfig{
			Expiry: sc.nodeExpiry,
			Ephemeral: types.EphemeralConfig{
				InactivityTimeout: sc.ephemeralTimeout,
			},
		},
		PrefixV4:     &prefixV4,
		PrefixV6:     &prefixV6,
		IPAllocation: types.IPAllocationStrategySequential,
		Database: types.DatabaseConfig{
			Type: "sqlite3",
			Sqlite: types.SqliteConfig{
				Path: tmpDir + "/headscale_test.db",
			},
		},
		Policy: types.PolicyConfig{
			Mode: types.PolicyModeDB,
		},
		Taildrop: types.TaildropConfig{Enabled: sc.taildropEnabled},
		Tuning: types.Tuning{
			BatchChangeDelay:               sc.batchDelay,
			BatcherWorkers:                 sc.batcherWorkers,
			NodeMapSessionBufferedChanSize: sc.bufferedChanSize,
			NodeStoreBatchTimeout:          sc.nodeStoreBatch,
		},
	}

	if sc.oidc != nil {
		cfg.OIDC = *sc.oidc
		cfg.OIDC.OnlyStartIfOIDCIsAvailable = true
	}

	if sc.dns != nil {
		cfg.DNSConfig = *sc.dns
		// NewHeadscale rebuilds the tailcfg form from DNSConfig, the
		// stored override and the MagicDNS zones; nil would mean "no DNS".
		cfg.TailcfgDNSConfig = &tailcfg.DNSConfig{}
	}

	app, err := hscontrol.NewHeadscale(&cfg)
	if err != nil {
		tb.Fatalf("servertest: NewHeadscale: %v", err)
	}

	// Set a minimal DERP map so MapResponse generation works.
	app.GetState().SetDERPMap(&tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			900: {
				RegionID:   900,
				RegionCode: "test",
				RegionName: "Test Region",
				Nodes: []*tailcfg.DERPNode{{
					Name:     "test0",
					RegionID: 900,
					HostName: "127.0.0.1",
					IPv4:     "127.0.0.1",
					DERPPort: -1, // not a real DERP, just needed for MapResponse
				}},
			},
		},
	})

	// Start subsystems.
	app.StartBatcherForTest(tb)
	app.StartEphemeralGCForTest(tb)

	// Start the HTTP server. By default it binds an in-memory network so all
	// TCP connections stay in-process; WithRealListener swaps in a real loopback
	// port so external binaries can connect.
	var (
		memNetwork memnet.Network
		ln         net.Listener
	)

	if sc.realListener {
		var lc net.ListenConfig

		ln, err = lc.Listen(tb.Context(), "tcp", "127.0.0.1:0")
	} else {
		ln, err = memNetwork.Listen("tcp", "127.0.0.1:443")
	}

	if err != nil {
		tb.Fatalf("servertest: Listen: %v", err)
	}

	httpServer := &http.Server{
		Handler:           app.HTTPHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)

	go func() {
		serveErr <- httpServer.Serve(ln)
	}()

	serverURL := "http://" + ln.Addr().String()

	ts := &TestServer{
		App:        app,
		URL:        serverURL,
		memNet:     &memNetwork,
		ln:         ln,
		httpServer: httpServer,
		st:         app.GetState(),
		tb:         tb,
		serveErr:   serveErr,
	}

	tb.Cleanup(ts.Close)

	// Now update the config to point at the real URL so that
	// MapResponse.ControlURL etc. are correct.
	app.SetServerURLForTest(tb, serverURL)

	return ts
}

// State returns the server's state manager for creating users,
// nodes, and pre-auth keys.
func (s *TestServer) State() *state.State {
	return s.st
}

// Close shuts down the in-memory HTTP server and listener.
// Subsystem cleanup (batcher, ephemeral GC) is handled by
// [testing.TB.Cleanup] callbacks registered in [hscontrol.Headscale.StartBatcherForTest] and
// [hscontrol.Headscale.StartEphemeralGCForTest].
func (s *TestServer) Close() {
	s.httpServer.Close()
	s.ln.Close()

	select {
	case err := <-s.serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.tb.Errorf("servertest: http.Server.Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		s.tb.Errorf("servertest: http.Server.Serve did not return after Close")
	}
}

// MemNet returns the in-memory network used by this server,
// so that [TestClient] dialers can be wired to it.
func (s *TestServer) MemNet() *memnet.Network {
	return s.memNet
}

// HTTPClient returns an [http.Client] that reaches the server's HTTP API. On
// the default in-memory network it dials the server address through memnet
// and every other address through the real network, so one client can follow
// a browser-style redirect chain that leaves the server for an identity
// provider on a loopback port and comes back. The client carries a cookie
// jar, which the OIDC state and CSRF cookies need, and follows redirects the
// way a browser does.
func (s *TestServer) HTTPClient(tb testing.TB) *http.Client {
	tb.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		tb.Fatalf("servertest: cookiejar.New: %v", err)
	}

	serverAddr := s.ln.Addr().String()

	var realDialer net.Dialer

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == serverAddr {
				return s.memNet.Dial(ctx, network, addr)
			}

			return realDialer.DialContext(ctx, network, addr)
		},
	}

	tb.Cleanup(transport.CloseIdleConnections)

	return &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   30 * time.Second,
	}
}

// CreateUser creates a test user and returns it.
func (s *TestServer) CreateUser(tb testing.TB, name string) *types.User {
	tb.Helper()

	u, _, err := s.st.CreateUser(types.User{Name: name})
	if err != nil {
		tb.Fatalf("servertest: CreateUser(%q): %v", name, err)
	}

	return u
}

// CreateAPIKey mints an API key string (the Bearer/Basic credential). When
// owner is non-nil the key is owned by that user, so the v2 API mints
// user-owned (untagged) auth keys on its behalf.
func (s *TestServer) CreateAPIKey(tb testing.TB, owner *types.User) string {
	tb.Helper()

	keyStr, key, err := s.st.CreateAPIKey(nil)
	if err != nil {
		tb.Fatalf("servertest: CreateAPIKey: %v", err)
	}

	if owner != nil {
		err := s.st.SetAPIKeyUser(key.ID, types.UserID(owner.ID))
		if err != nil {
			tb.Fatalf("servertest: SetAPIKeyUser: %v", err)
		}
	}

	return keyStr
}

// CreateRegisteredNode mints a registered node (allocated IPs, registered
// method) in BOTH the database and the in-memory NodeStore, then returns its
// view. The v2 device endpoints resolve nodes via State.GetNodeByID, which reads
// the NodeStore, so a DB-only node would be invisible to them.
func (s *TestServer) CreateRegisteredNode(tb testing.TB, owner *types.User, hostname ...string) types.NodeView {
	tb.Helper()

	node := s.st.CreateRegisteredNodeForTest(owner, hostname...)
	// CreateRegisteredNodeForTest sets UserID but leaves the User association
	// unloaded; the device response renders the owner login, so attach it.
	node.User = owner

	return s.st.PutNodeInStoreForTest(*node)
}

// CreatePreAuthKey creates a reusable pre-auth key for the given user.
func (s *TestServer) CreatePreAuthKey(tb testing.TB, userID types.UserID) string {
	tb.Helper()
	return s.createPreAuthKey(tb, userID, true, false, nil)
}

// CreateTaggedPreAuthKey creates a reusable pre-auth key with ACL tags.
func (s *TestServer) CreateTaggedPreAuthKey(tb testing.TB, userID types.UserID, tags []string) string {
	tb.Helper()
	return s.createPreAuthKey(tb, userID, true, false, tags)
}

// CreateEphemeralPreAuthKey creates an ephemeral pre-auth key.
func (s *TestServer) CreateEphemeralPreAuthKey(tb testing.TB, userID types.UserID) string {
	tb.Helper()
	return s.createPreAuthKey(tb, userID, false, true, nil)
}

// CreatePreAuthKeyFromSpec creates a pre-auth key with every property
// chosen by the caller and returns its plaintext.
func (s *TestServer) CreatePreAuthKeyFromSpec(tb testing.TB, spec types.PreAuthKeySpec) string {
	tb.Helper()

	pak, err := s.st.CreatePreAuthKeyFromSpec(spec)
	if err != nil {
		tb.Fatalf("servertest: CreatePreAuthKeyFromSpec: %v", err)
	}

	return pak.Key
}

func (s *TestServer) createPreAuthKey(
	tb testing.TB,
	userID types.UserID,
	reusable, ephemeral bool,
	tags []string,
) string {
	tb.Helper()

	pak, err := s.st.CreatePreAuthKey(&userID, reusable, ephemeral, nil, tags)
	if err != nil {
		tb.Fatalf("servertest: createPreAuthKey: %v", err)
	}

	return pak.Key
}
