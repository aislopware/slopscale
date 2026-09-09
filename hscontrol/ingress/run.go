package ingress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

// Node is what an ingress node needs to join the tailnet and listen.
type Node struct {
	// ControlURL is the slopscale server.
	ControlURL string
	// AuthKey registers the node; it must carry [types.FunnelIngressTag].
	AuthKey string
	// Hostname is the node's name.
	Hostname string
	// StateDir keeps the node's keys between runs.
	StateDir string
	// ListenAddrs are the public addresses to accept TLS on.
	ListenAddrs []string
	// Logf receives the tailnet client's logs; nil discards them.
	Logf logger.Logf
}

// ErrNoListenAddrs is returned when the node has nothing to listen on.
var ErrNoListenAddrs = errors.New("ingress needs at least one listen address")

// Run joins the tailnet and delivers connections from the listen
// addresses until ctx ends. ready, when non-nil, is told the bound
// addresses once every listener is open, so a caller may listen on port
// 0 and learn the port.
func Run(ctx context.Context, node Node, ready func(addrs []net.Addr)) error {
	if len(node.ListenAddrs) == 0 {
		return ErrNoListenAddrs
	}

	logf := node.Logf
	if logf == nil {
		logf = logger.Discard
	}

	srv := &tsnet.Server{
		Dir:        node.StateDir,
		Hostname:   node.Hostname,
		ControlURL: node.ControlURL,
		AuthKey:    node.AuthKey,
		Logf:       logf,
	}
	defer srv.Close()

	status, err := srv.Up(ctx)
	if err != nil {
		return fmt.Errorf("joining the tailnet: %w", err)
	}

	lc, err := srv.LocalClient()
	if err != nil {
		return fmt.Errorf("local client: %w", err)
	}

	proxy := New(srv.Dial, NewStatusResolver(lc.Status))

	listeners, err := listenAll(ctx, node.ListenAddrs)
	if err != nil {
		return err
	}

	addrs := make([]net.Addr, 0, len(listeners))
	for _, ln := range listeners {
		addrs = append(addrs, ln.Addr())
	}

	if ready != nil {
		ready(addrs)
	}

	e := log.Info().Str("hostname", node.Hostname)
	e = e.Strs("listen", addrStrings(addrs))
	e = e.Strs("addresses", ipStrings(status.TailscaleIPs))
	e.Msg("Funnel ingress listening")

	var g errgroup.Group

	for _, ln := range listeners {
		g.Go(func() error { return proxy.Serve(ctx, ln) })
	}

	err = g.Wait()
	if err != nil {
		return fmt.Errorf("serving: %w", err)
	}

	return nil
}

// listenAll opens every address, or none.
func listenAll(ctx context.Context, addrs []string) ([]net.Listener, error) {
	var lc net.ListenConfig

	listeners := make([]net.Listener, 0, len(addrs))

	for _, addr := range addrs {
		ln, err := lc.Listen(ctx, "tcp", addr)
		if err != nil {
			for _, open := range listeners {
				_ = open.Close()
			}

			return nil, fmt.Errorf("listening on %s: %w", addr, err)
		}

		listeners = append(listeners, ln)
	}

	return listeners, nil
}

func addrStrings(addrs []net.Addr) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}

	return out
}

func ipStrings(ips []netip.Addr) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}

	return out
}
