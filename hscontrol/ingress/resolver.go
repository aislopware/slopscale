package ingress

import (
	"context"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"tailscale.com/ipn/ipnstate"
)

// statusTTL is how long a status snapshot is used before the next
// lookup refreshes it; a name that is not in the snapshot refreshes it
// at once, so a node that just came up is found on its first
// connection.
const statusTTL = 5 * time.Second

// StatusFunc returns the ingress node's view of the tailnet, as
// [tailscale.com/client/local.Client.Status] does.
type StatusFunc func(ctx context.Context) (*ipnstate.Status, error)

// StatusResolver finds nodes by their MagicDNS name in the ingress
// node's status: the node's peer API address is what the status
// reports, so the ingress needs no table of its own and follows the
// policy exactly, since a node it may not reach is not a peer.
type StatusResolver struct {
	status StatusFunc

	mu      sync.Mutex
	fetched time.Time
	peers   map[string]netip.AddrPort
}

// NewStatusResolver returns a resolver over status.
func NewStatusResolver(status StatusFunc) *StatusResolver {
	return &StatusResolver{status: status}
}

// Resolve implements [Resolver].
func (r *StatusResolver) Resolve(ctx context.Context, host string) (netip.AddrPort, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))

	r.mu.Lock()
	defer r.mu.Unlock()

	if addr, ok := r.peers[host]; ok && time.Since(r.fetched) < statusTTL {
		return addr, true
	}

	r.refreshLocked(ctx)

	addr, ok := r.peers[host]

	return addr, ok
}

// refreshLocked reads the status again; a failed read keeps the last
// snapshot.
func (r *StatusResolver) refreshLocked(ctx context.Context) {
	st, err := r.status(ctx)
	if err != nil || st == nil {
		return
	}

	peers := make(map[string]netip.AddrPort, len(st.Peer))

	for _, peer := range st.Peer {
		addr, ok := peerAPIAddr(peer)
		if !ok {
			continue
		}

		peers[strings.ToLower(strings.TrimSuffix(peer.DNSName, "."))] = addr
	}

	r.peers = peers
	r.fetched = time.Now()
}

// peerAPIAddr picks the node's IPv4 peer API address, or the first one
// when it has none.
func peerAPIAddr(peer *ipnstate.PeerStatus) (netip.AddrPort, bool) {
	var first netip.AddrPort

	for _, raw := range peer.PeerAPIURL {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}

		addr, err := netip.ParseAddrPort(u.Host)
		if err != nil {
			continue
		}

		if addr.Addr().Is4() {
			return addr, true
		}

		if !first.IsValid() {
			first = addr
		}
	}

	return first, first.IsValid()
}
