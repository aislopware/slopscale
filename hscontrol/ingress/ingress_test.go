package ingress

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

// fakeNode stands in for a tailnet node's peer API: it takes an ingress
// request the way ipnlocal's handleServeIngress does, then terminates
// TLS with a self-signed certificate and answers HTTP over it.
type fakeNode struct {
	srv      *httptest.Server
	cert     tls.Certificate
	sources  []string
	targets  []string
	accepted atomic.Int32
	refuse   bool
}

func newFakeNode(t *testing.T) *fakeNode {
	t.Helper()

	n := &fakeNode{cert: selfSigned(t, "web.example.com")}
	n.srv = httptest.NewServer(http.HandlerFunc(n.handle))
	t.Cleanup(n.srv.Close)

	return n
}

func (n *fakeNode) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != ingressPath || r.Method != http.MethodPost {
		http.Error(w, "unexpected request", http.StatusNotFound)

		return
	}

	n.sources = append(n.sources, r.Header.Get(headerIngressSource))
	n.targets = append(n.targets, r.Header.Get(headerIngressTarget))

	if n.refuse {
		http.Error(w, "denied", http.StatusForbidden)

		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "not hijackable", http.StatusInternalServerError)

		return
	}

	conn, _, err := hijacker.Hijack()
	if err != nil {
		return
	}

	n.accepted.Add(1)

	_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\n\r\n")

	tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{n.cert}, MinVersion: tls.VersionTLS12})
	defer tlsConn.Close()

	err = tlsConn.HandshakeContext(r.Context())
	if err != nil {
		return
	}

	req, err := http.ReadRequest(bufio.NewReader(tlsConn))
	if err != nil {
		return
	}

	_, _ = io.WriteString(
		tlsConn,
		"HTTP/1.1 200 OK\r\nContent-Length: 6\r\nConnection: close\r\n\r\nhello "+req.Host[:0]+"\n",
	)
}

func (n *fakeNode) peerAPI(t *testing.T) netip.AddrPort {
	t.Helper()

	addr, err := netip.ParseAddrPort(strings.TrimPrefix(n.srv.URL, "http://"))
	require.NoError(t, err)

	return addr
}

type mapResolver map[string]netip.AddrPort

func (m mapResolver) Resolve(_ context.Context, host string) (netip.AddrPort, bool) {
	addr, ok := m[host]

	return addr, ok
}

func selfSigned(t *testing.T, name string) tls.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

// serveProxy runs the proxy on a loopback port and returns its address.
func serveProxy(t *testing.T, p *Proxy) net.Addr {
	t.Helper()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = p.Serve(ctx, ln)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return ln.Addr()
}

// dialTLS opens a TLS connection to addr with the given client config.
func dialTLS(t *testing.T, addr net.Addr, cfg *tls.Config) (net.Conn, error) {
	t.Helper()

	dialer := &tls.Dialer{Config: cfg}

	return dialer.DialContext(t.Context(), "tcp", addr.String())
}

// TestProxyDeliversByServerName proves a TLS connection reaches the node
// named by the SNI, that the node learns the public source and the
// host:port it was reached on, and that the bytes flow both ways through
// the node's own TLS.
func TestProxyDeliversByServerName(t *testing.T) {
	t.Parallel()

	node := newFakeNode(t)

	var dialer net.Dialer

	p := New(dialer.DialContext, mapResolver{"web.example.com": node.peerAPI(t)})
	addr := serveProxy(t, p)

	conn, err := dialTLS(t, addr, &tls.Config{
		ServerName:         "Web.Example.com.",
		InsecureSkipVerify: true,
	})
	require.NoError(t, err)

	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	require.True(t, ok, "the dialer returns a TLS connection")

	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: web.example.com\r\n\r\n")
	require.NoError(t, err)

	body, err := io.ReadAll(conn)
	require.NoError(t, err)
	assert.Contains(t, string(body), "200 OK")
	assert.Contains(t, string(body), "hello")

	require.Len(t, node.targets, 1)
	assert.Equal(t, "web.example.com:"+port(addr), node.targets[0])
	require.NotEmpty(t, tlsConn.ConnectionState().PeerCertificates, "TLS ended on the node")
	assert.Equal(t, []string{"web.example.com"}, tlsConn.ConnectionState().PeerCertificates[0].DNSNames)

	_, err = netip.ParseAddrPort(node.sources[0])
	assert.NoError(t, err, "the source is ip:port")
}

// TestProxyRefusals proves a connection without a server name, for an
// unknown name, or that the node refuses is closed without delivering.
func TestProxyRefusals(t *testing.T) {
	t.Parallel()

	node := newFakeNode(t)

	var dialer net.Dialer

	p := New(dialer.DialContext, mapResolver{"web.example.com": node.peerAPI(t)})
	p.HelloTimeout = time.Second
	addr := serveProxy(t, p)

	t.Run("unknown name", func(t *testing.T) {
		t.Parallel()

		_, err := dialTLS(t, addr, &tls.Config{
			ServerName:         "other.example.com",
			InsecureSkipVerify: true,
		})
		require.Error(t, err)
	})

	t.Run("no server name", func(t *testing.T) {
		t.Parallel()

		_, err := dialTLS(t, addr, &tls.Config{
			InsecureSkipVerify: true,
		})
		require.Error(t, err)
	})

	t.Run("not TLS", func(t *testing.T) {
		t.Parallel()

		conn, err := dialer.DialContext(t.Context(), "tcp", addr.String())
		require.NoError(t, err)

		defer conn.Close()

		_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\n\r\n")

		_, err = conn.Read(make([]byte, 1))
		require.Error(t, err, "closed without an answer")
	})

	t.Run("node refuses", func(t *testing.T) {
		t.Parallel()

		refusing := newFakeNode(t)
		refusing.refuse = true
		rp := New(dialer.DialContext, mapResolver{"web.example.com": refusing.peerAPI(t)})
		raddr := serveProxy(t, rp)

		_, err := dialTLS(t, raddr, &tls.Config{
			ServerName:         "web.example.com",
			InsecureSkipVerify: true,
		})
		require.Error(t, err)
		assert.Equal(t, int32(0), refusing.accepted.Load())
	})
}

// TestStatusResolver proves names resolve to the IPv4 peer API address
// from the status, case and trailing dot aside, that a snapshot is
// reused within its TTL, and that an unknown name refreshes it.
func TestStatusResolver(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	status := &ipnstate.Status{Peer: map[key.NodePublic]*ipnstate.PeerStatus{}}
	web := &ipnstate.PeerStatus{
		DNSName:    "Web.Example.com.",
		PeerAPIURL: []string{"http://[fd7a::2]:41641", "http://100.64.0.2:41641"},
	}
	status.Peer[key.NewNode().Public()] = web

	r := NewStatusResolver(func(context.Context) (*ipnstate.Status, error) {
		calls.Add(1)

		return status, nil
	})

	addr, ok := r.Resolve(t.Context(), "web.example.com.")
	require.True(t, ok)
	assert.Equal(t, "100.64.0.2:41641", addr.String())

	_, ok = r.Resolve(t.Context(), "WEB.example.com")
	assert.True(t, ok)
	assert.Equal(t, int32(1), calls.Load(), "the snapshot is reused")

	_, ok = r.Resolve(t.Context(), "gone.example.com")
	assert.False(t, ok)
	assert.Equal(t, int32(2), calls.Load(), "an unknown name refreshes")
}

func port(addr net.Addr) string {
	_, p, _ := net.SplitHostPort(addr.String())

	return p
}
