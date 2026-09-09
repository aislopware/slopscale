// Package egress bounds where the server's own outbound HTTP requests may
// go. Webhooks, log streams and DERP map URLs are operator input, so
// without a guard they turn the control server into a proxy for the
// networks only it can reach: its own loopback services and the link-local
// metadata service every cloud hands out (SSRF).
//
// The guard runs twice. A validator calls [Guard.CheckHost] when the URL is
// stored, so an operator sees a 400 instead of a delivery that quietly
// fails, and every outbound client dials through [DialContext], which
// checks the address the name actually resolved to; a name that resolves
// to a blocked address, or is repointed later, is refused at connect time.
package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// ErrBlocked is what every rejection wraps, so callers can map it (the API
// answers 400) without matching on the reason text.
var ErrBlocked = errors.New("blocked address")

// Policy is what the operator allows outbound requests to reach. The zero
// value is the default: loopback, link-local and unspecified addresses are
// refused, everything else is allowed.
type Policy struct {
	// AllowLoopback re-allows loopback, for a development server whose
	// receivers run on the same host.
	AllowLoopback bool

	// DenyPrivate refuses the private ranges too (10/8, 172.16/12,
	// 192.168/16, fc00::/7 and the tailnet's own 100.64/10). Off by
	// default because self-hosters run their webhook and ntfy receivers
	// on the LAN.
	DenyPrivate bool
}

// Guard checks addresses against one policy.
type Guard struct {
	policy Policy
}

// New returns a guard for the given policy.
func New(policy Policy) Guard {
	return Guard{policy: policy}
}

// CheckAddr reports whether the policy allows connecting to addr.
func (g Guard) CheckAddr(addr netip.Addr) error {
	// An IPv4-mapped IPv6 address (::ffff:127.0.0.1) reaches the same host
	// as its IPv4 form, so judge the unmapped address.
	addr = addr.Unmap()

	switch {
	case !addr.IsValid():
		return fmt.Errorf("%w (not an address)", ErrBlocked)

	case addr.IsUnspecified():
		return fmt.Errorf("%w (unspecified)", ErrBlocked)

	case addr.IsLoopback():
		if g.policy.AllowLoopback {
			return nil
		}

		return fmt.Errorf("%w (loopback)", ErrBlocked)

	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return fmt.Errorf("%w (link-local)", ErrBlocked)

	case g.policy.DenyPrivate && isPrivate(addr):
		return fmt.Errorf("%w (private)", ErrBlocked)
	}

	return nil
}

// CheckHost checks the host part of a URL. A name is left to the dial-time
// check, which sees what it resolved to; resolving here would make every
// validator wait on DNS and still tell us nothing binding.
func (g Guard) CheckHost(host string) error {
	addr, err := netip.ParseAddr(hostOnly(host))
	if err == nil {
		return g.CheckAddr(addr)
	}

	return nil
}

// Control is the [net.Dialer.Control] hook: it runs with the resolved
// address, before the connection is made.
func (g Guard) Control(_, address string, _ syscall.RawConn) error {
	addr, err := netip.ParseAddr(hostOnly(address))
	if err != nil {
		return fmt.Errorf("%w (%s is not an address)", ErrBlocked, address)
	}

	return g.CheckAddr(addr)
}

// isPrivate covers the RFC 1918 and unique-local ranges plus the tailnet's
// own 100.64/10, which netip does not count as private.
func isPrivate(addr netip.Addr) bool {
	return addr.IsPrivate() || cgnat.Contains(addr)
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// hostOnly strips the port from a "host:port" and the brackets from an
// IPv6 literal, leaving a bare host or address. A URL host carries the
// brackets with or without a port, and netip does not take them.
func hostOnly(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}

	return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
}

// current is the policy the server's own clients dial under. It is a
// package-level value because the clients that need it (webhooks, log
// streams, the DERP map fetch) are built where the config is not in reach,
// and the policy is one server-wide setting.
var current atomic.Pointer[Policy]

// SetDefault installs the policy every outbound client checks against.
// The server calls it while it reads the config.
func SetDefault(policy Policy) {
	current.Store(&policy)
}

// Default is the guard for the installed policy, or for the zero policy
// when nothing installed one.
func Default() Guard {
	policy := current.Load()
	if policy == nil {
		return Guard{}
	}

	return Guard{policy: *policy}
}

const (
	dialTimeout      = 10 * time.Second
	keepAlive        = 30 * time.Second
	idleTimeout      = 90 * time.Second
	handshakeTimeout = 10 * time.Second
	maxIdleConns     = 100
)

// DialContext dials like [net.Dialer] but refuses a blocked address. The
// policy is read per dial, so a client built before the config was read
// still honours it.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: keepAlive,
		Control: func(network, address string, conn syscall.RawConn) error {
			return Default().Control(network, address, conn)
		},
	}

	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", address, err)
	}

	return conn, nil
}

// Transport is a transport shaped like [http.DefaultTransport] that dials
// through the guard. Outbound clients that take operator-supplied URLs use
// it instead of the default.
func Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          maxIdleConns,
		IdleConnTimeout:       idleTimeout,
		TLSHandshakeTimeout:   handshakeTimeout,
		ExpectContinueTimeout: time.Second,
	}
}
