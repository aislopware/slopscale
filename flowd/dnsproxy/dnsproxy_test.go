package dnsproxy

import (
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/aislopware/slopscale/flowd/tailnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upstreamServer is a real resolver on loopback, UDP and TCP on one port,
// answering from a fixed zone.
type upstreamServer struct {
	addr netip.AddrPort

	mu      sync.Mutex
	queries []string // "udp name" / "tcp name"
}

func hdr(name string, ttl uint32) dns.Header {
	return dns.Header{Name: name, TTL: ttl, Class: dns.ClassINET}
}

// reply builds the zone's answer to query, for the test goroutine.
func reply(t *testing.T, query []byte) []byte {
	t.Helper()

	resp, err := zoneReply(query, false)
	require.NoError(t, err)

	return resp
}

// zoneReply builds the zone's answer to query.
func zoneReply(query []byte, overUDP bool) ([]byte, error) {
	q := &dns.Msg{Data: query}

	err := q.Unpack()
	if err != nil {
		return nil, err
	}

	m := new(dns.Msg)
	m.ID = q.ID
	m.Response = true
	m.RecursionDesired = true
	m.RecursionAvailable = true
	m.Question = q.Question

	switch normalise(q.Question[0].Header().Name) {
	case "www.example.test":
		m.Answer = []dns.RR{
			&dns.CNAME{Hdr: hdr("www.example.test.", 300), Target: "edge.CDN.test."},
			&dns.A{Hdr: hdr("edge.cdn.test.", 60), Addr: netip.MustParseAddr("192.0.2.10")},
			&dns.AAAA{Hdr: hdr("edge.cdn.test.", 120), Addr: netip.MustParseAddr("2001:db8::10")},
			// An unrelated record in the answer attributes nothing.
			&dns.A{Hdr: hdr("other.test.", 60), Addr: netip.MustParseAddr("192.0.2.99")},
		}
	case "big.test":
		if overUDP {
			m.Truncated = true
		} else {
			m.Answer = []dns.RR{&dns.A{Hdr: hdr("big.test.", 30), Addr: netip.MustParseAddr("192.0.2.20")}}
		}
	case "large.test":
		// Well over 512 bytes, well under 4096, and never truncated by the
		// upstream: the proxy has to fit it to the asker.
		for i := range 60 {
			m.Answer = append(m.Answer, &dns.A{
				Hdr:  hdr("large.test.", 60),
				Addr: netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)}),
			})
		}
	case "doh.test":
		m.Answer = []dns.RR{&dns.A{Hdr: hdr("doh.test.", 60), Addr: netip.MustParseAddr("127.0.0.1")}}
	case "loop.test":
		m.Answer = []dns.RR{
			&dns.CNAME{Hdr: hdr("loop.test.", 60), Target: "loop2.test."},
			&dns.CNAME{Hdr: hdr("loop2.test.", 60), Target: "loop.test."},
		}
	default:
		m.Rcode = dns.RcodeNameError
	}

	err = m.Pack()

	return m.Data, err
}

func startUpstream(t *testing.T) *upstreamServer {
	t.Helper()

	pc, ln, port := listenPair(t)

	u := &upstreamServer{addr: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), port)}

	t.Cleanup(func() {
		pc.Close()
		ln.Close()
	})

	go func() {
		buf := make([]byte, 65535)

		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}

			u.record("udp", buf[:n])

			resp, err := zoneReply(buf[:n], true)
			if err == nil {
				_, _ = pc.WriteTo(resp, from)
			}
		}
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go func() {
				defer conn.Close()

				query, err := readTCPMessage(conn)
				if err != nil {
					return
				}

				u.record("tcp", query)

				resp, err := zoneReply(query, false)
				if err != nil {
					return
				}

				_, _ = conn.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(resp))), resp...))
			}()
		}
	}()

	return u
}

// listenPair opens UDP and TCP on one loopback port. The kernel picks the
// UDP port, which some other process may hold for TCP, so a collision is
// retried rather than failing the test.
func listenPair(t *testing.T) (net.PacketConn, net.Listener, uint16) {
	t.Helper()

	var lc net.ListenConfig

	for range 20 {
		pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
		require.NoError(t, err)

		bound, err := netip.ParseAddrPort(pc.LocalAddr().String())
		require.NoError(t, err)

		ln, err := lc.Listen(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", itoa(int(bound.Port()))))
		if err == nil {
			return pc, ln, bound.Port()
		}

		pc.Close()
	}

	t.Fatal("no free UDP and TCP port pair")

	return nil, nil, 0
}

func (u *upstreamServer) record(network string, query []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.queries = append(u.queries, network+" "+questionName(query))
}

func (u *upstreamServer) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return append([]string(nil), u.queries...)
}

func itoa(n int) string {
	return netip.AddrPortFrom(netip.IPv4Unspecified(), uint16(n)).String()[len("0.0.0.0:"):]
}

// recorder keeps what the proxy reported.
type recorder struct {
	mu      sync.Mutex
	queries []string
	answers map[string][]netip.Addr
	ttls    map[string]time.Duration
}

func newRecorder() *recorder {
	return &recorder{answers: map[string][]netip.Addr{}, ttls: map[string]time.Duration{}}
}

func (r *recorder) onQuery(src netip.Addr, name string, failed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	status := "ok"
	if failed {
		status = "failed"
	}

	r.queries = append(r.queries, src.String()+" "+name+" "+status)
}

func (r *recorder) onAnswer(_ netip.Addr, name string, addrs []netip.Addr, ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.answers[name] = addrs
	r.ttls[name] = ttl
}

func (r *recorder) snapshot() ([]string, map[string][]netip.Addr, map[string]time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.queries...), r.answers, r.ttls
}

func startProxy(t *testing.T, rec *recorder, allowed func(netip.Addr) bool, upstreams ...string) *Server {
	t.Helper()

	s, err := Start(Config{
		Listen:    []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstreams: upstreams,
		Allowed:   allowed,
		OnQuery:   rec.onQuery,
		OnAnswer:  rec.onAnswer,
		Logger:    slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	return s
}

func allowAll(netip.Addr) bool { return true }

func query(t *testing.T, name string, qtype uint16) []byte {
	t.Helper()

	m := dns.NewMsg(name, qtype)
	require.NoError(t, m.Pack())

	return m.Data
}

func askUDP(t *testing.T, server netip.AddrPort, q []byte) *dns.Msg {
	t.Helper()

	conn, err := new(net.Dialer).DialContext(t.Context(), "udp", server.String())
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))

	_, err = conn.Write(q)
	require.NoError(t, err)

	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	require.NoError(t, err)

	m := &dns.Msg{Data: buf[:n]}
	require.NoError(t, m.Unpack())
	assert.Equal(t, binary.BigEndian.Uint16(q), m.ID)

	return m
}

func askTCP(t *testing.T, server netip.AddrPort, q []byte) *dns.Msg {
	t.Helper()

	conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", server.String())
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))

	_, err = conn.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(q))), q...))
	require.NoError(t, err)

	resp, err := readTCPMessage(conn)
	require.NoError(t, err)

	m := &dns.Msg{Data: resp}
	require.NoError(t, m.Unpack())

	return m
}

// TestForwardsAndAttributes asks through the proxy over UDP and TCP and
// checks the answer arrives unchanged while the proxy reports the name
// asked and the addresses at the end of its CNAME chain.
func TestForwardsAndAttributes(t *testing.T) {
	up := startUpstream(t)
	rec := newRecorder()
	proxy := startProxy(t, rec, allowAll, up.addr.String())
	addr := proxy.Addrs()[0]

	m := askUDP(t, addr, query(t, "WWW.Example.test.", dns.TypeA))
	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
	assert.Len(t, m.Answer, 4)

	m = askTCP(t, addr, query(t, "nothing.test.", dns.TypeAAAA))
	assert.Equal(t, uint16(dns.RcodeNameError), m.Rcode)

	queries, answers, ttls := rec.snapshot()
	assert.Equal(t, []string{"127.0.0.1 www.example.test ok", "127.0.0.1 nothing.test failed"}, queries)
	assert.Equal(t, map[string][]netip.Addr{
		"www.example.test": {netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10")},
	}, answers)
	assert.Equal(t, 60*time.Second, ttls["www.example.test"])
	assert.Equal(t, []string{"udp www.example.test", "udp nothing.test"}, up.seen())
}

func TestTruncatedAnswerRetriesOverTCP(t *testing.T) {
	up := startUpstream(t)
	rec := newRecorder()
	proxy := startProxy(t, rec, allowAll, up.addr.String())

	m := askUDP(t, proxy.Addrs()[0], query(t, "big.test.", dns.TypeA))
	require.Len(t, m.Answer, 1)
	assert.Equal(t, []string{"udp big.test", "tcp big.test"}, up.seen())
}

func TestCNAMELoopIsBounded(t *testing.T) {
	up := startUpstream(t)
	rec := newRecorder()
	proxy := startProxy(t, rec, allowAll, up.addr.String())

	askUDP(t, proxy.Addrs()[0], query(t, "loop.test.", dns.TypeA))

	_, answers, _ := rec.snapshot()
	assert.Empty(t, answers)
}

// TestRefusesNonTailnetSources runs with the agent's real predicate: a
// question from loopback is not from the tailnet, so it is refused without
// reaching the upstream.
func TestRefusesNonTailnetSources(t *testing.T) {
	up := startUpstream(t)
	rec := newRecorder()
	proxy := startProxy(t, rec, tailnet.Contains, up.addr.String())

	m := askUDP(t, proxy.Addrs()[0], query(t, "www.example.test.", dns.TypeA))
	assert.Equal(t, uint16(dns.RcodeRefused), m.Rcode)
	require.Len(t, m.Question, 1)
	assert.Equal(t, "www.example.test.", m.Question[0].Header().Name)
	assert.Empty(t, m.Answer)
	assert.Empty(t, up.seen())

	queries, _, _ := rec.snapshot()
	assert.Empty(t, queries)
}

// TestFailsOver sends to a dead upstream first: the proxy moves to the
// next one, then prefers it; with none left it answers SERVFAIL.
func TestFailsOver(t *testing.T) {
	dead, err := new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	deadAddr := dead.LocalAddr().String()
	require.NoError(t, dead.Close())

	up := startUpstream(t)
	rec := newRecorder()
	proxy := startProxy(t, rec, allowAll, deadAddr, up.addr.String())

	m := askUDP(t, proxy.Addrs()[0], query(t, "www.example.test.", dns.TypeA))
	assert.Equal(t, uint16(dns.RcodeSuccess), m.Rcode)
	assert.Equal(t, int32(1), proxy.preferred.Load())

	only := startProxy(t, rec, allowAll, deadAddr)
	m = askUDP(t, only.Addrs()[0], query(t, "www.example.test.", dns.TypeA))
	assert.Equal(t, uint16(dns.RcodeServerFailure), m.Rcode)
	require.Len(t, m.Question, 1)

	queries, _, _ := rec.snapshot()
	assert.Contains(t, queries, "127.0.0.1 www.example.test failed")
}

func TestDNSOverHTTPSUpstream(t *testing.T) {
	var requests int

	doh := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		if r.Header.Get("Content-Type") != dohContentType {
			http.Error(w, "bad type", http.StatusUnsupportedMediaType)

			return
		}

		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", dohContentType)
		_, _ = w.Write(reply(t, body))
	}))
	t.Cleanup(doh.Close)

	rec := newRecorder()

	s, err := Start(Config{
		Listen:     []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")},
		Upstreams:  []string{doh.URL + "/dns-query"},
		OnQuery:    rec.onQuery,
		OnAnswer:   rec.onAnswer,
		HTTPClient: doh.Client(),
		Logger:     slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })

	m := askUDP(t, s.Addrs()[0], query(t, "big.test.", dns.TypeA))
	require.Len(t, m.Answer, 1)
	assert.Equal(t, 1, requests)

	_, answers, _ := rec.snapshot()
	assert.Equal(t, []netip.Addr{netip.MustParseAddr("192.0.2.20")}, answers["big.test"])
}

func TestRateLimitPerSource(t *testing.T) {
	up := startUpstream(t)
	rec := newRecorder()
	proxy := startProxy(t, rec, allowAll, up.addr.String())

	src := netip.MustParseAddr("100.64.0.9")
	allowed := 0

	for range perSourceBurst + 50 {
		if proxy.allow(src) {
			allowed++
		}
	}

	assert.GreaterOrEqual(t, allowed, perSourceBurst)
	assert.Less(t, allowed, perSourceBurst+50)
	assert.True(t, proxy.allow(netip.MustParseAddr("100.64.0.10")), "another source has its own budget")
}

func TestIgnoresGarbage(t *testing.T) {
	up := startUpstream(t)
	rec := newRecorder()
	proxy := startProxy(t, rec, allowAll, up.addr.String())

	assert.Nil(t, proxy.answer(netip.MustParseAddr("127.0.0.1"), []byte{1, 2, 3}))

	// A response sent to the proxy is not a question.
	resp := reply(t, query(t, "www.example.test.", dns.TypeA))
	assert.Nil(t, proxy.answer(netip.MustParseAddr("127.0.0.1"), resp))
	assert.Empty(t, up.seen())
}

func TestStartRejects(t *testing.T) {
	_, err := Start(Config{Upstreams: []string{"1.1.1.1"}})
	require.Error(t, err, "no listen address")

	_, err = Start(
		Config{Listen: []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")}, Upstreams: []string{"not an ip"}},
	)
	require.Error(t, err)

	// Forwarding to itself is refused.
	self := netip.MustParseAddrPort("127.0.0.1:5399")
	_, err = Start(Config{Listen: []netip.AddrPort{self}, Upstreams: []string{self.String()}})
	require.Error(t, err)
}

func TestSystemUpstreams(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub.conf")
	realConf := filepath.Join(dir, "real.conf")

	require.NoError(t, os.WriteFile(stub, []byte("nameserver 127.0.0.53\noptions edns0\n"), 0o600))
	require.NoError(
		t,
		os.WriteFile(
			realConf,
			[]byte("# resolved\nnameserver 192.0.2.53\nnameserver fe80::1%eth0\nsearch lan\n"),
			0o600,
		),
	)

	got, err := SystemUpstreams(filepath.Join(dir, "missing.conf"), stub, realConf)
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.53", "fe80::1"}, got)

	_, err = SystemUpstreams(stub)
	require.Error(t, err)
}

func TestErrorReplyShape(t *testing.T) {
	q := query(t, "a.test.", dns.TypeA)
	resp := errorReply(q, dns.RcodeRefused)

	m := &dns.Msg{Data: resp}
	require.NoError(t, m.Unpack())
	assert.True(t, m.Response)
	assert.Equal(t, uint16(dns.RcodeRefused), m.Rcode)
	assert.Len(t, m.Question, 1)

	// A header without a parsable question gets an empty reply.
	resp = errorReply(q[:headerLen], dns.RcodeServerFailure)
	assert.Len(t, resp, headerLen)
}
