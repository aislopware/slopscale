package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

const (
	// bootstrapDNSRefresh is how long a resolved set of names is served
	// before it is resolved again, as tailscale's derper does.
	bootstrapDNSRefresh = 10 * time.Minute
	// bootstrapDNSResolveTimeout bounds one resolution of every name.
	bootstrapDNSResolveTimeout = 10 * time.Second
)

// bootstrapResolver is the part of [net.Resolver] the bootstrap DNS
// endpoint uses; tests swap in a fixed table.
type bootstrapResolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// BootstrapDNS serves /bootstrap-dns, the escape hatch a client uses when
// its own DNS is broken (tailscale.com/net/dnsfallback): it asks a DERP
// server it can still reach by IP for the addresses of a name, above all
// the control server's, which it needs before anything else works. The
// answer is the addresses of every DERP node and of the control server,
// resolved by this server on a schedule and served from the cache. The q
// parameter names what the client is after and narrows the answer to that
// name when it is one of ours; any other name gets the whole set, never a
// lookup, so the endpoint is not an open resolver.
//
// Described in https://github.com/tailscale/tailscale/issues/1405 and
// served by tailscale's derper (cmd/derper/bootstrap_dns.go).
type BootstrapDNS struct {
	derpMap  func() tailcfg.DERPMapView
	names    []string
	resolver bootstrapResolver
	now      func() time.Time

	mu       sync.Mutex
	entries  map[string][]netip.Addr
	resolved time.Time
}

// NewBootstrapDNS returns the endpoint for the DERP map the function
// returns now, plus the control server the URL names when it is a
// hostname rather than an address.
func NewBootstrapDNS(derpMap func() tailcfg.DERPMapView, serverURL string) *BootstrapDNS {
	b := &BootstrapDNS{
		derpMap:  derpMap,
		resolver: &net.Resolver{},
		now:      time.Now,
	}

	b.names = controlHostname(serverURL)

	return b
}

// controlHostname is the control server's hostname as a list of one, or
// empty when the URL names an address, which a client parses itself.
func controlHostname(serverURL string) []string {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil
	}

	host := u.Hostname()
	if host == "" {
		return nil
	}

	_, err = netip.ParseAddr(host)
	if err == nil {
		return nil
	}

	return []string{host}
}

// ServeHTTP answers the request from the cache, resolving the names first
// when the cache is empty or older than [bootstrapDNSRefresh].
func (b *BootstrapDNS) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	entries := b.current(req.Context())

	if q := req.URL.Query().Get("q"); q != "" {
		if addrs, ok := entries[q]; ok && len(addrs) > 0 {
			entries = map[string][]netip.Addr{q: addrs}
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	// A client asking here is bootstrapping and picks a DERP at random
	// each time, so a kept-alive connection is wasted.
	writer.Header().Set("Connection", "close")
	writer.WriteHeader(http.StatusOK)

	err := json.NewEncoder(writer).Encode(entries)
	if err != nil {
		log.Error().Caller().Err(err).Msg("Failed to write bootstrap DNS response")
	}
}

// current returns the resolved names, resolving them again when stale.
// Requests serialise on the refresh so a burst resolves once.
func (b *BootstrapDNS) current(ctx context.Context) map[string][]netip.Addr {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.entries != nil && b.now().Sub(b.resolved) < bootstrapDNSRefresh {
		return b.entries
	}

	resolved := b.resolve(ctx)

	// A failed round keeps what was known rather than answering nothing;
	// the next request tries again.
	if len(resolved) > 0 || b.entries == nil {
		b.entries = resolved
	}

	b.resolved = b.now()

	return b.entries
}

// resolve looks every name up and returns those that resolved.
func (b *BootstrapDNS) resolve(ctx context.Context) map[string][]netip.Addr {
	ctx, cancel := context.WithTimeout(ctx, bootstrapDNSResolveTimeout)
	defer cancel()

	entries := make(map[string][]netip.Addr)

	for _, name := range b.hostnames() {
		ips, err := b.resolver.LookupIP(ctx, "ip", name)
		if err != nil {
			log.Trace().Caller().Err(err).Str("name", name).Msg("bootstrap DNS lookup failed")

			continue
		}

		addrs := make([]netip.Addr, 0, len(ips))

		for _, ip := range ips {
			if addr, ok := netip.AddrFromSlice(ip); ok {
				addrs = append(addrs, addr.Unmap())
			}
		}

		if len(addrs) > 0 {
			entries[name] = addrs
		}
	}

	return entries
}

// hostnames lists the names to resolve: every DERP node's, then the
// control server's, each once.
func (b *BootstrapDNS) hostnames() []string {
	var names []string

	if b.derpMap != nil {
		for _, region := range b.derpMap().Regions().All() {
			for _, node := range region.Nodes().All() {
				if host := node.HostName(); host != "" {
					names = append(names, host)
				}
			}
		}
	}

	names = append(names, b.names...)
	slices.Sort(names)

	return slices.Compact(names)
}
