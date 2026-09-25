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
	"time"

	"codeberg.org/miekg/dns"
	"golang.org/x/time/rate"
)

const (
	headerLen        = 12
	queryTimeout     = 5 * time.Second
	tcpIdleTimeout   = 10 * time.Second
	maxInflight      = 512
	maxTCPConns      = 128
	perSourceRate    = 100 // questions per second
	perSourceBurst   = 200
	limiterIdle      = 10 * time.Minute
	flagQR           = 0x80 // first flags byte
	flagRA           = 0x80 // second flags byte
	rcodeMask        = 0x0f
	pointerMask      = 0xc0
	questionTailLen  = 4 // type and class
	countsOffset     = 4
	countFields      = 4
	maxLabelsPerName = 128
)

var (
	// ErrNoListen is returned by [Start] without an address to listen on.
	ErrNoListen = errors.New("no address to listen on")
	// ErrNoUpstream is returned by [Start] without a usable upstream.
	ErrNoUpstream = errors.New("no upstream resolver to forward to")
)

// Config is how a proxy runs.
type Config struct {
	// Listen are the addresses to answer on, UDP and TCP each.
	Listen []netip.AddrPort
	// Upstreams are the resolvers to forward to (IP, IP:port or https://).
	Upstreams []string
	// Allowed says whether a source may ask; others get REFUSED.
	Allowed func(netip.Addr) bool
	// OnQuery is told about every question: who asked, the name (lower
	// case, no trailing dot) and whether the answer was an error.
	OnQuery func(src netip.Addr, name string, failed bool)
	// OnAnswer is told the addresses an answer handed out for the name
	// asked, following CNAME chains, and the smallest TTL on the way.
	OnAnswer func(src netip.Addr, name string, addrs []netip.Addr, ttl time.Duration)
	// HTTPClient is used for DNS-over-HTTPS upstreams.
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
	tcpConns  chan struct{}

	udp   []net.PacketConn
	tcp   []net.Listener
	addrs []netip.AddrPort

	limMu    sync.Mutex
	limiters map[netip.Addr]*limiter

	ctx    context.Context //nolint:containedctx // the server's lifetime, cancelled by Close
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type limiter struct {
	*rate.Limiter

	lastUsed time.Time
}

// Start listens on every address in cfg and serves until [Server.Close].
func Start(cfg Config) (*Server, error) {
	if len(cfg.Listen) == 0 {
		return nil, ErrNoListen
	}

	s := &Server{
		cfg:      cfg,
		inflight: make(chan struct{}, maxInflight),
		tcpConns: make(chan struct{}, maxTCPConns),
		limiters: make(map[netip.Addr]*limiter),
	}
	s.ex.http = cmpOr(cfg.HTTPClient, http.DefaultClient)
	s.cfg.Logger = cmpOr(cfg.Logger, slog.New(slog.DiscardHandler))
	s.ctx, s.cancel = context.WithCancel(context.Background())

	for _, raw := range cfg.Upstreams {
		u, err := parseUpstream(raw)
		if err != nil {
			return nil, err
		}

		// Forwarding to one of our own addresses would loop.
		if u.url == "" && slices.Contains(cfg.Listen, u.addr) {
			continue
		}

		s.upstreams = append(s.upstreams, u)
	}

	if len(s.upstreams) == 0 {
		return nil, ErrNoUpstream
	}

	err := s.listen()
	if err != nil {
		_ = s.Close()

		return nil, err
	}

	return s, nil
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

// Close stops the proxy and waits for its goroutines.
func (s *Server) Close() error {
	s.cancel()

	errs := make([]error, 0, len(s.udp)+len(s.tcp))

	for _, pc := range s.udp {
		errs = append(errs, pc.Close())
	}

	for _, ln := range s.tcp {
		errs = append(errs, ln.Close())
	}

	s.wg.Wait()

	return errors.Join(errs...)
}

func (s *Server) listen() error {
	var lc net.ListenConfig

	for _, addr := range s.cfg.Listen {
		pc, err := lc.ListenPacket(s.ctx, "udp", addr.String())
		if err != nil {
			return fmt.Errorf("listening on udp %s: %w", addr, err)
		}

		s.udp = append(s.udp, pc)

		bound, err := netip.ParseAddrPort(pc.LocalAddr().String())
		if err != nil {
			return fmt.Errorf("reading the bound address: %w", err)
		}

		bound = netip.AddrPortFrom(bound.Addr().Unmap(), bound.Port())
		s.addrs = append(s.addrs, bound)

		// TCP on the same port as UDP, which matters when the caller
		// asked for port 0.
		ln, err := lc.Listen(s.ctx, "tcp", bound.String())
		if err != nil {
			return fmt.Errorf("listening on tcp %s: %w", bound, err)
		}

		s.tcp = append(s.tcp, ln)

		s.wg.Add(2)

		go s.serveUDP(pc)
		go s.serveTCP(ln)
	}

	return nil
}

func (s *Server) serveUDP(pc net.PacketConn) {
	defer s.wg.Done()

	buf := make([]byte, maxMessageSize)

	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if s.ctx.Err() == nil {
				s.cfg.Logger.Error("reading a DNS question", "err", err)
			}

			return
		}

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
				_, _ = pc.WriteTo(resp, src)
			}
		})
	}
}

func (s *Server) serveTCP(ln net.Listener) {
	defer s.wg.Done()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.ctx.Err() == nil {
				s.cfg.Logger.Error("accepting a DNS connection", "err", err)
			}

			return
		}

		select {
		case s.tcpConns <- struct{}{}:
		default:
			_ = conn.Close()

			continue
		}

		s.wg.Go(func() {
			defer func() { <-s.tcpConns }()
			defer conn.Close()

			s.serveTCPConn(conn)
		})
	}
}

func (s *Server) serveTCPConn(conn net.Conn) {
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}

	src := addr.AddrPort().Addr().Unmap()

	for s.ctx.Err() == nil {
		_ = conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout))

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

	if !s.allow(src) {
		return errorReply(query, dns.RcodeRefused)
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

func (s *Server) forward(ctx context.Context, query []byte) ([]byte, error) {
	first := int(s.preferred.Load())

	var errs []error

	for i := range s.upstreams {
		idx := (first + i) % len(s.upstreams)

		attempt, cancel := context.WithTimeout(ctx, attemptTimeout)
		resp, err := s.ex.exchange(attempt, s.upstreams[idx], query)

		cancel()

		if err == nil {
			s.preferred.Store(int32(idx)) //nolint:gosec // bounded by the upstream count

			return resp, nil
		}

		errs = append(errs, err)

		if ctx.Err() != nil {
			break
		}
	}

	return nil, errors.Join(errs...)
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
