package agent

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/aislopware/slopscale/flowd/dnsproxy"
	"github.com/aislopware/slopscale/flowd/rollup"
	"github.com/aislopware/slopscale/hscontrol/traffic"
)

// probeFailuresBeforeError is how many probes in a row must fail before a
// resolver that has answered before is reported broken; one lost probe
// should not take it out of every client's DNS.
const probeFailuresBeforeError = 2

// maxProbeTime bounds one probe, as long as a client's question may take.
const maxProbeTime = 5 * time.Second

// dnsRunner keeps the logging resolver running as the configuration and
// the node's addresses ask, and reports it healthy only while it answers.
type dnsRunner struct {
	agent    *Agent
	server   *dnsproxy.Server
	running  []string // the plan key the server runs with
	failures int      // probes failed in a row
	proven   bool     // a probe has succeeded since the server started
}

// runDNS runs the logging resolver while the configuration asks for it,
// on the node's current tailnet addresses.
func (a *Agent) runDNS(ctx context.Context) {
	r := &dnsRunner{agent: a}
	defer r.stop()

	for ctx.Err() == nil {
		cfg, changed := a.current()
		r.round(ctx, cfg)

		var died <-chan struct{}
		if r.server != nil {
			died = r.server.Died()
		}

		select {
		case <-ctx.Done():
		case <-changed:
		case <-time.After(a.opts.dnsCheckEvery):
		case <-died:
			// A listener stopped for good; start over on the next round,
			// a moment later so a failure that repeats does not spin.
			err := r.server.Err()
			r.stop()
			r.setStatus(err)
			sleep(ctx, time.Second)
		}
	}
}

func (r *dnsRunner) round(ctx context.Context, cfg traffic.Config) {
	if !cfg.DNS {
		r.stop()
		r.agent.setStatus(func(s *traffic.Status) { s.DNS = traffic.Collector{} })

		return
	}

	plan, err := r.agent.dnsPlan(ctx, cfg)

	switch {
	case err != nil && r.server == nil:
		r.setStatus(err)

		return
	case err != nil:
		// The node's addresses or resolv.conf could not be read this
		// time; the resolver keeps serving what it served.
		r.agent.log.Warn("checking the resolver's addresses failed; it keeps running", "err", err)
	case r.server == nil || !slices.Equal(plan.key, r.running):
		r.stop()

		server, err := r.agent.startDNS(plan)
		if err != nil {
			r.setStatus(err)

			return
		}

		r.server, r.running = server, plan.key
	}

	r.setStatus(r.probe(ctx))
}

// probe asks the upstreams through the running resolver. A resolver that
// never answered is broken at the first failure, one that has at
// [probeFailuresBeforeError] in a row.
func (r *dnsRunner) probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, min(r.agent.opts.dnsCheckEvery, maxProbeTime))
	defer cancel()

	err := r.server.Probe(ctx)
	if err == nil {
		r.failures, r.proven = 0, true

		return nil
	}

	r.failures++

	if r.proven && r.failures < probeFailuresBeforeError {
		return nil
	}

	return fmt.Errorf("the upstream resolvers are not answering: %w", err)
}

func (r *dnsRunner) setStatus(err error) {
	r.agent.setStatus(func(s *traffic.Status) { s.DNS = traffic.Collector{Enabled: true, Error: errString(err)} })
}

func (r *dnsRunner) stop() {
	if r.server != nil {
		_ = r.server.Close()
		r.server, r.running = nil, nil
		r.failures, r.proven = 0, false
	}

	r.agent.mu.Lock()
	r.agent.dnsListen = nil
	r.agent.mu.Unlock()
}

// dnsPlan is where the resolver should listen and where it should forward,
// with a key that changes when either does.
type dnsPlan struct {
	key       []string
	listen    []netip.AddrPort
	upstreams []string
	bootstrap []string
}

func (a *Agent) dnsPlan(ctx context.Context, cfg traffic.Config) (dnsPlan, error) {
	status, err := a.opts.Local.StatusWithoutPeers(ctx)
	if err != nil {
		return dnsPlan{}, fmt.Errorf("reading the node's tailnet addresses: %w", err)
	}

	if status.Self == nil || len(status.Self.TailscaleIPs) == 0 {
		return dnsPlan{}, errors.New("the node has no tailnet address yet")
	}

	plan := dnsPlan{listen: make([]netip.AddrPort, 0, len(status.Self.TailscaleIPs))}
	for _, ip := range status.Self.TailscaleIPs {
		addr := netip.AddrPortFrom(ip, a.opts.dnsPort)
		plan.listen = append(plan.listen, addr)
		plan.key = append(plan.key, addr.String())
	}

	// The system's resolvers are the upstreams when the server names
	// none, and otherwise look up the names of DoH upstreams.
	system, sysErr := dnsproxy.SystemUpstreams(a.opts.ResolvConf...)

	plan.upstreams = cfg.Upstreams
	if len(plan.upstreams) == 0 {
		if sysErr != nil {
			return dnsPlan{}, sysErr
		}

		plan.upstreams = system
	} else {
		plan.bootstrap = system
	}

	plan.key = append(append(plan.key, plan.upstreams...), plan.bootstrap...)

	return plan, nil
}

func (a *Agent) startDNS(plan dnsPlan) (*dnsproxy.Server, error) {
	server, err := dnsproxy.Start(dnsproxy.Config{
		Listen:    plan.listen,
		Upstreams: plan.upstreams,
		Bootstrap: plan.bootstrap,
		Allowed:   a.opts.allowDNS,
		OnQuery: func(src netip.Addr, name string, failed bool) {
			a.table.AddQuery(rollup.Bucket(time.Now()), src, name, failed)
		},
		OnAnswer: func(src netip.Addr, name string, addrs []netip.Addr, ttl time.Duration) {
			a.resolver.PutDNS(src, name, addrs, ttl, time.Now())
		},
		Logger: a.log,
	})
	if err != nil {
		return nil, fmt.Errorf("starting the resolver: %w", err)
	}

	for _, failure := range server.Failed() {
		a.log.Warn("the resolver is not answering on one of the node's addresses", "err", failure)
	}

	a.log.Info("resolver answering", "listen", server.Addrs(), "upstreams", plan.upstreams)

	a.mu.Lock()
	a.dnsListen = server.Addrs()
	a.mu.Unlock()

	return server, nil
}
