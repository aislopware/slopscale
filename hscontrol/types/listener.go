package types

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/aislopware/slopscale/hscontrol/conf"
)

// DefaultACMEListenAddr is where the ACME HTTP-01 challenge is answered
// when tls_letsencrypt_listen is empty, the address an [net/http.Server]
// with no Addr listens on.
const DefaultACMEListenAddr = ":http"

var errEmptyListenAddr = errors.New("address is empty")

// PortFromAddr returns the port of a host:port listen address. The port is
// a number or one of the service names "http" and "https"; the table is
// fixed so the result never depends on /etc/services.
func PortFromAddr(addr string) (int, error) {
	if addr == "" {
		return 0, errEmptyListenAddr
	}

	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("splitting host and port of %q: %w", addr, err)
	}

	switch port {
	case "http":
		return 80, nil //nolint:mnd // the http service port
	case "https":
		return 443, nil //nolint:mnd // the https service port
	}

	p, err := strconv.Atoi(port)
	if err != nil {
		return 0, fmt.Errorf("parsing port of %q: %w", addr, err)
	}

	return p, nil
}

// listenersOverlap reports whether two listen addresses on the same
// network would claim the same kernel socket: the same port with a
// wildcard host on either side, or the same port on the same host.
func listenersOverlap(aHost string, aPort int, bHost string, bPort int) bool {
	if aPort != bPort {
		return false
	}

	if isWildcardHost(aHost) || isWildcardHost(bHost) {
		return true
	}

	return aHost == bHost
}

func isWildcardHost(h string) bool {
	switch h {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}

	return false
}

// listenerSpec is one socket the server opens from the configuration.
type listenerSpec struct {
	key, addr, network string
	host               string
	port               int
}

// ACMEListenAddr is the address the ACME HTTP-01 challenge is answered on.
func ACMEListenAddr() string {
	if addr := conf.GetString("tls_letsencrypt_listen"); addr != "" {
		return addr
	}

	return DefaultACMEListenAddr
}

// configuredListeners lists the sockets the configuration makes the server
// open: the main and metrics listeners, the ACME HTTP-01 challenge while it
// is in use, the Funnel ingress while it is enabled and the embedded
// relay's STUN while it runs. The unix socket and the listeners on the
// tailnet are not host sockets and cannot collide with these.
func configuredListeners() []listenerSpec {
	var out []listenerSpec

	for _, key := range []string{"listen_addr", "metrics_listen_addr"} {
		if addr := conf.GetString(key); addr != "" {
			out = append(out, listenerSpec{key: key, addr: addr, network: "tcp"})
		}
	}

	if conf.GetString("tls_letsencrypt_hostname") != "" &&
		conf.GetString("tls_letsencrypt_challenge_type") == HTTP01ChallengeType {
		out = append(out, listenerSpec{key: "tls_letsencrypt_listen", addr: ACMEListenAddr(), network: "tcp"})
	}

	if conf.GetBool("funnel.enabled") {
		for i, addr := range conf.GetStringSlice("funnel.listen_addrs") {
			key := fmt.Sprintf("funnel.listen_addrs[%d]", i)
			out = append(out, listenerSpec{key: key, addr: addr, network: "tcp"})
		}
	}

	stunAddr := conf.GetString("derp.server.stun_listen_addr")
	if conf.GetBool("derp.server.enabled") && conf.GetBool("derp.server.stun_enabled") && stunAddr != "" {
		out = append(out, listenerSpec{key: "derp.server.stun_listen_addr", addr: stunAddr, network: "udp"})
	}

	return out
}

// validateListenerCollisions records an error for every listen address
// that cannot be parsed, blaming its own key, and for every pair of
// listeners that would bind the same socket.
func validateListenerCollisions(v *configValidator) {
	var parsed []listenerSpec

	for _, l := range configuredListeners() {
		port, err := PortFromAddr(l.addr)
		if err != nil {
			v.Add(&ConfigError{
				Reason:  "cannot parse " + l.key,
				Current: []KV{{l.key, l.addr}},
				Detail:  err.Error(),
				Hint:    `use host:port form, e.g. "0.0.0.0:8080"`,
			})

			continue
		}

		l.host, _, _ = net.SplitHostPort(l.addr)
		l.port = port
		parsed = append(parsed, l)
	}

	for i, a := range parsed {
		for _, b := range parsed[i+1:] {
			if a.network != b.network || !listenersOverlap(a.host, a.port, b.host, b.port) {
				continue
			}

			v.Add(&ConfigError{
				Reason:        fmt.Sprintf("%s and %s would bind the same %s socket", a.key, b.key, a.network),
				Current:       []KV{{a.key, a.addr}},
				ConflictsWith: []KV{{b.key, b.addr}},
				Hint:          "give each listener a distinct port, or bind them to different non-wildcard hosts",
				See:           docsURL + "ref/tls",
			})
		}
	}
}
