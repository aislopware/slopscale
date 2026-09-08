package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/rs/zerolog/log"
	"tailscale.com/derp"
	"tailscale.com/derp/derpserver"
	"tailscale.com/net/stun"
	"tailscale.com/net/wsconn"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// fastStartHeader is the header (with value "1") that signals to the HTTP
// server that the DERP HTTP client does not want the HTTP 101 response
// headers and it will begin writing & reading the DERP protocol immediately
// following its HTTP request.
const (
	fastStartHeader  = "Derp-Fast-Start"
	DerpVerifyScheme = "headscale-derp-verify"
)

// VerifyFunc answers the relay's admission check for one client: it reads
// a [tailcfg.DERPAdmitClientRequest] from the request and writes a
// [tailcfg.DERPAdmitClientResponse].
type VerifyFunc func(*http.Request, io.Writer) error

// DERPServer is the embedded relay. It is created once with the server's
// key and brought up or down at runtime with [DERPServer.Apply]: the HTTP
// handler answers only while enabled and STUN listens only then.
type DERPServer struct {
	key           key.NodePrivate
	tailscaleDERP *derpserver.Server
	verify        VerifyFunc
	// id keys the verify transport's registry, so several servers in one
	// process (tests) each verify against their own tailnet.
	id string

	enabled       atomic.Bool
	verifyClients atomic.Bool

	mu       sync.Mutex
	stunAddr string
	stunConn *net.UDPConn
	stunStop context.CancelFunc
	stunDone chan struct{}
}

// NewDERPServer creates the relay, off. verify decides which clients the
// relay admits while verification is on; nil admits every client.
func NewDERPServer(derpKey key.NodePrivate, verify VerifyFunc) *DERPServer {
	log.Trace().Caller().Msg("creating new embedded DERP server")

	server := derpserver.New(derpKey, util.TSLogfWrapper())

	d := &DERPServer{
		key:           derpKey,
		tailscaleDERP: server,
		verify:        verify,
		id:            verifyID(derpKey.Public()),
	}

	// The relay always asks; the transport answers "allow" while
	// verification is off. The Tailscale server reads its verify URL
	// without a lock, so it is set once here rather than toggled.
	registerVerifyTransport()
	verifyServers.Store(d.id, d)
	server.SetVerifyClientURL(DerpVerifyScheme + "://" + d.id + "/verify")
	server.SetVerifyClientURLFailOpen(false)

	return d
}

// Apply brings the relay to the settings: it starts STUN on the settings'
// address (rebinding when the address changed), turns client verification
// on or off and opens the handler; or, when the settings turn the relay
// off, stops STUN and closes the handler. A STUN bind failure leaves the
// relay as it was and is returned.
func (d *DERPServer) Apply(s types.DERPServerSettings) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !s.Enabled {
		d.enabled.Store(false)
		d.stopSTUNLocked()

		return nil
	}

	if d.stunConn == nil || d.stunAddr != s.STUNAddr {
		packetConn, err := new(net.ListenConfig).ListenPacket(context.Background(), "udp", s.STUNAddr)
		if err != nil {
			return fmt.Errorf("opening STUN listener on %s: %w", s.STUNAddr, err)
		}

		udpConn, ok := packetConn.(*net.UDPConn)
		if !ok {
			_ = packetConn.Close()

			return errSTUNNotUDP
		}

		d.stopSTUNLocked()
		d.startSTUNLocked(udpConn, s.STUNAddr)
	}

	d.verifyClients.Store(s.VerifyClients)
	d.enabled.Store(true)

	return nil
}

var errSTUNNotUDP = errors.New("stun listener is not a UDP listener")

// verifyID is the host the relay's verify URL carries: the public key as
// lowercase hex, which url.Parse accepts as a host name.
func verifyID(pub key.NodePublic) string {
	text, err := pub.MarshalText()
	if err != nil {
		return "relay"
	}

	return hex.EncodeToString(text)
}

// Close stops STUN and the relay; every connected client is dropped.
func (d *DERPServer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.enabled.Store(false)
	d.stopSTUNLocked()
	verifyServers.Delete(d.id)

	err := d.tailscaleDERP.Close()
	if err != nil {
		return fmt.Errorf("closing DERP server: %w", err)
	}

	return nil
}

// Enabled reports whether the relay is serving.
func (d *DERPServer) Enabled() bool {
	return d.enabled.Load()
}

// VerifyClients reports whether the relay admits only this tailnet's nodes.
func (d *DERPServer) VerifyClients() bool {
	return d.verifyClients.Load()
}

// STUNAddr is the address STUN is bound to, empty while the relay is off.
// It differs from the settings' address when that asked for port 0.
func (d *DERPServer) STUNAddr() string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stunConn == nil {
		return ""
	}

	return d.stunConn.LocalAddr().String()
}

func (d *DERPServer) DERPHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	log.Trace().Caller().Msgf("/derp request from %v", req.RemoteAddr)

	if !d.enabled.Load() {
		http.Error(writer, "embedded DERP server is off", http.StatusNotFound)

		return
	}

	upgrade := strings.ToLower(req.Header.Get("Upgrade"))

	if upgrade != "websocket" && upgrade != "derp" {
		if upgrade != "" {
			log.Warn().
				Caller().
				Msg("No Upgrade header in DERP server request. If headscale is behind a reverse proxy, " +
					"make sure it is configured to pass WebSockets through.")
		}

		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusUpgradeRequired)

		_, err := writer.Write([]byte("DERP requires connection upgrade"))
		if err != nil {
			log.Error().
				Caller().
				Err(err).
				Msg("Failed to write HTTP response")
		}

		return
	}

	if strings.Contains(req.Header.Get("Sec-WebSocket-Protocol"), "derp") {
		d.serveWebsocket(writer, req)
	} else {
		d.servePlain(writer, req)
	}
}

// DERPProbeHandler is the endpoint that js/wasm clients hit to measure
// DERP latency, since they can't do UDP STUN queries.
func DERPProbeHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	switch req.Method {
	case http.MethodHead, http.MethodGet:
		writer.Header().Set("Access-Control-Allow-Origin", "*")
		writer.WriteHeader(http.StatusOK)
	default:
		writer.WriteHeader(http.StatusMethodNotAllowed)

		_, err := writer.Write([]byte("bogus probe method"))
		if err != nil {
			log.Error().
				Caller().
				Err(err).
				Msg("Failed to write HTTP response")
		}
	}
}

func (d *DERPServer) serveWebsocket(writer http.ResponseWriter, req *http.Request) {
	websocketConn, err := websocket.Accept(writer, req, &websocket.AcceptOptions{
		Subprotocols:   []string{"derp"},
		OriginPatterns: []string{"*"},
		// Disable compression because DERP transmits WireGuard messages that
		// are not compressible.
		// Additionally, Safari has a broken implementation of compression
		// (see https://github.com/nhooyr/websocket/issues/218) that makes
		// enabling it actively harmful.
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		log.Error().
			Caller().
			Err(err).
			Msg("Failed to upgrade websocket request")

		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusInternalServerError)

		_, err = writer.Write([]byte("Failed to upgrade websocket request"))
		if err != nil {
			log.Error().
				Caller().
				Err(err).
				Msg("Failed to write HTTP response")
		}

		return
	}
	defer websocketConn.Close(websocket.StatusInternalError, "closing")

	if websocketConn.Subprotocol() != "derp" {
		websocketConn.Close(websocket.StatusPolicyViolation, "client must speak the derp subprotocol")

		return
	}

	wc := wsconn.NetConn(req.Context(), websocketConn, websocket.MessageBinary, req.RemoteAddr)
	brw := bufio.NewReadWriter(bufio.NewReader(wc), bufio.NewWriter(wc))
	d.tailscaleDERP.Accept(req.Context(), wc, brw, req.RemoteAddr)
}

func (d *DERPServer) servePlain(writer http.ResponseWriter, req *http.Request) {
	fastStart := req.Header.Get(fastStartHeader) == "1"

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		log.Error().Caller().Msg("derp requires Hijacker interface from Gin")
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusInternalServerError)

		_, err := writer.Write([]byte("HTTP does not support general TCP support"))
		if err != nil {
			log.Error().
				Caller().
				Err(err).
				Msg("Failed to write HTTP response")
		}

		return
	}

	netConn, conn, err := hijacker.Hijack()
	if err != nil {
		log.Error().Caller().Err(err).Msgf("hijack failed")
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusInternalServerError)

		_, err = writer.Write([]byte("HTTP does not support general TCP support"))
		if err != nil {
			log.Error().
				Caller().
				Err(err).
				Msg("Failed to write HTTP response")
		}

		return
	}

	log.Trace().Caller().Msgf("hijacked connection from %v", req.RemoteAddr)

	if !fastStart {
		pubKey := d.key.Public()
		pubKeyStr, _ := pubKey.MarshalText()
		fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: DERP\r\n"+
			"Connection: Upgrade\r\n"+
			"Derp-Version: %v\r\n"+
			"Derp-Public-Key: %s\r\n\r\n",
			derp.ProtocolVersion,
			string(pubKeyStr))
	}

	d.tailscaleDERP.Accept(req.Context(), netConn, conn, netConn.RemoteAddr().String())
}

func (d *DERPServer) startSTUNLocked(conn *net.UDPConn, addr string) {
	// The listener outlives the request that turned the relay on, so it
	// gets its own context, cancelled by stopSTUNLocked.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	d.stunAddr = addr
	d.stunConn = conn
	d.stunStop = cancel
	d.stunDone = done

	log.Info().Msgf("stun server started at %s", conn.LocalAddr())

	go func() {
		defer close(done)

		serverSTUNListener(ctx, conn)
	}()
}

func (d *DERPServer) stopSTUNLocked() {
	if d.stunConn == nil {
		return
	}

	d.stunStop()
	_ = d.stunConn.Close()
	<-d.stunDone

	log.Info().Msgf("stun server stopped at %s", d.stunAddr)

	d.stunAddr = ""
	d.stunConn = nil
	d.stunStop = nil
	d.stunDone = nil
}

func serverSTUNListener(ctx context.Context, packetConn *net.UDPConn) {
	var buf [64 << 10]byte

	for {
		bytesRead, udpAddr, err := packetConn.ReadFromUDP(buf[:])
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			log.Error().Caller().Err(err).Msgf("stun ReadFrom")

			// Rate limit error logging - wait before retrying, but respect context cancellation
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}

			continue
		}

		log.Trace().Caller().Msgf("stun request from %v", udpAddr)

		pkt := buf[:bytesRead]
		if !stun.Is(pkt) {
			log.Trace().Caller().Msgf("udp packet is not stun")

			continue
		}

		txid, err := stun.ParseBindingRequest(pkt)
		if err != nil {
			log.Trace().Caller().Err(err).Msgf("stun parse error")

			continue
		}

		addr, _ := netip.AddrFromSlice(udpAddr.IP)
		res := stun.Response(
			txid,
			netip.AddrPortFrom(addr, uint16(udpAddr.Port)), //nolint:gosec // port is always <=65535
		)

		_, err = packetConn.WriteTo(res, udpAddr)
		if err != nil {
			log.Trace().Caller().Err(err).Msgf("issue writing to UDP")

			continue
		}
	}
}

// verifyServers maps a relay's id to it, for the verify transport. The
// transport is registered on [http.DefaultTransport], which is process
// global, so it is installed once and dispatches on the URL's host.
var (
	verifyServers      sync.Map // string -> *DERPServer
	verifyRegisterOnce sync.Once
)

var (
	errVerifyUnknownServer  = errors.New("derp verify: unknown server")
	errDefaultTransportType = errors.New("http.DefaultTransport is not an *http.Transport")
)

func registerVerifyTransport() {
	verifyRegisterOnce.Do(func() {
		t, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			log.Error().Err(errDefaultTransportType).Msg("embedded DERP cannot verify clients")

			return
		}

		t.RegisterProtocol(DerpVerifyScheme, verifyTransport{})
	})
}

// verifyTransport answers the relay's admission requests in process: allow
// while the relay does not verify clients, otherwise the relay's
// [VerifyFunc] decides.
type verifyTransport struct{}

func (verifyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	v, ok := verifyServers.Load(req.URL.Host)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errVerifyUnknownServer, req.URL.Host)
	}

	d, ok := v.(*DERPServer)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errVerifyUnknownServer, req.URL.Host)
	}

	buf := new(bytes.Buffer)

	if !d.verifyClients.Load() || d.verify == nil {
		err := json.NewEncoder(buf).Encode(tailcfg.DERPAdmitClientResponse{Allow: true})
		if err != nil {
			return nil, fmt.Errorf("encoding DERP admit response: %w", err)
		}
	} else {
		err := d.verify(req, buf)
		if err != nil {
			log.Error().Caller().Err(err).Msg("failed to handle client verify request")

			return nil, err
		}
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(buf),
	}, nil
}
