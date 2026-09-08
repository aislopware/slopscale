package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/derp"
	"tailscale.com/derp/derphttp"
	"tailscale.com/net/netmon"
	"tailscale.com/net/stun"
	"tailscale.com/net/wsconn"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

const testTimeout = 10 * time.Second

// allowedDERPClients is what the test verify function admits.
var allowedDERPClients sync.Map // key.NodePublic -> struct{}

var errVerifyUnavailable = errors.New("verify handler unavailable")

func testVerify(req *http.Request, w io.Writer) error {
	var admit tailcfg.DERPAdmitClientRequest

	err := json.NewDecoder(req.Body).Decode(&admit)
	if err != nil {
		return err
	}

	_, allow := allowedDERPClients.Load(admit.NodePublic)

	return json.NewEncoder(w).Encode(tailcfg.DERPAdmitClientResponse{Allow: allow})
}

// testServerSettings turns the relay on with STUN on a free loopback port.
func testServerSettings() types.DERPServerSettings {
	return types.DERPServerSettings{
		Enabled:    true,
		RegionID:   999,
		RegionCode: "headscale",
		RegionName: "Headscale Embedded DERP",
		STUNAddr:   "127.0.0.1:0",
	}
}

// newTestDERPServer creates a relay and applies the settings.
func newTestDERPServer(t *testing.T, settings types.DERPServerSettings) *DERPServer {
	t.Helper()

	srv := NewDERPServer(key.NewNode(), testVerify)

	t.Cleanup(func() { _ = srv.Close() })

	require.NoError(t, srv.Apply(settings))

	return srv
}

// startDERPServer serves DERPHandler at /derp and returns the base URL.
func startDERPServer(t *testing.T, settings types.DERPServerSettings) (*DERPServer, string) {
	t.Helper()

	mux := http.NewServeMux()
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	srv := newTestDERPServer(t, settings)
	mux.HandleFunc("/derp", srv.DERPHandler)

	return srv, httpSrv.URL
}

func newDERPClient(ctx context.Context, t *testing.T, priv key.NodePrivate, baseURL string) *derphttp.Client {
	t.Helper()

	client, err := derphttp.NewClient(priv, baseURL+"/derp", t.Logf, netmon.NewStatic())
	require.NoError(t, err)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(func() { _ = client.Close() })

	return client
}

// recvOf drains recv until a message of type T arrives and returns it. The
// server sends ServerInfoMessage (and may send keepalives) before anything
// the test asked for. It runs in a goroutine so a broken handshake fails the
// test after testTimeout instead of hanging the suite.
func recvOf[T derp.ReceivedMessage](t *testing.T, recv func() (derp.ReceivedMessage, error)) T {
	t.Helper()

	type result struct {
		msg T
		err error
	}

	ch := make(chan result, 1)

	go func() {
		for {
			msg, err := recv()
			if err != nil {
				ch <- result{err: err}

				return
			}

			if want, ok := msg.(T); ok {
				ch <- result{msg: want}

				return
			}

			t.Logf("skipping %T while waiting for %T", msg, *new(T))
		}
	}()

	select {
	case res := <-ch:
		require.NoError(t, res.err)

		return res.msg
	case <-time.After(testTimeout):
		t.Fatalf("timed out waiting for %T", *new(T))

		return *new(T)
	}
}

func TestNewDERPServer(t *testing.T) {
	t.Parallel()

	priv := key.NewNode()

	srv := NewDERPServer(priv, testVerify)

	t.Cleanup(func() { _ = srv.Close() })

	assert.True(t, srv.key.Equal(priv))
	assert.NotNil(t, srv.tailscaleDERP)
	assert.False(t, srv.Enabled(), "a new relay is off until settings turn it on")
	assert.Empty(t, srv.STUNAddr())
}

func TestDERPServerApply(t *testing.T) {
	t.Parallel()

	srv := NewDERPServer(key.NewNode(), testVerify)

	t.Cleanup(func() { _ = srv.Close() })

	settings := testServerSettings()
	require.NoError(t, srv.Apply(settings))
	assert.True(t, srv.Enabled())
	assert.False(t, srv.VerifyClients())

	first := srv.STUNAddr()
	require.NotEmpty(t, first)

	// The same address keeps the listener; verification follows the settings.
	settings.VerifyClients = true
	require.NoError(t, srv.Apply(settings))
	assert.True(t, srv.VerifyClients())
	assert.Equal(t, first, srv.STUNAddr(), "unchanged STUN address keeps the socket")

	// A new address rebinds.
	settings.STUNAddr = "127.0.0.1:0"
	require.NoError(t, srv.Apply(settings))
	assert.NotEmpty(t, srv.STUNAddr())

	// Off stops STUN and closes the handler.
	settings.Enabled = false
	require.NoError(t, srv.Apply(settings))
	assert.False(t, srv.Enabled())
	assert.Empty(t, srv.STUNAddr())

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/derp", nil)
	req.Header.Set("Upgrade", "DERP")
	srv.DERPHandler(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDERPServerApplyBadSTUNAddrKeepsState(t *testing.T) {
	t.Parallel()

	srv := newTestDERPServer(t, testServerSettings())
	before := srv.STUNAddr()

	bad := testServerSettings()
	bad.STUNAddr = "256.0.0.1:0"
	require.Error(t, srv.Apply(bad))

	assert.True(t, srv.Enabled())
	assert.Equal(t, before, srv.STUNAddr())
}

func TestDERPServerSTUNAnswers(t *testing.T) {
	t.Parallel()

	srv := newTestDERPServer(t, testServerSettings())

	addr, err := net.ResolveUDPAddr("udp", srv.STUNAddr())
	require.NoError(t, err)

	clientConn, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.Close() })
	require.NoError(t, clientConn.SetDeadline(time.Now().Add(testTimeout)))

	txID := stun.NewTxID()
	_, err = clientConn.Write(stun.Request(txID))
	require.NoError(t, err)

	buf := make([]byte, 1500)
	n, err := clientConn.Read(buf)
	require.NoError(t, err)

	gotTxID, _, err := stun.ParseResponse(buf[:n])
	require.NoError(t, err)
	assert.Equal(t, txID, gotTxID)
}

func TestDERPHandlerRejectsNonUpgradeRequests(t *testing.T) {
	t.Parallel()

	srv := newTestDERPServer(t, testServerSettings())

	for _, upgrade := range []string{"", "h2c"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/derp", nil)
		if upgrade != "" {
			req.Header.Set("Upgrade", upgrade)
		}

		rec := httptest.NewRecorder()
		srv.DERPHandler(rec, req)

		assert.Equal(t, http.StatusUpgradeRequired, rec.Code, "Upgrade=%q", upgrade)
		assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
		assert.Equal(t, "DERP requires connection upgrade", rec.Body.String())
	}
}

func TestDERPHandlerPlainRequiresHijacker(t *testing.T) {
	t.Parallel()

	srv := newTestDERPServer(t, testServerSettings())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/derp", nil)
	req.Header.Set("Upgrade", "DERP")

	// httptest.ResponseRecorder does not implement http.Hijacker.
	rec := httptest.NewRecorder()
	srv.DERPHandler(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "HTTP does not support general TCP support", rec.Body.String())
}

func TestDERPHandlerPlainUpgradeResponse(t *testing.T) {
	t.Parallel()

	srv, baseURL := startDERPServer(t, testServerSettings())

	dialer := &net.Dialer{Timeout: testTimeout}

	conn, err := dialer.DialContext(t.Context(), "tcp", strings.TrimPrefix(baseURL, "http://"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetDeadline(time.Now().Add(testTimeout)))

	_, err = io.WriteString(conn, "GET /derp HTTP/1.1\r\nHost: derp\r\nUpgrade: DERP\r\nConnection: Upgrade\r\n\r\n")
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/derp", http.NoBody)
	require.NoError(t, err)

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	assert.Equal(t, "DERP", resp.Header.Get("Upgrade"))
	assert.Equal(t, "Upgrade", resp.Header.Get("Connection"))

	wantKey, err := srv.key.Public().MarshalText()
	require.NoError(t, err)
	assert.Equal(t, string(wantKey), resp.Header.Get("Derp-Public-Key"))
	assert.NotEmpty(t, resp.Header.Get("Derp-Version"))
}

func TestDERPHandlerPlainHandshakeAndRelay(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	srv, baseURL := startDERPServer(t, testServerSettings())

	alicePriv, bobPriv := key.NewNode(), key.NewNode()
	alice := newDERPClient(ctx, t, alicePriv, baseURL)
	bob := newDERPClient(ctx, t, bobPriv, baseURL)

	assert.Equal(t, srv.key.Public(), alice.ServerPublicKey())

	// Ping round trip proves the plain upgrade negotiated a working session.
	ping := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	require.NoError(t, alice.SendPing(ping))

	assert.Equal(t, derp.PongMessage(ping), recvOf[derp.PongMessage](t, alice.Recv))

	// A packet relayed between two clients proves the server routes by key.
	payload := []byte("hello from bob")
	require.NoError(t, bob.Send(alicePriv.Public(), payload))

	pkt := recvOf[derp.ReceivedPacket](t, alice.Recv)
	assert.Equal(t, bobPriv.Public(), pkt.Source)
	assert.Equal(t, payload, pkt.Data)
}

func TestDERPHandlerWebsocketHandshake(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	srv, baseURL := startDERPServer(t, testServerSettings())
	wsURL := "ws://" + strings.TrimPrefix(baseURL, "http://") + "/derp"

	wsConn, resp, err := websocket.Dial( //nolint:bodyclose // coder/websocket nils resp.Body on success
		ctx,
		wsURL,
		&websocket.DialOptions{
			Subprotocols: []string{"derp"},
		},
	)
	require.NoError(t, err)

	defer wsConn.Close(websocket.StatusNormalClosure, "done")

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	assert.Equal(t, "derp", wsConn.Subprotocol())

	netConn := wsconn.NetConn(ctx, wsConn, websocket.MessageBinary, "test")
	brw := bufio.NewReadWriter(bufio.NewReader(netConn), bufio.NewWriter(netConn))

	client, err := derp.NewClient(key.NewNode(), netConn, brw, t.Logf)
	require.NoError(t, err)
	assert.Equal(t, srv.key.Public(), client.ServerPublicKey())

	ping := [8]byte{8, 7, 6, 5, 4, 3, 2, 1}
	require.NoError(t, client.SendPing(ping))

	assert.Equal(t, derp.PongMessage(ping), recvOf[derp.PongMessage](t, client.Recv))
}

func TestDERPHandlerWebsocketRejectsWrongSubprotocol(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	_, baseURL := startDERPServer(t, testServerSettings())
	wsURL := "ws://" + strings.TrimPrefix(baseURL, "http://") + "/derp"

	// "derp-v2" routes into serveWebsocket (the header contains "derp") but
	// is not a subprotocol the server offers, so negotiation yields none.
	wsConn, _, err := websocket.Dial( //nolint:bodyclose // coder/websocket nils resp.Body on success
		ctx,
		wsURL,
		&websocket.DialOptions{
			Subprotocols: []string{"derp-v2"},
		},
	)
	require.NoError(t, err)

	defer wsConn.Close(websocket.StatusNormalClosure, "done")

	assert.Empty(t, wsConn.Subprotocol())

	_, _, err = wsConn.Read(ctx)
	require.Error(t, err)
	assert.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
}

func TestDERPHandlerVerifyClients(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	settings := testServerSettings()
	settings.VerifyClients = true

	_, baseURL := startDERPServer(t, settings)

	allowed := key.NewNode()
	allowedDERPClients.Store(allowed.Public(), struct{}{})

	client := newDERPClient(ctx, t, allowed, baseURL)
	require.NoError(t, client.SendPing([8]byte{1}))
	assert.Equal(t, derp.PongMessage([8]byte{1}), recvOf[derp.PongMessage](t, client.Recv))

	denied, err := derphttp.NewClient(key.NewNode(), baseURL+"/derp", t.Logf, netmon.NewStatic())
	require.NoError(t, err)
	t.Cleanup(func() { _ = denied.Close() })

	// The HTTP upgrade succeeds; the server closes the connection when the
	// admission check fails, which surfaces on the first frame exchange.
	err = denied.Connect(ctx)
	if err == nil {
		_, err = denied.Recv()
	}

	assert.Error(t, err)
}

func TestDERPProbeHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method   string
		wantCode int
		wantBody string
		wantCORS bool
	}{
		{method: http.MethodGet, wantCode: http.StatusOK, wantCORS: true},
		{method: http.MethodHead, wantCode: http.StatusOK, wantCORS: true},
		{method: http.MethodPost, wantCode: http.StatusMethodNotAllowed, wantBody: "bogus probe method"},
		{method: http.MethodPut, wantCode: http.StatusMethodNotAllowed, wantBody: "bogus probe method"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			DERPProbeHandler(rec, httptest.NewRequestWithContext(t.Context(), tt.method, "/derp/probe", nil))

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Equal(t, tt.wantBody, rec.Body.String())

			if tt.wantCORS {
				assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
			} else {
				assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestServerSTUNListener(t *testing.T) {
	t.Parallel()

	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		serverSTUNListener(ctx, serverConn)
	}()

	t.Cleanup(func() {
		cancel()

		_ = serverConn.Close()

		select {
		case <-done:
		case <-time.After(testTimeout):
			t.Error("STUN listener did not stop after context cancellation")
		}
	})

	serverAddr, ok := serverConn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)

	clientConn, err := net.DialUDP("udp", nil, serverAddr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.Close() })
	require.NoError(t, clientConn.SetDeadline(time.Now().Add(testTimeout)))

	// Non-STUN bytes and a STUN packet that is not a binding request must be
	// dropped without a reply; only the well-formed request that follows is
	// answered, so the response's transaction ID pins which one it was.
	_, err = clientConn.Write([]byte("not a stun packet"))
	require.NoError(t, err)

	notBinding := stun.Request(stun.NewTxID())
	notBinding[1] = 0x02
	require.True(t, stun.Is(notBinding))
	_, err = clientConn.Write(notBinding)
	require.NoError(t, err)

	txID := stun.NewTxID()
	_, err = clientConn.Write(stun.Request(txID))
	require.NoError(t, err)

	buf := make([]byte, 1500)
	n, err := clientConn.Read(buf)
	require.NoError(t, err)

	gotTxID, mapped, err := stun.ParseResponse(buf[:n])
	require.NoError(t, err)
	assert.Equal(t, txID, gotTxID)

	clientAddr, ok := clientConn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	assert.Equal(t, clientAddr.AddrPort(), mapped)
}

func TestVerifyTransportRoundTrip(t *testing.T) {
	t.Parallel()

	admitReq := func(t *testing.T, id string) *http.Request {
		t.Helper()

		body, err := json.Marshal(tailcfg.DERPAdmitClientRequest{NodePublic: key.NewNode().Public()})
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(
			t.Context(), http.MethodPost, DerpVerifyScheme+"://"+id+"/verify", bytes.NewReader(body),
		)
		require.NoError(t, err)

		return req
	}

	decode := func(t *testing.T, resp *http.Response) bool {
		t.Helper()

		defer resp.Body.Close()

		var admit tailcfg.DERPAdmitClientResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&admit))

		return admit.Allow
	}

	t.Run("allows every client while verification is off", func(t *testing.T) {
		t.Parallel()

		srv := newTestDERPServer(t, testServerSettings())

		resp, err := verifyTransport{}.RoundTrip(admitReq(t, srv.id)) //nolint:bodyclose // decode closes it
		require.NoError(t, err)
		assert.True(t, decode(t, resp))
	})

	t.Run("asks the verify function while verification is on", func(t *testing.T) {
		t.Parallel()

		settings := testServerSettings()
		settings.VerifyClients = true
		srv := newTestDERPServer(t, settings)

		resp, err := verifyTransport{}.RoundTrip(admitReq(t, srv.id)) //nolint:bodyclose // decode closes it
		require.NoError(t, err)
		assert.False(t, decode(t, resp), "an unknown key is refused")
	})

	t.Run("verify error is returned without a response", func(t *testing.T) {
		t.Parallel()

		srv := NewDERPServer(key.NewNode(), func(*http.Request, io.Writer) error {
			return errVerifyUnavailable
		})

		t.Cleanup(func() { _ = srv.Close() })

		settings := testServerSettings()
		settings.VerifyClients = true

		require.NoError(t, srv.Apply(settings))

		resp, err := verifyTransport{}.RoundTrip(admitReq(t, srv.id)) //nolint:bodyclose // resp is nil on error
		require.ErrorIs(t, err, errVerifyUnavailable)
		assert.Nil(t, resp)
	})

	t.Run("unknown server is refused", func(t *testing.T) {
		t.Parallel()

		resp, err := verifyTransport{}.RoundTrip(admitReq(t, "nobody")) //nolint:bodyclose // resp is nil on error
		require.ErrorIs(t, err, errVerifyUnknownServer)
		assert.Nil(t, resp)
	})
}
