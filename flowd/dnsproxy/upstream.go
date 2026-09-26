package dnsproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aislopware/slopscale/flowd/tailnet"
	"golang.org/x/crypto/cryptobyte"
)

const (
	dnsPort          = 53
	maxMessageSize   = 65535
	tcpLengthPrefix  = 2
	flagsOffset      = 2
	truncatedBit     = 0x02 // TC, in the first flags byte
	dohContentType   = "application/dns-message"
	stubResolverAddr = "127.0.0.53"

	// attemptTimeout bounds an attempt at an upstream that is not the
	// last one left; the last one gets what remains of the query's budget.
	attemptTimeout = 2 * time.Second
	// hedgeDelay is how long the proxy waits on one upstream before
	// asking the next one as well; the first answer wins.
	hedgeDelay = time.Second

	dohIdleConns   = 16
	dohIdleTimeout = 90 * time.Second
)

var (
	// errUpstreamID is returned for an answer that is not for the question.
	errUpstreamID = errors.New("upstream answered with a different message id")
	// ErrBadUpstream is returned for an upstream that is not an IP,
	// IP:port or https:// URL.
	ErrBadUpstream = errors.New("upstream is not an IP, IP:port or https:// URL")
	// ErrNoNameserver is returned when no resolv.conf names a usable
	// nameserver.
	ErrNoNameserver = errors.New("no usable nameserver")
	errDoHStatus    = errors.New("DoH upstream answered with an error status")
	errTooLong      = errors.New("message too long for DNS over TCP")
	errNoBootstrap  = errors.New("no plain resolver to look up the DoH upstream's name; give it by address")
)

// udpResends are the waits before sending a UDP question again, doubling
// after the last one: a lost datagram costs half a second, not the query.
var udpResends = []time.Duration{500 * time.Millisecond, time.Second}

// upstream is one resolver the proxy forwards to.
type upstream struct {
	// addr is set for a plain DNS resolver, and for a DoH one given by
	// address (with port 0).
	addr netip.AddrPort
	// url is set for a DNS-over-HTTPS resolver.
	url string
}

func (u upstream) String() string {
	if u.url != "" {
		return u.url
	}

	return u.addr.String()
}

// parseUpstream accepts an IP, IP:port or https:// URL.
func parseUpstream(s string) (upstream, error) {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "https://") {
		parsed, err := url.Parse(s)
		if err != nil || parsed.Hostname() == "" {
			return upstream{}, fmt.Errorf("%w: %q", ErrBadUpstream, s)
		}

		u := upstream{url: s}

		// A DoH upstream given by address keeps it, so a tailnet one is
		// left out like a plain one.
		addr, err := netip.ParseAddr(parsed.Hostname())
		if err == nil {
			u.addr = netip.AddrPortFrom(addr, 0)
		}

		return u, nil
	}

	ap, err := netip.ParseAddrPort(s)
	if err == nil {
		return upstream{addr: ap}, nil
	}

	addr, err := netip.ParseAddr(s)
	if err != nil {
		return upstream{}, fmt.Errorf("%w: %q", ErrBadUpstream, s)
	}

	return upstream{addr: netip.AddrPortFrom(addr, dnsPort)}, nil
}

// SystemUpstreams reads the nameservers of the first resolv.conf in paths
// that names any usable one. systemd-resolved's stub (127.0.0.53) is
// skipped, since its real upstreams are in its own resolv.conf, which
// should come first in paths, and so are tailnet addresses.
func SystemUpstreams(paths ...string) ([]string, error) {
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		var servers []string

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 || fields[0] != "nameserver" {
				continue
			}

			addr, err := netip.ParseAddr(strings.SplitN(fields[1], "%", 2)[0])
			// A tailnet address (MagicDNS at 100.100.100.100) sends the
			// question back into the tailnet, whose resolver may be this
			// proxy.
			if err != nil || addr.String() == stubResolverAddr || tailnet.Contains(addr) {
				continue
			}

			servers = append(servers, addr.String())
		}

		_ = f.Close()

		if len(servers) > 0 {
			return servers, nil
		}
	}

	return nil, fmt.Errorf("%w in %s", ErrNoNameserver, strings.Join(paths, ", "))
}

type result struct {
	idx  int
	resp []byte
	err  error
}

// forward asks the upstreams, the preferred one first. Another one joins
// when an attempt fails or has not answered within [hedgeDelay]; the first
// answer wins and becomes the preferred upstream.
func (s *Server) forward(ctx context.Context, query []byte) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	n := len(s.upstreams)
	first := int(s.preferred.Load())
	results := make(chan result, n)
	launched, pending := 0, 0

	launch := func() {
		idx := (first + launched) % n
		attempt, done := ctx, context.CancelFunc(func() {})

		if launched < n-1 {
			attempt, done = context.WithTimeout(ctx, attemptTimeout)
		}

		launched++
		pending++

		go func() {
			defer done()

			resp, err := s.ex.exchange(attempt, s.upstreams[idx], query)
			results <- result{idx: idx, resp: resp, err: err}
		}()
	}

	launch()

	hedge := time.NewTimer(hedgeDelay)
	defer hedge.Stop()

	var errs []error

	for {
		select {
		case r := <-results:
			pending--

			if r.err == nil {
				s.preferred.Store(int32(r.idx)) //nolint:gosec // bounded by the upstream count

				return r.resp, nil
			}

			errs = append(errs, r.err)

			if launched < n {
				launch()
				hedge.Reset(hedgeDelay)
			} else if pending == 0 {
				return nil, errors.Join(errs...)
			}
		case <-hedge.C:
			if launched < n {
				launch()
				hedge.Reset(hedgeDelay)
			}
		case <-ctx.Done():
			return nil, errors.Join(append(errs, ctx.Err())...)
		}
	}
}

// exchanger sends one query to one upstream.
type exchanger struct {
	dialer net.Dialer
	http   *http.Client
}

func (e *exchanger) exchange(ctx context.Context, u upstream, query []byte) ([]byte, error) {
	if u.url != "" {
		return e.doh(ctx, u.url, query)
	}

	resp, err := e.plain(ctx, "udp", u.addr, query)
	if err != nil {
		return nil, err
	}

	if len(resp) > flagsOffset && resp[flagsOffset]&truncatedBit != 0 {
		return e.plain(ctx, "tcp", u.addr, query)
	}

	return resp, nil
}

func (e *exchanger) plain(ctx context.Context, network string, addr netip.AddrPort, query []byte) ([]byte, error) {
	conn, err := e.dialer.DialContext(ctx, network, addr.String())
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", addr, err)
	}
	defer conn.Close()

	// Closing the connection when ctx ends unblocks a read that no
	// deadline bounds, as when a winning upstream cancels the others.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	var resp []byte

	if network == "tcp" {
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		}

		resp, err = exchangeTCP(conn, query)
	} else {
		resp, err = exchangeUDP(ctx, conn, query)
	}

	if err != nil {
		return nil, fmt.Errorf("asking %s: %w", addr, err)
	}

	return resp, nil
}

// exchangeUDP sends the question and sends it again on the [udpResends]
// schedule until an answer comes or ctx ends.
func exchangeUDP(ctx context.Context, conn net.Conn, query []byte) ([]byte, error) {
	buf := make([]byte, maxMessageSize)

	var wait time.Duration

	for sent := 0; ; sent++ {
		_, err := conn.Write(query)
		if err != nil {
			return nil, fmt.Errorf("sending: %w", err)
		}

		if sent < len(udpResends) {
			wait = udpResends[sent]
		} else {
			wait *= 2
		}

		resp, err := readUDPAnswer(ctx, conn, query, buf, time.Now().Add(wait))
		if err == nil {
			return resp, nil
		}

		var nerr net.Error
		if ctx.Err() != nil || !errors.As(err, &nerr) || !nerr.Timeout() {
			return nil, err
		}
	}
}

// readUDPAnswer reads until the answer to query arrives or the earlier of
// the resend time and ctx's deadline passes.
func readUDPAnswer(ctx context.Context, conn net.Conn, query, buf []byte, resendAt time.Time) ([]byte, error) {
	deadline := resendAt
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	_ = conn.SetReadDeadline(deadline)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("receiving: %w", err)
		}

		// A connected UDP socket only hears from the upstream; a stray
		// datagram with another id is skipped.
		if n >= 2 && bytes.Equal(buf[:2], query[:2]) {
			return bytes.Clone(buf[:n]), nil
		}
	}
}

func exchangeTCP(conn net.Conn, query []byte) ([]byte, error) {
	msg, err := withLengthPrefix(query)
	if err != nil {
		return nil, err
	}

	_, err = conn.Write(msg)
	if err != nil {
		return nil, fmt.Errorf("sending: %w", err)
	}

	resp, err := readTCPMessage(conn)
	if err != nil {
		return nil, err
	}

	if len(resp) < 2 || !bytes.Equal(resp[:2], query[:2]) {
		return nil, errUpstreamID
	}

	return resp, nil
}

// withLengthPrefix frames a message for DNS over TCP.
func withLengthPrefix(msg []byte) ([]byte, error) {
	var b cryptobyte.Builder

	b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) { b.AddBytes(msg) })

	out, err := b.Bytes()
	if err != nil {
		return nil, errTooLong
	}

	return out, nil
}

func readTCPMessage(r io.Reader) ([]byte, error) {
	var prefix [tcpLengthPrefix]byte

	_, err := io.ReadFull(r, prefix[:])
	if err != nil {
		return nil, fmt.Errorf("reading the length: %w", err)
	}

	msg := make([]byte, binary.BigEndian.Uint16(prefix[:]))

	_, err = io.ReadFull(r, msg)
	if err != nil {
		return nil, fmt.Errorf("reading the message: %w", err)
	}

	return msg, nil
}

func (e *exchanger) doh(ctx context.Context, endpoint string, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(query))
	if err != nil {
		return nil, fmt.Errorf("building the DoH request: %w", err)
	}

	req.Header.Set("Content-Type", dohContentType)
	req.Header.Set("Accept", dohContentType)

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s answered %s", errDoHStatus, endpoint, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessageSize))
	if err != nil {
		return nil, fmt.Errorf("reading the DoH answer: %w", err)
	}

	if len(body) < 2 || !bytes.Equal(body[:2], query[:2]) {
		return nil, errUpstreamID
	}

	return body, nil
}

// bootstrapClient is the HTTP client for DoH upstreams. It looks their host
// names up through plain resolvers, never through the system resolver,
// which on a tailnet node may be MagicDNS and so, in the end, this proxy.
func bootstrapClient(bootstrap []string, upstreams []upstream) (*http.Client, error) {
	var resolvers []netip.AddrPort

	for _, u := range upstreams {
		if u.url == "" {
			resolvers = append(resolvers, u.addr)
		}
	}

	for _, raw := range bootstrap {
		u, err := parseUpstream(raw)
		if err != nil || u.url != "" {
			return nil, fmt.Errorf("bootstrap resolver %q: %w", raw, ErrBadUpstream)
		}

		if !tailnet.Contains(u.addr.Addr()) {
			resolvers = append(resolvers, u.addr)
		}
	}

	var dialer net.Dialer

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			if len(resolvers) == 0 {
				return nil, errNoBootstrap
			}

			var errs []error

			for _, r := range resolvers {
				conn, err := dialer.DialContext(ctx, network, r.String())
				if err == nil {
					return conn, nil
				}

				errs = append(errs, err)
			}

			return nil, errors.Join(errs...)
		},
	}

	transport := &http.Transport{
		DialContext:         (&net.Dialer{Resolver: resolver, Timeout: attemptTimeout}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        dohIdleConns,
		IdleConnTimeout:     dohIdleTimeout,
		TLSHandshakeTimeout: attemptTimeout,
	}

	return &http.Client{Transport: transport}, nil
}
