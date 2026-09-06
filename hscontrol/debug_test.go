package hscontrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

const (
	debugLoopbackAddr = "127.0.0.1:41234"
	debugUserName     = "debug-user"
	debugNodeName     = "debug-node"
	debugAllowAllACL  = `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`
)

// debugTestEnv is a Headscale with one user and one registered node in the
// NodeStore, plus the debug mux as served by debugHTTPServer.
type debugTestEnv struct {
	app     *Headscale
	handler http.Handler
	node    types.NodeView
}

func newDebugTestEnv(t *testing.T) debugTestEnv {
	t.Helper()

	app := createTestApp(t)

	user := app.state.CreateUserForTest(debugUserName)
	node := app.state.CreateRegisteredNodeForTest(user, debugNodeName)
	node.User = user
	view := app.state.PutNodeInStoreForTest(*node)

	return debugTestEnv{
		app:     app,
		handler: app.debugHTTPServer().Handler,
		node:    view,
	}
}

// debugRequest performs a request from loopback, which is the access model
// the debug mux is gated on, and returns the recorded response.
func (e debugTestEnv) debugRequest(t *testing.T, target, accept string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	req.RemoteAddr = debugLoopbackAddr

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)

	return rec
}

func (e debugTestEnv) text(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := e.debugRequest(t, target, "")
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))

	return rec
}

func (e debugTestEnv) jsonInto(t *testing.T, target string, out any) {
	t.Helper()

	rec := e.debugRequest(t, target, "application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
}

// connectNodeToBatcher registers the env node as connected on the batcher
// so the batcher and ping endpoints have something to report.
func (e debugTestEnv) connectNodeToBatcher(t *testing.T) {
	t.Helper()

	ch := make(chan *tailcfg.MapResponse, 16)
	require.NoError(t, e.app.mapBatcher.AddNode(e.node.ID(), ch, tailcfg.CapabilityVersion(100), nil))
	t.Cleanup(func() { e.app.mapBatcher.RemoveNode(e.node.ID(), ch) })
}

func TestProtectedDebugHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		remoteAddr string
		forwarded  string
		wantCode   int
	}{
		{name: "loopback", remoteAddr: "127.0.0.1:1", wantCode: http.StatusOK},
		{name: "ipv6 loopback", remoteAddr: "[::1]:1", wantCode: http.StatusOK},
		{name: "tailscale cgnat", remoteAddr: "100.64.0.9:1", wantCode: http.StatusOK},
		{name: "rfc1918", remoteAddr: "10.20.30.40:1", wantCode: http.StatusOK},
		{name: "rfc4193", remoteAddr: "[fd12:3456::1]:1", wantCode: http.StatusOK},
		{name: "public", remoteAddr: "203.0.113.5:1", wantCode: http.StatusForbidden},
		{name: "malformed", remoteAddr: "not-an-addr", wantCode: http.StatusForbidden},
		{
			// tsweb refuses proxied requests outright; the private fallback
			// only trusts the socket peer, not the forwarded header.
			name:       "forwarded private from public peer",
			remoteAddr: "203.0.113.5:1",
			forwarded:  "10.0.0.1",
			wantCode:   http.StatusForbidden,
		},
		{
			name:       "forwarded from private peer",
			remoteAddr: "192.168.1.2:1",
			forwarded:  "203.0.113.5",
			wantCode:   http.StatusOK,
		},
	}

	handler := protectedDebugHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/debug/statsviz/", nil)
			req.RemoteAddr = tt.remoteAddr

			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)

			if tt.wantCode == http.StatusOK {
				assert.Equal(t, "ok", rec.Body.String())
			} else {
				assert.Contains(t, rec.Body.String(), "debug access denied")
			}
		})
	}
}

func TestDebugMuxRejectsNonLocalPeers(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	for _, target := range []string{"/debug/", "/debug/overview", "/debug/config", "/debug/statsviz/"} {
		// httptest.NewRequest defaults RemoteAddr to 192.0.2.1, a TEST-NET
		// address that is neither loopback, Tailscale, nor private.
		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

		assert.Equal(t, http.StatusForbidden, rec.Code, target)
		assert.Contains(t, rec.Body.String(), "debug access denied", target)
	}
}

func TestDebugIndexListsEndpoints(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	rec := env.debugRequest(t, "/debug/", "")
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	for _, slug := range []string{
		"overview", "config", "policy", "filter", "ssh", "derp", "nodestore",
		"registration-cache", "routes", "policy-manager", "mapresponses",
		"batcher", "ping", "statsviz",
	} {
		assert.Contains(t, body, `href="/debug/`+slug, slug)
	}

	assert.Contains(t, body, `href="/metrics"`)
}

func TestDebugOverview(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	body := env.text(t, "/debug/overview").Body.String()
	assert.Contains(t, body, "=== Headscale State Overview ===")
	assert.Contains(t, body, "Nodes: 1 total")
	assert.Contains(t, body, "Users: 1 total")
	assert.Contains(t, body, "  - "+debugUserName+": 1 nodes")
	assert.Contains(t, body, "  - Mode: "+string(types.PolicyModeDB))
	assert.NotContains(t, body, "  - Path:")

	var info state.DebugOverviewInfo

	env.jsonInto(t, "/debug/overview", &info)
	assert.Equal(t, 1, info.Nodes.Total)
	assert.Equal(t, 0, info.Nodes.Online)
	assert.Equal(t, 1, info.TotalUsers)
	assert.Equal(t, map[string]int{debugUserName: 1}, info.Users)
	assert.Equal(t, string(types.PolicyModeDB), info.Policy.Mode)
	assert.Empty(t, info.Policy.Path)
}

func TestDebugConfig(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	// The config endpoint is JSON regardless of Accept.
	rec := env.debugRequest(t, "/debug/config", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var cfg types.Config

	//nolint:musttag // types.Config carries no json tags; the debug endpoint marshals it by field name
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cfg))
	assert.Equal(t, env.app.cfg.ServerURL, cfg.ServerURL)
	assert.Equal(t, env.app.cfg.Policy.Mode, cfg.Policy.Mode)
}

func TestDebugPolicy(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	_, err := env.app.state.SetPolicyInDB(debugAllowAllACL)
	require.NoError(t, err)

	rec := env.text(t, "/debug/policy")
	assert.JSONEq(t, debugAllowAllACL, rec.Body.String())

	rec = env.debugRequest(t, "/debug/policy", "application/json")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, debugAllowAllACL, rec.Body.String())
}

func TestDebugPolicyMissingIsServerError(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	// createTestApp uses policy mode db with no policy row, so GetPolicy
	// fails and the handler surfaces it as a generic 500.
	rec := env.debugRequest(t, "/debug/policy", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")
}

func TestDebugFilter(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	_, err := env.app.state.SetPolicy([]byte(debugAllowAllACL))
	require.NoError(t, err)

	var rules []tailcfg.FilterRule

	env.jsonInto(t, "/debug/filter", &rules)
	require.NotEmpty(t, rules)
	// "*" is expanded to the tailnet prefixes, so check shape rather than text.
	assert.NotEmpty(t, rules[0].SrcIPs)
	assert.NotEmpty(t, rules[0].DstPorts)
}

func TestDebugSSH(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	var policies map[string]*tailcfg.SSHPolicy

	env.jsonInto(t, "/debug/ssh", &policies)

	wantKey := "id:" + strconv.FormatUint(env.node.ID().Uint64(), 10) +
		" hostname:" + debugNodeName + " givenname:" + env.node.GivenName()
	assert.Contains(t, policies, wantKey)
}

func TestDebugDERP(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	env.app.state.SetDERPMap(nil)

	assert.Equal(t, "DERP Map: not configured\n", env.text(t, "/debug/derp").Body.String())

	var info state.DebugDERPInfo

	env.jsonInto(t, "/debug/derp", &info)
	assert.False(t, info.Configured)
	assert.Equal(t, 0, info.TotalRegions)

	env.app.state.SetDERPMap(&tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			900: {
				RegionID:   900,
				RegionName: "Debug Region",
				Nodes: []*tailcfg.DERPNode{
					{Name: "900a", RegionID: 900, HostName: "derp.headscale.test", DERPPort: 443, STUNPort: 3478},
				},
			},
		},
	})

	body := env.text(t, "/debug/derp").Body.String()
	assert.Contains(t, body, "=== DERP Map Configuration ===")
	assert.Contains(t, body, "Total Regions: 1")
	assert.Contains(t, body, "Region 900: Debug Region")
	assert.Contains(t, body, "    - 900a (derp.headscale.test:443)")
	assert.Contains(t, body, "      STUN: 3478")

	info = state.DebugDERPInfo{}

	env.jsonInto(t, "/debug/derp", &info)
	assert.True(t, info.Configured)
	assert.Equal(t, 1, info.TotalRegions)
	require.Contains(t, info.Regions, tailcfg.DERPRegionID(900))
	require.Len(t, info.Regions[900].Nodes, 1)
	assert.Equal(t, "derp.headscale.test", info.Regions[900].Nodes[0].HostName)
	assert.Equal(t, 3478, info.Regions[900].Nodes[0].STUNPort)
}

func TestDebugNodeStore(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	body := env.text(t, "/debug/nodestore").Body.String()
	assert.Contains(t, body, "=== NodeStore Debug Information ===")
	assert.Contains(t, body, "Total Nodes: 1")

	var nodes map[string]types.Node

	env.jsonInto(t, "/debug/nodestore", &nodes)
	require.Len(t, nodes, 1)

	node, ok := nodes[strconv.FormatUint(env.node.ID().Uint64(), 10)]
	require.True(t, ok)
	assert.Equal(t, debugNodeName, node.Hostname)
}

func TestDebugRegistrationCache(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	var info map[string]any

	env.jsonInto(t, "/debug/registration-cache", &info)
	assert.Equal(t, "expirable-lru", info["type"])
	assert.Equal(t, "active", info["status"])
	assert.InDelta(t, 0, info["current_len"], 0)
	assert.NotEmpty(t, info["expiration"])
}

func TestDebugRoutes(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	// No node is online, so neither form lists anything.
	assert.Empty(t, env.text(t, "/debug/routes").Body.String())

	var routes types.DebugRoutes

	env.jsonInto(t, "/debug/routes", &routes)
	assert.Empty(t, routes.AvailableRoutes)
	assert.Empty(t, routes.PrimaryRoutes)
	assert.Empty(t, routes.UnhealthyNodes)

	// Announce and approve a route, then bring the node online so it is
	// eligible for election.
	route := netip.MustParsePrefix("10.33.0.0/24")
	_, _, err := env.app.state.SetApprovedRoutes(env.node.ID(), []netip.Prefix{route})
	require.NoError(t, err)

	nodeStruct := env.node.AsStruct()
	nodeStruct.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{route}}
	nodeStruct.ApprovedRoutes = []netip.Prefix{route}
	env.app.state.PutNodeInStoreForTest(*nodeStruct)
	env.app.state.Connect(env.node.ID())

	idStr := strconv.FormatUint(env.node.ID().Uint64(), 10)
	assert.Equal(t, route.String()+": "+idStr+"\n", env.text(t, "/debug/routes").Body.String())

	routes = types.DebugRoutes{}

	env.jsonInto(t, "/debug/routes", &routes)
	assert.Equal(t, []netip.Prefix{route}, routes.AvailableRoutes[env.node.ID()])
	assert.Equal(t, env.node.ID(), routes.PrimaryRoutes[route.String()])
}

func TestDebugPolicyManager(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	body := env.text(t, "/debug/policy-manager").Body.String()
	assert.Contains(t, body, "PolicyManager (v")

	var info state.DebugStringInfo

	env.jsonInto(t, "/debug/policy-manager", &info)
	assert.Equal(t, body, info.Content)
}

func TestDebugMapResponsesWithoutDumpPath(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	rec := env.debugRequest(t, "/debug/mapresponses", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "HEADSCALE_DEBUG_DUMP_MAPRESPONSE_PATH not set", rec.Body.String())
}

func TestDebugBatcher(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	body := env.text(t, "/debug/batcher").Body.String()
	assert.Contains(t, body, "=== Batcher Connected Nodes ===")
	assert.Contains(t, body, "Summary: 0 connected, 0 total")

	var info DebugBatcherInfo

	env.jsonInto(t, "/debug/batcher", &info)
	assert.Equal(t, 0, info.TotalNodes)
	assert.Empty(t, info.ConnectedNodes)

	env.connectNodeToBatcher(t)

	idStr := strconv.FormatUint(env.node.ID().Uint64(), 10)

	body = env.text(t, "/debug/batcher").Body.String()
	assert.Contains(t, body, "Node "+idStr+":\tconnected (1 connections)")
	assert.Contains(t, body, "Summary: 1 connected, 1 total")

	info = DebugBatcherInfo{}

	env.jsonInto(t, "/debug/batcher", &info)
	assert.Equal(t, 1, info.TotalNodes)
	assert.Equal(t, DebugBatcherNodeInfo{Connected: true, ActiveConnections: 1}, info.ConnectedNodes[idStr])
}

func TestDebugPing(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)
	idStr := strconv.FormatUint(env.node.ID().Uint64(), 10)

	assertHTML := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
		assert.Contains(t, rec.Body.String(), "Ping Node")

		return rec.Body.String()
	}

	t.Run("form without query", func(t *testing.T) {
		t.Parallel()

		body := assertHTML(t, env.debugRequest(t, "/debug/ping", ""))
		assert.NotContains(t, body, "not found")
		assert.NotContains(t, body, "not connected")
	})

	t.Run("unknown node", func(t *testing.T) {
		t.Parallel()

		body := assertHTML(t, env.debugRequest(t, "/debug/ping?node=no-such-node", ""))
		assert.Contains(t, body, "not found")
	})

	t.Run("disconnected node via post", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/debug/ping",
			strings.NewReader("node="+idStr),
		)
		req.RemoteAddr = debugLoopbackAddr
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		body := assertHTML(t, rec)
		assert.Contains(t, body, "Node "+idStr+" is not connected.")
	})

	t.Run("malformed form", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			"/debug/ping",
			strings.NewReader("node=%zz"),
		)
		req.RemoteAddr = debugLoopbackAddr
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		env.handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "bad form data")
	})
}

func TestDebugPingConnectedNode(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)
	env.connectNodeToBatcher(t)

	idStr := strconv.FormatUint(env.node.ID().Uint64(), 10)

	// The connected node is offered as a quick-ping link.
	rec := env.debugRequest(t, "/debug/ping", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), debugNodeName)

	// A ping to a connected node waits for the node to call back. A context
	// that is already cancelled makes doPing return without waiting, which
	// pins the request-cancelled branch deterministically.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/debug/ping?node="+idStr, nil)
	req.RemoteAddr = debugLoopbackAddr

	rec = httptest.NewRecorder()
	env.handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Request cancelled.")
}

func TestDebugMetrics(t *testing.T) {
	t.Parallel()

	env := newDebugTestEnv(t)

	// /metrics is served by promhttp directly and is not gated on the peer
	// address, so the default TEST-NET RemoteAddr is fine here.
	rec := httptest.NewRecorder()
	env.handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "go_goroutines")
}
