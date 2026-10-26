package state

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/aislopware/slopscale/hscontrol/types"
)

const (
	// defaultRegisterCacheMaxPerSource is the default number of
	// unauthenticated registrations one source network may hold in the auth
	// cache at once. A real machine holds one while its user logs in.
	defaultRegisterCacheMaxPerSource = 16

	// ipv6SourceBits is the IPv6 prefix one source is counted under: a host
	// that gets a /64 picks any address inside it for free.
	ipv6SourceBits = 64
)

// ErrTooManyPendingRegistrations is returned when the source network of an
// unauthenticated registration already holds its share of the auth cache.
var ErrTooManyPendingRegistrations = errors.New("too many pending registrations from this network")

// registrationSources counts the unauthenticated registrations each source
// network holds in the auth cache, so one network cannot fill the cache and
// evict every other client's pending login. An entry is counted once, by
// [types.AuthID], from [State.SetPendingRegistration] until the cache's
// eviction callback releases it; the callback fires on completion
// ([State.HandleNodeFromAuthPath] removes the entry), on Remove, on TTL
// expiry and on LRU eviction alike.
type registrationSources struct {
	mu      sync.Mutex
	limit   int
	pending map[netip.Prefix]int
	byID    map[types.AuthID]netip.Prefix
}

func newRegistrationSources(limit int) *registrationSources {
	return &registrationSources{
		limit:   limit,
		pending: make(map[netip.Prefix]int),
		byID:    make(map[types.AuthID]netip.Prefix),
	}
}

// sourceNetwork is the network a client address is counted under: the
// address itself for IPv4, its /64 for IPv6.
func sourceNetwork(addr netip.Addr) netip.Prefix {
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		return netip.PrefixFrom(addr, addr.BitLen())
	}

	return netip.PrefixFrom(addr, ipv6SourceBits).Masked()
}

// reserve counts id against network, or refuses when network is at its
// limit. An id already counted is not counted again.
func (r *registrationSources) reserve(id types.AuthID, network netip.Prefix) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[id]; ok {
		return nil
	}

	if r.pending[network] >= r.limit {
		return fmt.Errorf("%w: %s holds %d", ErrTooManyPendingRegistrations, network, r.limit)
	}

	r.pending[network]++
	r.byID[id] = network

	return nil
}

// release uncounts id. It runs inside the auth cache's eviction callback,
// under the cache's lock, so it must never call back into the cache.
func (r *registrationSources) release(id types.AuthID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	network, ok := r.byID[id]
	if !ok {
		return
	}

	delete(r.byID, id)

	if r.pending[network] <= 1 {
		delete(r.pending, network)

		return
	}

	r.pending[network]--
}

// snapshot returns the pending count per source network.
func (r *registrationSources) snapshot() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]int, len(r.pending))

	for network, n := range r.pending {
		out[network.String()] = n
	}

	return out
}
