package servertest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/netmap"
)

// postMachine sends a JSON body to a /machine endpoint over the node's
// Noise connection, as tailscaled does, and returns the status and body.
func postMachine(
	t *testing.T,
	srv *servertest.TestServer,
	node *servertest.TestClient,
	path string,
	body any,
) (int, []byte) {
	t.Helper()

	raw, err := json.Marshal(body)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	target := strings.Replace(srv.URL+"/machine/"+path, "http://", "https://", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	require.NoError(t, err)

	resp, err := node.Direct().DoNoiseRequest(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	var out bytes.Buffer

	_, err = out.ReadFrom(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, out.Bytes()
}

// TestPeerEntriesCarryThePeersOwnVersion pins [tailcfg.Node.Cap] on a peer
// entry to the version that peer sent, not the viewer's: magicsock decides
// from it whether a peer speaks peer relay. A node that registered but
// never polled has no known version and shows as 0.
func TestPeerEntriesCarryThePeersOwnVersion(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	bob := servertest.NewClient(t, srv, "bob", servertest.WithUser(owner))

	alice.WaitForPeerCount(t, 1, 5*time.Second)
	bob.WaitForPeerCount(t, 1, 5*time.Second)

	// Bob's entry may first arrive from his registration, before his first
	// map request told the server his version; the patch that follows
	// carries it.
	alice.WaitForCondition(t, "bob's entry carries bob's version", 5*time.Second, func(nm *netmap.NetworkMap) bool {
		for _, p := range nm.Peers {
			if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == "bob" {
				return p.Cap() == tailcfg.CurrentCapabilityVersion
			}
		}

		return false
	})
	assert.Equal(t, tailcfg.CurrentCapabilityVersion, alice.Netmap().SelfNode.Cap())

	// A node created straight in the database has never sent a map
	// request, so its version is unknown until it polls.
	registered := srv.CreateRegisteredNode(t, owner, "silent")
	srv.App.Change(change.NodeAdded(registered.ID()))

	alice.WaitForCondition(t, "silent shows an unknown version", 5*time.Second, func(nm *netmap.NetworkMap) bool {
		for _, p := range nm.Peers {
			if p.ID() == registered.ID().NodeID() {
				return p.Cap() == 0
			}
		}

		return false
	})
}

// TestPeerHostinfoOmitsNetInfo pins that a peer entry's Hostinfo carries
// the fields peers read but not NetInfo, the node's own NAT and DERP
// latency findings; the DERP home still travels as HomeDERP.
func TestPeerHostinfoOmitsNetInfo(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	bob := servertest.NewClient(
		t,
		srv,
		"bob",
		servertest.WithUser(owner),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.OS = "linux"
			hi.NetInfo = &tailcfg.NetInfo{PreferredDERP: 7, MappingVariesByDestIP: "true"}
		}),
	)

	alice.WaitForPeerCount(t, 1, 5*time.Second)
	bob.WaitForPeerCount(t, 1, 5*time.Second)

	peer, ok := alice.PeerByName("bob")
	require.True(t, ok)
	assert.Equal(t, tailcfg.DERPRegionID(7), peer.HomeDERP())
	assert.Equal(t, "linux", peer.Hostinfo().OS())
	assert.False(t, peer.Hostinfo().NetInfo().Valid(), "NetInfo is the node's own business")

	// The node itself keeps its NetInfo.
	assert.True(t, bob.Netmap().SelfNode.Hostinfo().NetInfo().Valid())
}

// TestNonStreamingMapRequestGetsOneResponse pins [tailcfg.MapRequest.Stream]
// off without OmitPeers: the client expects exactly one MapResponse and
// then the end of the connection, not an empty 200.
func TestNonStreamingMapRequestGetsOneResponse(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	servertest.NewClient(t, srv, "bob", servertest.WithUser(owner))

	alice.WaitForPeerCount(t, 1, 5*time.Second)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	nm, err := alice.Direct().FetchNetMapForTest(ctx)
	require.NoError(t, err)
	require.NotNil(t, nm.SelfNode)
	assert.Len(t, nm.Peers, 1)
}

// TestClientAuditLogIsRecorded pins /machine/audit-log: the client posts
// one entry per audited action and never retries, so the server records it
// as an audit event with the machine as actor and answers 200.
func TestClientAuditLogIsRecorded(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")
	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))

	status, _ := postMachine(t, srv, alice, "audit-log", tailcfg.AuditLogRequest{
		Version:   tailcfg.CurrentCapabilityVersion,
		NodeKey:   alice.NodePrivateKey().Public(),
		Action:    tailcfg.AuditNodeDisconnect,
		Details:   "leaving for the day",
		Timestamp: time.Now(),
	})
	require.Equal(t, http.StatusOK, status)

	events, err := srv.State().ListAuditEvents(types.AuditQuery{Action: "node.client.disconnect"})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, types.ActorNode, events[0].ActorKind)
	assert.Equal(t, "alice", events[0].ActorName)
	assert.Equal(t, types.UserID(owner.ID), events[0].ActorUserID)
	assert.Equal(t, "node", events[0].TargetKind)
	assert.Equal(t, "leaving for the day", events[0].Detail["details"])

	// A node key from another session is refused.
	bob := servertest.NewClient(t, srv, "bob", servertest.WithUser(owner))
	status, _ = postMachine(t, srv, bob, "audit-log", tailcfg.AuditLogRequest{
		NodeKey: alice.NodePrivateKey().Public(),
		Action:  tailcfg.AuditNodeDisconnect,
	})
	assert.Equal(t, http.StatusUnauthorized, status)
}

// TestFeatureQueryTellsHowToEnable pins /machine/feature/query, which
// `tailscale serve` asks when the node lacks the https cap: it names the
// missing attribute and points at the policy page, reports the feature as
// on once the policy grants it, and says plainly that Funnel is not
// available here.
func TestFeatureQueryTellsHowToEnable(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")
	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))

	query := func(feature string) tailcfg.QueryFeatureResponse {
		status, body := postMachine(t, srv, alice, "feature/query", tailcfg.QueryFeatureRequest{
			Feature: feature,
			NodeKey: alice.NodePrivateKey().Public(),
		})
		require.Equal(t, http.StatusOK, status)

		var resp tailcfg.QueryFeatureResponse

		require.NoError(t, json.Unmarshal(body, &resp))

		return resp
	}

	off := query("serve")
	assert.False(t, off.Complete)
	assert.Contains(t, off.Text, "granting the https node attribute")
	assert.Equal(t, srv.URL+"/admin/policy", off.URL)

	reloadPolicy(t, srv, `{
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}],
		"nodeAttrs": [{"target": ["*"], "attr": ["https"]}]
	}`)

	alice.WaitForCondition(t, "https cap granted", 5*time.Second, func(nm *netmap.NetworkMap) bool {
		return hasCap(nm, nodecap.HTTPS)
	})

	assert.True(t, query("serve").Complete)

	funnel := query("funnel")
	assert.False(t, funnel.Complete)
	assert.Contains(t, funnel.Text, "not available on this server")
	assert.Empty(t, funnel.URL)

	assert.Contains(t, query("teleport").Text, "not a feature this server knows")
}

// TestMaxKeyDurationCapFollowsTheSetting pins tailnet.maxKeyDuration on the
// self node: absent while the key expiry cap is off, the cap in seconds
// once it is set.
func TestMaxKeyDurationCapFollowsTheSetting(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	alice.WaitForCondition(t, "first netmap", 5*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm.SelfNode.Valid()
	})
	assert.False(t, hasCap(alice.Netmap(), nodecap.MaxKeyDuration))

	require.NoError(t, srv.State().SetKeyExpiry(48*time.Hour))

	bob := servertest.NewClient(t, srv, "bob", servertest.WithUser(owner))
	bob.WaitForCondition(t, "first netmap", 5*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm.SelfNode.Valid()
	})

	caps := bob.Netmap().SelfNode.CapMap()
	values, ok := caps.GetOk(nodecap.MaxKeyDuration)
	require.True(t, ok)
	require.Equal(t, 1, values.Len())
	assert.JSONEq(t, "172800", string(values.At(0)))
}

// TestClientWarningsReachTheAPI pins that the warn-* flags a client sends
// in its map request are shown on the node through the v1 API, so an
// operator can see why a subnet router forwards nothing.
func TestClientWarningsReachTheAPI(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")

	servertest.NewClient(t, srv, "router", servertest.WithUser(owner),
		servertest.WithDebugFlags("warn-ip-forwarding-off", "warn-router-unhealthy", "warn-ip-forwarding-off"))
	servertest.NewClient(t, srv, "quiet", servertest.WithUser(owner))

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/node", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+srv.CreateAPIKey(t, owner))

	resp, err := srv.HTTPClient(t).Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	var body struct {
		Nodes []struct {
			GivenName      string   `json:"givenName"`
			ClientWarnings []string `json:"clientWarnings"`
		} `json:"nodes"`
	}

	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Nodes, 2)

	for _, n := range body.Nodes {
		switch n.GivenName {
		case "router":
			assert.Equal(t, []string{"ip-forwarding-off", "router-unhealthy"}, n.ClientWarnings)
		case "quiet":
			assert.Equal(t, []string{}, n.ClientWarnings)
		}
	}
}
