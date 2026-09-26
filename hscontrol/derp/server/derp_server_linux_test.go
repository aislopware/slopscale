package server

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/stun"
)

// TestSTUNRepliesFromTheAddressAsked sends from 127.0.0.1 to a wildcard
// listener at 127.0.0.2 (all of 127/8 is local on Linux). Routing alone
// would answer from 127.0.0.1, which a client behind NAT or conntrack does
// not match to its request.
func TestSTUNRepliesFromTheAddressAsked(t *testing.T) {
	t.Parallel()

	for _, network := range []string{"udp4", "udp"} {
		t.Run(network, func(t *testing.T) {
			t.Parallel()

			serverConn, err := net.ListenUDP(network, &net.UDPAddr{})
			if network == "udp" && err != nil {
				t.Skipf("no dual-stack socket: %v", err)
			}

			require.NoError(t, err)

			require.True(t, enablePktInfo(serverConn))

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})

			go func() {
				defer close(done)

				serverSTUNListener(ctx, serverConn, true)
			}()

			t.Cleanup(func() {
				cancel()

				_ = serverConn.Close()

				<-done
			})

			bound, ok := serverConn.LocalAddr().(*net.UDPAddr)
			require.True(t, ok)

			serverAddr := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.2"), bound.AddrPort().Port())

			clientConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			t.Cleanup(func() { _ = clientConn.Close() })
			require.NoError(t, clientConn.SetDeadline(time.Now().Add(testTimeout)))

			txID := stun.NewTxID()
			_, err = clientConn.WriteToUDPAddrPort(stun.Request(txID), serverAddr)
			require.NoError(t, err)

			buf := make([]byte, 1500)
			n, from, err := clientConn.ReadFromUDPAddrPort(buf)
			require.NoError(t, err)

			assert.Equal(t, serverAddr, from, "the reply comes from the address the request went to")

			clientAddr, ok := clientConn.LocalAddr().(*net.UDPAddr)
			require.True(t, ok)
			assert.Equal(t, stun.Response(txID, clientAddr.AddrPort()), buf[:n])
		})
	}
}
