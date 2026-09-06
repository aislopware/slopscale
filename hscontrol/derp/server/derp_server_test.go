package server

import (
	"bufio"
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

// allowedDERPClients is consulted by the package-wide verify transport.
// The transport is registered on http.DefaultTransport, which is process
// global, so it is installed once and dispatches on the client key.
var (
	allowedDERPClients   sync.Map // key.NodePublic -> struct{}
	registerVerifyOnce   sync.Once
	errVerifyUnavailable = errors.New("verify handler unavailable")
)

func registerTestVerifyTransport(t *testing.T) {
	t.Helper()

	registerVerifyOnce.Do(func() {
		transport, ok := http.DefaultTransport.(*http.Transport)
		require.True(t, ok)

		transport.RegisterProtocol(DerpVerifyScheme, NewDERPVerifyTransport(
			func(req *http.Request, w io.Writer) error {
				var admit tailcfg.DERPAdmitClientRequest

				err := json.NewDecoder(req.Body).Decode(&admit)
				if err != nil {
					return err
				}

				_, allow := allowedDERPClients.Load(admit.NodePublic)

				return json.NewEncoder(w).Encode(tailcfg.DERPAdmitClientResponse{Allow: allow})
			},
		))
	})
}

func testDERPConfig() *types.DERPConfig {
	return &types.DERPConfig{
		ServerRegionID:   999,
		ServerRegionCode: "headscale",
		ServerRegionName: "Headscale Embedded DERP",
		STUNAddr:         "0.0.0.0:3478",
	}
}

func newTestDERPServer(t *testing.T, serverURL string, cfg *types.DERPConfig) *DERPServer {
	t.Helper()

	srv, err := NewDERPServer(serverURL, key.NewNode(), cfg)
	require.NoError(t, err)

	return srv
}

// startDERPServer serves DERPHandler at /derp and returns the base URL.
func startDERPServer(t *testing.T, cfg *types.DERPConfig) (*DERPServer, string) {
	t.Helper()

	mux := http.NewServeMux()
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	srv := newTestDERPServer(t, httpSrv.URL, cfg)
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

	for _, verify := range []bool{false, true} {
		cfg := testDERPConfig()
		cfg.ServerVerifyClients = verify

		priv := key.NewNode()

		srv, err := NewDERPServer("https://headscale.example.com", priv, cfg)
		require.NoError(t, err)
		assert.Equal(t, "https://headscale.example.com", srv.serverURL)
		assert.True(t, srv.key.Equal(priv))
		assert.Same(t, cfg, srv.cfg)
		assert.NotNil(t, srv.tailscaleDERP)
	}
}

func TestGenerateRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		serverURL string
		stunAddr  string
		wantHost  string
		wantPort  int
		wantSTUN  int
		wantErr   bool
	}{
		{
			name:      "https hostname defaults to 443",
			serverURL: "https://headscale.example.com",
			stunAddr:  "0.0.0.0:3478",
			wantHost:  "headscale.example.com",
			wantPort:  443,
			wantSTUN:  3478,
		},
		{
			name:      "http hostname defaults to 80",
			serverURL: "http://headscale.example.com",
			stunAddr:  "0.0.0.0:3478",
			wantHost:  "headscale.example.com",
			wantPort:  80,
			wantSTUN:  3478,
		},
		{
			name:      "hostname with explicit port",
			serverURL: "https://headscale.example.com:8443",
			stunAddr:  "[::]:3479",
			wantHost:  "headscale.example.com",
			wantPort:  8443,
			wantSTUN:  3479,
		},
		{
			name:      "ipv4 with port",
			serverURL: "http://192.0.2.10:8080",
			stunAddr:  "192.0.2.10:3478",
			wantHost:  "192.0.2.10",
			wantPort:  8080,
			wantSTUN:  3478,
		},
		{
			name:      "ipv6 with port",
			serverURL: "https://[2001:db8::1]:8443",
			stunAddr:  "[2001:db8::1]:3478",
			wantHost:  "2001:db8::1",
			wantPort:  8443,
			wantSTUN:  3478,
		},
		{
			name:      "unparseable server url",
			serverURL: "://bad",
			stunAddr:  "0.0.0.0:3478",
			wantErr:   true,
		},
		{
			name:      "non numeric server port",
			serverURL: "https://headscale.example.com:derp",
			stunAddr:  "0.0.0.0:3478",
			wantErr:   true,
		},
		{
			name:      "stun addr without port",
			serverURL: "https://headscale.example.com",
			stunAddr:  "0.0.0.0",
			wantErr:   true,
		},
		{
			name:      "non numeric stun port",
			serverURL: "https://headscale.example.com",
			stunAddr:  "0.0.0.0:stun",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testDERPConfig()
			cfg.STUNAddr = tt.stunAddr
			cfg.IPv4 = "198.51.100.7"
			cfg.IPv6 = "2001:db8::7"

			srv := newTestDERPServer(t, tt.serverURL, cfg)

			region, err := srv.GenerateRegion()
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, cfg.ServerRegionID, region.RegionID)
			assert.Equal(t, cfg.ServerRegionCode, region.RegionCode)
			assert.Equal(t, cfg.ServerRegionName, region.RegionName)
			require.Len(t, region.Nodes, 1)

			node := region.Nodes[0]
			assert.Equal(t, cfg.ServerRegionID.String(), node.Name)
			assert.Equal(t, cfg.ServerRegionID, node.RegionID)
			assert.Equal(t, tt.wantHost, node.HostName)
			assert.Equal(t, tt.wantPort, node.DERPPort)
			assert.Equal(t, tt.wantSTUN, node.STUNPort)
			assert.Equal(t, cfg.IPv4, node.IPv4)
			assert.Equal(t, cfg.IPv6, node.IPv6)
		})
	}
}

func TestDERPHandlerRejectsNonUpgradeRequests(t *testing.T) {
	t.Parallel()

	srv := newTestDERPServer(t, "http://localhost:8080", testDERPConfig())

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

	srv := newTestDERPServer(t, "http://localhost:8080", testDERPConfig())

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

	srv, baseURL := startDERPServer(t, testDERPConfig())

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

	srv, baseURL := startDERPServer(t, testDERPConfig())

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

	srv, baseURL := startDERPServer(t, testDERPConfig())
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

	_, baseURL := startDERPServer(t, testDERPConfig())
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

	registerTestVerifyTransport(t)

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	cfg := testDERPConfig()
	cfg.ServerVerifyClients = true

	_, baseURL := startDERPServer(t, cfg)

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

func TestDERPBootstrapDNSHandler(t *testing.T) {
	t.Parallel()

	derpMap := &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			1: {
				RegionID: 1,
				Nodes: []*tailcfg.DERPNode{
					// IP literals resolve without touching DNS, which keeps
					// the test deterministic offline.
					{Name: "1a", RegionID: 1, HostName: "127.0.0.1"},
					{Name: "1b", RegionID: 1, HostName: "::1"},
				},
			},
			2: {
				RegionID: 2,
				Nodes: []*tailcfg.DERPNode{
					// RFC 2606 reserves .invalid; the lookup fails and the
					// entry is skipped rather than failing the response.
					{Name: "2a", RegionID: 2, HostName: "derp.headscale.invalid"},
				},
			},
		},
	}

	rec := httptest.NewRecorder()
	DERPBootstrapDNSHandler(
		derpMap.View(),
	)(
		rec,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/bootstrap-dns", nil),
	)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var entries map[string][]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &entries))

	assert.Equal(t, []string{"127.0.0.1"}, entries["127.0.0.1"])
	assert.Equal(t, []string{"::1"}, entries["::1"])
	assert.NotContains(t, entries, "derp.headscale.invalid")
}

func TestDERPBootstrapDNSHandlerEmptyMap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	DERPBootstrapDNSHandler(
		(&tailcfg.DERPMap{}).View(),
	)(
		rec,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/bootstrap-dns", nil),
	)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, "{}", rec.Body.String())
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

func TestDERPVerifyTransportRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("handler output becomes the response body", func(t *testing.T) {
		t.Parallel()

		var gotURL string

		transport := NewDERPVerifyTransport(func(req *http.Request, w io.Writer) error {
			gotURL = req.URL.String()

			return json.NewEncoder(w).Encode(tailcfg.DERPAdmitClientResponse{Allow: true})
		})

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, DerpVerifyScheme+"://verify", http.NoBody)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, DerpVerifyScheme+"://verify", gotURL)

		var admit tailcfg.DERPAdmitClientResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&admit))
		assert.True(t, admit.Allow)
	})

	t.Run("handler error is returned without a response", func(t *testing.T) {
		t.Parallel()

		transport := NewDERPVerifyTransport(func(*http.Request, io.Writer) error {
			return errVerifyUnavailable
		})

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, DerpVerifyScheme+"://verify", http.NoBody)
		require.NoError(t, err)

		resp, err := transport.RoundTrip(req) //nolint:bodyclose // resp is nil on error
		require.ErrorIs(t, err, errVerifyUnavailable)
		assert.Nil(t, resp)
	})
}
