// Package ingress is the Funnel ingress: it takes TLS connections from
// the public internet, reads the server name from the client hello, and
// hands the raw bytes to the tailnet node that serves that name over the
// node's peer API, the way the hosted control plane's ingress servers
// do. The node terminates TLS itself with its own certificate; the
// ingress never sees plaintext.
//
// The ingress is a tailnet node of its own, tagged
// [types.FunnelIngressTag]; the policy gives it the ingress peer
// capability on every node granted the funnel attribute, which is what
// a node checks before it takes a connection (tailscale.com
// ipn/ipnlocal, handleServeIngress). The node then admits only the
// host:port pairs its own serve config allows Funnel for.
package ingress

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Peer API protocol, as the client expects it.
const (
	ingressPath         = "/v0/ingress"
	headerIngressSource = "Tailscale-Ingress-Src"
	headerIngressTarget = "Tailscale-Ingress-Target"
)

// Timeouts: a client that sends no client hello, and a node that does
// not answer the ingress request.
const (
	defaultHelloTimeout = 10 * time.Second
	defaultDialTimeout  = 15 * time.Second
)

// refusalBodyLimit bounds how much of a refusing node's answer goes into
// the error.
const refusalBodyLimit = 1 << 10

var (
	// ErrNoServerName is returned when the client hello names no server,
	// so there is no node to deliver to.
	ErrNoServerName = errors.New("client hello carries no server name")
	// ErrUnknownHost is returned when no node serves the name.
	ErrUnknownHost = errors.New("no node serves this name")
	// ErrIngressRefused is returned when the node did not take the
	// connection: it has no Funnel for the target or does not trust the
	// ingress.
	ErrIngressRefused = errors.New("node refused the ingress connection")

	errPeeked = errors.New("server name read")
)

// Dialer opens a connection over the tailnet, as [tsnet.Server.Dial]
// does.
type Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

// Resolver maps a host name to the peer API address of the node that
// serves it.
type Resolver interface {
	// Resolve returns the node's peer API address, ip:port, or false
	// when no node serves the host.
	Resolve(ctx context.Context, host string) (netip.AddrPort, bool)
}

// Proxy delivers public TLS connections to tailnet nodes.
type Proxy struct {
	dial    Dialer
	resolve Resolver

	// HelloTimeout is how long a client has to send its client hello.
	HelloTimeout time.Duration
	// DialTimeout bounds opening the peer API connection and the node's
	// answer.
	DialTimeout time.Duration

	// wg tracks the connections in flight, so Serve returns after the
	// last one ends.
	wg sync.WaitGroup
}

// New returns a proxy that reaches nodes through dial and finds them
// through resolve.
func New(dial Dialer, resolve Resolver) *Proxy {
	return &Proxy{
		dial:         dial,
		resolve:      resolve,
		HelloTimeout: defaultHelloTimeout,
		DialTimeout:  defaultDialTimeout,
	}
}

// Serve accepts connections on ln and delivers each until ln is closed
// or ctx ends. It returns nil once the listener is closed.
func (p *Proxy) Serve(ctx context.Context, ln net.Listener) error {
	defer p.wg.Wait()

	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}

			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}

			return fmt.Errorf("accepting: %w", err)
		}

		p.wg.Go(func() { p.HandleConn(ctx, conn) })
	}
}

// HandleConn delivers one accepted connection and closes it when done,
// or when the context ends: a shutdown must not wait for an idle client.
func (p *Proxy) HandleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	err := p.deliver(ctx, conn)
	if err != nil && ctx.Err() == nil {
		connLogger(conn).Debug().Err(err).Msg("Funnel ingress connection ended")
	}
}

// deliver reads the server name, finds the node and splices the bytes.
func (p *Proxy) deliver(ctx context.Context, conn net.Conn) error {
	host, replay, err := peekServerName(ctx, conn, p.HelloTimeout)
	if err != nil {
		return err
	}

	peerAPI, ok := p.resolve.Resolve(ctx, host)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownHost, host)
	}

	target := net.JoinHostPort(host, strconv.Itoa(localPort(conn)))

	peer, buffered, err := p.openIngress(ctx, peerAPI, conn.RemoteAddr(), target)
	if err != nil {
		return fmt.Errorf("opening ingress to %s for %s: %w", peerAPI, target, err)
	}
	defer peer.Close()

	// The splice ends when either side closes; on shutdown that is us,
	// on both sides, so a copy blocked on a silent peer returns too.
	stop := context.AfterFunc(ctx, func() { _ = peer.Close() })
	defer stop()

	connLogger(conn).Debug().Str("host", host).Stringer("peerAPI", peerAPI).Msg("Funnel ingress connection opened")

	splice(replay, peer, buffered)

	return nil
}

// openIngress asks the node over its peer API to take the connection
// and returns the peer connection once the node switched protocols,
// with whatever the node sent after the response.
func (p *Proxy) openIngress(
	ctx context.Context,
	peerAPI netip.AddrPort,
	src net.Addr,
	target string,
) (net.Conn, []byte, error) {
	dialCtx, cancel := context.WithTimeout(ctx, p.DialTimeout)
	defer cancel()

	peer, err := p.dial(dialCtx, "tcp", peerAPI.String())
	if err != nil {
		return nil, nil, fmt.Errorf("dialing peer API: %w", err)
	}

	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Scheme: "http", Host: peerAPI.String(), Path: ingressPath},
		Host:   peerAPI.String(),
		Header: http.Header{
			headerIngressSource: []string{src.String()},
			headerIngressTarget: []string{target},
		},
	}

	deadline, _ := dialCtx.Deadline()
	_ = peer.SetDeadline(deadline)

	err = req.Write(peer)
	if err != nil {
		peer.Close()

		return nil, nil, fmt.Errorf("writing ingress request: %w", err)
	}

	reader := bufio.NewReader(peer)

	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		peer.Close()

		return nil, nil, fmt.Errorf("reading ingress response: %w", err)
	}

	_ = peer.SetDeadline(time.Time{})

	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, refusalBodyLimit))
		resp.Body.Close()
		peer.Close()

		return nil, nil, fmt.Errorf("%w: %s %s", ErrIngressRefused, resp.Status, strings.TrimSpace(string(body)))
	}

	buffered := make([]byte, reader.Buffered())
	_, _ = io.ReadFull(reader, buffered)

	return peer, buffered, nil
}

// splice copies bytes both ways until either side ends, half-closing
// the other side's write end so it learns the stream is over.
func splice(client, peer net.Conn, peerBuffered []byte) {
	var wg sync.WaitGroup

	wg.Go(func() {
		_, _ = io.Copy(peer, client)
		closeWrite(peer)
	})

	wg.Go(func() {
		_, _ = io.Copy(client, io.MultiReader(bytes.NewReader(peerBuffered), peer))
		closeWrite(client)
	})

	wg.Wait()
}

// closeWrite half-closes a connection that supports it, and closes the
// rest outright.
func closeWrite(conn net.Conn) {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()

		return
	}

	_ = conn.Close()
}

// localPort is the port the client connected to, which is the port the
// node's serve config allows Funnel for.
func localPort(conn net.Conn) int {
	if addr, ok := conn.LocalAddr().(*net.TCPAddr); ok {
		return addr.Port
	}

	_, port, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return 0
	}

	n, _ := strconv.Atoi(port)

	return n
}

// peekServerName reads the client hello without consuming it and returns
// the server name and a connection that replays the bytes read.
func peekServerName(ctx context.Context, conn net.Conn, timeout time.Duration) (string, net.Conn, error) {
	peek := &peekConn{Conn: conn}

	var host string

	deadline := time.Now().Add(timeout)

	helloCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	_ = conn.SetReadDeadline(deadline)

	// The handshake stops at the client hello: the callback refuses to
	// pick a config, and whatever alert the server side wants to send
	// lands in peekConn.Write, which drops it.
	err := tls.Server(peek, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			host = hello.ServerName

			return nil, errPeeked
		},
	}).HandshakeContext(helloCtx)

	_ = conn.SetReadDeadline(time.Time{})

	if host == "" {
		if err != nil && !errors.Is(err, errPeeked) {
			return "", nil, fmt.Errorf("reading client hello: %w", err)
		}

		return "", nil, ErrNoServerName
	}

	host = strings.ToLower(strings.TrimSuffix(host, "."))

	return host, &replayConn{Conn: conn, reader: io.MultiReader(bytes.NewReader(peek.read.Bytes()), conn)}, nil
}

// peekConn records what is read from the connection and drops writes,
// so a TLS handshake can parse the client hello without sending
// anything.
type peekConn struct {
	net.Conn

	read bytes.Buffer
}

func (c *peekConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Write(p[:n])

	return n, err //nolint:wrapcheck // a net.Conn passes its errors through
}

func (c *peekConn) Write(p []byte) (int, error) {
	return len(p), nil
}

// replayConn is the client connection with the peeked bytes put back in
// front.
type replayConn struct {
	net.Conn

	reader io.Reader
}

func (c *replayConn) Read(p []byte) (int, error) {
	return c.reader.Read(p) //nolint:wrapcheck // a net.Conn passes its errors through
}

// CloseWrite half-closes the client side when the connection can.
func (c *replayConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}

	return c.Close() //nolint:wrapcheck // a net.Conn passes its errors through
}

func connLogger(conn net.Conn) *zerolog.Logger {
	l := log.With().Stringer("src", conn.RemoteAddr()).Logger()

	return &l
}
