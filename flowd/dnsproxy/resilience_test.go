package dnsproxy

import (
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/aislopware/slopscale/flowd/tailnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedUpstream is a UDP-only resolver whose answer to the n-th datagram
// (from 1) is decided by respond: nil drops it, a delay holds the answer.
func scriptedUpstream(t *testing.T, respond func(n int) (answer bool, delay time.Duration)) (string, *atomic.Int32) {
	t.Helper()

	pc, err := new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { pc.Close() })

	var count atomic.Int32

	go func() {
		buf := make([]byte, 65535)

		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}

			answer, delay := respond(int(count.Add(1)))
			if !answer {
				continue
			}

			resp, err := zoneReply(buf[:n], true)
			if err != nil {
				continue
			}

			time.AfterFunc(delay, func() { _, _ = pc.WriteTo(resp, from) })
		}
	}()

	return pc.LocalAddr().String(), &count
}

// TestLostDatagramIsResent drops the first question: the proxy sends it
// again within the same attempt instead of answering SERVFAIL.
func TestLostDatagramIsResent(t *testing.T) {
	addr, count := scriptedUpstream(t, func(n int) (bool, time.Duration) { return n > 1, 0 })
	proxy := startProxy(t, newRecorder(), allowAll, addr)

	start := time.Now()
	m := askUDP(t, proxy.Addrs()[0], query(t, "www.example.test.", dns.TypeA))

	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
	assert.Equal(t, int32(2), count.Load())
	assert.Less(t, time.Since(start), 1500*time.Millisecond)
}

// TestSlowOnlyUpstreamGetsTheWholeBudget answers after 2.5 s, longer than
// one attempt: the only upstream is also the last one, so it may take what
// is left of the query's five seconds.
func TestSlowOnlyUpstreamGetsTheWholeBudget(t *testing.T) {
	addr, _ := scriptedUpstream(t, func(n int) (bool, time.Duration) { return n == 1, 2500 * time.Millisecond })
	proxy := startProxy(t, newRecorder(), allowAll, addr)

	m := askUDP(t, proxy.Addrs()[0], query(t, "www.example.test.", dns.TypeA))
	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
}

// TestHedgesToTheNextUpstream has a first upstream that never answers: the
// second one is asked after a second, not after the first attempt's two.
func TestHedgesToTheNextUpstream(t *testing.T) {
	silent, _ := scriptedUpstream(t, func(int) (bool, time.Duration) { return false, 0 })
	up := startUpstream(t)
	proxy := startProxy(t, newRecorder(), allowAll, silent, up.addr.String())

	start := time.Now()
	m := askUDP(t, proxy.Addrs()[0], query(t, "www.example.test.", dns.TypeA))

	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
	assert.Less(t, time.Since(start), 1800*time.Millisecond)
	assert.Equal(t, int32(1), proxy.preferred.Load())
}

// TestCloseClosesIdleTCPClients holds a connection open without asking:
// Close does not wait for its idle timeout.
func TestCloseClosesIdleTCPClients(t *testing.T) {
	up := startUpstream(t)

	s, err := Start(Config{
		Listen:    []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstreams: []string{up.addr.String()},
		Allowed:   allowAll,
	})
	require.NoError(t, err)

	conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", s.Addrs()[0].String())
	require.NoError(t, err)

	defer conn.Close()

	require.Eventually(t, func() bool {
		s.connMu.Lock()
		defer s.connMu.Unlock()

		return len(s.conns) == 1
	}, 2*time.Second, 5*time.Millisecond)

	start := time.Now()

	require.NoError(t, s.Close())
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}

// closedPromptly reports whether the server closed conn without it having
// sent anything.
func closedPromptly(t *testing.T, conn net.Conn) bool {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))

	_, err := conn.Read(make([]byte, 1))

	return errors.Is(err, io.EOF)
}

// TestTCPRefusesOutsidersAtAccept connects from loopback, which is not a
// tailnet address: the connection is closed at once and takes no slot.
func TestTCPRefusesOutsidersAtAccept(t *testing.T) {
	up := startUpstream(t)
	proxy := startProxy(t, newRecorder(), tailnet.Contains, up.addr.String())

	conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", proxy.Addrs()[0].String())
	require.NoError(t, err)

	defer conn.Close()

	assert.True(t, closedPromptly(t, conn))

	proxy.connMu.Lock()
	defer proxy.connMu.Unlock()

	assert.Empty(t, proxy.conns)
}

// TestTCPPerSourceCap lets one source hold only so many connections, and
// drops the silent ones after the first-message timeout.
func TestTCPPerSourceCap(t *testing.T) {
	up := startUpstream(t)
	proxy := startProxy(t, newRecorder(), allowAll, up.addr.String())
	addr := proxy.Addrs()[0].String()

	held := make([]net.Conn, 0, maxTCPPerSource)

	for range maxTCPPerSource {
		conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", addr)
		require.NoError(t, err)

		held = append(held, conn)
	}

	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()

	require.Eventually(t, func() bool {
		proxy.connMu.Lock()
		defer proxy.connMu.Unlock()

		return proxy.perSource[netip.MustParseAddr("127.0.0.1")] == maxTCPPerSource
	}, 2*time.Second, 5*time.Millisecond)

	extra, err := new(net.Dialer).DialContext(t.Context(), "tcp", addr)
	require.NoError(t, err)

	defer extra.Close()

	assert.True(t, closedPromptly(t, extra), "over the per-source cap")

	// The silent ones go after the first-message timeout, and the source
	// may connect again.
	require.Eventually(t, func() bool {
		proxy.connMu.Lock()
		defer proxy.connMu.Unlock()

		return len(proxy.conns) == 0
	}, tcpFirstTimeout+2*time.Second, 20*time.Millisecond)

	m := askTCP(t, proxy.Addrs()[0], query(t, "www.example.test.", dns.TypeA))
	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
}

// TestFitsAnswersToTheAskersUDPSize gets an answer over 512 bytes: without
// EDNS the asker gets a truncated reply and must use TCP; with a 4096 byte
// buffer it gets the answer.
func TestFitsAnswersToTheAskersUDPSize(t *testing.T) {
	up := startUpstream(t)
	proxy := startProxy(t, newRecorder(), allowAll, up.addr.String())
	addr := proxy.Addrs()[0]

	m := askUDP(t, addr, query(t, "large.test.", dns.TypeA))
	assert.True(t, m.Truncated)
	assert.Empty(t, m.Answer)
	require.Len(t, m.Question, 1)

	big := dns.NewMsg("large.test.", dns.TypeA)
	big.UDPSize = 4096
	require.NoError(t, big.Pack())

	m = askUDP(t, addr, big.Data)
	assert.False(t, m.Truncated)
	assert.Len(t, m.Answer, 60)

	m = askTCP(t, addr, query(t, "large.test.", dns.TypeA))
	assert.Len(t, m.Answer, 60)
}

// TestOverTheRateIsDropped gets no reply, not REFUSED, once a source is
// over its rate.
func TestOverTheRateIsDropped(t *testing.T) {
	up := startUpstream(t)
	proxy := startProxy(t, newRecorder(), allowAll, up.addr.String())
	src := netip.MustParseAddr("127.0.0.1")

	drained := 0
	for proxy.allow(src) {
		drained++
	}

	assert.GreaterOrEqual(t, drained, perSourceBurst)
	assert.Nil(t, proxy.answer(src, query(t, "www.example.test.", dns.TypeA)))
}

// TestNeverForwardsIntoTheTailnet leaves MagicDNS and every tailnet
// address out of the upstreams, from the configuration and resolv.conf.
func TestNeverForwardsIntoTheTailnet(t *testing.T) {
	_, err := Start(Config{
		Listen:    []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstreams: []string{"100.100.100.100", "100.64.0.24:53", "fd7a:115c:a1e0::1", "https://100.64.0.2/dns-query"},
	})
	require.ErrorIs(t, err, ErrNoUpstream)

	conf := filepath.Join(t.TempDir(), "resolv.conf")
	require.NoError(t, os.WriteFile(conf, []byte("nameserver 100.100.100.100\nnameserver 192.0.2.53\n"), 0o600))

	got, err := SystemUpstreams(conf)
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.53"}, got)

	onlyMagic := filepath.Join(t.TempDir(), "resolv.conf")
	require.NoError(t, os.WriteFile(onlyMagic, []byte("nameserver 100.100.100.100\n"), 0o600))

	_, err = SystemUpstreams(onlyMagic)
	require.ErrorIs(t, err, ErrNoNameserver)
}

// TestDoHNameIsLookedUpThroughThePlainUpstreams names the DoH upstream by
// host: the proxy's client resolves it through the bootstrap resolver,
// not the system's.
func TestDoHNameIsLookedUpThroughThePlainUpstreams(t *testing.T) {
	up := startUpstream(t)

	var requests atomic.Int32

	doh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", dohContentType)
		_, _ = w.Write(reply(t, body))
	}))
	t.Cleanup(doh.Close)

	client, err := bootstrapClient([]string{up.addr.String()}, nil)
	require.NoError(t, err)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)

	testTransport, ok := doh.Client().Transport.(*http.Transport)
	require.True(t, ok)

	transport.TLSClientConfig = testTransport.TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "example.com" // the test certificate's name

	port := strconv.Itoa(int(netip.MustParseAddrPort(doh.Listener.Addr().String()).Port()))
	rec := newRecorder()

	s, err := Start(Config{
		Listen:     []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstreams:  []string{"https://doh.test:" + port + "/dns-query"},
		OnQuery:    rec.onQuery,
		HTTPClient: client,
		Logger:     slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	m := askUDP(t, s.Addrs()[0], query(t, "www.example.test.", dns.TypeA))
	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
	assert.Equal(t, int32(1), requests.Load())
	assert.Contains(t, up.seen(), "udp doh.test")

	// Without any plain resolver a DoH name cannot be looked up, and the
	// system resolver is not asked instead.
	none, err := bootstrapClient(nil, nil)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://doh.test:"+port+"/", http.NoBody)
	require.NoError(t, err)

	_, err = none.Do(req) //nolint:bodyclose // it fails before any response
	require.ErrorContains(t, err, errNoBootstrap.Error())
}

// TestServesTheAddressesItCanBind skips an address that is not on the host
// instead of refusing to run at all.
func TestServesTheAddressesItCanBind(t *testing.T) {
	up := startUpstream(t)

	s, err := Start(Config{
		Listen: []netip.AddrPort{
			netip.MustParseAddrPort("127.0.0.1:0"),
			netip.MustParseAddrPort("192.0.2.1:0"), // TEST-NET-1, on no interface
		},
		Upstreams: []string{up.addr.String()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	require.Len(t, s.Addrs(), 1)
	assert.Equal(t, "127.0.0.1", s.Addrs()[0].Addr().String())
	require.Len(t, s.Failed(), 1)
	assert.Contains(t, s.Failed()[0].Error(), "192.0.2.1")

	_, err = Start(Config{
		Listen:    []netip.AddrPort{netip.MustParseAddrPort("192.0.2.1:0")},
		Upstreams: []string{up.addr.String()},
	})
	require.ErrorIs(t, err, ErrNoListen)
}

// TestLeavesAPortServedOnAllAddresses finds a resolver on *:53 in the
// socket tables and reports it rather than racing it for the port.
func TestLeavesAPortServedOnAllAddresses(t *testing.T) {
	dir := t.TempDir()
	table := "  sl  local_address rem_address   st\n" +
		"   0: 3500007F:0035 00000000:0000 07\n" + // 127.0.0.53:53, not a wildcard
		"   1: 00000000:0035 00000000:0000 07\n" // 0.0.0.0:53
	require.NoError(t, os.WriteFile(filepath.Join(dir, "udp"), []byte(table), 0o600))

	owner, ok := wildcardListener(dir, 53)
	assert.True(t, ok)
	assert.Equal(t, "udp *:53", owner)

	_, ok = wildcardListener(dir, 5353)
	assert.False(t, ok)

	_, ok = wildcardListener(t.TempDir(), 53)
	assert.False(t, ok, "no tables, nothing to report")

	previous := procNetDir
	procNetDir = dir

	t.Cleanup(func() { procNetDir = previous })

	_, err := Start(Config{
		Listen:    []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:53")},
		Upstreams: []string{"192.0.2.53"},
	})
	require.ErrorIs(t, err, ErrPortTaken)
}

// TestDeadListenerIsReported closes a listener under the proxy: the loop
// stops and says so, instead of leaving a resolver that answers nothing.
func TestDeadListenerIsReported(t *testing.T) {
	up := startUpstream(t)
	proxy := startProxy(t, newRecorder(), allowAll, up.addr.String())

	require.NoError(t, proxy.udp[0].Close())

	select {
	case <-proxy.Died():
	case <-time.After(2 * time.Second):
		t.Fatal("the dead listener was not reported")
	}

	require.ErrorIs(t, proxy.Err(), net.ErrClosed)
}

// TestTruncatedReplyShape keeps the question and the answer's rcode.
func TestTruncatedReplyShape(t *testing.T) {
	q := query(t, "large.test.", dns.TypeA)
	resp := reply(t, q)
	require.Greater(t, len(resp), minUDPSize)

	out := fitUDP(q, resp)

	m := &dns.Msg{Data: out}
	require.NoError(t, m.Unpack())
	assert.True(t, m.Truncated)
	assert.True(t, m.Response)
	assert.Equal(t, binary.BigEndian.Uint16(q), m.ID)
	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
	require.Len(t, m.Question, 1)

	small := query(t, "www.example.test.", dns.TypeA)
	fits := reply(t, small)
	assert.Equal(t, fits, fitUDP(small, fits), "a reply that fits goes out unchanged")
}
