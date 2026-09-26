package servertest_test

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/idtoken"
	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tsclient "tailscale.com/client/tailscale/v2"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
	"tailscale.com/types/opt"
)

const trafficWait = 10 * time.Second

// asnTableServer serves an ip2asn-combined table big enough to pass the
// size check: synthetic /24s plus the networks the test reports traffic
// to. It fails every request once failing is set.
func asnTableServer(t *testing.T, failing *atomic.Bool) string {
	t.Helper()

	var b strings.Builder

	b.WriteString("8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n")

	for i := range 100_000 {
		v := 11<<24 + i*256
		fmt.Fprintf(&b, "%d.%d.%d.0\t%d.%d.%d.255\t64512\tZZ\tSYNTHETIC\n",
			v>>24, v>>16&0xff, v>>8&0xff, v>>24, v>>16&0xff, v>>8&0xff)
	}

	b.WriteString("140.82.112.0\t140.82.127.255\t36459\tUS\tGITHUB\n")

	var gz bytes.Buffer

	w := gzip.NewWriter(&gz)
	_, err := w.Write([]byte(b.String()))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failing.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)

			return
		}

		_, _ = w.Write(gz.Bytes())
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// idToken fetches the node's identity token for audience over its noise
// connection, the way `tailscale id-token` does for the agent.
func idToken(t *testing.T, srv *servertest.TestServer, node *servertest.TestClient, audience string) string {
	t.Helper()

	status, body := machineDo(t, srv, node, http.MethodPost, "id-token", tailcfg.TokenRequest{
		CapVersion: tailcfg.CurrentCapabilityVersion,
		NodeKey:    node.NodePrivateKey().Public(),
		Audience:   audience,
	})
	require.Equal(t, http.StatusOK, status, string(body))

	var response tailcfg.TokenResponse

	require.NoError(t, json.Unmarshal(body, &response))

	return response.IDToken
}

// signedToken is a traffic token for node signed with signer, issued at
// issued and naming key: with the server's own signer it is what the
// server would have issued then, with another it is a forgery.
func signedToken(
	t *testing.T,
	srv *servertest.TestServer,
	signer *idtoken.Signer,
	node *servertest.TestClient,
	key string,
	issued time.Time,
) string {
	t.Helper()

	id, err := strconv.ParseUint(node.NodeIDString(), 10, 64)
	require.NoError(t, err)

	claims := idtoken.NewClaims(srv.URL, traffic.Audience, issued)
	claims.NodeID = id
	claims.Key = key

	token, err := signer.Sign(claims)
	require.NoError(t, err)

	return token
}

// postReport sends a report the way the agent does and returns the status
// and body.
func postReport(t *testing.T, client *http.Client, srvURL, token string, body []byte, encoding string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srvURL+traffic.ReportPath,
		bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(raw)
}

func reportJSON(t *testing.T, r traffic.Report) []byte {
	t.Helper()

	raw, err := json.Marshal(r)
	require.NoError(t, err)

	return raw
}

func sendReport(t *testing.T, client *http.Client, srvURL, token string, r traffic.Report) traffic.Response {
	t.Helper()

	status, body := postReport(t, client, srvURL, token, reportJSON(t, r), "")
	require.Equal(t, http.StatusOK, status, body)

	var resp traffic.Response

	require.NoError(t, json.Unmarshal([]byte(body), &resp))

	return resp
}

func nodeIP4(t *testing.T, node *servertest.TestClient) netip.Addr {
	t.Helper()

	nm := node.Netmap()
	require.NotNil(t, nm)

	for _, p := range nm.SelfNode.Addresses().All() {
		if p.Addr().Is4() {
			return p.Addr()
		}
	}

	require.FailNow(t, "no IPv4 address", node.Name)

	return netip.Addr{}
}

// resolvesThrough reports whether the netmap's DNS sends queries to addr
// first, also while an exit node is in use.
func resolvesThrough(nm *netmap.NetworkMap, addr netip.Addr) bool {
	if nm == nil || len(nm.DNS.Resolvers) == 0 {
		return false
	}

	first := nm.DNS.Resolvers[0]

	return first.Addr == addr.String() && first.UseWithExitNode
}

// resolverAddrs lists the netmap's default resolvers, failing the test
// on one a client would drop while it uses an exit node.
func resolverAddrs(t *testing.T, nm *netmap.NetworkMap) []string {
	t.Helper()

	out := make([]string, 0, len(nm.DNS.Resolvers))

	for _, r := range nm.DNS.Resolvers {
		assert.True(t, r.UseWithExitNode, "resolver %s is dropped with an exit node", r.Addr)

		out = append(out, r.Addr)
	}

	return out
}

// useExitNode reports gw as the exit node the client uses, the way a
// Tailscale 1.86 or later client does; an empty gw reports none.
func useExitNode(t *testing.T, c *servertest.TestClient, gw tailcfg.StableNodeID, opts ...func(*tailcfg.Hostinfo)) {
	t.Helper()

	hostinfo := &tailcfg.Hostinfo{BackendLogID: "servertest-" + c.Name, Hostname: c.Name, ExitNodeID: gw}
	for _, o := range opts {
		o(hostinfo)
	}

	c.Direct().SetHostinfo(hostinfo)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, c.Direct().SendUpdate(ctx))
}

func nodeIP6(t *testing.T, node *servertest.TestClient) netip.Addr {
	t.Helper()

	for _, p := range node.Netmap().SelfNode.Addresses().All() {
		if p.Addr().Is6() {
			return p.Addr()
		}
	}

	require.FailNow(t, "no IPv6 address", node.Name)

	return netip.Addr{}
}

// oauthToken mints an access token holding only scopes, through an OAuth
// client the key's user creates.
func oauthToken(t *testing.T, client *http.Client, srv *servertest.TestServer, key string, scopes ...string) string {
	t.Helper()

	status, body := apiCall(t, client, key, http.MethodPost, srv.URL+"/api/v1/oauth-client",
		map[string]any{"description": "scoped", "scopes": scopes})
	require.Equal(t, http.StatusOK, status, body)

	secret, ok := body["clientSecret"].(string)
	require.True(t, ok)

	return accessToken(t, client, srv.URL, secret)
}

func mentionsResolver(nm *netmap.NetworkMap, addr netip.Addr) bool {
	for _, r := range slices.Concat(nm.DNS.Resolvers, nm.DNS.FallbackResolvers) {
		if r.Addr == addr.String() {
			return true
		}
	}

	return false
}

// opensDNS reports whether the node's packet filter admits DNS to addr.
func opensDNS(nm *netmap.NetworkMap, addr netip.Addr) bool {
	if nm == nil {
		return false
	}

	for _, m := range nm.PacketFilter {
		for _, dst := range m.Dsts {
			if dst.Net == netip.PrefixFrom(addr, addr.BitLen()) && dst.Ports.First == 53 && dst.Ports.Last == 53 {
				return true
			}
		}
	}

	return false
}

func minuteBucket(at time.Time) int64 {
	return at.Unix() - at.Unix()%60
}

// TestTrafficMonitor drives the traffic monitor end to end: a gateway's
// agent authenticates with its own identity token fetched over noise,
// reports flows and DNS questions, and the server attributes them to the
// sending nodes, rolls them up, names the networks from a downloaded ASN
// table, folds and prunes them, serves them on API v1 and v2 by role, and
// points the DNS of the gateway's exit node users at its resolver only
// while it reports. The subtests build on one another, so they run in
// order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestTrafficMonitor(t *testing.T) {
	t.Parallel()

	var asnDown atomic.Bool

	srv := servertest.NewServer(t,
		servertest.WithRealListener(),
		servertest.WithDNS(types.DNSConfig{
			MagicDNS:    true,
			BaseDomain:  "traffic.test",
			Nameservers: types.Nameservers{Global: []string{"1.1.1.1", "100.100.100.100"}},
		}),
		servertest.WithTraffic(types.TrafficConfig{
			ASNDatabaseURL: asnTableServer(t, &asnDown),
			ASNCachePath:   t.TempDir() + "/ip2asn.tsv.gz",
		}),
	)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "traffic-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	// The nodes may only use exit nodes, so the only way to one's port 53
	// is the grant the DNS log adds. The stranger has no grant at all, so
	// it is no peer of the gateway.
	setStatePolicy(t, srv, `{
		"tagOwners": {"tag:gateway": ["traffic-owner@"], "tag:connector": ["traffic-owner@"]},
		"grants": [{"src": ["traffic-owner@"], "dst": ["autogroup:internet"], "ip": ["*"]}]
	}`)

	runsConnector := servertest.WithHostinfo(func(h *tailcfg.Hostinfo) { h.AppConnector = opt.NewBool(true) })

	exit := servertest.NewClient(t, srv, "exit-1", servertest.WithUser(owner), servertest.WithTags("tag:gateway"))
	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	phone := servertest.NewClient(t, srv, "phone", servertest.WithUser(owner))
	plain := servertest.NewClient(t, srv, "plain", servertest.WithUser(owner), runsConnector)
	connector := servertest.NewClient(t, srv, "connector", servertest.WithUser(owner),
		servertest.WithTags("tag:connector"), runsConnector)
	stranger := servertest.NewClient(t, srv, "stranger", servertest.WithUser(srv.CreateUser(t, "traffic-stranger")))

	exit.Direct().SetHostinfo(&tailcfg.Hostinfo{
		BackendLogID: "servertest-exit-1",
		Hostname:     "exit-1",
		RoutableIPs:  tsaddr.ExitRoutes(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	_ = exit.Direct().SendUpdate(ctx)

	cancel()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/approve_routes",
			map[string]any{"routes": []string{"0.0.0.0/0", "::/0"}})
		// Refused until the exit node's routes reach the server; field
		// fails the whole test rather than the attempt, so it waits for 200.
		if !assert.Equal(c, http.StatusOK, status, body) {
			return
		}

		assert.Len(c, field(t, body, "node", "approvedRoutes"), 2)
	}, trafficWait, 100*time.Millisecond)

	laptop.WaitForCondition(t, "the exit node as a peer", trafficWait, func(_ *netmap.NetworkMap) bool {
		_, ok := laptop.PeerByName("exit-1")

		return ok
	})

	exitIP, laptopIP, phoneIP := nodeIP4(t, exit), nodeIP4(t, laptop), nodeIP4(t, phone)
	strangerIP := nodeIP4(t, stranger)
	token := idToken(t, srv, exit, traffic.Audience)
	now := time.Now()
	minute := minuteBucket(now) - 60

	signer, err := srv.State().IDTokenSigner()
	require.NoError(t, err)

	t.Run("the ASN table downloads, and a failed refresh keeps it", func(t *testing.T) {
		require.NoError(t, srv.State().RefreshASN(t.Context()))
		assert.Equal(t, 100_002, srv.State().ASNRanges())

		asnDown.Store(true)
		require.Error(t, srv.State().RefreshASN(t.Context()))
		assert.Equal(t, 100_002, srv.State().ASNRanges(), "a failed download keeps the table in use")
		asnDown.Store(false)
	})

	first := traffic.Report{
		Version:  "0.1.0",
		Instance: "boot-1",
		Seq:      1,
		SentAt:   now,
		Status: traffic.Status{
			Conntrack: traffic.Collector{Enabled: true},
			SNI:       traffic.Collector{Enabled: true},
		},
		Flows: []traffic.Flow{
			{
				Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("140.82.112.3"), Proto: 6, Port: 443,
				Host: "GitHub.com.", HostSource: traffic.HostSNI,
				TxBytes: 1000, RxBytes: 50_000, TxPackets: 10, RxPackets: 40, Conns: 2,
			},
			{
				Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("140.82.112.4"), Proto: 6, Port: 443,
				Host: "github.com", HostSource: traffic.HostDNS, TxBytes: 500, RxBytes: 500, Conns: 1,
			},
			{
				Bucket: minute, Src: phoneIP, Dst: netip.MustParseAddr("8.8.8.8"), Proto: 17, Port: 53,
				TxBytes: 100, RxBytes: 200, Conns: 1,
			},
			{
				Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("192.168.1.10"), Proto: 6, Port: 22,
				TxBytes: 300, RxBytes: 300, Conns: 1,
			},
			// Between two nodes: not the gateway's to report.
			{Bucket: minute, Src: laptopIP, Dst: phoneIP, Proto: 6, Port: 80, TxBytes: 9999},
			// From an address no node holds.
			{Bucket: minute, Src: netip.MustParseAddr("100.99.99.99"), Dst: netip.MustParseAddr("1.1.1.1"), TxBytes: 1},
			// From a node that cannot reach the gateway: a gateway cannot
			// pin traffic on a node it never carried.
			{Bucket: minute, Src: strangerIP, Dst: netip.MustParseAddr("1.1.1.1"), TxBytes: 77_777},
		},
		Queries: []traffic.Query{
			{Bucket: minute, Src: laptopIP, Name: "GitHub.com.", Count: 5, Failed: 1},
			{Bucket: minute, Src: phoneIP, Name: "github.com", Count: 2},
			{Bucket: minute, Src: phoneIP, Name: "example.org", Count: 1, Failed: 3},
		},
	}

	t.Run("a gateway reports and the traffic lands on the sending nodes", func(t *testing.T) {
		resp := sendReport(t, client, srv.URL, token, first)
		assert.Equal(t, uint64(1), resp.Seq)
		assert.True(t, resp.Config.SNI)
		assert.False(t, resp.Config.DNS)
		assert.Equal(t, 60, resp.Config.ReportInterval)
		assert.Empty(t, resp.Config.Upstreams,
			"with no nameserver kept for exit node users, the agent forwards to the gateway's own")

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/summary", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.InDelta(t, 60, field(t, body, "resolution"), 0)
		assert.InDelta(t, 1900, field(t, body, "total", "txBytes"), 0)
		assert.InDelta(t, 51_000, field(t, body, "total", "rxBytes"), 0)
		assert.InDelta(t, 5, field(t, body, "total", "conns"), 0)
		assert.Equal(t, "laptop", field(t, body, "nodes", "0", "nodeName"), "the largest sender first")
		assert.InDelta(t, 1800, field(t, body, "nodes", "0", "txBytes"), 0)
		assert.Equal(t, "phone", field(t, body, "nodes", "1", "nodeName"))
		assert.Equal(t, "exit-1", field(t, body, "reporters", "0", "nodeName"))

		series, ok := field(t, body, "series").([]any)
		require.True(t, ok)
		assert.InDelta(t, 24*60, len(series), 1, "a point per minute, zeros included")

		status, body = apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/traffic/destinations?groupBy=host", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.InDelta(t, 3600, field(t, body, "resolution"), 0, "destinations are hourly at the finest")
		assert.Equal(t, "github.com", field(t, body, "destinations", "0", "host"),
			"two addresses of one name are one row")
		assert.InDelta(t, 50_500, field(t, body, "destinations", "0", "rxBytes"), 0)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/destinations", nil)
		require.Equal(t, http.StatusOK, status, body)

		rows := map[string]map[string]any{}

		destinations, ok := field(t, body, "destinations").([]any)
		require.True(t, ok)

		for _, d := range destinations {
			row, ok := d.(map[string]any)
			require.True(t, ok)

			dst, ok := row["dst"].(string)
			require.True(t, ok)

			rows[dst] = row
		}

		require.Len(t, rows, 4, "nothing between nodes, nothing from strangers or non-peers")
		assert.Equal(t, "GITHUB", rows["140.82.112.3"]["asName"])
		assert.InDelta(t, 36459, rows["140.82.112.3"]["asn"], 0)
		assert.Equal(t, "US", rows["140.82.112.3"]["country"])
		assert.Equal(t, "github.com", rows["140.82.112.3"]["host"], "names are stored lower case")
		assert.Equal(t, "GOOGLE", rows["8.8.8.8"]["asName"])
		assert.Equal(t, true, rows["192.168.1.10"]["private"])
		assert.Equal(t, false, rows["8.8.8.8"]["private"])

		// Grouped by host or network, a group of private destinations
		// still says so, and has no network of its own.
		for _, groupBy := range []string{"host", "asn", "country"} {
			status, body = apiCall(t, client, ownerKey, http.MethodGet,
				v1+"/traffic/destinations?groupBy="+groupBy, nil)
			require.Equal(t, http.StatusOK, status, body)

			groups, ok := field(t, body, "destinations").([]any)
			require.True(t, ok)

			private := 0

			for _, g := range groups {
				row, ok := g.(map[string]any)
				require.True(t, ok)

				if row["private"] == true {
					private++

					assert.InDelta(t, 300, row["txBytes"], 0, "by %s: only the private destination", groupBy)
					assert.InDelta(t, 0, row["asn"], 0)
					assert.Empty(t, row["asName"])
				}
			}

			assert.Equal(t, 1, private, "by %s: one private group", groupBy)
		}

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/dns", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "github.com", field(t, body, "names", "0", "name"))
		assert.InDelta(t, 7, field(t, body, "names", "0", "queries"), 0)
		assert.InDelta(t, 2, field(t, body, "names", "0", "nodes"), 0)
		assert.InDelta(t, 1, field(t, body, "names", "1", "failed"), 0, "failures never exceed the questions")

		status, body = apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/traffic/dns?groupBy=node&nodeId="+phone.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "phone", field(t, body, "names", "0", "nodeName"))
		assert.InDelta(t, 3, field(t, body, "names", "0", "queries"), 0)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "exit-1", field(t, body, "reporters", "0", "nodeName"))
		assert.Equal(t, "0.1.0", field(t, body, "reporters", "0", "version"))
		assert.InDelta(t, 2, field(t, body, "reporters", "0", "unattributed"), 0,
			"the unknown address and the node that is no peer of the gateway")
		assert.Empty(t, field(t, body, "reporters", "0", "refused"))
		assert.Equal(t, false, field(t, body, "reporters", "0", "stale"))
		assert.Equal(t, true, field(t, body, "reporters", "0", "collectors", "conntrack", "enabled"))
		assert.InDelta(t, 100_002, field(t, body, "asnRanges"), 0)
	})

	t.Run("a resent report is acknowledged, not counted again", func(t *testing.T) {
		resp := sendReport(t, client, srv.URL, token, first)
		assert.Equal(t, uint64(1), resp.Seq)

		older := first
		older.Seq = 0
		status, raw := postReport(t, client, srv.URL, token, reportJSON(t, older), "")
		assert.Equal(t, http.StatusBadRequest, status, raw)

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/summary", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.InDelta(t, 1900, field(t, body, "total", "txBytes"), 0)

		// A restarted agent starts its sequence again under a new
		// instance, and that report counts.
		restarted := traffic.Report{
			Version: "0.1.0", Instance: "boot-2", Seq: 1,
			Flows: []traffic.Flow{{
				Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("140.82.112.3"), Proto: 6, Port: 443,
				Host: "github.com", TxBytes: 100,
			}},
		}

		// zstd, the way the agent compresses a large report.
		var compressed bytes.Buffer

		enc, err := zstd.NewWriter(&compressed)
		require.NoError(t, err)
		_, err = enc.Write(reportJSON(t, restarted))
		require.NoError(t, err)
		require.NoError(t, enc.Close())

		status, raw = postReport(t, client, srv.URL, token, compressed.Bytes(), "zstd")
		require.Equal(t, http.StatusOK, status, raw)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/summary", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.InDelta(t, 2000, field(t, body, "total", "txBytes"), 0)
	})

	t.Run("every credential but the gateway's current token is refused", func(t *testing.T) {
		report := reportJSON(t, traffic.Report{Instance: "boot-2", Seq: 2})

		refused := func(t *testing.T, token string, want int, why string) {
			t.Helper()

			status, body := postReport(t, client, srv.URL, token, report, "")
			assert.Equal(t, want, status, "%s: %s", why, body)
		}

		refused(t, "", http.StatusUnauthorized, "no credential")
		refused(t, idToken(t, srv, exit, "https://vault.example.com"), http.StatusUnauthorized, "another audience")

		forger, err := idtoken.New(mustKey(t))
		require.NoError(t, err)

		currentKey := exit.NodePrivateKey().Public().String()
		refused(t, signedToken(t, srv, forger, exit, currentKey, time.Now()), http.StatusUnauthorized,
			"signed by another key")
		refused(t, signedToken(t, srv, signer, exit, currentKey, time.Now().Add(-time.Hour)),
			http.StatusUnauthorized, "expired")
		refused(t, signedToken(t, srv, signer, exit, laptop.NodePrivateKey().Public().String(), time.Now()),
			http.StatusUnauthorized, "a node key the gateway no longer has")
		refused(t, idToken(t, srv, laptop, traffic.Audience), http.StatusForbidden, "not a gateway")

		// Anyone can advertise an app connector or routes on their own
		// device; only a tag, which the operator hands out, makes a
		// gateway. And a tagged connector reports only once an app
		// selects it.
		status, body := postReport(t, client, srv.URL, idToken(t, srv, plain, traffic.Audience), report, "")
		assert.Equal(t, http.StatusForbidden, status, body)
		assert.Contains(t, body, "not tagged")

		status, body = postReport(t, client, srv.URL, idToken(t, srv, connector, traffic.Audience), report, "")
		assert.Equal(t, http.StatusForbidden, status, body)
		assert.Contains(t, body, "is not an app connector for any app")

		status, apiBody := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/suspend", map[string]any{"suspended": true})
		require.Equal(t, http.StatusOK, status, apiBody)
		refused(t, token, http.StatusForbidden, "suspended")

		status, apiBody = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/suspend", map[string]any{"suspended": false})
		require.Equal(t, http.StatusOK, status, apiBody)

		status, apiBody = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/approve", map[string]any{"approved": false})
		require.Equal(t, http.StatusOK, status, apiBody)
		refused(t, token, http.StatusForbidden, "waiting for approval")

		status, apiBody = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/approve", map[string]any{"approved": true})
		require.Equal(t, http.StatusOK, status, apiBody)
		refused(t, token, http.StatusOK, "approved again")
	})

	t.Run("reports outside the limits are refused", func(t *testing.T) {
		flow := traffic.Flow{Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("8.8.8.8"), TxBytes: 1}
		seq := uint64(10)

		refused := func(t *testing.T, r traffic.Report, want int, why string) {
			t.Helper()

			seq++
			r.Instance, r.Seq = cmp.Or(r.Instance, "boot-2"), cmp.Or(r.Seq, seq)
			status, body := postReport(t, client, srv.URL, token, reportJSON(t, r), "")
			assert.Equal(t, want, status, "%s: %s", why, body)
		}

		misaligned := flow
		misaligned.Bucket++
		refused(t, traffic.Report{Flows: []traffic.Flow{misaligned}}, http.StatusBadRequest, "misaligned bucket")

		future := flow
		future.Bucket = minuteBucket(time.Now().Add(time.Hour))
		refused(t, traffic.Report{Flows: []traffic.Flow{future}}, http.StatusBadRequest, "bucket in the future")

		refused(t, traffic.Report{Instance: strings.Repeat("i", 65)}, http.StatusBadRequest, "long instance")
		refused(t, traffic.Report{Version: strings.Repeat("v", 65)}, http.StatusBadRequest, "long version")
		refused(t, traffic.Report{DNSListen: make([]netip.AddrPort, 9)}, http.StatusBadRequest,
			"too many resolver addresses")
		refused(t, traffic.Report{Flows: make([]traffic.Flow, traffic.MaxFlowsPerReport+1)},
			http.StatusRequestEntityTooLarge, "too many flows")

		status, body := postReport(t, client, srv.URL, token,
			[]byte(`{"version":"`+strings.Repeat("a", traffic.MaxReportBytes)+`"}`), "")
		assert.Equal(t, http.StatusRequestEntityTooLarge, status, body)

		status, body = postReport(t, client, srv.URL, token, []byte("{}"), "gzip")
		assert.Equal(t, http.StatusUnsupportedMediaType, status, body)

		status, body = postReport(t, client, srv.URL, token, []byte("{"), "")
		assert.Equal(t, http.StatusBadRequest, status, body)

		// A refused report changed nothing.
		status, apiBody := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/summary", nil)
		require.Equal(t, http.StatusOK, status, apiBody)
		assert.InDelta(t, 2000, field(t, apiBody, "total", "txBytes"), 0)
	})

	t.Run("the resolution follows the range and the retention", func(t *testing.T) {
		resolution := func(path string, start, end time.Time) float64 {
			t.Helper()

			q := url.Values{"start": {start.Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}}
			status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+path+"?"+q.Encode(), nil)
			require.Equal(t, http.StatusOK, status, body)

			res, ok := field(t, body, "resolution").(float64)
			require.True(t, ok)

			return res
		}

		end := time.Now()
		assert.InDelta(t, 60, resolution("/traffic/summary", end.Add(-2*time.Hour), end), 0)
		assert.InDelta(t, 3600, resolution("/traffic/summary", end.Add(-72*time.Hour), end), 0,
			"too many minutes for one chart")
		assert.InDelta(t, 3600, resolution("/traffic/summary", end.Add(-50*time.Hour), end.Add(-49*time.Hour)), 0,
			"minutes are gone past their retention")
		assert.InDelta(t, 86400, resolution("/traffic/summary", end.AddDate(0, 0, -20), end), 0,
			"hours are gone past their retention")
		assert.InDelta(t, 3600, resolution("/traffic/destinations", end.Add(-2*time.Hour), end), 0)
		assert.InDelta(t, 3600, resolution("/traffic/dns", end.Add(-2*time.Hour), end), 0)

		q := url.Values{"start": {end.Format(time.RFC3339)}, "end": {end.Add(-time.Hour).Format(time.RFC3339)}}
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/summary?"+q.Encode(), nil)
		assert.Equal(t, http.StatusBadRequest, status, body)

		// More daily buckets than a chart holds is refused, not scanned.
		q = url.Values{"start": {end.AddDate(-5, 0, 0).Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}}
		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/destinations?"+q.Encode(), nil)
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/destinations?groupBy=bogus", nil)
		assert.Equal(t, http.StatusUnprocessableEntity, status, body)
	})

	t.Run("maintenance folds the small destinations and prunes by retention", func(t *testing.T) {
		hour := time.Now().Truncate(time.Hour).Add(-2 * time.Hour)
		old := hour.Unix() + 60

		flows := make([]traffic.Flow, 0, 600)
		for i := range 600 {
			flows = append(flows, traffic.Flow{
				Bucket: old, Src: phoneIP, Proto: 6, Port: 443,
				Dst:     netip.AddrFrom4([4]byte{11, 0, byte(i >> 8), byte(i)}),
				TxBytes: uint64(1000 + i), Conns: 1,
			})
		}

		sendReport(t, client, srv.URL, token, traffic.Report{Instance: "boot-2", Seq: 100, Flows: flows})

		rangeOf := url.Values{
			"start":  {hour.Format(time.RFC3339)},
			"end":    {hour.Add(time.Hour).Format(time.RFC3339)},
			"nodeId": {phone.NodeIDString()},
			"limit":  {"1000"},
		}
		count := func() (int, map[string]any) {
			status, body := apiCall(t, client, ownerKey, http.MethodGet,
				v1+"/traffic/destinations?"+rangeOf.Encode(), nil)
			require.Equal(t, http.StatusOK, status, body)

			rows, ok := field(t, body, "destinations").([]any)
			require.True(t, ok)

			for _, r := range rows {
				row, ok := r.(map[string]any)
				require.True(t, ok)

				if row["dst"] == "" {
					return len(rows), row
				}
			}

			return len(rows), nil
		}

		n, other := count()
		require.Equal(t, 600, n)
		require.Nil(t, other)

		require.NoError(t, srv.State().TrafficMaintenance(time.Now()))

		n, other = count()
		assert.Equal(t, 501, n, "the top 500 and one remainder")
		require.NotNil(t, other)

		// The remainder is the 100 smallest: 1000..1099.
		assert.InDelta(t, 100*1000+99*100/2, other["txBytes"], 0)
		assert.InDelta(t, 100, other["conns"], 0)

		// Folding again changes nothing.
		require.NoError(t, srv.State().TrafficMaintenance(time.Now()))

		n, _ = count()
		assert.Equal(t, 501, n)

		minuteRows := func() int {
			points, err := srv.State().TrafficSeries(types.TrafficFilter{
				Resolution: types.TrafficMinute, Start: hour, End: hour.Add(time.Hour),
				NodeID: types.NodeID(mustID(t, phone)),
			})
			require.NoError(t, err)

			return len(points)
		}

		assert.Equal(t, 1, minuteRows())

		status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"retention": map[string]any{"minuteHours": 1}})
		require.Equal(t, http.StatusOK, status, body)
		require.NoError(t, srv.State().TrafficMaintenance(time.Now()))

		assert.Zero(t, minuteRows(), "minutes older than the retention are gone")

		n, _ = count()
		assert.Equal(t, 501, n, "the hourly rows stay")

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"retention": map[string]any{"minuteHours": 48}})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"retention": map[string]any{"minuteHours": 24 * 30}})
		assert.Equal(t, http.StatusBadRequest, status, "minutes may not outlive hours: %v", body)
	})

	t.Run("the DNS log uses a gateway resolver once approved, and only while it reports", func(t *testing.T) {
		dnsStatus := traffic.Status{
			Conntrack: traffic.Collector{Enabled: true},
			DNS:       traffic.Collector{Enabled: true},
		}
		seq := uint64(200)
		report := func(listen ...netip.AddrPort) traffic.Report {
			seq++

			return traffic.Report{Instance: "boot-2", Seq: seq, Status: dnsStatus, DNSListen: listen}
		}
		ownResolver := netip.AddrPortFrom(exitIP, 53)
		resolvers := func() []any {
			t.Helper()

			status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
			require.Equal(t, http.StatusOK, status, body)

			list, ok := body["resolvers"].([]any)
			require.True(t, ok)

			return list
		}

		// The laptop uses the gateway as its exit node; the phone uses
		// none, so its DNS is never touched.
		useExitNode(t, laptop, types.NodeID(mustID(t, exit)).StableID())

		phoneDNS := phone.Netmap().DNS

		// Off by default: a working resolver is not used.
		sendReport(t, client, srv.URL, token, report(ownResolver))
		assert.False(t, mentionsResolver(laptop.Netmap(), exitIP))
		assert.False(t, opensDNS(exit.Netmap(), exitIP))

		// Moving the exit node users' DNS takes the dns scope on top of
		// the monitor's.
		monitorOnly := oauthToken(t, client, srv, ownerKey, "logs:network")
		status, body := apiCall(t, client, monitorOnly, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"dnsLogging": true})
		assert.Equal(t, http.StatusForbidden, status, body)

		status, body = apiCall(t, client, monitorOnly, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"sni": true, "dnsLogging": false})
		assert.Equal(t, http.StatusOK, status, "an unchanged dnsLogging needs no dns scope: %v", body)

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"dnsLogging": true})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["dnsLogging"])

		// Addresses that are not the gateway's own port 53 are never used,
		// so an agent cannot point the tailnet at another node.
		resp := sendReport(t, client, srv.URL, token,
			report(netip.AddrPortFrom(laptopIP, 53), netip.AddrPortFrom(exitIP, 5353)))
		assert.True(t, resp.Config.DNS)
		assert.Empty(t, resp.Config.LogSources, "nobody's DNS points at the gateway")
		assert.Empty(t, resolvers())

		// A working resolver on the gateway's own address is still not
		// used until an operator approves it.
		sendReport(t, client, srv.URL, token, report(ownResolver))
		assert.Empty(t, resolvers(), "not approved yet")

		approve := func(key string, on bool) (int, map[string]any) {
			return apiCall(t, client, key, http.MethodPatch, v1+"/traffic/reporters/"+exit.NodeIDString(),
				map[string]any{"resolver": on})
		}

		status, body = approve(monitorOnly, true)
		assert.Equal(t, http.StatusForbidden, status, body)
		assert.Empty(t, resolvers())

		status, body = approve(ownerKey, true)
		require.Equal(t, http.StatusOK, status, body)
		assert.NotEmpty(t, body["resolverApprovedAt"])
		assert.Equal(t, true, body["resolverActive"])

		laptop.WaitForCondition(t, "DNS through the gateway", trafficWait, func(nm *netmap.NetworkMap) bool {
			return resolvesThrough(nm, exitIP)
		})
		assert.Equal(t, []string{exitIP.String()}, resolverAddrs(t, laptop.Netmap()),
			"the gateway announced no peer API, so there is no fallback to it")

		exit.WaitForCondition(t, "port 53 open on the gateway", trafficWait, func(nm *netmap.NetworkMap) bool {
			return opensDNS(nm, exitIP)
		})
		assert.False(t, mentionsResolver(exit.Netmap(), exitIP), "the gateway never resolves through itself")
		assert.Equal(t, phoneDNS, phone.Netmap().DNS, "a node without an exit node keeps its DNS")
		assert.Equal(t, []any{exitIP.String()}, resolvers())

		resp = sendReport(t, client, srv.URL, token, report(ownResolver))
		assert.Equal(t, []netip.Addr{laptopIP, nodeIP6(t, laptop)}, resp.Config.LogSources,
			"the agent records the laptop only")

		// A resolver that stopped working is dropped at once.
		broken := report(ownResolver)
		broken.Status.DNS.Error = "bind: address in use"
		sendReport(t, client, srv.URL, token, broken)
		laptop.WaitForCondition(t, "DNS back to the tailnet's", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !mentionsResolver(nm, exitIP)
		})
		exit.WaitForCondition(t, "port 53 closed", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !opensDNS(nm, exitIP)
		})

		// So is one whose gateway stops qualifying, without waiting for
		// a tick.
		sendReport(t, client, srv.URL, token, report(ownResolver))
		laptop.WaitForCondition(t, "DNS through the gateway again", trafficWait, func(nm *netmap.NetworkMap) bool {
			return resolvesThrough(nm, exitIP)
		})

		status, apiBody := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/suspend", map[string]any{"suspended": true})
		require.Equal(t, http.StatusOK, status, apiBody)
		assert.Empty(t, resolvers(), "a suspended gateway's resolver goes at once")
		laptop.WaitForCondition(t, "the suspended gateway's resolver dropped", trafficWait,
			func(nm *netmap.NetworkMap) bool { return !mentionsResolver(nm, exitIP) })

		status, apiBody = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/suspend", map[string]any{"suspended": false})
		require.Equal(t, http.StatusOK, status, apiBody)
		laptop.WaitForCondition(t, "the resolver back with the gateway", trafficWait,
			func(nm *netmap.NetworkMap) bool { return resolvesThrough(nm, exitIP) })

		// A gateway that stops reporting leaves its users' DNS 90 seconds
		// after its last report, at the next tick. Right after a start,
		// the start counts as the last report. The reports are backdated
		// through the state; each change is published the way the report
		// handler and the scheduler publish theirs.
		gateway, ok := srv.State().GetNodeByID(types.NodeID(mustID(t, exit)))
		require.True(t, ok)

		ingestAt := func(at time.Time) {
			t.Helper()

			_, c, err := srv.State().IngestTrafficReport(gateway, report(ownResolver), at)
			require.NoError(t, err)
			srv.App.Change(c)
		}
		tick := func() {
			t.Helper()

			c, err := srv.State().TrafficTick(time.Now())
			require.NoError(t, err)
			srv.App.Change(c)
		}

		srv.State().SetTrafficBootForTest(time.Now())
		ingestAt(time.Now().Add(-2 * time.Minute))
		tick()
		assert.Equal(t, []any{exitIP.String()}, resolvers(), "the start counts as a report")

		srv.State().SetTrafficBootForTest(time.Now().Add(-time.Hour))
		ingestAt(time.Now().Add(-80 * time.Second))
		tick()
		assert.Equal(t, []any{exitIP.String()}, resolvers(), "80 seconds is fresh")

		ingestAt(time.Now().Add(-91 * time.Second))
		tick()
		assert.Empty(t, resolvers(), "91 seconds is not")

		laptop.WaitForCondition(t, "the stale resolver dropped", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !mentionsResolver(nm, exitIP)
		})
		exit.WaitForCondition(t, "port 53 closed again", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !opensDNS(nm, exitIP)
		})

		status, apiBody = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
		require.Equal(t, http.StatusOK, status, apiBody)
		assert.Equal(t, true, field(t, apiBody, "reporters", "0", "stale"))

		// Reporting again brings it back, on its IPv4 address when it
		// answers on both; withdrawing the approval, or switching the log
		// off, takes it away.
		sendReport(t, client, srv.URL, token, report(netip.AddrPortFrom(nodeIP6(t, exit), 53), ownResolver))
		laptop.WaitForCondition(t, "the resolver back", trafficWait, func(nm *netmap.NetworkMap) bool {
			return resolvesThrough(nm, exitIP)
		})
		assert.Equal(t, []any{exitIP.String()}, resolvers())

		status, body = approve(ownerKey, false)
		require.Equal(t, http.StatusOK, status, body)
		assert.Nil(t, body["resolverApprovedAt"])
		laptop.WaitForCondition(t, "the approval withdrawn", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !mentionsResolver(nm, exitIP)
		})

		status, body = approve(ownerKey, true)
		require.Equal(t, http.StatusOK, status, body)
		laptop.WaitForCondition(t, "approved again", trafficWait, func(nm *netmap.NetworkMap) bool {
			return resolvesThrough(nm, exitIP)
		})

		// The DNS log needs no global nameserver: without one kept for
		// exit node users, the agent forwards to the gateway's own
		// resolvers, as the gateway does for them without the monitor.
		code, reply := apiCall(t, client, ownerKey, http.MethodPut, v1+"/dns",
			map[string]any{"nameservers": []string{"100.100.100.100"}})
		require.Equal(t, http.StatusOK, code, reply)
		assert.Equal(t, []any{exitIP.String()}, resolvers())

		resp = sendReport(t, client, srv.URL, token, report(ownResolver))
		assert.Empty(t, resp.Config.Upstreams)

		status, apiBody = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/dns", nil)
		require.Equal(t, http.StatusOK, status, apiBody)

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"dnsLogging": false})
		require.Equal(t, http.StatusOK, status, body)
		laptop.WaitForCondition(t, "the log off", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !mentionsResolver(nm, exitIP)
		})
		exit.WaitForCondition(t, "port 53 closed with the log off", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !opensDNS(nm, exitIP)
		})

		resp = sendReport(t, client, srv.URL, token, report(ownResolver))
		assert.Empty(t, resp.Config.LogSources, "the log off records nobody")

		useExitNode(t, laptop, "")
	})

	t.Run("the v2 network log carries the traffic in Tailscale's shape", func(t *testing.T) {
		var logs []tsclient.NetworkFlowLog

		start := time.Unix(minute, 0).Truncate(time.Hour)
		err := goClient(t, srv.URL, ownerKey).Logging().GetNetworkFlowLogs(t.Context(),
			tsclient.NetworkFlowLogsRequest{Start: start, End: start.Add(time.Hour)},
			func(l tsclient.NetworkFlowLog) error {
				logs = append(logs, l)

				return nil
			})
		require.NoError(t, err)
		require.Len(t, logs, 1, "one record per gateway and hour")

		record := logs[0]
		assert.Equal(t, exit.NodeIDString(), record.NodeID)
		assert.Equal(t, start.UTC(), record.Start.UTC())
		assert.Equal(t, start.Add(time.Hour).UTC(), record.End.UTC())
		assert.Contains(t, record.ExitTraffic, tsclient.TrafficStats{
			Proto: 6, Src: laptopIP.String() + ":0", Dst: "140.82.112.3:443",
			TxPkts: 10, TxBytes: 1100, RxPkts: 40, RxBytes: 50_000,
		})
		assert.Equal(t, []tsclient.TrafficStats{{
			Proto: 6, Src: laptopIP.String() + ":0", Dst: "192.168.1.10:22", TxBytes: 300, RxBytes: 300,
		}}, record.SubnetTraffic)

		q := url.Values{
			"start": {start.Format(time.RFC3339)},
			"end":   {start.AddDate(0, 0, 8).Format(time.RFC3339)},
		}
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			srv.URL+"/api/v2/tailnet/-/logging/network?"+q.Encode(), nil)
		assert.Equal(t, http.StatusBadRequest, status, "more than a week: %v", body)
	})

	t.Run("roles bound who reads and who changes the monitor", func(t *testing.T) {
		keys := map[types.Role]string{}

		for _, role := range []types.Role{types.RoleNetworkAdmin, types.RoleITAdmin, types.RoleAuditor} {
			u := srv.CreateUser(t, "traffic-"+string(role))
			status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(u)+"/role",
				map[string]string{"role": string(role)})
			require.Equal(t, http.StatusOK, status, body)

			keys[role] = srv.CreateAPIKey(t, u)
		}

		keys[types.RoleMember] = srv.CreateAPIKey(t, srv.CreateUser(t, "traffic-member"))

		v2Logs := srv.URL + "/api/v2/tailnet/-/logging/network?start=" +
			url.QueryEscape(time.Now().Add(-time.Hour).Format(time.RFC3339)) +
			"&end=" + url.QueryEscape(time.Now().Format(time.RFC3339))

		for _, tt := range []struct {
			role         types.Role
			read, change bool
		}{
			{types.RoleNetworkAdmin, true, true},
			{types.RoleITAdmin, true, false},
			{types.RoleAuditor, true, false},
			{types.RoleMember, false, false},
		} {
			key := keys[tt.role]
			status := func(want bool) int {
				if want {
					return http.StatusOK
				}

				return http.StatusForbidden
			}

			for _, path := range []string{
				"/traffic/summary", "/traffic/destinations", "/traffic/dns", "/traffic/reporters",
				"/traffic/settings",
			} {
				got, body := apiCall(t, client, key, http.MethodGet, v1+path, nil)
				assert.Equal(t, status(tt.read), got, "%s GET %s: %v", tt.role, path, body)
			}

			got, body := apiCall(t, client, key, http.MethodGet, v2Logs, nil)
			assert.Equal(t, status(tt.read), got, "%s v2 network log: %v", tt.role, body)

			got, body = apiCall(t, client, key, http.MethodPatch, v1+"/traffic/settings",
				map[string]any{"sni": true})
			assert.Equal(t, status(tt.change), got, "%s PATCH settings: %v", tt.role, body)

			if !tt.change {
				got, body = apiCall(t, client, key, http.MethodDelete,
					v1+"/traffic/reporters/"+exit.NodeIDString(), nil)
				assert.Equal(t, http.StatusForbidden, got, "%s DELETE reporter: %v", tt.role, body)
			}
		}

		status, body := apiCall(t, client, keys[types.RoleNetworkAdmin], http.MethodDelete,
			v1+"/traffic/reporters/"+exit.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{}, body["reporters"])

		status, body = apiCall(t, client, ownerKey, http.MethodDelete,
			v1+"/traffic/reporters/"+exit.NodeIDString(), nil)
		assert.Equal(t, http.StatusNotFound, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/summary", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.InDelta(t, 2000, field(t, body, "total", "txBytes"), 0, "what it reported stays")
	})

	t.Run("a picked host, address or name keeps that one, not every one containing it", func(t *testing.T) {
		sendReport(t, client, srv.URL, idToken(t, srv, exit, traffic.Audience), traffic.Report{
			Version: "0.1.0", Instance: "exact-1", Seq: 1,
			Flows: []traffic.Flow{
				{
					Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("140.82.112.5"), Proto: 6, Port: 443,
					Host: "api.github.com", TxBytes: 70,
				},
				{Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("1.1.1.1"), Proto: 17, Port: 53, TxBytes: 11},
				{Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("1.1.1.10"), Proto: 17, Port: 53, TxBytes: 13},
				{Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("1.1.1.1"), Proto: 6, Port: 853, TxBytes: 17},
			},
			Queries: []traffic.Query{{Bucket: minute, Src: laptopIP, Name: "api.github.com", Count: 4}},
		})

		hosts := func(query string) map[string]float64 {
			t.Helper()

			status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/destinations?"+query, nil)
			require.Equal(t, http.StatusOK, status, body)

			rows, ok := field(t, body, "destinations").([]any)
			require.True(t, ok)

			out := map[string]float64{}

			for _, r := range rows {
				row, ok := r.(map[string]any)
				require.True(t, ok)

				key, _ := row["host"].(string)
				if key == "" {
					key, _ = row["dst"].(string)
				}

				tx, _ := row["txBytes"].(float64)
				out[key] += tx
			}

			return out
		}

		assert.Contains(t, hosts("groupBy=host&q=github.com"), "api.github.com", "the search still matches a part")
		assert.Equal(t, map[string]float64{"github.com": 1600},
			hosts("groupBy=host&host=GitHub.com"), "the pick is exact and case-blind")
		assert.Equal(t, map[string]float64{"1.1.1.1": 28},
			hosts("groupBy=host&host=1.1.1.1"), "an unnamed destination is picked by its address")
		assert.Len(t, hosts("groupBy=destination&q=1.1.1.1"), 2, "the search matches 1.1.1.10 too")
		assert.Equal(t, map[string]float64{"1.1.1.1": 11},
			hosts("groupBy=destination&dst=1.1.1.1&proto=17&port=53"), "one address and one service")
		assert.Equal(t, map[string]float64{"192.168.1.10": 300},
			hosts("groupBy=destination&private=true"), "only the LAN destinations")

		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/traffic/destinations?dst=github.com", nil)
		assert.Equal(t, http.StatusBadRequest, status, body)

		names := func(query string) []any {
			t.Helper()

			status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/dns?"+query, nil)
			require.Equal(t, http.StatusOK, status, body)

			rows, ok := field(t, body, "names").([]any)
			require.True(t, ok)

			return rows
		}

		assert.Len(t, names("q=github.com"), 2, "the search finds github.com and api.github.com")

		exact := names("name=github.com.")
		require.Len(t, exact, 1)
		assert.Equal(t, "github.com", field(t, exact[0], "name"))
	})
}

// endlessBody is a request body of JSON that never ends, counting what the
// server read of it.
type endlessBody struct {
	read atomic.Int64
	sent bool
}

func (b *endlessBody) Read(p []byte) (int, error) {
	n := len(p)
	b.read.Add(int64(n))

	for i := range p {
		p[i] = 'a'
	}

	if !b.sent {
		b.sent = true
		n = copy(p, `{"version":"`)
	}

	return n, nil
}

// stalledBody is a request body that blocks its reader until released.
type stalledBody struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *stalledBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release

	return 0, io.ErrUnexpectedEOF
}

// TestTrafficReportAdmission proves what a report costs the server before
// it is accepted: a credential that is not a gateway's current token is
// refused without reading a byte of the body, a gateway's body is read no
// further than the bound, and ingests beyond the server-wide limit are
// told to come back instead of queueing. It does not run in parallel, so
// the allocations it measures are the handler's.
//
//nolint:paralleltest // measures the process's allocations, which parallel tests would disturb
func TestTrafficReportAdmission(t *testing.T) {
	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	owner := srv.CreateUser(t, "admission-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	setStatePolicy(t, srv, `{
		"tagOwners": {"tag:gateway": ["admission-owner@"]},
		"grants": [{"src": ["admission-owner@"], "dst": ["autogroup:internet"], "ip": ["*"]}]
	}`)

	gateway := servertest.NewClient(t, srv, "gateway", servertest.WithUser(owner),
		servertest.WithTags("tag:gateway"),
		servertest.WithHostinfo(func(h *tailcfg.Hostinfo) { h.RoutableIPs = tsaddr.ExitRoutes() }))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			srv.URL+"/api/v1/node/"+gateway.NodeIDString()+"/approve_routes",
			map[string]any{"routes": []string{"0.0.0.0/0", "::/0"}})
		assert.Equal(c, http.StatusOK, status, body)
	}, trafficWait, 100*time.Millisecond)

	token := idToken(t, srv, gateway, traffic.Audience)

	post := func(token string, body io.Reader) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, traffic.ReportPath, body)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		rec := httptest.NewRecorder()
		srv.App.TrafficReportHandler(rec, req)

		return rec
	}

	t.Run("a stranger's body is never read", func(t *testing.T) {
		const requests = 10

		var before, after runtime.MemStats

		body := &endlessBody{}

		runtime.GC()
		runtime.ReadMemStats(&before)

		for range requests {
			assert.Equal(t, http.StatusUnauthorized, post("forged", body).Code)
			assert.Equal(t, http.StatusUnauthorized, post("", body).Code)
		}

		runtime.ReadMemStats(&after)

		perRequest := (after.TotalAlloc - before.TotalAlloc) / (2 * requests)
		t.Logf("a refused report allocates %d bytes and reads %d of its body", perRequest, body.read.Load())
		assert.Zero(t, body.read.Load())
		assert.Less(t, perRequest, uint64(64<<10))
	})

	t.Run("a gateway's body is read no further than the bound", func(t *testing.T) {
		body := &endlessBody{}

		rec := post(token, body)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
		t.Logf("an endless report read %d bytes before the refusal", body.read.Load())
		assert.LessOrEqual(t, body.read.Load(), int64(traffic.MaxReportBytes+64<<10))
	})

	t.Run("ingests beyond the limit are told to come back", func(t *testing.T) {
		var wg sync.WaitGroup

		stalled := make([]*stalledBody, 0, 4)

		for range 4 {
			b := &stalledBody{started: make(chan struct{}), release: make(chan struct{})}
			stalled = append(stalled, b)

			wg.Go(func() { post(token, b) })

			<-b.started
		}

		rec := post(token, bytes.NewReader(reportJSON(t, traffic.Report{Instance: "admission", Seq: 1})))
		assert.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
		assert.Equal(t, "5", rec.Header().Get("Retry-After"))

		for _, b := range stalled {
			close(b.release)
		}

		wg.Wait()

		rec = post(token, bytes.NewReader(reportJSON(t, traffic.Report{Instance: "admission", Seq: 1})))
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}

func mustKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := idtoken.GenerateKey()
	require.NoError(t, err)

	return key
}

func mustID(t *testing.T, node *servertest.TestClient) uint64 {
	t.Helper()

	id, err := strconv.ParseUint(node.NodeIDString(), 10, 64)
	require.NoError(t, err)

	return id
}

// TestTrafficNamesNetworksReportedBeforeTheTable proves the first reports
// of a fresh server get their networks: they arrive before the ASN table
// is in use, are stored without one, and the table the first report sets
// off names them once it is in use. A destination the table does not
// know stays unnamed.
func TestTrafficNamesNetworksReportedBeforeTheTable(t *testing.T) {
	t.Parallel()

	var asnDown atomic.Bool

	srv := servertest.NewServer(t,
		servertest.WithRealListener(),
		servertest.WithTraffic(types.TrafficConfig{
			ASNDatabaseURL: asnTableServer(t, &asnDown),
			ASNCachePath:   t.TempDir() + "/ip2asn.tsv.gz",
		}),
	)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "backfill-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	setStatePolicy(t, srv, `{
		"tagOwners": {"tag:gateway": ["backfill-owner@"]},
		"grants": [{"src": ["backfill-owner@"], "dst": ["autogroup:internet"], "ip": ["*"]}]
	}`)

	exit := servertest.NewClient(t, srv, "exit-b", servertest.WithUser(owner), servertest.WithTags("tag:gateway"))
	laptop := servertest.NewClient(t, srv, "laptop-b", servertest.WithUser(owner))

	exit.Direct().SetHostinfo(&tailcfg.Hostinfo{
		BackendLogID: "servertest-exit-b",
		Hostname:     "exit-b",
		RoutableIPs:  tsaddr.ExitRoutes(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	_ = exit.Direct().SendUpdate(ctx)

	cancel()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/approve_routes",
			map[string]any{"routes": []string{"0.0.0.0/0", "::/0"}})
		// Refused until the exit node's routes reach the server; field
		// fails the whole test rather than the attempt, so it waits for 200.
		if !assert.Equal(c, http.StatusOK, status, body) {
			return
		}

		assert.Len(c, field(t, body, "node", "approvedRoutes"), 2)
	}, trafficWait, 100*time.Millisecond)

	laptop.WaitForCondition(t, "the exit node as a peer", trafficWait, func(_ *netmap.NetworkMap) bool {
		_, ok := laptop.PeerByName("exit-b")

		return ok
	})

	require.Zero(t, srv.State().ASNRanges(), "no table before the first report")

	minute := minuteBucket(time.Now()) - 60
	laptopIP := nodeIP4(t, laptop)

	sendReport(t, client, srv.URL, idToken(t, srv, exit, traffic.Audience), traffic.Report{
		Version:  "0.1.0",
		Instance: "first",
		Seq:      1,
		SentAt:   time.Now(),
		Status:   traffic.Status{Conntrack: traffic.Collector{Enabled: true}},
		Flows: []traffic.Flow{
			{Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("140.82.112.3"), Proto: 6, Port: 443, TxBytes: 10},
			{Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("8.8.8.8"), Proto: 17, Port: 53, TxBytes: 20},
			{Bucket: minute, Src: laptopIP, Dst: netip.MustParseAddr("203.0.113.7"), Proto: 6, Port: 22, TxBytes: 30},
		},
	})

	asns := func() map[string]any {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/destinations", nil)
		require.Equal(t, http.StatusOK, status, body)

		out := map[string]any{}

		rows, ok := body["destinations"].([]any)
		require.True(t, ok)

		for _, r := range rows {
			row, ok := r.(map[string]any)
			require.True(t, ok)

			dst, ok := row["dst"].(string)
			require.True(t, ok)

			out[dst] = row["asn"]
		}

		return out
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, map[string]any{
			"140.82.112.3": float64(36459),
			"8.8.8.8":      float64(15169),
			"203.0.113.7":  float64(0),
		}, asns())
	}, trafficWait, 100*time.Millisecond, "the table the first report set off names what it stored")

	assert.Positive(t, srv.State().ASNRanges())
	require.NoError(t, srv.State().BackfillTrafficASN(t.Context()), "a second walk with the same table is a no-op")
}

// TestTrafficDNSLogFollowsTheExitNode proves the DNS log reaches only the
// nodes that use a gateway as their exit node, and only while they do: a
// node that picks a gateway gets its resolver, then the gateway's own
// exit node resolver, then the nameservers kept for exit node users; a
// node without an exit node, or a client too old to say which one it
// uses, keeps the tailnet's DNS untouched. Each agent is told exactly
// which nodes it may record. The subtests move one node from gateway to
// gateway, so they run in order.
//
//nolint:tparallel // each step starts from the exit node the one before left
func TestTrafficDNSLogFollowsTheExitNode(t *testing.T) {
	t.Parallel()

	var asnDown atomic.Bool

	srv := servertest.NewServer(t,
		servertest.WithRealListener(),
		servertest.WithDNS(types.DNSConfig{
			MagicDNS:         true,
			BaseDomain:       "exitdns.test",
			OverrideLocalDNS: true,
			Nameservers: types.Nameservers{
				Global:          []string{"1.1.1.1", "9.9.9.9", "100.100.100.100"},
				UseWithExitNode: []string{"9.9.9.9", "100.100.100.100"},
			},
		}),
		servertest.WithTraffic(types.TrafficConfig{
			ASNDatabaseURL: asnTableServer(t, &asnDown),
			ASNCachePath:   t.TempDir() + "/ip2asn.tsv.gz",
		}),
	)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "exitdns-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	setStatePolicy(t, srv, `{
		"tagOwners": {"tag:gateway": ["exitdns-owner@"]},
		"grants": [{"src": ["exitdns-owner@"], "dst": ["autogroup:internet"], "ip": ["*"]}]
	}`)

	gw1 := servertest.NewClient(t, srv, "gw-1", servertest.WithUser(owner), servertest.WithTags("tag:gateway"))
	gw2 := servertest.NewClient(t, srv, "gw-2", servertest.WithUser(owner), servertest.WithTags("tag:gateway"))
	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	phone := servertest.NewClient(t, srv, "phone", servertest.WithUser(owner))
	old := servertest.NewClient(t, srv, "old", servertest.WithUser(owner))

	// gw-1 announces its peer API, so its users can fall back to the
	// gateway's own DNS proxy; gw-2 does not.
	const peerAPIPort = 40001

	for _, gw := range []*servertest.TestClient{gw1, gw2} {
		hostinfo := &tailcfg.Hostinfo{
			BackendLogID: "servertest-" + gw.Name,
			Hostname:     gw.Name,
			RoutableIPs:  tsaddr.ExitRoutes(),
		}
		if gw == gw1 {
			hostinfo.Services = []tailcfg.Service{{Proto: tailcfg.PeerAPI4, Port: peerAPIPort}}
		}

		gw.Direct().SetHostinfo(hostinfo)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		require.NoError(t, gw.Direct().SendUpdate(ctx))
		cancel()

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			status, body := apiCall(t, client, ownerKey, http.MethodPost,
				v1+"/node/"+gw.NodeIDString()+"/approve_routes",
				map[string]any{"routes": []string{"0.0.0.0/0", "::/0"}})
			assert.Equal(c, http.StatusOK, status, body)
		}, trafficWait, 100*time.Millisecond)
	}

	laptop.WaitForCondition(t, "both gateways as peers", trafficWait, func(_ *netmap.NetworkMap) bool {
		_, ok1 := laptop.PeerByName("gw-1")
		_, ok2 := laptop.PeerByName("gw-2")

		return ok1 && ok2
	})

	gw1IP, gw2IP := nodeIP4(t, gw1), nodeIP4(t, gw2)
	gw1Stable := types.NodeID(mustID(t, gw1)).StableID()
	gw2Stable := types.NodeID(mustID(t, gw2)).StableID()
	laptopIPs := []netip.Addr{nodeIP4(t, laptop), nodeIP6(t, laptop)}

	// The old client uses gw-1 too, but predates Hostinfo.ExitNodeID
	// (capability version 122, Tailscale 1.86), so it never says so.
	useExitNode(t, old, "", func(h *tailcfg.Hostinfo) { h.IPNVersion = "1.84.0" })

	tailnetDNS := phone.Netmap().DNS

	tailnetResolvers := make([]string, 0, len(tailnetDNS.Resolvers))
	for _, r := range tailnetDNS.Resolvers {
		tailnetResolvers = append(tailnetResolvers, r.Addr)
	}

	require.Equal(t, []string{"1.1.1.1", "9.9.9.9", "100.100.100.100"}, tailnetResolvers)

	tokens := map[*servertest.TestClient]string{
		gw1: idToken(t, srv, gw1, traffic.Audience),
		gw2: idToken(t, srv, gw2, traffic.Audience),
	}
	seq := uint64(0)
	report := func(gw *servertest.TestClient) traffic.Config {
		t.Helper()

		seq++

		return sendReport(t, client, srv.URL, tokens[gw], traffic.Report{
			Instance: "exitdns-" + gw.Name,
			Seq:      seq,
			Status: traffic.Status{
				Conntrack: traffic.Collector{Enabled: true},
				DNS:       traffic.Collector{Enabled: true},
			},
			DNSListen: []netip.AddrPort{netip.AddrPortFrom(nodeIP4(t, gw), 53)},
		}).Config
	}

	status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
		map[string]any{"dnsLogging": true})
	require.Equal(t, http.StatusOK, status, body)

	for _, gw := range []*servertest.TestClient{gw1, gw2} {
		report(gw)

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/reporters/"+gw.NodeIDString(),
			map[string]any{"resolver": true})
		require.Equal(t, http.StatusOK, status, body)
		require.Equal(t, true, body["resolverActive"], body)
	}

	for _, gw := range []*servertest.TestClient{gw1, gw2} {
		cfg := report(gw)
		assert.True(t, cfg.DNS)
		assert.Empty(t, cfg.LogSources, "%s: nobody uses an exit node yet", gw.Name)
		assert.Equal(t, []string{"9.9.9.9"}, cfg.Upstreams,
			"the agents forward where exit node users would, never to a tailnet address")
	}

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []any{"100.100.100.100"}, body["skippedUpstreams"])

	assert.Equal(t, tailnetDNS, laptop.Netmap().DNS, "no exit node, no change")

	resolvesThrough := func(c *servertest.TestClient, want ...string) {
		t.Helper()

		c.WaitForCondition(t, fmt.Sprintf("DNS through %v", want), trafficWait, func(nm *netmap.NetworkMap) bool {
			got := make([]string, 0, len(nm.DNS.Resolvers))
			for _, r := range nm.DNS.Resolvers {
				got = append(got, r.Addr)
			}

			return slices.Equal(got, want)
		})
	}

	t.Run("a node that picks a gateway resolves through it, and only it is recorded", func(t *testing.T) {
		useExitNode(t, laptop, gw1Stable)

		gw1DoH := "http://" + netip.AddrPortFrom(gw1IP, peerAPIPort).String() + "/dns-query"
		resolvesThrough(laptop, gw1IP.String(), gw1DoH, "9.9.9.9", "100.100.100.100")
		assert.Equal(t, []string{gw1IP.String(), gw1DoH, "9.9.9.9", "100.100.100.100"},
			resolverAddrs(t, laptop.Netmap()))
		assert.Equal(t, tailnetDNS.Routes, laptop.Netmap().DNS.Routes, "split DNS stays the tailnet's")

		assert.Equal(t, laptopIPs, report(gw1).LogSources)
		assert.Empty(t, report(gw2).LogSources)

		for _, c := range []*servertest.TestClient{phone, old, gw1, gw2} {
			assert.Equal(t, tailnetDNS, c.Netmap().DNS, "%s keeps the tailnet's DNS", c.Name)
		}
	})

	t.Run("switching to another gateway moves the node to that resolver", func(t *testing.T) {
		useExitNode(t, laptop, gw2Stable)

		resolvesThrough(laptop, gw2IP.String(), "9.9.9.9", "100.100.100.100")
		assert.Empty(t, report(gw1).LogSources)
		assert.Equal(t, laptopIPs, report(gw2).LogSources)
	})

	t.Run("switching the exit node off gives the node the tailnet's DNS back", func(t *testing.T) {
		useExitNode(t, laptop, "")

		resolvesThrough(laptop, tailnetResolvers...)
		assert.Equal(t, tailnetDNS, laptop.Netmap().DNS)
		assert.Empty(t, report(gw1).LogSources)
		assert.Empty(t, report(gw2).LogSources)
	})

	t.Run("an exit node that is no gateway resolver changes nothing", func(t *testing.T) {
		useExitNode(t, laptop, "some-other-exit-node")

		assert.Never(t, func() bool {
			nm := laptop.Netmap()

			return mentionsResolver(nm, gw1IP) || mentionsResolver(nm, gw2IP)
		}, time.Second, 50*time.Millisecond)
		assert.Equal(t, tailnetDNS, laptop.Netmap().DNS)
		assert.Empty(t, report(gw1).LogSources)
	})
}
