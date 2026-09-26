package types

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTestBindFailure = errors.New("listen tcp :80: bind: address already in use")

func TestPortFromAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		want    int
		wantErr bool
	}{
		{"named-http", ":http", 80, false},
		{"numeric-80", ":80", 80, false},
		{"wildcard-numeric", "0.0.0.0:80", 80, false},
		{"named-https", ":https", 443, false},
		{"numeric-443", "0.0.0.0:443", 443, false},
		{"ipv6-wildcard", "[::]:8080", 8080, false},
		{"specific-ipv4", "192.168.1.1:8080", 8080, false},
		{"empty", "", 0, true},
		{"no-port", "0.0.0.0", 0, true},
		{"bare-port", "3478", 0, true},
		{"unknown-named", ":bogus", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PortFromAddr(tt.addr)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListenersOverlap(t *testing.T) {
	tests := []struct {
		name        string
		aHost       string
		aPort       int
		bHost       string
		bPort       int
		wantOverlap bool
	}{
		{"different-ports", "", 80, "", 443, false},
		{"same-port-wildcard", "", 80, "", 80, true},
		{"wildcard-vs-loopback-same-port", "0.0.0.0", 80, "127.0.0.1", 80, true},
		{"loopback-vs-wildcard-same-port", "127.0.0.1", 80, "0.0.0.0", 80, true},
		{"ipv6-wildcard-vs-numeric", "::", 80, "0.0.0.0", 80, true},
		{"different-specific-hosts-same-port", "192.168.1.1", 80, "192.168.1.2", 80, false},
		{"same-specific-host-same-port", "127.0.0.1", 80, "127.0.0.1", 80, true},
		{"same-specific-host-different-port", "127.0.0.1", 80, "127.0.0.1", 81, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOverlap, listenersOverlap(tt.aHost, tt.aPort, tt.bHost, tt.bPort))
		})
	}
}

func TestIsWildcardHost(t *testing.T) {
	for _, h := range []string{"", "0.0.0.0", "::", "[::]"} {
		assert.True(t, isWildcardHost(h), h)
	}

	for _, h := range []string{"127.0.0.1", "192.168.1.1", "::1", "example.com"} {
		assert.False(t, isWildcardHost(h), h)
	}
}

// TestValidateListenerCollisions_BlamesParseFailure pins each parse error
// to the key whose address is malformed, even next to a well-formed one.
func TestValidateListenerCollisions_BlamesParseFailure(t *testing.T) {
	tests := []struct {
		name    string
		bad     string
		good    string
		badAddr string
	}{
		{"bad-listen_addr", "listen_addr", "metrics_listen_addr", "garbage"},
		{"bad-metrics_listen_addr", "metrics_listen_addr", "listen_addr", "no-port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf.Reset()
			conf.Set(tt.bad, tt.badAddr)
			conf.Set(tt.good, ":9999")

			v := &configValidator{}
			validateListenerCollisions(v)

			errs := ConfigErrors(v.Err())
			require.Len(t, errs, 1, "expected exactly one parse error")
			assert.Equal(t, "cannot parse "+tt.bad, errs[0].Reason)
			require.Len(t, errs[0].Current, 1)
			assert.Equal(t, tt.bad, errs[0].Current[0].Key)
			assert.Equal(t, tt.badAddr, errs[0].Current[0].Value)
		})
	}
}

func TestACMEListenAddr(t *testing.T) {
	conf.Reset()
	assert.Equal(t, DefaultACMEListenAddr, ACMEListenAddr(), "unset answers where net/http would")

	conf.Set("tls_letsencrypt_listen", "")
	assert.Equal(t, DefaultACMEListenAddr, ACMEListenAddr(), "empty answers where net/http would")

	conf.Set("tls_letsencrypt_listen", "127.0.0.1:8080")
	assert.Equal(t, "127.0.0.1:8080", ACMEListenAddr())
}

func TestListenerBindError_IsEADDRINUSE(t *testing.T) {
	bindErr := &ListenerBindError{
		Listener:  "main HTTP",
		ConfigKey: "listen_addr",
		Network:   "tcp",
		Addr:      "0.0.0.0:80",
		Err:       &net.OpError{Op: "listen", Net: "tcp", Err: syscall.EADDRINUSE},
	}

	wrapped := fmt.Errorf("serve: %w", bindErr)
	require.ErrorIs(t, wrapped, syscall.EADDRINUSE)

	got, ok := errors.AsType[*ListenerBindError](wrapped)
	require.True(t, ok)
	assert.Equal(t, "main HTTP", got.Listener)
	assert.Equal(t, "listen_addr", got.ConfigKey)
	assert.Equal(t, "0.0.0.0:80", got.Addr)
}

func TestListenerBindError_Render(t *testing.T) {
	bindErr := &ListenerBindError{
		Listener:  "ACME HTTP-01 challenge",
		ConfigKey: "tls_letsencrypt_listen",
		Network:   "tcp",
		Addr:      ":http",
		Err:       errTestBindFailure,
	}
	assert.Equal(t,
		`binding ACME HTTP-01 challenge listener (tls_letsencrypt_listen=":http"): `+
			`listen tcp :80: bind: address already in use`,
		bindErr.Error(),
	)
}
