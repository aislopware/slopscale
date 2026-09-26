// Package dnsproxy is the agent's logging resolver. It listens on the
// gateway's tailnet addresses, so each question arrives from the asking
// node's own tailnet address, forwards it unchanged to the upstream
// resolvers, returns the answer unchanged, and tells the agent who asked
// what and which addresses the answer handed out.
package dnsproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/aislopware/slopscale/flowd/tailnet"
	"golang.org/x/time/rate"
)

const (
	headerLen        = 12
	queryTimeout     = 5 * time.Second
	tcpIdleTimeout   = 10 * time.Second
	tcpFirstTimeout  = 2 * time.Second
	maxInflight      = 512
	maxTCPConns      = 128
	maxTCPPerSource  = 8
	perSourceRate    = 500 // questions per second
	perSourceBurst   = 1000
	limiterIdle      = 10 * time.Minute
	flagQR           = 0x80 // first flags byte
	flagRA           = 0x80 // second flags byte
	rcodeMask        = 0x0f
	pointerMask      = 0xc0
	questionTailLen  = 4 // type and class
	countsOffset     = 4
	countFields      = 4
	maxLabelsPerName = 128
	minUDPSize       = 512
	bindAttempts     = 10
	acceptBackoffMin = 5 * time.Millisecond
	acceptBackoffMax = time.Second
)

var (
	// ErrNoListen is returned by [Start] without an address to listen on,
	// or when none of them could be bound.
	ErrNoListen = errors.New("no address to listen on")
	// ErrNoUpstream is returned by [Start] without a usable upstream.
	ErrNoUpstream = errors.New("no upstream resolver to forward to")
	// ErrPortTaken is returned for a listen address whose port another
	// process already serves on every address, like a LAN resolver
	// (dnsmasq, Pi-hole) on 0.0.0.0:53. The proxy does not fight it for
	// the port: whichever started second would fail, and the LAN's DNS
	// with it.
	ErrPortTaken = errors.New("another process listens on this port on all addresses")

	errProbe = errors.New("the probe failed")
)

// Config is how a proxy runs.
type Config struct {
	// Listen are the addresses to answer on, UDP and TCP each.
	Listen []netip.AddrPort
	// Upstreams are the resolvers to forward to (IP, IP:port or https://).
	// Tailnet addresses are left out: the tailnet's resolvers may be this
	// proxy, so forwarding there would loop.
	Upstreams []string
	// Bootstrap are plain resolvers (IP or IP:port) that look up the host
	// names of DoH upstreams after the IP upstreams among Upstreams.
	// Without either, a DoH upstream must be given by address.
	Bootstrap []string
	// Allowed says whether a source may ask; others get REFUSED over UDP
	// and a closed connection over TCP.
	Allowed func(netip.Addr) bool
	// OnQuery is told about every question: who asked, the name (lower
	// case, no trailing dot) and whether the answer was an error.
	OnQuery func(src netip.Addr, name string, failed bool)
	// OnAnswer is told the addresses an answer handed out for the name
	// asked, following CNAME chains, and the smallest TTL on the way.
	OnAnswer func(src netip.Addr, name string, addrs []netip.Addr, ttl time.Duration)
	// HTTPClient is used for DNS-over-HTTPS upstreams; nil builds one
	// that resolves through Bootstrap.
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// Server is a running proxy.
type Server struct {
	cfg       Config
	upstreams []upstream
	preferred atomic.Int32
	ex        exchanger
	inflight  chan struct{}

	udp    []net.PacketConn
	tcp    []net.Listener
	addrs  []netip.AddrPort
	failed []error

	connMu    sync.Mutex
	conns     map[net.Conn]netip.Addr
	perSource map[netip.Addr]int

	limMu    sync.Mutex
	limiters map[netip.Addr]*limiter

	died     chan struct{}
	dieOnce  sync.Once
	dieCause atomic.Pointer[error]

	ctx    context.Context //nolint:containedctx // the server's lifetime, cancelled by Close
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type limiter struct {
	*rate.Limiter

	lastUsed time.Time
}

// Start listens on the addresses in cfg it can bind and serves until
// [Server.Close]. An address that cannot be bound is skipped and reported
// by [Server.Failed]; Start fails only when none can be.
func Start(cfg Config) (*Server, error) {
	if len(cfg.Listen) == 0 {
		return nil, ErrNoListen
	}

	s := &Server{
		cfg:       cfg,
		inflight:  make(chan struct{}, maxInflight),
		conns:     make(map[net.Conn]netip.Addr),
		perSource: make(map[netip.Addr]int),
		limiters:  make(map[netip.Addr]*limiter),
		died:      make(chan struct{}),
	}
	s.cfg.Logger = cmpOr(cfg.Logger, slog.New(slog.DiscardHandler))
	s.ctx, s.cancel = context.WithCancel(context.Background())

	var err error

	s.upstreams, err = parseUpstreams(s.cfg)
	if err != nil {
		return nil, err
	}

	s.ex.http = cfg.HTTPClient
	if s.ex.http == nil {
		s.ex.http, err = bootstrapClient(cfg.Bootstrap, s.upstreams)
		if err != nil {
			return nil, err
		}
	}

	s.listen()

	if len(s.addrs) == 0 {
		_ = s.Close()

		return nil, fmt.Errorf("%w: %w", ErrNoListen, errors.Join(s.failed...))
	}

	return s, nil
}

// parseUpstreams returns the upstreams in cfg it may forward to.
func parseUpstreams(cfg Config) ([]upstream, error) {
	var out []upstream

	for _, raw := range cfg.Upstreams {
		u, err := parseUpstream(raw)
		if err != nil {
			return nil, err
		}

		switch {
		case u.url == "" && slices.Contains(cfg.Listen, u.addr):
			cfg.Logger.Warn("not forwarding to the resolver's own address", "upstream", raw)
		case u.addr.IsValid() && tailnet.Contains(u.addr.Addr()):
			cfg.Logger.Warn("not forwarding to a tailnet address, which may be this resolver", "upstream", raw)
		default:
			out = append(out, u)
		}
	}

	if len(out) == 0 {
		return nil, ErrNoUpstream
	}

	return out, nil
}

func cmpOr[T comparable](v, fallback T) T {
	var zero T
	if v == zero {
		return fallback
	}

	return v
}

// Addrs are the addresses the proxy answers on.
func (s *Server) Addrs() []netip.AddrPort {
	return slices.Clone(s.addrs)
}

// Failed are the listen addresses the proxy could not bind, one error each.
func (s *Server) Failed() []error {
	return slices.Clone(s.failed)
}

// Died is closed when a listener stops for good while the proxy is meant
// to be running, so its owner can start a new one; [Server.Err] says why.
func (s *Server) Died() <-chan struct{} {
	return s.died
}

// Err is why the proxy died, nil while it runs.
func (s *Server) Err() error {
	if p := s.dieCause.Load(); p != nil {
		return *p
	}

	return nil
}

// Close stops the proxy, closing its listeners and every open client
// connection, and waits for its goroutines.
func (s *Server) Close() error {
	s.cancel()

	errs := make([]error, 0, len(s.udp)+len(s.tcp))
	keep := func(err error) {
		// A listener that died is already closed.
		if !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}

	for _, pc := range s.udp {
		keep(pc.Close())
	}

	for _, ln := range s.tcp {
		keep(ln.Close())
	}

	s.connMu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.connMu.Unlock()

	s.wg.Wait()

	return errors.Join(errs...)
}

// Probe asks the upstreams for the root's name servers, the way a question
// from a client would travel, and says whether an answer came back.
func (s *Server) Probe(ctx context.Context) error {
	msg := dns.NewMsg(".", dns.TypeNS)

	err := msg.Pack()
	if err != nil {
		return fmt.Errorf("building the probe: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	resp, err := s.forward(ctx, msg.Data)
	if err != nil {
		return err
	}

	if rcode := resp[flagsOffset+1] & rcodeMask; rcode == dns.RcodeServerFailure || rcode == dns.RcodeRefused {
		return fmt.Errorf("%w: the upstream answered %s", errProbe, dns.RcodeToString[uint16(rcode)])
	}

	return nil
}

func (s *Server) listen() {
	for _, addr := range s.cfg.Listen {
		if addr.Port() != 0 {
			if owner, ok := wildcardListener(procNetDir, addr.Port()); ok {
				s.failed = append(s.failed, fmt.Errorf("%s: %w (%s)", addr, ErrPortTaken, owner))

				continue
			}
		}

		pc, ln, err := s.bind(addr)
		if err != nil {
			s.failed = append(s.failed, err)

			continue
		}

		bound, _ := netip.ParseAddrPort(pc.LocalAddr().String())
		s.addrs = append(s.addrs, netip.AddrPortFrom(bound.Addr().Unmap(), bound.Port()))
		s.udp = append(s.udp, pc)
		s.tcp = append(s.tcp, ln)

		s.wg.Add(2)

		go s.serveUDP(pc)
		go s.serveTCP(ln)
	}
}

// bind opens UDP and TCP on the same port. With port 0 the kernel picks
// the UDP port, which another process may hold for TCP (the two are
// separate namespaces), so the pair is retried a few times.
func (s *Server) bind(addr netip.AddrPort) (net.PacketConn, net.Listener, error) {
	var lc net.ListenConfig

	for attempt := 1; ; attempt++ {
		pc, err := lc.ListenPacket(s.ctx, "udp", addr.String())
		if err != nil {
			return nil, nil, fmt.Errorf("listening on udp %s: %w", addr, bindError(err))
		}

		bound, err := netip.ParseAddrPort(pc.LocalAddr().String())
		if err != nil {
			_ = pc.Close()

			return nil, nil, fmt.Errorf("reading the bound address: %w", err)
		}

		ln, err := lc.Listen(s.ctx, "tcp", netip.AddrPortFrom(bound.Addr().Unmap(), bound.Port()).String())
		if err == nil {
			return pc, ln, nil
		}

		_ = pc.Close()

		if addr.Port() != 0 || attempt == bindAttempts || !errors.Is(err, syscall.EADDRINUSE) {
			return nil, nil, fmt.Errorf("listening on tcp %s: %w", bound, bindError(err))
		}
	}
}

// bindError explains an address that is in use, the usual reason being a
// local resolver that started first.
func bindError(err error) error {
	if errors.Is(err, syscall.EADDRINUSE) {
		return fmt.Errorf("%w (another process serves this address)", err)
	}

	return err
}

// die records that a listener stopped for good.
func (s *Server) die(err error) {
	if s.ctx.Err() != nil {
		return
	}

	s.dieOnce.Do(func() {
		s.dieCause.Store(&err)
		s.cfg.Logger.Error("a resolver listener stopped", "err", err)
		close(s.died)
	})
}

// retryable reports whether a read or accept error is worth waiting out:
// anything but the socket being closed (running out of descriptors or
// buffers passes).
func retryable(err error) bool {
	return !errors.Is(err, net.ErrClosed)
}

func (s *Server) backoff(d time.Duration) time.Duration {
	d = min(max(2*d, acceptBackoffMin), acceptBackoffMax)

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-s.ctx.Done():
	case <-timer.C:
	}

	return d
}

func (s *Server) serveUDP(pc net.PacketConn) {
	defer s.wg.Done()

	buf := make([]byte, maxMessageSize)

	var delay time.Duration

	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}

			if !retryable(err) {
				s.die(fmt.Errorf("reading DNS questions: %w", err))

				return
			}

			s.cfg.Logger.Warn("reading a DNS question failed; retrying", "err", err)
			delay = s.backoff(delay)

			continue
		}

		delay = 0

		src, ok := from.(*net.UDPAddr)
		if !ok {
			continue
		}

		query := slices.Clone(buf[:n])

		select {
		case s.inflight <- struct{}{}:
		default:
			continue // overloaded: drop, the client retries
		}

		s.wg.Go(func() {
			defer func() { <-s.inflight }()

			resp := s.answer(src.AddrPort().Addr().Unmap(), query)
			if resp != nil {
				_, _ = pc.WriteTo(fitUDP(query, resp), src)
			}
		})
	}
}

func (s *Server) serveTCP(ln net.Listener) {
	defer s.wg.Done()

	var delay time.Duration

	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}

			if !retryable(err) {
				s.die(fmt.Errorf("accepting DNS connections: %w", err))

				return
			}

			s.cfg.Logger.Warn("accepting a DNS connection failed; retrying", "err", err)
			delay = s.backoff(delay)

			continue
		}

		delay = 0

		src, ok := s.admit(conn)
		if !ok {
			_ = conn.Close()

			continue
		}

		s.wg.Go(func() {
			defer s.release(conn, src)

			s.serveTCPConn(conn, src)
		})
	}
}

// admit takes a slot for a new connection: its source must be allowed and
// hold fewer than [maxTCPPerSource], and the proxy fewer than
// [maxTCPConns], so no source can starve the others of TCP.
func (s *Server) admit(conn net.Conn) (netip.Addr, bool) {
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return netip.Addr{}, false
	}

	src := addr.AddrPort().Addr().Unmap()

	if s.cfg.Allowed != nil && !s.cfg.Allowed(src) {
		return src, false
	}

	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.ctx.Err() != nil || len(s.conns) >= maxTCPConns || s.perSource[src] >= maxTCPPerSource {
		return src, false
	}

	s.conns[conn] = src
	s.perSource[src]++

	return src, true
}

func (s *Server) release(conn net.Conn, src netip.Addr) {
	_ = conn.Close()

	s.connMu.Lock()
	defer s.connMu.Unlock()

	delete(s.conns, conn)

	s.perSource[src]--
	if s.perSource[src] <= 0 {
		delete(s.perSource, src)
	}
}

func (s *Server) serveTCPConn(conn net.Conn, src netip.Addr) {
	// A client that connects and says nothing gets a short leash; one
	// that asks keeps the connection for more.
	timeout := tcpFirstTimeout

	for s.ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		timeout = tcpIdleTimeout

		query, err := readTCPMessage(conn)
		if err != nil {
			return
		}

		resp := s.answer(src, query)
		if resp == nil {
			return
		}

		out, err := withLengthPrefix(resp)
		if err != nil {
			return
		}

		_ = conn.SetWriteDeadline(time.Now().Add(tcpIdleTimeout))

		_, err = conn.Write(out)
		if err != nil {
			return
		}
	}
}

// answer returns the reply to a question from src, or nil for input that
// deserves none.
func (s *Server) answer(src netip.Addr, query []byte) []byte {
	if len(query) < headerLen || query[flagsOffset]&flagQR != 0 {
		return nil
	}

	if s.cfg.Allowed != nil && !s.cfg.Allowed(src) {
		return errorReply(query, dns.RcodeRefused)
	}

	// Over the rate the question is dropped rather than refused: the
	// client retries later instead of taking a refusal for an answer.
	if !s.allow(src) {
		return nil
	}

	name := questionName(query)

	ctx, cancel := context.WithTimeout(s.ctx, queryTimeout)
	defer cancel()

	resp, err := s.forward(ctx, query)
	if err != nil {
		s.cfg.Logger.Debug("no upstream answered", "name", name, "err", err)
		s.report(src, name, true)

		return errorReply(query, dns.RcodeServerFailure)
	}

	s.inspect(src, name, resp)

	return resp
}

// fitUDP returns resp when it fits the UDP size the question advertised
// (512 without EDNS), else an empty truncated reply, so the client asks
// again over TCP instead of losing a fragmented datagram.
func fitUDP(query, resp []byte) []byte {
	limit := minUDPSize

	q := &dns.Msg{Data: query}
	if q.Unpack() == nil && q.UDPSize > minUDPSize {
		limit = int(q.UDPSize)
	}

	if len(resp) <= limit {
		return resp
	}

	out := errorReply(query, 0)
	out[flagsOffset] |= truncatedBit
	out[flagsOffset+1] = out[flagsOffset+1]&^rcodeMask | resp[flagsOffset+1]&rcodeMask

	return out
}

// inspect reports the question and the addresses the answer handed out.
func (s *Server) inspect(src netip.Addr, name string, resp []byte) {
	msg := &dns.Msg{Data: resp}
	if msg.Unpack() != nil {
		s.report(src, name, false)

		return
	}

	failed := msg.Rcode == dns.RcodeNameError || msg.Rcode == dns.RcodeServerFailure || msg.Rcode == dns.RcodeRefused
	s.report(src, name, failed)

	if name == "" || s.cfg.OnAnswer == nil || failed {
		return
	}

	addrs, ttl := answerAddrs(name, msg.Answer)
	if len(addrs) > 0 {
		s.cfg.OnAnswer(src, name, addrs, ttl)
	}
}

func (s *Server) report(src netip.Addr, name string, failed bool) {
	if name != "" && s.cfg.OnQuery != nil {
		s.cfg.OnQuery(src, name, failed)
	}
}

// answerAddrs returns the A and AAAA addresses the answer gives for name,
// following CNAMEs from it, and the smallest TTL along the way.
func answerAddrs(name string, answer []dns.RR) ([]netip.Addr, time.Duration) {
	aliases := map[string]bool{name: true}

	var minTTL uint32 = 1<<32 - 1

	// Chains are short; iterate until no new alias appears, bounded by
	// the answer count so a loop cannot spin.
	for range answer {
		grew := false

		for _, rr := range answer {
			cname, ok := rr.(*dns.CNAME)
			if !ok || !aliases[normalise(cname.Hdr.Name)] || aliases[normalise(cname.Target)] {
				continue
			}

			aliases[normalise(cname.Target)] = true
			minTTL = min(minTTL, cname.Hdr.TTL)
			grew = true
		}

		if !grew {
			break
		}
	}

	var addrs []netip.Addr

	for _, rr := range answer {
		switch rr := rr.(type) {
		case *dns.A:
			if aliases[normalise(rr.Hdr.Name)] {
				addrs = append(addrs, rr.Addr)
				minTTL = min(minTTL, rr.Hdr.TTL)
			}
		case *dns.AAAA:
			if aliases[normalise(rr.Hdr.Name)] {
				addrs = append(addrs, rr.Addr)
				minTTL = min(minTTL, rr.Hdr.TTL)
			}
		}
	}

	return addrs, time.Duration(minTTL) * time.Second
}

func normalise(name string) string {
	return strings.TrimSuffix(strings.ToLower(name), ".")
}

// questionName returns the first question's name, lower case without the
// trailing dot, or "" when the message has none that parses.
func questionName(query []byte) string {
	msg := &dns.Msg{Data: query, Options: dns.MsgOptionUnpackQuestion}
	if msg.Unpack() != nil || len(msg.Question) == 0 {
		return ""
	}

	return normalise(msg.Question[0].Header().Name)
}

func (s *Server) allow(src netip.Addr) bool {
	now := time.Now()

	s.limMu.Lock()
	defer s.limMu.Unlock()

	lim, ok := s.limiters[src]
	if !ok {
		for addr, l := range s.limiters {
			if now.Sub(l.lastUsed) > limiterIdle {
				delete(s.limiters, addr)
			}
		}

		lim = &limiter{Limiter: rate.NewLimiter(perSourceRate, perSourceBurst)}
		s.limiters[src] = lim
	}

	lim.lastUsed = now

	return lim.AllowN(now, 1)
}

// errorReply answers query with rcode, echoing its question section and
// nothing else.
func errorReply(query []byte, rcode uint8) []byte {
	end := questionEnd(query)
	if end < 0 {
		end = headerLen
	}

	resp := slices.Clone(query[:end])
	resp[flagsOffset] |= flagQR
	resp[flagsOffset+1] = resp[flagsOffset+1]&^rcodeMask | flagRA | rcode&rcodeMask

	counts := resp[countsOffset : countsOffset+2*countFields]
	if end == headerLen {
		clear(counts)
	} else {
		clear(counts[2:])
	}

	return resp
}

// questionEnd returns the offset after the question section of a message
// with exactly one question, or -1.
func questionEnd(msg []byte) int {
	if len(msg) < headerLen || binary.BigEndian.Uint16(msg[countsOffset:]) != 1 {
		return -1
	}

	off := headerLen

	for range maxLabelsPerName {
		if off >= len(msg) {
			return -1
		}

		l := int(msg[off])

		switch {
		case l == 0:
			off++

			if off+questionTailLen > len(msg) {
				return -1
			}

			return off + questionTailLen
		case l&pointerMask != 0:
			return -1 // questions are not compressed
		default:
			off += 1 + l
		}
	}

	return -1
}
