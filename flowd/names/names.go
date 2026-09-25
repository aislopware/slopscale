// Package names attributes a hostname to a connection from what the agent
// saw: the server name in the connection's own TLS or QUIC handshake, the
// DNS answers its resolver handed out, and the gateway's app connector
// domains.
package names

import (
	"net/netip"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic"
)

const (
	// sniTTL is how long a handshake's server name is kept for the
	// connection tracking reader to pick up; it reads every 30 s.
	sniTTL = 5 * time.Minute
	// minAnswerTTL and maxAnswerTTL clamp how long a DNS answer
	// attributes connections: clients keep using an address past a short
	// TTL, and a long one would outlive the address's meaning.
	minAnswerTTL = 5 * time.Minute
	maxAnswerTTL = time.Hour

	maxSNIEntries    = 1 << 16
	maxAnswerEntries = 1 << 18
)

// Conn identifies a connection by its original 5-tuple.
type Conn struct {
	Src, Dst netip.AddrPort
	Proto    uint8
}

type sniEntry struct {
	host    string
	ech     bool
	expires time.Time
}

type srcDst struct {
	src, dst netip.Addr
}

type nameEntry struct {
	name    string
	expires time.Time
}

// Resolver holds what the agent learnt. It is safe for concurrent use.
type Resolver struct {
	mu        sync.Mutex
	sni       map[Conn]sniEntry
	bySrc     map[srcDst]nameEntry
	shared    map[netip.Addr]nameEntry
	appc      map[netip.Addr]string
	lastSweep time.Time
}

// NewResolver returns an empty resolver.
func NewResolver() *Resolver {
	return &Resolver{
		sni:    make(map[Conn]sniEntry),
		bySrc:  make(map[srcDst]nameEntry),
		shared: make(map[netip.Addr]nameEntry),
		appc:   make(map[netip.Addr]string),
	}
}

// PutSNI records the server name a connection's handshake asked for.
func (r *Resolver) PutSNI(c Conn, host string, ech bool, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweepLocked(now)

	if _, ok := r.sni[c]; !ok && len(r.sni) >= maxSNIEntries {
		return
	}

	r.sni[c] = sniEntry{host: host, ech: ech, expires: now.Add(sniTTL)}
}

// PutDNS records that src resolved name to addrs, valid for ttl.
func (r *Resolver) PutDNS(src netip.Addr, name string, addrs []netip.Addr, ttl time.Duration, now time.Time) {
	entry := nameEntry{name: name, expires: now.Add(min(max(ttl, minAnswerTTL), maxAnswerTTL))}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.sweepLocked(now)

	for _, addr := range addrs {
		addr = addr.Unmap()

		key := srcDst{src: src.Unmap(), dst: addr}
		if _, ok := r.bySrc[key]; ok || len(r.bySrc) < maxAnswerEntries {
			r.bySrc[key] = entry
		}

		if _, ok := r.shared[addr]; ok || len(r.shared) < maxAnswerEntries {
			r.shared[addr] = entry
		}
	}
}

// SetAppConnector replaces the app connector's domain map.
func (r *Resolver) SetAppConnector(domains map[string][]netip.Addr) {
	byAddr := make(map[netip.Addr]string)

	for domain, addrs := range domains {
		for _, addr := range addrs {
			addr = addr.Unmap()
			// Several domains can share an address; keep one stably.
			if cur, ok := byAddr[addr]; !ok || domain < cur {
				byAddr[addr] = domain
			}
		}
	}

	r.mu.Lock()
	r.appc = byAddr
	r.mu.Unlock()
}

// Lookup returns the best hostname for a connection and how it was learnt,
// in this order: the connection's own handshake; a DNS answer the same
// node got; the app connector's domain; the public name of an encrypted
// handshake; a DNS answer another node got.
func (r *Resolver) Lookup(c Conn, now time.Time) (string, traffic.HostSource) {
	src, dst := c.Src.Addr().Unmap(), c.Dst.Addr().Unmap()

	r.mu.Lock()
	defer r.mu.Unlock()

	sni, hasSNI := r.sni[c]
	if hasSNI && now.After(sni.expires) {
		hasSNI = false
	}

	if hasSNI && !sni.ech {
		return sni.host, traffic.HostSNI
	}

	if e, ok := r.bySrc[srcDst{src: src, dst: dst}]; ok && !now.After(e.expires) {
		return e.name, traffic.HostDNS
	}

	if domain, ok := r.appc[dst]; ok {
		return domain, traffic.HostAppConnector
	}

	if hasSNI {
		return sni.host, traffic.HostECH
	}

	if e, ok := r.shared[dst]; ok && !now.After(e.expires) {
		return e.name, traffic.HostDNSShared
	}

	return "", ""
}

// sweepLocked drops expired entries at most once a minute.
func (r *Resolver) sweepLocked(now time.Time) {
	if now.Sub(r.lastSweep) < time.Minute {
		return
	}

	r.lastSweep = now

	for k, e := range r.sni {
		if now.After(e.expires) {
			delete(r.sni, k)
		}
	}

	for k, e := range r.bySrc {
		if now.After(e.expires) {
			delete(r.bySrc, k)
		}
	}

	for k, e := range r.shared {
		if now.After(e.expires) {
			delete(r.shared, k)
		}
	}
}
