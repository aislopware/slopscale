package cli

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errClassifierUnrelated = errors.New("not a bind error")

func bindError(listener, key, network, addr string, errno error) *types.ListenerBindError {
	return &types.ListenerBindError{
		Listener:  listener,
		ConfigKey: key,
		Network:   network,
		Addr:      addr,
		Err:       &net.OpError{Op: "listen", Net: network, Err: errno},
	}
}

func TestClassifyServeError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantHint      []string
		wantNoHint    []string
		wantUnchanged bool
	}{
		{
			name: "eaddrinuse-numeric-port",
			err:  bindError("main HTTP", "listen_addr", "tcp", "0.0.0.0:443", syscall.EADDRINUSE),
			wantHint: []string{
				"another socket on this host is bound to the same address",
				"sudo ss -tlnp 'sport = :443'",
			},
		},
		{
			name:     "eaddrinuse-named-port",
			err:      bindError("ACME HTTP-01 challenge", "tls_letsencrypt_listen", "tcp", ":http", syscall.EADDRINUSE),
			wantHint: []string{"sudo ss -tlnp 'sport = :80'"},
		},
		{
			name: "eaddrinuse-udp-stun",
			err: bindError(
				"embedded DERP STUN", "derp.server.stun_listen_addr", "udp", "0.0.0.0:3478", syscall.EADDRINUSE),
			wantHint:   []string{"sudo ss -ulnp 'sport = :3478'"},
			wantNoHint: []string{"-tlnp"},
		},
		{
			name: "eacces-privileged-port",
			err:  bindError("main HTTP", "listen_addr", "tcp", "0.0.0.0:80", syscall.EACCES),
			wantHint: []string{
				"privileged port",
				"CAP_NET_BIND_SERVICE",
				"setcap cap_net_bind_service=+ep",
			},
		},
		{
			name:          "non-bind-error-passes-through",
			err:           errClassifierUnrelated,
			wantUnchanged: true,
		},
		{
			name: "bind-error-without-known-errno",
			err: &types.ListenerBindError{
				Listener:  "main HTTP",
				ConfigKey: "listen_addr",
				Network:   "tcp",
				Addr:      "0.0.0.0:80",
				Err:       errClassifierUnrelated,
			},
			wantUnchanged: true,
		},
		{
			name: "wrapped-eaddrinuse-still-classified",
			err: fmt.Errorf("building DERP map: %w",
				bindError("Funnel ingress", "funnel.listen_addrs", "tcp", "0.0.0.0:8443", syscall.EADDRINUSE)),
			wantHint: []string{"sudo ss -tlnp 'sport = :8443'"},
		},
		{
			name:       "eaddrinuse-unparseable-addr-omits-port",
			err:        bindError("main HTTP", "listen_addr", "tcp", "garbage", syscall.EADDRINUSE),
			wantHint:   []string{"sudo ss -tlnp"},
			wantNoHint: []string{"sport ="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyServeError(tt.err)
			require.ErrorIs(t, got, tt.err)

			if tt.wantUnchanged {
				assert.Equal(t, tt.err.Error(), got.Error())

				return
			}

			_, ok := errors.AsType[*types.ListenerBindError](got)
			assert.True(t, ok, "the bind error stays reachable")

			for _, want := range tt.wantHint {
				assert.Contains(t, got.Error(), want)
			}

			for _, unwanted := range tt.wantNoHint {
				assert.NotContains(t, got.Error(), unwanted)
			}
		})
	}
}

// TestClassifyServeErrorRealBind classifies the error a real second bind of
// a taken port returns, not a hand-built errno.
func TestClassifyServeErrorRealBind(t *testing.T) {
	taken, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = taken.Close() })

	addr := taken.Addr().String()
	_, err = new(net.ListenConfig).Listen(t.Context(), "tcp", addr)
	require.Error(t, err)

	port, err2 := types.PortFromAddr(addr)
	require.NoError(t, err2)

	got := classifyServeError(&types.ListenerBindError{
		Listener: "main HTTP", ConfigKey: "listen_addr", Network: "tcp", Addr: addr, Err: err,
	})
	assert.Contains(t, got.Error(), fmt.Sprintf("binding main HTTP listener (listen_addr=%q)", addr))
	assert.Contains(t, got.Error(), fmt.Sprintf("sudo ss -tlnp 'sport = :%d'", port))
}
