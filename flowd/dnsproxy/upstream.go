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
	"os"
	"strings"
	"time"

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
)

// upstream is one resolver the proxy forwards to.
type upstream struct {
	// addr is set for a plain DNS resolver.
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
		return upstream{url: s}, nil
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
// should come first in paths.
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
			if err != nil || addr.String() == stubResolverAddr {
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

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	var resp []byte

	if network == "tcp" {
		resp, err = exchangeTCP(conn, query)
	} else {
		resp, err = exchangeUDP(conn, query)
	}

	if err != nil {
		return nil, fmt.Errorf("asking %s: %w", addr, err)
	}

	return resp, nil
}

func exchangeUDP(conn net.Conn, query []byte) ([]byte, error) {
	_, err := conn.Write(query)
	if err != nil {
		return nil, fmt.Errorf("sending: %w", err)
	}

	buf := make([]byte, maxMessageSize)

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

func (e *exchanger) doh(ctx context.Context, url string, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(query))
	if err != nil {
		return nil, fmt.Errorf("building the DoH request: %w", err)
	}

	req.Header.Set("Content-Type", dohContentType)
	req.Header.Set("Accept", dohContentType)

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s answered %s", errDoHStatus, url, resp.Status)
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

// attemptTimeout bounds one upstream attempt.
const attemptTimeout = 2 * time.Second
