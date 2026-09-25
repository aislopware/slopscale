package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/rdata"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/quic"
)

// The end-to-end test builds a gateway out of network namespaces inside a
// privileged container: a tailnet node (netns fl-client, 100.64.10.2 and
// fd7a:115c:a1e0:10::2) routes through this namespace, which masquerades
// towards "the internet" (netns fl-inet, 198.51.100.2 and 2001:db8:100::2),
// exactly as Tailscale does on an exit node. Run it with
// flowd/testdata/netns-test.sh.

const (
	helperEnv   = "FLOWD_E2E_HELPER"
	clientNS    = "fl-client"
	inetNS      = "fl-inet"
	gwLink      = "fl-gw"
	upLink      = "fl-up"
	clientV4    = "100.64.10.2"
	clientV6    = "fd7a:115c:a1e0:10::2"
	gatewayV4   = "100.64.10.1"
	gatewayV6   = "fd7a:115c:a1e0:10::1"
	internetV4  = "198.51.100.2"
	internetV6  = "2001:db8:100::2"
	filesHost   = "files.example.test"
	streamHost  = "stream.example.test"
	v6Host      = "v6.example.test"
	quicHost    = "quic.example.test"
	plainHost   = "plain.example.test"
	filesUp     = 200_000
	filesDown   = 1_000_000
	streamUp    = 50_000
	streamDown  = 400_000
	v6Up        = 30_000
	v6Down      = 60_000
	plainUp     = 5_000
	plainDown   = 70_000
	helperClock = 3 * time.Second
)

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "server":
		runServerHelper()
	case "client":
		runClientHelper()
	default:
		os.Exit(m.Run())
	}
}

func sh(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	require.NoError(t, err, "%s: %s", strings.Join(args, " "), out)
}

func setupNetwork(t *testing.T) {
	t.Helper()

	cleanup := func() {
		for _, args := range [][]string{
			{"ip", "netns", "del", clientNS}, {"ip", "netns", "del", inetNS},
			{"ip", "link", "del", gwLink}, {"ip", "link", "del", upLink},
			{"nft", "delete", "table", "inet", "flowde2e"},
		} {
			_ = exec.Command(args[0], args[1:]...).Run()
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, cmd := range []string{
		"ip netns add " + clientNS,
		"ip netns add " + inetNS,
		"ip link add " + gwLink + " type veth peer name fl-c",
		"ip link set fl-c netns " + clientNS,
		"ip link add " + upLink + " type veth peer name fl-s",
		"ip link set fl-s netns " + inetNS,
		"ip addr add " + gatewayV4 + "/24 dev " + gwLink,
		"ip -6 addr add " + gatewayV6 + "/64 dev " + gwLink + " nodad",
		"ip addr add 198.51.100.1/24 dev " + upLink,
		"ip -6 addr add 2001:db8:100::1/64 dev " + upLink + " nodad",
		"ip link set " + gwLink + " up",
		"ip link set " + upLink + " up",
		"ip netns exec " + clientNS + " ip addr add " + clientV4 + "/24 dev fl-c",
		"ip netns exec " + clientNS + " ip -6 addr add " + clientV6 + "/64 dev fl-c nodad",
		"ip netns exec " + clientNS + " ip link set fl-c up",
		"ip netns exec " + clientNS + " ip link set lo up",
		"ip netns exec " + clientNS + " ip route add default via " + gatewayV4,
		"ip netns exec " + clientNS + " ip -6 route add default via " + gatewayV6,
		"ip netns exec " + inetNS + " ip addr add " + internetV4 + "/24 dev fl-s",
		"ip netns exec " + inetNS + " ip -6 addr add " + internetV6 + "/64 dev fl-s nodad",
		"ip netns exec " + inetNS + " ip link set fl-s up",
		"ip netns exec " + inetNS + " ip link set lo up",
		"sysctl -qw net.ipv4.ip_forward=1",
		"sysctl -qw net.ipv6.conf.all.forwarding=1",
		"nft add table inet flowde2e",
		"nft add chain inet flowde2e post { type nat hook postrouting priority 100 ; }",
		"nft add rule inet flowde2e post oifname " + upLink + " masquerade",
	} {
		sh(t, strings.Fields(cmd)...)
	}
}

// startHelper runs this test binary as a helper inside a namespace.
func startHelper(t *testing.T, ns, role string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command("ip", "netns", "exec", ns, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"="+role)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())

	return cmd
}

// TestEndToEndThroughNetfilter runs the whole agent as it runs on an exit
// node: real connection tracking, real handshake capture, the real
// resolver, all against real TCP, QUIC and DNS traffic from another
// namespace; and checks what reaches the report endpoint.
func TestEndToEndThroughNetfilter(t *testing.T) {
	if os.Getenv("FLOWD_NETNS_TEST") == "" || os.Geteuid() != 0 {
		t.Skip("needs root in a privileged Linux container; run flowd/testdata/netns-test.sh")
	}

	setupNetwork(t)

	server := startHelper(t, inetNS, "server")
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	})

	srv := &reportServer{config: traffic.Config{
		SNI:            true,
		DNS:            true,
		Upstreams:      []string{internetV4},
		LogSources:     []netip.Addr{netip.MustParseAddr(clientV4), netip.MustParseAddr(clientV6)},
		ReportInterval: 10,
	}}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	stateDir := t.TempDir()
	seed, err := json.Marshal(srv.config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, configFile), seed, 0o600))

	local := &fakeLocal{ips: []netip.Addr{netip.MustParseAddr(gatewayV4), netip.MustParseAddr(gatewayV6)}}
	local.prefs.ControlURL = ts.URL

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, Options{
			Local:      local,
			Interface:  gwLink,
			StateDir:   stateDir,
			SpoolBytes: 8 << 20,
			Version:    "e2e",
			Logger:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
			dumpEvery:  time.Second,
		})
	}()

	// Wait for the resolver, and for the first report, whose response
	// names the client as a node whose DNS the agent records (the list is
	// never saved), then let the client run its traffic.
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(gatewayV4, "53"), 200*time.Millisecond)
		if err == nil {
			c.Close()
		}

		srv.mu.Lock()
		reported := len(srv.reports) > 0
		srv.mu.Unlock()

		return err == nil && reported
	}, 20*time.Second, 100*time.Millisecond)

	client := startHelper(t, clientNS, "client")
	require.NoError(t, client.Wait(), "the client helper failed")

	// No wait for the dump ticker: stopping dumps the table once more and
	// flushes, so what the client sent last is still counted.
	cancel()
	require.NoError(t, <-done)

	srv.mu.Lock()
	reports := srv.reports
	srv.mu.Unlock()

	require.NotEmpty(t, reports)

	type key struct {
		src   string
		host  string
		proto uint8
		port  uint16
	}

	flows := map[key]traffic.Flow{}
	sources := map[string]traffic.HostSource{}

	var queries []traffic.Query

	for _, r := range reports {
		assert.Empty(t, r.Status.Conntrack.Error)
		assert.Empty(t, r.Status.SNI.Error)
		assert.Empty(t, r.Status.DNS.Error)

		for _, f := range r.Flows {
			assert.Contains(t, []string{clientV4, clientV6}, f.Src.String(), "only the tailnet node's traffic is reported")

			k := key{src: f.Src.String(), host: f.Host, proto: f.Proto, port: f.Port}
			agg := flows[k]
			agg.TxBytes += f.TxBytes
			agg.RxBytes += f.RxBytes
			agg.Conns += f.Conns
			agg.Dst = f.Dst
			flows[k] = agg
			sources[f.Host] = f.HostSource
		}

		queries = append(queries, r.Queries...)
	}

	t.Logf("flows: %+v", flows)

	within := func(name string, got, sent uint64) {
		t.Helper()
		assert.GreaterOrEqual(t, got, uint64(sent), "%s: fewer bytes than were sent", name)
		assert.LessOrEqual(t, got, uint64(float64(sent)*1.25)+8000, "%s: far more bytes than were sent", name)
	}

	files := flows[key{src: clientV4, host: filesHost, proto: 6, port: 443}]
	within("files upload", files.TxBytes, filesUp)
	within("files download", files.RxBytes, filesDown)
	assert.Equal(t, uint32(1), files.Conns)
	assert.Equal(t, internetV4, files.Dst.String(), "the destination is the real one, not the masqueraded tuple")
	assert.Equal(t, traffic.HostSNI, sources[filesHost])

	stream := flows[key{src: clientV4, host: streamHost, proto: 6, port: 443}]
	within("stream upload", stream.TxBytes, streamUp)
	within("stream download", stream.RxBytes, streamDown)
	assert.Equal(t, uint32(1), stream.Conns)

	v6 := flows[key{src: clientV6, host: v6Host, proto: 6, port: 443}]
	within("v6 upload", v6.TxBytes, v6Up)
	within("v6 download", v6.RxBytes, v6Down)

	quicFlow := flows[key{src: clientV4, host: quicHost, proto: 17, port: 443}]
	assert.GreaterOrEqual(t, quicFlow.TxBytes, uint64(1200), "the QUIC Initial went out")
	assert.Equal(t, traffic.HostSNI, sources[quicHost])

	// A connection with no handshake to read is named from the DNS answer
	// the same node got.
	plain := flows[key{src: clientV4, host: plainHost, proto: 6, port: 8080}]
	within("plain upload", plain.TxBytes, plainUp)
	within("plain download", plain.RxBytes, plainDown)
	assert.Equal(t, traffic.HostDNS, sources[plainHost])

	asked := map[string]uint32{}
	for _, q := range queries {
		assert.Equal(t, clientV4, q.Src.String())
		asked[q.Name] += q.Count
	}

	assert.Positive(t, asked[filesHost])
	assert.Positive(t, asked[plainHost])
	assert.Positive(t, asked["missing.example.test"])
}

// runServerHelper is "the internet": a TLS and a plain TCP server that
// send what the client asks for, a UDP sink for QUIC, and the DNS upstream.
func runServerHelper() {
	cert := selfSigned()

	for _, addr := range []string{internetV4 + ":443", "[" + internetV6 + "]:443"} {
		ln, err := tls.Listen("tcp", addr, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		if err != nil {
			fail(err)
		}

		go serveTransfers(ln)
	}

	plain, err := net.Listen("tcp", internetV4+":8080")
	if err != nil {
		fail(err)
	}

	go serveTransfers(plain)

	sink, err := net.ListenPacket("udp", internetV4+":443")
	if err != nil {
		fail(err)
	}

	go func() {
		buf := make([]byte, 65535)
		for {
			if _, _, err := sink.ReadFrom(buf); err != nil {
				return
			}
		}
	}()

	resolver, err := net.ListenPacket("udp", internetV4+":53")
	if err != nil {
		fail(err)
	}

	serveDNS(resolver)
}

// serveTransfers reads an 8-byte upload size, that many bytes, an 8-byte
// download size, and writes that many bytes back.
func serveTransfers(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}

		go func() {
			defer conn.Close()

			var hdr [8]byte
			if _, err := io.ReadFull(conn, hdr[:]); err != nil {
				return
			}

			if _, err := io.CopyN(io.Discard, conn, int64(binary.BigEndian.Uint64(hdr[:]))); err != nil {
				return
			}

			if _, err := io.ReadFull(conn, hdr[:]); err != nil {
				return
			}

			_, _ = io.CopyN(conn, zeroReader{}, int64(binary.BigEndian.Uint64(hdr[:])))
		}()
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

func serveDNS(pc net.PacketConn) {
	buf := make([]byte, 65535)

	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}

		q := &dns.Msg{Data: append([]byte(nil), buf[:n]...)}
		if q.Unpack() != nil || len(q.Question) == 0 {
			continue
		}

		m := new(dns.Msg)
		m.ID, m.Response, m.Question = q.ID, true, q.Question
		name := q.Question[0].Header().Name

		switch strings.TrimSuffix(strings.ToLower(name), ".") {
		case filesHost, plainHost:
			m.Answer = []dns.RR{&dns.A{
				Hdr: dns.Header{Name: name, TTL: 60, Class: dns.ClassINET},
				A:   rdata.A{Addr: netip.MustParseAddr(internetV4)},
			}}
		default:
			m.Rcode = dns.RcodeNameError
		}

		if m.Pack() == nil {
			_, _ = pc.WriteTo(m.Data, from)
		}
	}
}

func selfSigned() tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fail(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "e2e"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{filesHost, streamHost, v6Host, quicHost},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		fail(err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// runClientHelper is the tailnet node: it resolves through the gateway's
// resolver and makes the connections the test checks.
func runClientHelper() {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer

			return d.DialContext(ctx, network, net.JoinHostPort(gatewayV4, "53"))
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, name := range []string{filesHost, plainHost} {
		addrs, err := resolver.LookupHost(ctx, name)
		if err != nil || len(addrs) == 0 || addrs[0] != internetV4 {
			fail(fmt.Errorf("resolving %s through the gateway: %v %w", name, addrs, err))
		}
	}

	if _, err := resolver.LookupHost(ctx, "missing.example.test"); err == nil {
		fail(fmt.Errorf("missing.example.test resolved"))
	}

	transfer(internetV4+":443", filesHost, filesUp, filesDown, 0)
	transfer("["+internetV6+"]:443", v6Host, v6Up, v6Down, 0)
	transfer(internetV4+":8080", "", plainUp, plainDown, 0)
	// A connection that stays open across several dumps.
	transfer(internetV4+":443", streamHost, streamUp, streamDown, helperClock)

	quicInitial()
}

func transfer(addr, serverName string, up, down int64, pause time.Duration) {
	var (
		conn net.Conn
		err  error
	)

	if serverName == "" {
		conn, err = net.Dial("tcp", addr)
	} else {
		conn, err = tls.Dial("tcp", addr, &tls.Config{ServerName: serverName, InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // a throwaway certificate
	}

	if err != nil {
		fail(err)
	}
	defer conn.Close()

	var hdr [8]byte

	binary.BigEndian.PutUint64(hdr[:], uint64(up))
	if _, err := conn.Write(hdr[:]); err != nil {
		fail(err)
	}

	if _, err := io.CopyN(conn, zeroReader{}, up); err != nil {
		fail(err)
	}

	time.Sleep(pause) //nolint:forbidigo // keeps the connection open across dumps

	binary.BigEndian.PutUint64(hdr[:], uint64(down))
	if _, err := conn.Write(hdr[:]); err != nil {
		fail(err)
	}

	if _, err := io.CopyN(io.Discard, conn, down); err != nil {
		fail(err)
	}
}

// quicInitial sends a real QUIC client's Initial; nobody answers it.
func quicInitial() {
	endpoint, err := quic.Listen("udp", "0.0.0.0:0", nil)
	if err != nil {
		fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _ = endpoint.Dial(ctx, "udp", internetV4+":443", &quic.Config{
		TLSConfig: &tls.Config{ServerName: quicHost, MinVersion: tls.VersionTLS13, NextProtos: []string{"h3"}},
	})

	_ = endpoint.Close(context.Background())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "helper:", err)
	os.Exit(1)
}
