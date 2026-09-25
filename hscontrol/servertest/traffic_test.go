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
	"slices"
	"strconv"
	"strings"
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
// points every client's DNS at the gateway's resolver only while it
// reports. The subtests build on one another, so they run in order.
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
	// is the grant the DNS log adds.
	setStatePolicy(t, srv, `{"grants": [
		{"src": ["traffic-owner@"], "dst": ["autogroup:internet"], "ip": ["*"]}
	]}`)

	exit := servertest.NewClient(t, srv, "exit-1", servertest.WithUser(owner))
	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	phone := servertest.NewClient(t, srv, "phone", servertest.WithUser(owner))
	plain := servertest.NewClient(t, srv, "plain", servertest.WithUser(owner))

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
		assert.Equal(c, http.StatusOK, status, body)
		assert.Len(c, field(t, body, "node", "approvedRoutes"), 2)
	}, trafficWait, 100*time.Millisecond)

	laptop.WaitForCondition(t, "the exit node as a peer", trafficWait, func(_ *netmap.NetworkMap) bool {
		_, ok := laptop.PeerByName("exit-1")

		return ok
	})

	exitIP, laptopIP, phoneIP := nodeIP4(t, exit), nodeIP4(t, laptop), nodeIP4(t, phone)
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
		assert.Equal(t, []string{"1.1.1.1"}, resp.Config.Upstreams, "a tailnet resolver is never an upstream")

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

		require.Len(t, rows, 4, "nothing between nodes, nothing from strangers")
		assert.Equal(t, "GITHUB", rows["140.82.112.3"]["asName"])
		assert.InDelta(t, 36459, rows["140.82.112.3"]["asn"], 0)
		assert.Equal(t, "US", rows["140.82.112.3"]["country"])
		assert.Equal(t, "github.com", rows["140.82.112.3"]["host"], "names are stored lower case")
		assert.Equal(t, "GOOGLE", rows["8.8.8.8"]["asName"])
		assert.Equal(t, true, rows["192.168.1.10"]["private"])
		assert.Equal(t, false, rows["8.8.8.8"]["private"])

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
		assert.InDelta(t, 1, field(t, body, "reporters", "0", "unattributed"), 0)
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
		refused(t, idToken(t, srv, plain, traffic.Audience), http.StatusForbidden, "not a gateway")

		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/suspend", map[string]any{"suspended": true})
		require.Equal(t, http.StatusOK, status, body)
		refused(t, token, http.StatusForbidden, "suspended")

		status, body = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/suspend", map[string]any{"suspended": false})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/approve", map[string]any{"approved": false})
		require.Equal(t, http.StatusOK, status, body)
		refused(t, token, http.StatusForbidden, "waiting for approval")

		status, body = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+exit.NodeIDString()+"/approve", map[string]any{"approved": true})
		require.Equal(t, http.StatusOK, status, body)
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

	t.Run("the DNS log points clients at a fresh gateway resolver only", func(t *testing.T) {
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

		// Off by default: a working resolver is not used.
		sendReport(t, client, srv.URL, token, report(ownResolver))
		assert.False(t, mentionsResolver(laptop.Netmap(), exitIP))
		assert.False(t, opensDNS(exit.Netmap(), exitIP))

		status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"dnsLogging": true})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["dnsLogging"])

		// Addresses that are not the gateway's own port 53 are never used,
		// so an agent cannot point the tailnet at another node.
		resp := sendReport(t, client, srv.URL, token,
			report(netip.AddrPortFrom(laptopIP, 53), netip.AddrPortFrom(exitIP, 5353)))
		assert.True(t, resp.Config.DNS)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{}, body["resolvers"])

		sendReport(t, client, srv.URL, token, report(ownResolver))

		for _, c := range []*servertest.TestClient{laptop, phone} {
			c.WaitForCondition(t, "DNS through the gateway", trafficWait, func(nm *netmap.NetworkMap) bool {
				return resolvesThrough(nm, exitIP) && mentionsResolver(nm, netip.MustParseAddr("1.1.1.1"))
			})
		}

		exit.WaitForCondition(t, "port 53 open on the gateway", trafficWait, func(nm *netmap.NetworkMap) bool {
			return opensDNS(nm, exitIP)
		})
		assert.False(t, mentionsResolver(exit.Netmap(), exitIP), "the gateway never resolves through itself")

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{exitIP.String()}, body["resolvers"])
		assert.Equal(t, true, field(t, body, "reporters", "0", "resolverActive"))

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

		// A gateway whose last report is older than three minutes is
		// dropped by the scheduler's tick. The report is backdated
		// through the state, with a token the server issued back then.
		sendReport(t, client, srv.URL, token, report(ownResolver))
		laptop.WaitForCondition(t, "DNS through the gateway again", trafficWait, func(nm *netmap.NetworkMap) bool {
			return resolvesThrough(nm, exitIP)
		})

		then := time.Now().Add(-4 * time.Minute)
		backdated := signedToken(t, srv, signer, exit, exit.NodePrivateKey().Public().String(), then)
		_, c, err := srv.State().IngestTrafficReport(backdated, report(ownResolver), then)
		require.NoError(t, err)
		assert.True(t, c.IsEmpty(), "still fresh as of then")

		c, err = srv.State().TrafficTick(time.Now())
		require.NoError(t, err)
		require.False(t, c.IsEmpty(), "the stale resolver is a change")
		srv.App.Change(c)

		laptop.WaitForCondition(t, "the stale resolver dropped", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !mentionsResolver(nm, exitIP)
		})
		exit.WaitForCondition(t, "port 53 closed again", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !opensDNS(nm, exitIP)
		})

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/traffic/reporters", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "reporters", "0", "stale"))

		// Reporting again brings it back; switching the log off takes it
		// away for good.
		sendReport(t, client, srv.URL, token, report(ownResolver))
		laptop.WaitForCondition(t, "the resolver back", trafficWait, func(nm *netmap.NetworkMap) bool {
			return resolvesThrough(nm, exitIP)
		})

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/traffic/settings",
			map[string]any{"dnsLogging": false})
		require.Equal(t, http.StatusOK, status, body)
		laptop.WaitForCondition(t, "the log off", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !mentionsResolver(nm, exitIP)
		})
		exit.WaitForCondition(t, "port 53 closed with the log off", trafficWait, func(nm *netmap.NetworkMap) bool {
			return !opensDNS(nm, exitIP)
		})
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
