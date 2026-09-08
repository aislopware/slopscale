package hscontrol

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/davecgh/go-spew/spew"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/metrics"
	apiv1 "github.com/juanfont/headscale/hscontrol/api/v1"
	apiv2 "github.com/juanfont/headscale/hscontrol/api/v2"
	"github.com/juanfont/headscale/hscontrol/capver"
	"github.com/juanfont/headscale/hscontrol/db"
	derpServer "github.com/juanfont/headscale/hscontrol/derp/server"
	"github.com/juanfont/headscale/hscontrol/dns"
	"github.com/juanfont/headscale/hscontrol/dnsprovider"
	"github.com/juanfont/headscale/hscontrol/mapper"
	"github.com/juanfont/headscale/hscontrol/recorder"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/juanfont/headscale/hscontrol/util/zlog/zf"
	"github.com/juanfont/headscale/web"
	"github.com/pkg/profile"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sync/errgroup"
	"tailscale.com/envknob"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

var errUnsupportedLetsEncryptChallengeType = errors.New(
	"unknown value for Lets Encrypt challenge type",
)

const (
	updateInterval     = 5 * time.Second
	privateKeyFileMode = 0o600
	headscaleDirPerm   = 0o700
)

// Headscale represents the base app of the service.
type Headscale struct {
	cfg             *types.Config
	state           *state.State
	noisePrivateKey *key.MachinePrivate
	ephemeralGC     *db.EphemeralGarbageCollector

	DERPServer *derpServer.DERPServer

	// realIPMiddleware is nil when cfg.TrustedProxies is empty; the
	// router skips the mount and r.RemoteAddr stays as the TCP peer.
	realIPMiddleware func(http.Handler) http.Handler

	// Things that generate changes
	extraRecordMan *dns.ExtraRecordsMan

	// derpRefreshing is set while a DERP refresh runs in the background.
	derpRefreshing atomic.Bool
	authProvider   AuthProvider
	mapBatcher     *mapper.Batcher

	// recorder indexes and serves SSH session recordings; the embedded
	// recorder node feeds it when cfg.SSHRecording.Enabled.
	recorder *recorder.Recorder

	// dnsProvider publishes ACME challenge records for machines' HTTPS
	// certificates; nil when cfg.HTTPSCerts is off.
	dnsProvider dnsprovider.Provider

	clientStreamsOpen sync.WaitGroup
}

var (
	profilingEnabled = envknob.Bool("HEADSCALE_DEBUG_PROFILING_ENABLED")
	profilingPath    = envknob.String("HEADSCALE_DEBUG_PROFILING_PATH")
	tailsqlEnabled   = envknob.Bool("HEADSCALE_DEBUG_TAILSQL_ENABLED")
	tailsqlStateDir  = envknob.String("HEADSCALE_DEBUG_TAILSQL_STATE_DIR")
	tailsqlTSKey     = envknob.String("TS_AUTHKEY")
	dumpConfig       = envknob.Bool("HEADSCALE_DEBUG_DUMP_CONFIG")
)

func NewHeadscale(cfg *types.Config) (*Headscale, error) {
	var err error

	if profilingEnabled {
		runtime.SetBlockProfileRate(1)
	}

	noisePrivateKey, err := readOrCreatePrivateKey(cfg.NoisePrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading or creating Noise protocol private key: %w", err)
	}

	s, err := state.NewState(cfg)
	if err != nil {
		return nil, fmt.Errorf("init state: %w", err)
	}

	app := Headscale{
		cfg:               cfg,
		noisePrivateKey:   noisePrivateKey,
		clientStreamsOpen: sync.WaitGroup{},
		state:             s,
	}

	app.recorder = newRecorder(cfg, s, s.NodeByIP)

	app.dnsProvider, err = newDNSProvider(cfg)
	if err != nil {
		return nil, err
	}

	if len(cfg.TrustedProxies) > 0 {
		app.realIPMiddleware, err = trustedProxyRealIP(cfg.TrustedProxies)
		if err != nil {
			return nil, fmt.Errorf("building trusted_proxies middleware: %w", err)
		}
	}

	// Initialize ephemeral garbage collector
	ephemeralGC := db.NewEphemeralGarbageCollector(func(ni types.NodeID) {
		node, ok := app.state.GetNodeByID(ni)
		if !ok {
			log.Error().Uint64("node.id", ni.Uint64()).Msg("ephemeral node deletion failed")
			log.Debug().
				Caller().
				Uint64("node.id", ni.Uint64()).
				Msg("ephemeral node deletion failed because node not found in NodeStore")

			return
		}

		policyChanged, deleteErr := app.state.DeleteNode(node)
		if deleteErr != nil {
			log.Error().Err(deleteErr).EmbedObject(node).Msg("ephemeral node deletion failed")
			return
		}

		app.Change(policyChanged)
		log.Debug().Caller().EmbedObject(node).Msg("ephemeral node deleted because garbage collection timeout reached")
	})
	app.ephemeralGC = ephemeralGC

	authProvider, err := setupAuthProvider(cfg, &app)
	if err != nil {
		return nil, err
	}

	app.authProvider = authProvider

	// The DNS override from the settings table is applied by NewState; the
	// rebuild adds the MagicDNS reverse zones for the tailnet's prefixes.
	cfg.RebuildTailcfgDNS()

	embeddedDERPServer, err := setupEmbeddedDERPServer(cfg, noisePrivateKey, &app)
	if err != nil {
		return nil, err
	}

	app.DERPServer = embeddedDERPServer

	return &app, nil
}

// setupAuthProvider builds the CLI-based auth provider used to hand out
// registration/auth URLs, upgrading to OIDC when cfg.OIDC.Issuer is set. On
// OIDC setup failure it falls back to the CLI provider unless
// cfg.OIDC.OnlyStartIfOIDCIsAvailable requires a hard failure.
func setupAuthProvider(cfg *types.Config, app *Headscale) (AuthProvider, error) {
	authProvider := AuthProvider(NewAuthProviderWeb(cfg.ServerURL))

	if cfg.OIDC.Issuer == "" {
		return authProvider, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	oidcProvider, err := NewAuthProviderOIDC(
		ctx,
		app,
		cfg.ServerURL,
		&cfg.OIDC,
	)
	if err != nil {
		if cfg.OIDC.OnlyStartIfOIDCIsAvailable {
			return nil, err
		}

		log.Warn().Err(err).Msg("failed to set up OIDC provider, falling back to CLI based authentication")

		return authProvider, nil
	}

	return oidcProvider, nil
}

// setupEmbeddedDERPServer creates the embedded DERP relay, off, from the
// key at cfg.DERP.ServerPrivateKeyPath; the settings turn it on. Without
// a key path there is no relay and the settings cannot enable it.
func setupEmbeddedDERPServer(
	cfg *types.Config,
	noisePrivateKey *key.MachinePrivate,
	app *Headscale,
) (*derpServer.DERPServer, error) {
	if cfg.DERP.ServerPrivateKeyPath == "" {
		return nil, nil //nolint:nilnil // intentional: no relay key, no embedded relay
	}

	derpServerKey, err := readOrCreatePrivateKey(cfg.DERP.ServerPrivateKeyPath)
	if err != nil {
		if cfg.DERP.ServerEnabled {
			return nil, fmt.Errorf("reading or creating DERP server private key: %w", err)
		}

		// The relay is off in the file and its key cannot be read or
		// made, which a read-only key directory causes; the server runs
		// without a relay rather than refusing to start over one it was
		// not asked to run.
		log.Warn().Err(err).Str("path", cfg.DERP.ServerPrivateKeyPath).
			Msg("no embedded DERP relay: its key cannot be read or created")

		return nil, nil //nolint:nilnil // intentional: no relay key, no embedded relay
	}

	if derpServerKey.Equal(*noisePrivateKey) {
		return nil, errDERPKeyIsNoiseKey
	}

	return derpServer.NewDERPServer(key.NodePrivate(*derpServerKey), app.handleVerifyRequest), nil
}

var errDERPKeyIsNoiseKey = errors.New("DERP server private key and noise private key are the same")

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// serveHumaMux dispatches to a Huma mux mounted under the outer chi router.
// Huma registers operations at absolute paths (/api/v1/...), so chi's route
// context must be cleared for the inner mux to re-match against the original URL.
func serveHumaMux(mux http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		mux.ServeHTTP(w, req.WithContext(
			context.WithValue(req.Context(), chi.RouteCtxKey, nil),
		))
	}
}

// Serve launches the HTTP servers that run Headscale and its API.
//
//nolint:gocognit,gocyclo,cyclop,funlen,maintidx // legacy: wires many independent listeners; splitting is a redesign
func (h *Headscale) Serve() error {
	var err error

	capver.CanOldCodeBeCleanedUp()

	if profilingEnabled {
		if profilingPath != "" {
			err = os.MkdirAll(profilingPath, os.ModePerm)
			if err != nil {
				log.Fatal().Err(err).Msg("failed to create profiling directory")
			}

			defer profile.Start(profile.ProfilePath(profilingPath)).Stop()
		} else {
			defer profile.Start().Stop()
		}
	}

	if dumpConfig {
		spew.Dump(h.cfg)
	}

	versionInfo := types.GetVersionInfo()
	log.Info().Str("version", versionInfo.Version).Str("commit", versionInfo.Commit).Msg("starting headscale")
	log.Info().
		Str("minimum_version", capver.TailscaleVersion(capver.MinSupportedCapabilityVersion)).
		Msg("Clients with a lower minimum version will be rejected")

	h.mapBatcher = mapper.NewBatcherAndMapper(h.cfg, h.state)

	h.mapBatcher.Start()
	defer h.mapBatcher.Close()

	h.state.SetDERPRelay(h.DERPServer)

	err = h.state.LoadDERPMap(context.Background())
	if err != nil {
		return fmt.Errorf("building DERP map: %w", err)
	}

	h.state.LogDERPMap()

	if h.DERPServer != nil {
		defer h.DERPServer.Close()
	}

	// Start ephemeral node garbage collector and schedule all nodes
	// that are already in the database and ephemeral. If they are still
	// around between restarts, they will reconnect and the GC will
	// be cancelled.
	go h.ephemeralGC.Start()

	ephmNodes := h.state.ListEphemeralNodes()
	for _, node := range ephmNodes.All() {
		h.ephemeralGC.Schedule(node.ID(), h.cfg.Node.Ephemeral.InactivityTimeout)
	}

	if h.cfg.DNSConfig.ExtraRecordsPath != "" {
		h.extraRecordMan, err = dns.NewExtraRecordsManager(h.cfg.DNSConfig.ExtraRecordsPath)
		if err != nil {
			return fmt.Errorf("setting up extrarecord manager: %w", err)
		}

		h.cfg.SetExtraRecords(h.extraRecordMan.Records())

		go h.extraRecordMan.Run()
		defer h.extraRecordMan.Close()
	}

	// Start all scheduled tasks, e.g. expiring nodes, derp updates and
	// records updates
	scheduleCtx, scheduleCancel := context.WithCancel(context.Background())
	defer scheduleCancel()

	go h.scheduledTasks(scheduleCtx)

	// Prepare group for running listeners
	errorGroup := new(errgroup.Group)

	ctx := context.Background()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	//
	//
	// Set up LOCAL listeners
	//

	err = h.ensureUnixSocketIsAbsent()
	if err != nil {
		return fmt.Errorf("removing old socket file: %w", err)
	}

	socketDir := filepath.Dir(h.cfg.UnixSocket)

	err = util.EnsureDir(socketDir)
	if err != nil {
		return fmt.Errorf("setting up unix socket: %w", err)
	}

	socketListener, err := new(net.ListenConfig).Listen(context.Background(), "unix", h.cfg.UnixSocket)
	if err != nil {
		return fmt.Errorf("setting up socket: %w", err)
	}

	// Change socket permissions
	chmodErr := os.Chmod(h.cfg.UnixSocket, h.cfg.UnixSocketPermission)
	if chmodErr != nil {
		return fmt.Errorf("changing socket permission: %w", chmodErr)
	}

	// The Huma v1 API mux matches full /api/v1/... paths and is shared by
	// the local unix socket (served without authentication, local trust)
	// and the remote TCP router (served behind the API-key middleware).
	humaMux, _ := apiv1.Handler(h.apiV1Backend())

	// The Headscale v2 API. Served behind Basic/Bearer auth on the remote
	// listener, and over the local unix socket (local trust) so the CLI can
	// manage OAuth clients through the same v2 keys handler the Tailscale
	// ecosystem uses.
	humaV2Mux, _ := apiv2.Handler(apiv2.Backend{
		State:  h.state,
		Change: h.Change,
		Cfg:    h.cfg,
	})

	// Serve both Huma APIs over the unix socket without TLS or auth: socket
	// access implies trust. WithLocalTrust marks these requests so each API's
	// security middleware skips the credential check. v2 paths route to the v2
	// mux; everything else (the v1 paths) to v1.
	socketHandler := http.NewServeMux()
	socketHandler.Handle("/api/v2/", apiv2.WithLocalTrust(humaV2Mux))
	socketHandler.Handle("/", apiv1.WithLocalTrust(humaMux))

	socketServer := &http.Server{
		Handler:     socketHandler,
		ReadTimeout: types.HTTPTimeout,
	}

	errorGroup.Go(func() error { return socketServer.Serve(socketListener) })

	//
	//
	// Set up REMOTE listeners
	//

	tlsConfig, err := h.getTLSSettings()
	if err != nil {
		return fmt.Errorf("configuring TLS settings: %w", err)
	}

	//
	//
	// HTTP setup
	//
	// This is the regular router that we expose
	// over our main Addr
	router := h.createRouter(humaMux, humaV2Mux)

	httpServer := &http.Server{
		Addr:        h.cfg.Addr,
		Handler:     router,
		ReadTimeout: types.HTTPTimeout,

		// Long polling should not have any timeout, this is overridden
		// further down the chain
		WriteTimeout: types.HTTPTimeout,
	}

	var httpListener net.Listener

	if tlsConfig != nil {
		httpServer.TLSConfig = tlsConfig
		httpListener, err = tls.Listen("tcp", h.cfg.Addr, tlsConfig)
	} else {
		httpListener, err = new(net.ListenConfig).Listen(context.Background(), "tcp", h.cfg.Addr)
	}

	if err != nil {
		return fmt.Errorf("binding to TCP address: %w", err)
	}

	errorGroup.Go(func() error { return httpServer.Serve(httpListener) })

	log.Info().
		Msgf("listening and serving HTTP on: %s", h.cfg.Addr)

	// Only start debug/metrics server if address is configured
	var debugHTTPServer *http.Server

	var debugHTTPListener net.Listener

	if h.cfg.MetricsAddr != "" {
		debugHTTPListener, err = (&net.ListenConfig{}).Listen(ctx, "tcp", h.cfg.MetricsAddr)
		if err != nil {
			return fmt.Errorf("binding to TCP address: %w", err)
		}

		debugHTTPServer = h.debugHTTPServer()

		errorGroup.Go(func() error { return debugHTTPServer.Serve(debugHTTPListener) })

		log.Info().
			Msgf("listening and serving debug and metrics on: %s", h.cfg.MetricsAddr)
	} else {
		log.Info().Msg("metrics server disabled (metrics_listen_addr is empty)")
	}

	var tailsqlCancel context.CancelFunc

	if tailsqlEnabled {
		if h.cfg.Database.Type != types.DatabaseSqlite {
			//nolint:gocritic // exitAfterDefer: Fatal exits during initialization before servers start
			log.Fatal().
				Str("type", h.cfg.Database.Type).
				Msgf("tailsql only support %q", types.DatabaseSqlite)
		}

		if tailsqlTSKey == "" {
			log.Fatal().Msg("tailsql requires TS_AUTHKEY to be set")
		}

		var tailsqlCtx context.Context

		tailsqlCtx, tailsqlCancel = context.WithCancel(ctx)

		errorGroup.Go(func() error {
			return runTailSQLService(tailsqlCtx, util.TSLogfWrapper(), tailsqlStateDir, h.cfg.Database.Sqlite.Path)
		})
	}

	if h.cfg.SSHRecording.Enabled {
		errorGroup.Go(func() error { return h.runSSHRecorder(ctx) })
	}

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGHUP)

	sigFunc := func(c chan os.Signal) {
		// Wait for a SIGINT or SIGKILL:
		for {
			sig := <-c
			switch sig {
			case syscall.SIGHUP:
				log.Info().
					Str("signal", sig.String()).
					Msg("Received SIGHUP, reloading ACL policy")

				if h.cfg.Policy.IsEmpty() {
					continue
				}

				changes, reloadErr := h.state.ReloadPolicy()
				if reloadErr != nil {
					log.Error().Err(reloadErr).Msgf("reloading policy")
					continue
				}

				h.Change(changes...)

			default:
				info := func(msg string) { log.Info().Msg(msg) }

				log.Info().
					Str("signal", sig.String()).
					Msg("Received signal to stop, shutting down gracefully")

				scheduleCancel()
				h.ephemeralGC.Close()

				// Gracefully shut down servers
				// This case always returns right after using shutdownCtx below,
				// so cancel is called explicitly here rather than deferred:
				// a defer inside this for-loop's case would accumulate if the
				// switch ever gained another path that continues the loop.
				shutdownCtx, cancel := context.WithTimeout(
					context.WithoutCancel(ctx),
					types.HTTPShutdownTimeout,
				)

				if debugHTTPServer != nil {
					info("shutting down debug http server")

					debugErr := debugHTTPServer.Shutdown(shutdownCtx)
					if debugErr != nil {
						log.Error().Err(debugErr).Msg("failed to shutdown prometheus http")
					}
				}

				info("shutting down main http server")

				httpErr := httpServer.Shutdown(shutdownCtx)
				if httpErr != nil {
					log.Error().Err(httpErr).Msg("failed to shutdown http")
				}

				info("closing batcher")
				h.mapBatcher.Close()

				info("waiting for netmap stream to close")
				h.clientStreamsOpen.Wait()

				info("shutting down api server (socket)")

				socketErr := socketServer.Shutdown(shutdownCtx)
				if socketErr != nil {
					log.Error().Err(socketErr).Msg("failed to shutdown socket server")
				}

				if tailsqlCancel != nil {
					info("shutting down tailsql")
					tailsqlCancel()
				}

				// Close network listeners
				info("closing network listeners")

				if debugHTTPListener != nil {
					debugHTTPListener.Close()
				}

				httpListener.Close()

				// Stop listening (and unlink the socket if unix type):
				info("closing socket listener")
				socketListener.Close()

				// Close state connections
				info("closing state and database")

				err = h.state.Close()
				if err != nil {
					log.Error().Err(err).Msg("failed to close state")
				}

				cancel()

				log.Info().
					Msg("Headscale stopped")

				return
			}
		}
	}

	errorGroup.Go(func() error {
		sigFunc(sigc)

		return nil
	})

	err = errorGroup.Wait()
	if err != nil {
		return fmt.Errorf("running error group: %w", err)
	}

	return nil
}

func readOrCreatePrivateKey(path string) (*key.MachinePrivate, error) {
	dir := filepath.Dir(path)

	err := util.EnsureDir(dir)
	if err != nil {
		return nil, fmt.Errorf("ensuring private key directory: %w", err)
	}

	privateKey, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Info().Str("path", path).Msg("no private key file at path, creating...")

		machineKey := key.NewMachine()

		machineKeyStr, marshalErr := machineKey.MarshalText()
		if marshalErr != nil {
			return nil, fmt.Errorf(
				"converting private key to string for saving: %w",
				marshalErr,
			)
		}

		writeErr := os.WriteFile(path, machineKeyStr, privateKeyFileMode)
		if writeErr != nil {
			return nil, fmt.Errorf(
				"saving private key to disk at path %q: %w",
				path,
				writeErr,
			)
		}

		return &machineKey, nil
	} else if err != nil {
		return nil, fmt.Errorf("reading private key file: %w", err)
	}

	trimmedPrivateKey := strings.TrimSpace(string(privateKey))

	var machineKey key.MachinePrivate

	err = machineKey.UnmarshalText([]byte(trimmedPrivateKey))
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	return &machineKey, nil
}

// Change is used to send changes to nodes.
// All change should be enqueued here and empty will be automatically
// ignored.
func (h *Headscale) Change(cs ...change.Change) {
	h.mapBatcher.AddWork(cs...)
}

// HTTPHandler returns an [http.Handler] for the [Headscale] control server.
// The handler serves the Tailscale control protocol including the /key
// endpoint and /ts2021 Noise upgrade path.
func (h *Headscale) HTTPHandler() http.Handler {
	humaMux, _ := apiv1.Handler(h.apiV1Backend())

	humaV2Mux, _ := apiv2.Handler(apiv2.Backend{
		State:  h.state,
		Change: h.Change,
		Cfg:    h.cfg,
	})

	return h.createRouter(humaMux, humaV2Mux)
}

// NoisePublicKey returns the server's Noise protocol public key.
func (h *Headscale) NoisePublicKey() key.MachinePublic {
	return h.noisePrivateKey.Public()
}

// GetState returns the server's state manager for programmatic access
// to users, nodes, policies, and other server state.
func (h *Headscale) GetState() *state.State {
	return h.state
}

// SetServerURLForTest updates the server URL in the configuration.
// This is needed for test servers where the URL is not known until
// the HTTP test server starts.
// It panics when called outside of tests.
func (h *Headscale) SetServerURLForTest(tb testing.TB, url string) {
	tb.Helper()

	h.cfg.ServerURL = url

	// Both providers captured the placeholder URL at construction as the
	// base of their auth URLs, the OIDC one also as the OAuth redirect
	// URL, so the interactive login only works if they follow the update.
	switch provider := h.authProvider.(type) {
	case *AuthProviderOIDC:
		provider.serverURL = url
		provider.oauth2Config.RedirectURL = strings.TrimSuffix(url, "/") + "/oidc/callback"
	case *AuthProviderWeb:
		provider.serverURL = url
	}
}

// StartBatcherForTest initialises and starts the map response batcher.
// It registers a cleanup function on tb to stop the batcher.
// It panics when called outside of tests.
func (h *Headscale) StartBatcherForTest(tb testing.TB) {
	tb.Helper()

	h.mapBatcher = mapper.NewBatcherAndMapper(h.cfg, h.state)
	h.mapBatcher.Start()
	tb.Cleanup(func() { h.mapBatcher.Close() })
}

// MapBatcher returns the map response batcher (for test use).
func (h *Headscale) MapBatcher() *mapper.Batcher {
	return h.mapBatcher
}

// StartDERPForTest hands the state the embedded relay and builds the map
// from the effective settings, as [Headscale.Serve] does.
func (h *Headscale) StartDERPForTest(tb testing.TB) {
	tb.Helper()

	h.state.SetDERPRelay(h.DERPServer)

	err := h.state.LoadDERPMap(tb.Context())
	if err != nil {
		tb.Fatalf("loading DERP map: %v", err)
	}

	if h.DERPServer != nil {
		tb.Cleanup(func() { _ = h.DERPServer.Close() })
	}
}

// StartEphemeralGCForTest starts the ephemeral node garbage collector.
// It registers a cleanup function on tb to stop the collector.
// It panics when called outside of tests.
func (h *Headscale) StartEphemeralGCForTest(tb testing.TB) {
	tb.Helper()

	go h.ephemeralGC.Start()

	tb.Cleanup(func() { h.ephemeralGC.Close() })
}

// SetExtraRecordsForTest serves records as if the extra-records file held
// them, for tests that do not run [Headscale.Serve] and its file watcher.
func (h *Headscale) SetExtraRecordsForTest(records []tailcfg.DNSRecord) {
	h.cfg.SetExtraRecords(records)
	h.Change(change.ExtraRecords())
}

// apiV1Backend is the v1 API's view of the server, including whether the
// console can sign in through the identity provider.
func (h *Headscale) apiV1Backend() apiv1.Backend {
	b := apiv1.Backend{
		State:    h.state,
		Change:   h.Change,
		Cfg:      h.cfg,
		Recorder: h.recorder,
	}

	if provider, ok := h.authProvider.(*AuthProviderOIDC); ok {
		b.ConsoleLogin = &apiv1.ConsoleLogin{
			Provider: provider.ConsoleProvider().Name,
			Path:     ConsoleLoginPath,
		}
	}

	return b
}

// Redirect to our TLS url.
func (h *Headscale) redirect(w http.ResponseWriter, req *http.Request) {
	target := h.cfg.ServerURL + req.URL.RequestURI()
	http.Redirect(w, req, target, http.StatusFound) //nolint:gosec // G710: target prefixed by trusted ServerURL
}

func (h *Headscale) scheduledTasks(ctx context.Context) {
	expireTicker := time.NewTicker(updateInterval)
	defer expireTicker.Stop()

	lastExpiryCheck := time.Unix(0, 0)

	// The DERP refresh interval follows the effective settings, which
	// can change at runtime, so a timer is re-armed after every tick and
	// every settings change instead of a fixed ticker.
	derpTimer := time.NewTimer(h.derpRefreshInterval())
	defer derpTimer.Stop()

	var extraRecordsUpdate <-chan []tailcfg.DNSRecord
	if h.extraRecordMan != nil {
		extraRecordsUpdate = h.extraRecordMan.UpdateCh()
	}

	var (
		haProber     *state.HAHealthProber
		haHealthChan <-chan time.Time
	)
	if h.cfg.Node.Routes.HA.ProbeInterval > 0 {
		haProber = state.NewHAHealthProber(
			h.state,
			h.cfg.Node.Routes.HA,
			h.cfg.ServerURL,
			h.mapBatcher.IsConnected,
		)

		haTicker := time.NewTicker(h.cfg.Node.Routes.HA.ProbeInterval)
		defer haTicker.Stop()

		haHealthChan = haTicker.C

		log.Info().
			Dur("interval", h.cfg.Node.Routes.HA.ProbeInterval).
			Dur("timeout", h.cfg.Node.Routes.HA.ProbeTimeout).
			Msg("HA subnet router health probing enabled")
	}

	var revokedKeyGCChan <-chan time.Time

	if h.cfg.PreAuthKeys.RevokedRetention > 0 {
		revokedKeyTicker := time.NewTicker(time.Hour)
		defer revokedKeyTicker.Stop()

		revokedKeyGCChan = revokedKeyTicker.C
	}

	// OAuth access tokens are short-lived (1h) and re-minted on demand; reap
	// expired rows hourly so the table stays bounded. Console sessions and
	// the audit log ride the same hour.
	accessTokenTicker := time.NewTicker(time.Hour)
	defer accessTokenTicker.Stop()

	// Posture identity is asked of every connected node whose report is
	// stale, and custom attributes with an expiry are swept every minute
	// so a temporary grant ends when it says.
	postureTicker := time.NewTicker(state.PostureCollectionInterval)
	defer postureTicker.Stop()

	// attributeTicker sweeps expired attributes and posture schedule
	// boundaries; both have minute granularity.
	attributeTicker := time.NewTicker(time.Minute)
	defer attributeTicker.Stop()

	lastScheduleCheck := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Info().Caller().Msg("scheduled task worker is shutting down.")
			return

		case <-revokedKeyGCChan:
			h.reapRevokedPreAuthKeys()

		case <-accessTokenTicker.C:
			h.reapExpiredAccessTokens()
			h.reapExpiredSessions()
			h.reapAuditEvents()

		case <-expireTicker.C:
			lastExpiryCheck = h.expireNodesTick(lastExpiryCheck)

		case <-derpTimer.C:
			// The timer also fires while automatic updates are off, so
			// it re-reads the settings; it fetches only when they say so,
			// or when the last fetch failed and the map is missing regions.
			if h.state.EffectiveDERP().AutoUpdate || h.state.DERPFetchFailed() {
				h.refreshDERPMapInBackground(ctx)
			}

			derpTimer.Reset(h.derpRefreshInterval())

		case <-h.state.DERPChanged():
			if !derpTimer.Stop() {
				select {
				case <-derpTimer.C:
				default:
				}
			}

			derpTimer.Reset(h.derpRefreshInterval())

		case records, ok := <-extraRecordsUpdate:
			if !h.applyExtraRecords(records, ok) {
				continue
			}

		case <-haHealthChan:
			haProber.ProbeOnce(ctx, h.Change)

		case <-postureTicker.C:
			h.state.CollectStalePostures(ctx, h.mapBatcher.IsConnected, h.Change)

		case now := <-attributeTicker.C:
			h.expireNodeAttributes()
			h.expireAccess(lastScheduleCheck, now)

			if h.postureBoundaryPassed(lastScheduleCheck, now) {
				h.recompilePostures()
			}

			lastScheduleCheck = now
		}
	}
}

// postureBoundaryPassed reports whether a scheduled posture in use opened
// or closed between the two instants.
func (h *Headscale) postureBoundaryPassed(since, now time.Time) bool {
	next := h.state.NextPostureBoundary(since)

	return !next.IsZero() && !next.After(now)
}

// recompilePostures rebuilds the policy at a schedule boundary and
// publishes the result.
func (h *Headscale) recompilePostures() {
	c, err := h.state.RecompilePostures()
	if err != nil {
		log.Error().Err(err).Msg("recompiling postures at a schedule boundary")

		return
	}

	if !c.IsEmpty() {
		h.Change(c)
	}
}

// expireAccess ends the temporary rules and memberships that ran out
// between the two instants and publishes the rebuilt policy.
func (h *Headscale) expireAccess(since, now time.Time) {
	c, err := h.state.ExpireAccess(since, now)
	if err != nil {
		log.Error().Err(err).Msg("ending expired temporary access")

		return
	}

	if !c.IsEmpty() {
		h.Change(c)
	}
}

// expireNodeAttributes drops custom posture attributes past their expiry
// and publishes the recompute.
func (h *Headscale) expireNodeAttributes() {
	c, err := h.state.ExpireNodeAttributes(time.Now())
	if err != nil {
		log.Error().Err(err).Msg("expiring node attributes")

		return
	}

	if !c.IsEmpty() {
		h.Change(c)
	}
}

// postureConnectDelay leaves a freshly connected client time to read its
// first map before a c2n request follows it.
const postureConnectDelay = 2 * time.Second

// collectPostureOnConnect asks a node that just connected for its
// identity when the setting is on and its report is missing or a day
// old, so a new machine's serial shows up without waiting for the cycle.
func (h *Headscale) collectPostureOnConnect(ctx context.Context, nodeID types.NodeID) {
	if !h.state.Settings().PostureIdentityOn {
		return
	}

	node, ok := h.state.GetNodeByID(nodeID)
	if !ok || node.Posture().Valid() && time.Since(node.Posture().CollectedAt()) < state.PostureMaxAge {
		return
	}

	// The stream's context ends with the stream; the collection outlives
	// the map request that started it.
	ctx = context.WithoutCancel(ctx)

	time.AfterFunc(postureConnectDelay, func() {
		_, c, err := h.state.CollectPosture(ctx, nodeID, h.mapBatcher.IsConnected(nodeID), h.Change)
		if err != nil {
			log.Debug().Err(err).Uint64(zf.NodeID, nodeID.Uint64()).Msg("posture collection on connect failed")

			return
		}

		if !c.IsEmpty() {
			h.Change(c)
		}
	})
}

// reapRevokedPreAuthKeys destroys pre-auth keys that were revoked more than
// cfg.PreAuthKeys.RevokedRetention ago.
func (h *Headscale) reapRevokedPreAuthKeys() {
	cutoff := time.Now().Add(-h.cfg.PreAuthKeys.RevokedRetention)

	reaped, err := h.state.DestroyRevokedPreAuthKeysBefore(cutoff)
	if err != nil {
		log.Error().Err(err).Msg("reaping revoked pre-auth keys")
	} else if reaped > 0 {
		log.Info().Int("count", reaped).Msg("reaped revoked pre-auth keys")
	}
}

// reapExpiredAccessTokens destroys OAuth access tokens that are past their expiry.
func (h *Headscale) reapExpiredAccessTokens() {
	reaped, err := h.state.DeleteExpiredAccessTokens(time.Now())
	if err != nil {
		log.Error().Err(err).Msg("reaping expired oauth access tokens")
	} else if reaped > 0 {
		log.Debug().Int64("count", reaped).Msg("reaped expired oauth access tokens")
	}
}

// reapExpiredSessions removes console sessions past their expiry.
func (h *Headscale) reapExpiredSessions() {
	reaped, err := h.state.DeleteExpiredSessions(time.Now())
	if err != nil {
		log.Error().Err(err).Msg("reaping expired console sessions")
	} else if reaped > 0 {
		log.Debug().Int64("count", reaped).Msg("reaped expired console sessions")
	}
}

// reapAuditEvents applies cfg.Audit.Retention; zero keeps everything.
func (h *Headscale) reapAuditEvents() {
	if h.cfg.Audit.Retention <= 0 {
		return
	}

	reaped, err := h.state.DeleteAuditEventsBefore(time.Now().Add(-h.cfg.Audit.Retention))
	if err != nil {
		log.Error().Err(err).Msg("reaping audit events")
	} else if reaped > 0 {
		log.Info().Int64("count", reaped).Msg("reaped audit events past retention")
	}
}

// expireNodesTick runs one pass of node expiry since lastExpiryCheck,
// sending a change for every node that expired, and returns the checkpoint
// to pass in on the next tick.
func (h *Headscale) expireNodesTick(lastExpiryCheck time.Time) time.Time {
	lastExpiryCheck, expiredNodeChanges, changed := h.state.ExpireExpiredNodes(lastExpiryCheck)

	if changed {
		log.Trace().Interface("changes", expiredNodeChanges).Msgf("expiring nodes")

		// Send the changes directly since they're already in the new format
		for _, nodeChange := range expiredNodeChanges {
			h.Change(nodeChange)
		}
	}

	return lastExpiryCheck
}

// derpRefreshInterval is how long until the map sources are refetched:
// the effective settings' frequency, or a day's wait while auto update
// is off, after which the scheduler re-reads the settings without
// fetching.
func (h *Headscale) derpRefreshInterval() time.Duration {
	if h.state.DERPFetchFailed() {
		return derpRefreshRetry
	}

	settings := h.state.EffectiveDERP()
	if !settings.AutoUpdate || settings.UpdateFrequency < types.DERPMinUpdateFrequency {
		return derpRefreshIdle
	}

	return settings.UpdateFrequency
}

const (
	derpRefreshIdle  = 24 * time.Hour
	derpRefreshRetry = 5 * time.Minute
)

// refreshDERPMapInBackground runs [Headscale.refreshDERPMap] off the
// scheduler goroutine, which must keep serving expiry and health ticks
// while a fetch backs off, and skips the run while one is in flight.
func (h *Headscale) refreshDERPMapInBackground(ctx context.Context) {
	if !h.derpRefreshing.CompareAndSwap(false, true) {
		return
	}

	go func() {
		defer h.derpRefreshing.Store(false)

		err := h.refreshDERPMap(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to build new DERPMap, retrying later")
		}
	}()
}

// refreshDERPMap refetches the map sources, retrying with backoff until
// ctx ends, and pushes the map when it changed. It returns an error
// rather than applying a partial map when the fetch keeps failing.
func (h *Headscale) refreshDERPMap(ctx context.Context) error {
	log.Info().Msg("fetching DERPMap updates")

	changed, err := backoff.Retry(ctx, func() (bool, error) {
		return h.state.RefreshDERPMap(ctx)
	}, backoff.WithBackOff(backoff.NewExponentialBackOff()))
	if err != nil {
		return fmt.Errorf("fetching DERP map: %w", err)
	}

	if changed {
		h.Change(change.DERPMap())
	}

	return nil
}

// applyExtraRecords updates the served extra DNS records and notifies nodes
// of the change. ok mirrors the extraRecordMan update channel's closed
// state; when false there is nothing to apply and it returns false.
func (h *Headscale) applyExtraRecords(records []tailcfg.DNSRecord, ok bool) bool {
	if !ok {
		return false
	}

	h.cfg.SetExtraRecords(records)

	h.Change(change.ExtraRecords())

	return true
}

// ensureUnixSocketIsAbsent will check if the given path for headscales unix socket is clear
// and will remove it if it is not.
func (h *Headscale) ensureUnixSocketIsAbsent() error {
	// File does not exist, all fine
	_, err := os.Stat(h.cfg.UnixSocket)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	err = os.Remove(h.cfg.UnixSocket)
	if err != nil {
		return fmt.Errorf("removing existing unix socket %q: %w", h.cfg.UnixSocket, err)
	}

	return nil
}

func (h *Headscale) createRouter(apiV1Mux, apiV2Mux http.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Use(metrics.Collector(metrics.CollectorOpts{
		Host:  false,
		Proto: true,
		Skip: func(r *http.Request) bool {
			return r.Method == http.MethodOptions
		},
	}))
	r.Use(middleware.RequestID)

	if h.realIPMiddleware != nil {
		r.Use(h.realIPMiddleware)
	}

	r.Use(middleware.RequestLogger(&zerologRequestLogger{}))
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	// TS2021 accepts both the native client's HTTP POST upgrade and the
	// browser/WASM client's WebSocket GET upgrade; NoiseUpgradeHandler
	// dispatches on the Upgrade header, not the method. Registering GET as
	// well keeps the router from rejecting the WebSocket handshake with 405.
	r.Get(ts2021UpgradePath, h.NoiseUpgradeHandler)
	r.Post(ts2021UpgradePath, h.NoiseUpgradeHandler)

	r.Get("/robots.txt", h.RobotsHandler)
	r.Get("/health", h.HealthHandler)
	r.Get("/version", h.VersionHandler)
	r.Get("/key", h.KeyHandler)
	r.Get("/register/{auth_id}", h.authProvider.RegisterHandler)
	r.Get("/auth/{auth_id}", h.authProvider.AuthHandler)

	if provider, ok := h.authProvider.(*AuthProviderOIDC); ok {
		r.Get("/oidc/callback", provider.OIDCCallbackHandler)
		r.Get(ConsoleLoginPath, provider.ConsoleLoginHandler)
		r.Post("/register/confirm/{auth_id}", provider.RegisterConfirmHandler)
	}

	r.Get("/apple", h.AppleConfigMessage)
	r.Get("/apple/{platform}", h.ApplePlatformConfig)
	r.Get("/windows", h.WindowsConfigMessage)

	r.Post("/verify", h.VerifyHandler)

	// The relay routes are always mounted: the handler answers 404 while
	// the settings keep the embedded relay off, so turning it on at
	// runtime needs no restart.
	if h.DERPServer != nil {
		r.HandleFunc("/derp", h.DERPServer.DERPHandler)
		r.HandleFunc("/derp/probe", derpServer.DERPProbeHandler)
		r.HandleFunc("/derp/latency-check", derpServer.DERPProbeHandler)
	}

	r.Handle("/bootstrap-dns", derpServer.NewBootstrapDNS(h.state.DERPMap, h.cfg.ServerURL))

	// Auth is enforced inside each Huma mux per-operation, so the whole API
	// mounts as one handler per version: operations need an API key while the
	// OpenAPI document and docs UI stay public. v1 is the headscale-native admin
	// API; v2 is Headscale's v2 API, which ports some endpoints from Tailscale.
	r.Route("/api", func(r chi.Router) {
		r.Handle("/v1/*", serveHumaMux(apiV1Mux))
		r.Handle("/v2/*", serveHumaMux(apiV2Mux))
	})
	// Ping response endpoint: receives HEAD from clients responding
	// to a [tailcfg.PingRequest]. The unguessable ping ID serves as authentication.
	r.Head("/machine/ping-response", h.PingResponseHandler)

	// The admin console is a static bundle embedded at build time; it
	// authenticates against /api/v1 with an API key, so nothing here is
	// privileged. See package web.
	r.Handle(strings.TrimSuffix(web.Prefix, "/"), web.Handler())
	r.Handle(web.Prefix+"*", web.Handler())

	r.Get("/favicon.ico", FaviconHandler)
	r.Get("/", BlankHandler)

	return r
}

func (h *Headscale) getTLSSettings() (*tls.Config, error) {
	tlsEnabled := h.cfg.TLS.LetsEncrypt.Hostname != "" || h.cfg.TLS.CertPath != ""
	if tlsEnabled && !strings.HasPrefix(h.cfg.ServerURL, "https://") {
		log.Warn().Msg("listening with TLS but ServerURL does not start with https://")
	} else if !tlsEnabled && !strings.HasPrefix(h.cfg.ServerURL, "http://") {
		log.Warn().Msg("listening without TLS but ServerURL does not start with http://")
	}

	if h.cfg.TLS.LetsEncrypt.Hostname != "" {
		certManager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(h.cfg.TLS.LetsEncrypt.Hostname),
			Cache:      autocert.DirCache(h.cfg.TLS.LetsEncrypt.CacheDir),
			Client: &acme.Client{
				DirectoryURL: h.cfg.ACMEURL,
				HTTPClient: &http.Client{
					Transport: &acmeLogger{
						rt: http.DefaultTransport,
					},
				},
			},
			Email: h.cfg.ACMEEmail,
		}

		switch h.cfg.TLS.LetsEncrypt.ChallengeType {
		case types.TLSALPN01ChallengeType:
			// Configuration via autocert with TLS-ALPN-01 (https://tools.ietf.org/html/rfc8737)
			// The RFC requires that the validation is done on port 443; in other words, headscale
			// must be reachable on port 443.
			return certManager.TLSConfig(), nil

		case types.HTTP01ChallengeType:
			// Configuration via autocert with HTTP-01. This requires listening on
			// port 80 for the certificate validation in addition to the headscale
			// service, which can be configured to run on any other port.
			server := &http.Server{
				Addr:        h.cfg.TLS.LetsEncrypt.Listen,
				Handler:     certManager.HTTPHandler(http.HandlerFunc(h.redirect)),
				ReadTimeout: types.HTTPTimeout,
			}

			go func() {
				err := server.ListenAndServe()
				log.Fatal().
					Caller().
					Err(err).
					Msg("failed to set up a HTTP server")
			}()

			return certManager.TLSConfig(), nil

		default:
			return nil, errUnsupportedLetsEncryptChallengeType
		}
	}

	if h.cfg.TLS.CertPath == "" {
		return nil, nil //nolint:nilnil // intentional: no TLS config when neither LetsEncrypt nor a cert path is set
	}

	tlsConfig := &tls.Config{
		NextProtos:   []string{"http/1.1"},
		Certificates: make([]tls.Certificate, 1),
		MinVersion:   tls.VersionTLS12,
	}

	cert, err := tls.LoadX509KeyPair(h.cfg.TLS.CertPath, h.cfg.TLS.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("loading TLS key pair: %w", err)
	}

	tlsConfig.Certificates[0] = cert

	return tlsConfig, nil
}

// Provide some middleware that can inspect the ACME/autocert https calls
// and log when things are failing.
type acmeLogger struct {
	rt http.RoundTripper
}

// RoundTrip will log when ACME/autocert failures happen either when err != nil OR
// when http status codes indicate a failure has occurred.
func (l *acmeLogger) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := l.rt.RoundTrip(req)
	if err != nil {
		log.Error().Err(err).Str("url", req.URL.String()).Msg("acme request failed")
		return nil, fmt.Errorf("performing ACME HTTP request: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		log.Error().
			Int("status_code", resp.StatusCode).
			Str("url", req.URL.String()).
			Bytes("body", body).
			Msg("acme request returned error")
	}

	return resp, nil
}

// [zerologRequestLogger] implements chi's [middleware.LogFormatter]
// to route HTTP request logs through zerolog.
type zerologRequestLogger struct{}

func (z *zerologRequestLogger) NewLogEntry(
	r *http.Request,
) middleware.LogEntry {
	return &zerologLogEntry{
		method: r.Method,
		path:   r.URL.Path,
		proto:  r.Proto,
		remote: r.RemoteAddr,
	}
}

type zerologLogEntry struct {
	method string
	path   string
	proto  string
	remote string
}

func (e *zerologLogEntry) Write(
	status, bytes int,
	_ http.Header,
	elapsed time.Duration,
	_ any,
) {
	log.Info().
		Str("method", e.method).
		Str("path", e.path).
		Str("proto", e.proto).
		Str("remote", e.remote).
		Int("status", status).
		Int("bytes", bytes).
		Dur("elapsed", elapsed).
		Msg("http request")
}

func (e *zerologLogEntry) Panic(
	v any,
	stack []byte,
) {
	log.Error().
		Interface("panic", v).
		Bytes("stack", stack).
		Msg("http handler panic")
}
