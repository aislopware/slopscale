package integration

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	policyv2 "github.com/aislopware/slopscale/hscontrol/policy/v2"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/integration/dockertestutil"
	"github.com/aislopware/slopscale/integration/hsic"
	"github.com/aislopware/slopscale/integration/integrationutil"
	"github.com/aislopware/slopscale/integration/tsic"
	"github.com/ory/dockertest/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
)

const (
	// trafficTLSHost is the name the client asks for in its TLS handshake;
	// nothing resolves it, so only the gateway's SNI capture can name it.
	trafficTLSHost = "traffic.example.test"
	// trafficBlobBytes is what the client downloads through the exit node.
	trafficBlobBytes = 4 << 20
	// trafficFlowdPath is where the agent binary goes in a client container.
	trafficFlowdPath = "/usr/local/bin/slopscale-flowd"
	trafficFlowdLog  = "/tmp/slopscale-flowd.log"
	// trafficReportWait covers a minute bucket closing, the agent's
	// one-minute report interval and the upload.
	trafficReportWait = 4 * time.Minute
	// trafficStaleWait covers the server's 90 s without a report, the 15 s
	// tick that drops the resolver, and the map update.
	trafficStaleWait = 4 * time.Minute
	// trafficGatewayTag marks the exit node; only tagged gateways report.
	trafficGatewayTag = "tag:gateway"
)

// TestTrafficMonitor runs the real slopscale-flowd on a real exit node
// and proves the whole path: a client's download through the exit node is
// attributed to the client with its size and SNI name, DNS logging points
// the client at the gateway's resolver and records what it resolves, a
// stopped agent's resolver leaves the client's DNS again, and an agent on
// a node that is no gateway is refused.
func TestTrafficMonitor(t *testing.T) {
	IntegrationSkip(t)

	certPEM, keyPEM := trafficTLSCert(t)

	spec := ScenarioSpec{
		NodesPerUser: 1,
		Users:        []string{"user1", "user2"},
		Versions:     []string{"head"},
		Networks: map[string]NetworkSpec{
			"usernet1": {Users: []string{"user1"}},
			"usernet2": {Users: []string{"user2"}},
		},
		ExtraService: map[string][]extraServiceFunc{
			"usernet1": {trafficWebservice(certPEM, keyPEM)},
		},
	}

	scenario, err := NewScenario(spec)

	require.NoErrorf(t, err, "failed to create scenario: %s", err)
	defer scenario.ShutdownAssertNoPanics(t)

	err = scenario.CreateSlopscaleEnv([]tsic.Option{},
		hsic.WithTestName("traffic"),
		hsic.WithACLPolicy(trafficPolicy()),
		// The ASN table would be fetched from the internet; naming
		// networks is covered by the servertest suite.
		hsic.WithConfigEnv(map[string]string{"SLOPSCALE_TRAFFIC_ASN_DATABASE_URL": ""}),
	)
	requireNoErrSlopscaleEnv(t, err)

	allClients, err := scenario.ListTailscaleClients()
	requireNoErrListClients(t, err)

	err = scenario.WaitForTailscaleSync()
	requireNoErrSync(t, err)

	slopscale, err := scenario.Slopscale()
	requireNoErrGetSlopscale(t, err)

	var gateway, client TailscaleClient

	for _, c := range allClients {
		s := c.MustStatus()
		switch s.User[s.Self.UserID].LoginName {
		case "user1@test.no":
			gateway = c
		case "user2@test.no":
			client = c
		}
	}

	require.NotNil(t, gateway)
	require.NotNil(t, client)

	// Deferred after the scenario's shutdown, so it runs before it.
	defer trafficSaveFlowdLog(gateway)
	defer trafficSaveFlowdLog(client)

	route, err := scenario.SubnetOfNetwork("usernet1")
	require.NoError(t, err)

	// The subnet is advertised with the exit routes because an exit node
	// does not forward to its locally connected networks otherwise.
	_, _, err = gateway.Execute([]string{
		"tailscale", "set", "--advertise-exit-node", "--advertise-routes=" + route.String(),
	})
	require.NoErrorf(t, err, "failed to advertise routes: %s", err)

	var nodes []*clientv1.Node

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		var listErr error

		nodes, listErr = slopscale.ListNodes()
		assert.NoError(c, listErr)
		assert.Len(c, nodes, 2)

		for _, node := range nodes {
			if node.Name == gateway.Hostname() {
				requireNodeRouteCountWithCollect(c, node, 3, 0, 0)
			}
		}
	}, integrationutil.ScaledTimeout(10*time.Second), 100*time.Millisecond, "route advertisements should propagate")

	gatewayID := trafficNodeID(t, nodes, gateway.Hostname())
	clientID := trafficNodeID(t, nodes, client.Hostname())

	// A gateway is infrastructure: only a tagged node may report.
	require.NoError(t, slopscale.SetNodeTags(mustParseID(gatewayID), []string{trafficGatewayTag}))

	_, err = slopscale.ApproveRoutes(
		mustParseID(gatewayID),
		[]netip.Prefix{tsaddr.AllIPv4(), tsaddr.AllIPv6(), *route},
	)
	require.NoError(t, err)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		status, statusErr := client.Status()
		assert.NoError(c, statusErr)

		for _, peerKey := range status.Peers() {
			assert.True(c, status.Peer[peerKey].ExitNodeOption, "the gateway should be an exit node option")
		}
	}, integrationutil.ScaledTimeout(10*time.Second), integrationutil.SlowPoll, "exit routes should reach the client")

	_, _, err = client.Execute([]string{"tailscale", "set", "--exit-node", gateway.Hostname()})
	require.NoErrorf(t, err, "failed to set exit node: %s", err)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		status, statusErr := client.Status()
		assert.NoError(c, statusErr)
		assert.NotNil(c, status.ExitNodeStatus, "exit node should be active")
	}, 30*time.Second, 500*time.Millisecond, "exit node activation")

	usernet1, err := scenario.Network("usernet1")
	require.NoError(t, err)

	services, err := scenario.Services("usernet1")
	require.NoError(t, err)
	require.Len(t, services, 1)

	web := services[0]
	webIP := web.GetIPInNetwork(usernet1)
	webName := strings.TrimPrefix(web.Container().Name, "/")

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		_, curlErr := client.Curl("http://" + webIP + "/hostname")
		assert.NoError(c, curlErr)
	}, 30*time.Second, 500*time.Millisecond, "the client should reach the web service through the exit node")

	api := newTrafficAPI(t, slopscale)

	flowd := trafficBuildFlowd(t)
	trafficStartFlowd(t, gateway, flowd)

	// --- The gateway reports, with connection tracking and SNI working.
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		var out trafficReportersOut

		assert.NoError(c, api.get("/api/v1/traffic/reporters", &out))

		reporter, ok := out.find(gatewayID)
		if !assert.True(c, ok, "the gateway should be a reporter") {
			return
		}

		assert.False(c, reporter.Stale)
		assert.True(c, reporter.Collectors.Conntrack.Enabled)
		assert.Empty(c, reporter.Collectors.Conntrack.Error)
		assert.True(c, reporter.Collectors.SNI.Enabled)
		assert.Empty(c, reporter.Collectors.SNI.Error)
	}, trafficReportWait, 2*time.Second, "the gateway's agent should report healthy collectors")

	// --- One HTTPS download through the exit node, named only by SNI.
	stdout, _, err := client.Execute([]string{
		"curl", "--silent", "--show-error", "--insecure", "--max-time", "60",
		"--resolve", fmt.Sprintf("%s:443:%s", trafficTLSHost, webIP),
		"--output", "/dev/null", "--write-out", "%{size_download}",
		"https://" + trafficTLSHost + "/blob",
	})
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(trafficBlobBytes), strings.TrimSpace(stdout), "the download should be complete")

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		var out trafficDestinationsOut

		assert.NoError(c, api.get("/api/v1/traffic/destinations?groupBy=host&nodeId="+clientID, &out))

		dest, ok := out.find(func(d trafficDestination) bool { return d.Host == trafficTLSHost })
		if !assert.True(c, ok, "the download should be named %s: %+v", trafficTLSHost, out.Destinations) {
			return
		}

		// Counted as IP packets: the payload plus TCP/IP and TLS framing.
		assert.GreaterOrEqual(c, dest.RxBytes, uint64(trafficBlobBytes))
		assert.LessOrEqual(c, dest.RxBytes, uint64(trafficBlobBytes*11/10))
		assert.Positive(c, dest.TxBytes)
		assert.Positive(c, dest.Conns)
	}, trafficReportWait, 2*time.Second, "the download should be attributed to its SNI name")

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		var out trafficSummaryOut

		assert.NoError(c, api.get("/api/v1/traffic/summary", &out))

		node, ok := out.findNode(clientID)
		if !assert.True(c, ok, "the client should be a top node: %+v", out.Nodes) {
			return
		}

		assert.GreaterOrEqual(c, node.RxBytes, uint64(trafficBlobBytes))
		assert.LessOrEqual(c, node.RxBytes, uint64(trafficBlobBytes*12/10))

		_, gatewayListed := out.findNode(gatewayID)
		assert.False(c, gatewayListed, "the gateway sent nothing through itself")

		reporter, ok := out.findReporter(gatewayID)
		if assert.True(c, ok, "the gateway should carry the volume") {
			assert.GreaterOrEqual(c, reporter.RxBytes, uint64(trafficBlobBytes))
		}
	}, trafficReportWait, 2*time.Second, "the client should be attributed with the download")

	// --- DNS logging points the client at the gateway's resolver once an
	// operator approves it. The harness already gives the tailnet global
	// nameservers (127.0.0.11 and 1.1.1.1), which DNS logging requires and
	// the gateway forwards to, so Docker's resolver answers container names.
	require.NoError(t, api.patch("/api/v1/traffic/settings", map[string]any{"dnsLogging": true}))
	require.NoError(t, api.patch("/api/v1/traffic/reporters/"+gatewayID, map[string]any{"resolver": true}))

	gatewayIPv4 := gateway.MustIPv4()
	resolverAddr := gatewayIPv4.String()

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		nm, nmErr := client.Netmap()
		if !assert.NoError(c, nmErr) {
			return
		}

		var found bool

		for _, r := range nm.DNS.Resolvers {
			if strings.HasPrefix(r.Addr, resolverAddr) {
				found = true

				assert.True(c, r.UseWithExitNode, "the gateway resolver should survive the exit node")
			}
		}

		assert.True(
			c,
			found,
			"the client's DNS should list the gateway resolver %s: %+v",
			resolverAddr,
			nm.DNS.Resolvers,
		)
	}, trafficReportWait, 2*time.Second, "the gateway resolver should reach the client's DNS")

	// The client's MagicDNS resolver forwards to the gateway, which asks
	// Docker's resolver for the web service's container name.
	var resolved string

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		out, _, lookupErr := client.Execute([]string{"nslookup", webName, "100.100.100.100"})
		assert.NoError(c, lookupErr)
		assert.Contains(c, out, webIP)

		resolved = out
	}, 60*time.Second, time.Second, "the client should resolve the web service through the gateway")

	t.Logf("lookup through the gateway:\n%s", resolved)

	// A plain HTTP download of the name it just resolved: no handshake to
	// read, so the gateway names it from the client's own DNS answer.
	stdout, _, err = client.Execute([]string{
		"curl", "--silent", "--show-error", "--max-time", "60",
		"--resolve", fmt.Sprintf("%s:80:%s", webName, webIP),
		"--output", "/dev/null", "--write-out", "%{size_download}",
		"http://" + webName + "/blob",
	})
	require.NoError(t, err)
	require.Equal(t, strconv.Itoa(trafficBlobBytes), strings.TrimSpace(stdout))

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		var out trafficNamesOut

		assert.NoError(c, api.get("/api/v1/traffic/dns?nodeId="+clientID, &out))
		assert.True(c, slices.ContainsFunc(out.Names, func(n trafficName) bool {
			return n.Name == webName && n.Queries > 0
		}), "the client's lookup of %s should be logged: %+v", webName, out.Names)
	}, trafficReportWait, 2*time.Second, "the DNS log should hold the client's lookup")

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		var out trafficDestinationsOut

		assert.NoError(c, api.get("/api/v1/traffic/destinations?groupBy=host&nodeId="+clientID, &out))

		dest, ok := out.find(func(d trafficDestination) bool { return d.Host == webName })
		if assert.True(c, ok, "the HTTP download should be named %s: %+v", webName, out.Destinations) {
			assert.GreaterOrEqual(c, dest.RxBytes, uint64(trafficBlobBytes))
		}
	}, trafficReportWait, 2*time.Second, "the HTTP download should be named from the DNS answer")

	// --- An agent on a node that is no gateway is refused.
	token := trafficIDToken(t, client)
	status := trafficPostReport(t, slopscale, token)
	assert.Equal(t, http.StatusForbidden, status, "a node that routes nothing may not report")

	trafficStartFlowd(t, client, flowd)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		log, readErr := client.ReadFile(trafficFlowdLog)
		assert.NoError(c, readErr)
		assert.Contains(c, string(log), "403", "the agent should log the refusal")
	}, trafficReportWait, 2*time.Second, "the client's agent should be refused")

	var reporters trafficReportersOut

	require.NoError(t, api.get("/api/v1/traffic/reporters", &reporters))

	_, clientReports := reporters.find(clientID)
	assert.False(t, clientReports, "a refused agent is not a reporter")

	trafficStopFlowd(t, client)

	// --- A stopped agent's resolver leaves the client's DNS.
	trafficStopFlowd(t, gateway)

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		nm, nmErr := client.Netmap()
		if !assert.NoError(c, nmErr) {
			return
		}

		for _, r := range nm.DNS.Resolvers {
			assert.False(c, strings.HasPrefix(r.Addr, resolverAddr),
				"the stale gateway resolver should be gone: %+v", nm.DNS.Resolvers)
		}
	}, trafficStaleWait, 5*time.Second, "a stale gateway's resolver should leave the client's DNS")

	var after trafficReportersOut

	require.NoError(t, api.get("/api/v1/traffic/reporters", &after))

	reporter, ok := after.find(gatewayID)
	require.True(t, ok)
	assert.True(t, reporter.Stale)
	assert.False(t, reporter.ResolverActive)
}

// trafficPolicy lets every node reach everything, the internet through the
// exit node included, and lets user1 own the gateway tag.
func trafficPolicy() *policyv2.Policy {
	return &policyv2.Policy{
		TagOwners: policyv2.TagOwners{
			trafficGatewayTag: policyv2.Owners{new(policyv2.Username("user1@"))},
		},
		ACLs: []policyv2.ACL{{
			Action:  "accept",
			Sources: []policyv2.Alias{policyv2.Wildcard},
			Destinations: []policyv2.AliasWithPorts{
				{Alias: policyv2.Wildcard, Ports: []tailcfg.PortRange{tailcfg.PortRangeAny}},
			},
		}},
	}
}

// trafficNodeID is the id of the node with hostname.
func trafficNodeID(t *testing.T, nodes []*clientv1.Node, hostname string) string {
	t.Helper()

	for _, node := range nodes {
		if node.Name == hostname {
			return node.Id
		}
	}

	require.FailNow(t, "no node named "+hostname)

	return ""
}

// trafficBuildFlowd builds the agent for the containers' platform from
// this checkout.
func trafficBuildFlowd(t *testing.T) []byte {
	t.Helper()

	out := filepath.Join(t.TempDir(), "slopscale-flowd")

	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", out, "../cmd/slopscale-flowd")

	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)

	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "building slopscale-flowd: %s", output)

	binary, err := os.ReadFile(out)
	require.NoError(t, err)

	return binary
}

// trafficStartFlowd installs the agent in a client container and starts
// it in the background, as its systemd unit would, on the container's
// tailnet interface.
func trafficStartFlowd(t *testing.T, node TailscaleClient, binary []byte) {
	t.Helper()

	container, ok := node.(*tsic.TailscaleInContainer)
	require.True(t, ok, "the agent runs in a Tailscale container")
	require.NoError(t, container.WriteFile(trafficFlowdPath, binary))

	_, _, err := node.Execute([]string{"chmod", "0755", trafficFlowdPath})
	require.NoError(t, err)

	_, _, err = node.Execute([]string{
		"sh", "-c",
		trafficFlowdPath + " --interface tsdev --verbose </dev/null >" + trafficFlowdLog + " 2>&1 &",
	})
	require.NoError(t, err)
}

// trafficStopFlowd stops the agent.
func trafficStopFlowd(t *testing.T, node TailscaleClient) {
	t.Helper()

	_, _, err := node.Execute([]string{"pkill", "-TERM", "-f", trafficFlowdPath})
	require.NoError(t, err)
}

// trafficSaveFlowdLog keeps the agent's log with the run's artefacts, if
// the agent ran on the node.
func trafficSaveFlowdLog(node TailscaleClient) {
	log, err := node.ReadFile(trafficFlowdLog)
	if err == nil {
		_ = os.WriteFile(fmt.Sprintf("/tmp/control/%s_slopscale-flowd.log", node.Hostname()), log, 0o600)
	}
}

// trafficIDToken asks the node's tailscaled for the identity token the
// agent would send.
func trafficIDToken(t *testing.T, node TailscaleClient) string {
	t.Helper()

	stdout, _, err := node.Execute(
		[]string{"env", "TAILSCALE_USE_WIP_CODE=1", "tailscale", "id-token", traffic.Audience},
	)
	require.NoError(t, err)

	token := strings.TrimSpace(stdout)
	require.NotEmpty(t, token)

	return token
}

// trafficPostReport sends an empty report with the token and returns the
// status the server answers.
func trafficPostReport(t *testing.T, hs ControlServer, token string) int {
	t.Helper()

	body, err := json.Marshal(traffic.Report{
		Version:  "integration",
		Instance: "integration",
		Seq:      1,
		SentAt:   time.Now(),
	})
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		hs.GetEndpoint()+traffic.ReportPath, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := trafficHTTPClient().Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode
}

func trafficHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// trafficAPI calls the server's API v1 with an API key.
type trafficAPI struct {
	endpoint string
	key      string
	client   *http.Client
}

func newTrafficAPI(t *testing.T, hs ControlServer) *trafficAPI {
	t.Helper()

	var key string

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		out, err := hs.Execute([]string{"slopscale", "apikeys", "create", "--expiration", "24h"})
		assert.NoError(c, err)
		assert.NotEmpty(c, out)

		key = strings.TrimSpace(out)
	}, integrationutil.ScaledTimeout(20*time.Second), time.Second, "creating an API key")

	return &trafficAPI{endpoint: hs.GetEndpoint(), key: key, client: trafficHTTPClient()}
}

func (a *trafficAPI) do(method, path string, body, out any) error {
	var reader io.Reader

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}

		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, a.endpoint+path, reader) //nolint:noctx // bounded by the client timeout
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.key)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, raw)
	}

	if out == nil {
		return nil
	}

	return json.Unmarshal(raw, out)
}

func (a *trafficAPI) get(path string, out any) error {
	return a.do(http.MethodGet, path, nil, out)
}

func (a *trafficAPI) patch(path string, body any) error {
	return a.do(http.MethodPatch, path, body, nil)
}

type trafficCounts struct {
	TxBytes uint64 `json:"txBytes"`
	RxBytes uint64 `json:"rxBytes"`
	Conns   uint64 `json:"conns"`
}

type trafficNode struct {
	trafficCounts

	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
}

type trafficSummaryOut struct {
	Nodes     []trafficNode `json:"nodes"`
	Reporters []trafficNode `json:"reporters"`
}

func (o trafficSummaryOut) findNode(id string) (trafficNode, bool) {
	i := slices.IndexFunc(o.Nodes, func(n trafficNode) bool { return n.NodeID == id })
	if i < 0 {
		return trafficNode{}, false
	}

	return o.Nodes[i], true
}

func (o trafficSummaryOut) findReporter(id string) (trafficNode, bool) {
	i := slices.IndexFunc(o.Reporters, func(n trafficNode) bool { return n.NodeID == id })
	if i < 0 {
		return trafficNode{}, false
	}

	return o.Reporters[i], true
}

type trafficDestination struct {
	trafficCounts

	Dst  string `json:"dst"`
	Port int    `json:"port"`
	Host string `json:"host"`
}

type trafficDestinationsOut struct {
	Destinations []trafficDestination `json:"destinations"`
}

func (o trafficDestinationsOut) find(match func(trafficDestination) bool) (trafficDestination, bool) {
	i := slices.IndexFunc(o.Destinations, match)
	if i < 0 {
		return trafficDestination{}, false
	}

	return o.Destinations[i], true
}

type trafficName struct {
	Name    string `json:"name"`
	Queries uint64 `json:"queries"`
}

type trafficNamesOut struct {
	Names []trafficName `json:"names"`
}

type trafficCollector struct {
	Enabled bool   `json:"enabled"`
	Error   string `json:"error"`
}

type trafficReporter struct {
	NodeID     string `json:"nodeId"`
	Stale      bool   `json:"stale"`
	Collectors struct {
		Conntrack trafficCollector `json:"conntrack"`
		SNI       trafficCollector `json:"sni"`
		DNS       trafficCollector `json:"dns"`
	} `json:"collectors"`
	DNSListen      []string `json:"dnsListen"`
	ResolverActive bool     `json:"resolverActive"`
}

type trafficReportersOut struct {
	Reporters []trafficReporter `json:"reporters"`
}

func (o trafficReportersOut) find(id string) (trafficReporter, bool) {
	i := slices.IndexFunc(o.Reporters, func(r trafficReporter) bool { return r.NodeID == id })
	if i < 0 {
		return trafficReporter{}, false
	}

	return o.Reporters[i], true
}

// trafficTLSCert is a self-signed certificate for [trafficTLSHost]; the
// client does not verify it, the gateway only reads the name the client
// asks for.
func trafficTLSCert(t *testing.T) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: trafficTLSHost},
		DNSNames:     []string{trafficTLSHost},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return string(certPEM), string(keyPEM)
}

// trafficWebServer serves /blob, [trafficBlobBytes] of random data, and
// /hostname over HTTP on port 80 and HTTPS on port 443.
const trafficWebServer = `
import base64, http.server, os, ssl, threading
os.makedirs("/srv", exist_ok=True)
with open("/srv/blob", "wb") as f:
    f.write(os.urandom(int(os.environ["BLOB_BYTES"])))
with open("/srv/hostname", "w") as f:
    f.write(os.uname().nodename)
with open("/tmp/cert.pem", "wb") as f:
    f.write(base64.b64decode(os.environ["TLS_CERT"]))
with open("/tmp/key.pem", "wb") as f:
    f.write(base64.b64decode(os.environ["TLS_KEY"]))
os.chdir("/srv")
handler = http.server.SimpleHTTPRequestHandler
plain = http.server.ThreadingHTTPServer(("0.0.0.0", 80), handler)
tls = http.server.ThreadingHTTPServer(("0.0.0.0", 443), handler)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain("/tmp/cert.pem", "/tmp/key.pem")
tls.socket = ctx.wrap_socket(tls.socket, server_side=True)
threading.Thread(target=tls.serve_forever, daemon=True).start()
plain.serve_forever()
`

// trafficWebservice runs [trafficWebServer] on the network, out of the
// control server's image like [Webservice].
func trafficWebservice(certPEM, keyPEM string) extraServiceFunc {
	return func(s *Scenario, networkName string) (dockertest.ClosableResource, error) {
		hostname := "hs-trafficweb-" + strings.ToLower(rand.Text()[:6])

		network, ok := s.networks[s.prefixedNetworkName(networkName)]
		if !ok {
			return nil, fmt.Errorf("network does not exist: %s", networkName)
		}

		opts := &dockertestutil.RunSpec{
			Name:     hostname,
			Cmd:      []string{"python3", "-c", trafficWebServer},
			Networks: []*dockertestutil.Network{network},
			Env: []string{
				"BLOB_BYTES=" + strconv.Itoa(trafficBlobBytes),
				"TLS_CERT=" + base64.StdEncoding.EncodeToString([]byte(certPEM)),
				"TLS_KEY=" + base64.StdEncoding.EncodeToString([]byte(keyPEM)),
			},
		}

		dockertestutil.DockerAddIntegrationLabels(opts, "web")

		return dockertestutil.RunPrebuiltOrBuild(
			s.pool,
			hsic.SlopscaleImageEnv,
			&dockertest.BuildOptions{
				Dockerfile: hsic.IntegrationTestDockerFileName,
				ContextDir: dockerContextPath,
			},
			opts,
			dockertestutil.DockerRestartPolicy,
		)
	}
}
