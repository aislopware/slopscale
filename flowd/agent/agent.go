// Package agent runs slopscale-flowd: it follows the gateway's connections
// and DNS questions, rolls them up per minute and delivers the rollups to
// the slopscale server that coordinates the gateway's tailnet.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/flowd/capture"
	"github.com/aislopware/slopscale/flowd/conntrack"
	"github.com/aislopware/slopscale/flowd/names"
	"github.com/aislopware/slopscale/flowd/rollup"
	"github.com/aislopware/slopscale/flowd/spool"
	"github.com/aislopware/slopscale/flowd/tailnet"
	"github.com/aislopware/slopscale/flowd/upload"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/appctype"
)

const (
	configFile            = "config.json"
	defaultReportInterval = 60
	minReportInterval     = 10
	maxReportInterval     = 600
	dumpInterval          = 30 * time.Second
	restartBackoff        = 30 * time.Second
	recheckInterval       = 30 * time.Second
	appConnectorInterval  = time.Minute
	shutdownFlushTimeout  = 5 * time.Second
	dnsPort               = 53
	hostedControlHost     = "controlplane.tailscale.com"
)

// DefaultResolvConf are the files the resolver's upstreams come from when
// the server names none: systemd-resolved's list of real upstreams first.
var DefaultResolvConf = []string{"/run/systemd/resolve/resolv.conf", "/etc/resolv.conf"}

// LocalAPI is what the agent asks of the gateway's tailscaled;
// tailscale.com/client/local.Client implements it.
type LocalAPI interface {
	IDToken(ctx context.Context, aud string) (*tailcfg.TokenResponse, error)
	StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error)
	GetPrefs(ctx context.Context) (*ipn.Prefs, error)
	GetAppConnectorRouteInfo(ctx context.Context) (appctype.RouteInfo, error)
}

// Options configure the agent.
type Options struct {
	Local LocalAPI
	// Server is the slopscale URL; empty takes the node's control URL.
	Server string
	// Interface is the tailnet interface the SNI capture listens on.
	Interface string
	// StateDir holds the spool, the instance id and the last config.
	StateDir string
	// SpoolBytes bounds the spool.
	SpoolBytes int64
	// ResolvConf are the files to read the resolver's upstreams from
	// when the server names none.
	ResolvConf []string
	Version    string
	HTTPClient *http.Client
	Logger     *slog.Logger

	// dnsPort and allowDNS let tests run the resolver on loopback;
	// dumpEvery and dnsCheckEvery let them read the table and check the
	// resolver more often.
	dnsPort       uint16
	allowDNS      func(netip.Addr) bool
	dumpEvery     time.Duration
	dnsCheckEvery time.Duration
}

// Agent is a running agent.
type Agent struct {
	opts     Options
	log      *slog.Logger
	table    *rollup.Table
	resolver *names.Resolver
	spool    *spool.Spool
	uploader *upload.Uploader

	mu        sync.Mutex
	config    traffic.Config
	changed   chan struct{} // closed and replaced on every config change
	status    traffic.Status
	dnsListen []netip.AddrPort
	userspace bool // tailscaled forwards in userspace, past connection tracking
}

// Run runs the agent until ctx ends, then flushes what it holds.
func Run(ctx context.Context, opts Options) error {
	a, err := newAgent(ctx, opts)
	if err != nil {
		return err
	}

	return a.run(ctx)
}

func newAgent(ctx context.Context, opts Options) (*Agent, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}

	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{}
	}

	if len(opts.ResolvConf) == 0 {
		opts.ResolvConf = DefaultResolvConf
	}

	if opts.dnsPort == 0 {
		opts.dnsPort = dnsPort
	}

	if opts.allowDNS == nil {
		opts.allowDNS = tailnet.Contains
	}

	if opts.dumpEvery == 0 {
		opts.dumpEvery = dumpInterval
	}

	if opts.dnsCheckEvery == 0 {
		opts.dnsCheckEvery = recheckInterval
	}

	server, err := serverURL(ctx, opts)
	if err != nil {
		return nil, err
	}

	sp, err := spool.Open(filepath.Join(opts.StateDir, "spool"), opts.SpoolBytes)
	if err != nil {
		return nil, err
	}

	a := &Agent{
		opts:     opts,
		log:      opts.Logger,
		table:    rollup.NewTable(),
		resolver: names.NewResolver(),
		spool:    sp,
		config:   loadConfig(opts.StateDir),
		changed:  make(chan struct{}),
	}
	a.status.Conntrack.Enabled = true

	tokens := upload.NewTokens(func(ctx context.Context) (string, error) {
		resp, err := opts.Local.IDToken(ctx, traffic.Audience)
		if err != nil {
			return "", fmt.Errorf("asking tailscaled: %w", err)
		}

		return resp.IDToken, nil
	})
	a.uploader = upload.New(server, opts.HTTPClient, tokens, sp, a.applyConfig, a.log)

	if recovered := sp.Recovered(); recovered != nil {
		a.log.Warn("the spool state was unreadable; reporting as a new instance", "err", recovered)
	}

	a.log.Info("agent starting", "server", server, "instance", sp.Instance(), "spooled", sp.Len())

	return a, nil
}

// serverURL is the configured server or the node's control URL; the
// hosted control plane is refused, it has no use for these reports.
func serverURL(ctx context.Context, opts Options) (string, error) {
	if opts.Server != "" {
		return opts.Server, nil
	}

	prefs, err := opts.Local.GetPrefs(ctx)
	if err != nil {
		return "", fmt.Errorf("reading tailscaled's control URL: %w", err)
	}

	if prefs.ControlURL == "" || strings.Contains(prefs.ControlURL, hostedControlHost) {
		return "", errors.New("the node does not use a slopscale control server; pass --server")
	}

	return prefs.ControlURL, nil
}

func loadConfig(dir string) traffic.Config {
	cfg := traffic.Config{SNI: true, ReportInterval: defaultReportInterval}

	raw, err := os.ReadFile(filepath.Join(dir, configFile))
	if err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}

	return normaliseConfig(cfg)
}

func normaliseConfig(cfg traffic.Config) traffic.Config {
	if cfg.ReportInterval == 0 {
		cfg.ReportInterval = defaultReportInterval
	}

	cfg.ReportInterval = min(max(cfg.ReportInterval, minReportInterval), maxReportInterval)

	return cfg
}

// applyConfig takes the configuration from a server response.
func (a *Agent) applyConfig(cfg traffic.Config) {
	cfg = normaliseConfig(cfg)

	a.mu.Lock()
	defer a.mu.Unlock()

	if configEqual(cfg, a.config) {
		return
	}

	a.log.Info("configuration changed", "sni", cfg.SNI, "dns", cfg.DNS,
		"upstreams", cfg.Upstreams, "report_interval", cfg.ReportInterval)
	a.config = cfg
	close(a.changed)
	a.changed = make(chan struct{})

	raw, err := json.Marshal(cfg)
	if err == nil {
		err = os.WriteFile(filepath.Join(a.opts.StateDir, configFile), raw, 0o600)
	}

	if err != nil {
		a.log.Warn("saving the configuration failed", "err", err)
	}
}

func configEqual(x, y traffic.Config) bool {
	return x.SNI == y.SNI && x.DNS == y.DNS && x.ReportInterval == y.ReportInterval && slices.Equal(x.Upstreams, y.Upstreams)
}

// current returns the configuration and a channel closed when it changes.
func (a *Agent) current() (traffic.Config, <-chan struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.config, a.changed
}

func (a *Agent) setStatus(update func(*traffic.Status)) {
	a.mu.Lock()
	update(&a.status)
	a.mu.Unlock()
}

func (a *Agent) run(ctx context.Context) error {
	workers, stop := context.WithCancel(ctx)
	defer stop()

	var wg sync.WaitGroup

	wg.Go(func() { a.uploader.Run(workers) })
	wg.Go(func() { a.runConntrack(workers) })
	wg.Go(func() { a.runAppConnector(workers) })
	wg.Go(func() { a.runSNI(workers) })
	wg.Go(func() { a.runDNS(workers) })
	wg.Go(func() { a.runModeChecks(workers) })

	a.runReports(ctx)
	stop()
	wg.Wait()

	// Whatever is still open goes to the spool, and gets one chance to
	// leave before the process does.
	a.report(math.MaxInt64)

	flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownFlushTimeout)
	defer cancel()

	if _, err := a.uploader.Drain(flush); err != nil {
		a.log.Warn("could not deliver everything before stopping; it stays spooled", "err", err)
	}

	return nil
}

func (a *Agent) runReports(ctx context.Context) {
	for {
		cfg, changed := a.current()
		timer := time.NewTimer(time.Duration(cfg.ReportInterval) * time.Second)

		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-changed:
			timer.Stop()

			continue
		case <-timer.C:
		}

		a.report(a.bucket())
	}
}

// bucket is the current bucket by the server's clock, so a gateway whose
// clock is off neither files traffic under minutes the server takes for
// the future nor under ones it has already closed.
func (a *Agent) bucket() int64 {
	return rollup.Bucket(time.Now().Add(a.uploader.Skew()))
}

// onDelta files a connection's traffic under the current bucket and the
// best name known for its destination.
func (a *Agent) onDelta(d conntrack.Delta) {
	now := time.Now()
	host, source := a.resolver.Lookup(d.Conn, now)

	a.table.AddFlow(rollup.FlowKey{
		Bucket:     a.bucket(),
		Src:        d.Conn.Src.Addr(),
		Dst:        d.Conn.Dst.Addr(),
		Proto:      d.Conn.Proto,
		Port:       servicePort(d.Conn),
		Host:       host,
		HostSource: source,
	}, d.Counters)
}

// servicePort is the destination port of protocols that have ports.
func servicePort(c names.Conn) uint16 {
	const (
		dccp    = 33
		sctp    = 132
		udpLite = 136
		tcp     = 6
		udp     = 17
	)

	switch c.Proto {
	case tcp, udp, dccp, sctp, udpLite:
		return c.Dst.Port()
	default:
		return 0
	}
}

// supervise runs job until ctx ends, restarting it after a failure and
// recording the failure with fail (nil error on each start).
func (a *Agent) supervise(ctx context.Context, name string, fail func(error), job func(context.Context) error) {
	for ctx.Err() == nil {
		fail(nil)

		err := job(ctx)
		if ctx.Err() != nil {
			return
		}

		if err == nil {
			err = errors.New("stopped unexpectedly")
		}

		a.log.Error(name+" failed; restarting", "err", err, "retry_in", restartBackoff)
		fail(err)

		sleep(ctx, restartBackoff)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func (a *Agent) runConntrack(ctx context.Context) {
	collector := &conntrack.Collector{Interval: a.opts.dumpEvery, Sink: a.onDelta, Log: a.log}

	a.supervise(ctx, "connection tracking", func(err error) {
		a.setStatus(func(s *traffic.Status) { s.Conntrack.Error = errString(err) })
	}, collector.Run)
}

func (a *Agent) runAppConnector(ctx context.Context) {
	for {
		a.refreshAppConnector(ctx)

		select {
		case <-ctx.Done():
			return
		case <-time.After(appConnectorInterval):
		}
	}
}

func (a *Agent) refreshAppConnector(ctx context.Context) {
	prefs, err := a.opts.Local.GetPrefs(ctx)
	if err != nil {
		a.setStatus(func(s *traffic.Status) { s.AppConnector.Error = errString(err) })

		return
	}

	if !prefs.AppConnector.Advertise {
		a.resolver.SetAppConnector(nil)
		a.setStatus(func(s *traffic.Status) { s.AppConnector = traffic.Collector{} })

		return
	}

	info, err := a.opts.Local.GetAppConnectorRouteInfo(ctx)
	if err == nil {
		a.resolver.SetAppConnector(info.Domains)
	}

	a.setStatus(func(s *traffic.Status) {
		s.AppConnector = traffic.Collector{Enabled: true, Error: errString(err)}
	})
}

// runSNI runs the handshake capture while the configuration asks for it.
func (a *Agent) runSNI(ctx context.Context) {
	for ctx.Err() == nil {
		cfg, changed := a.current()
		a.setStatus(func(s *traffic.Status) { s.SNI = traffic.Collector{Enabled: cfg.SNI} })

		if !cfg.SNI {
			select {
			case <-ctx.Done():
			case <-changed:
			}

			continue
		}

		run, stop := context.WithCancel(ctx)

		go func() {
			select {
			case <-changed:
				stop()
			case <-run.Done():
			}
		}()

		processor := capture.NewProcessor(func(h capture.Hello) {
			a.resolver.PutSNI(h.Conn, h.Hello.ServerName, h.Hello.ECH, time.Now())
		})

		a.supervise(run, "handshake capture", func(err error) {
			a.setStatus(func(s *traffic.Status) { s.SNI.Error = errString(err) })
		}, func(ctx context.Context) error {
			return capture.Run(ctx, a.opts.Interface, processor.Packet)
		})

		stop()
	}
}
