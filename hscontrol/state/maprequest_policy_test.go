package state

import (
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// autoApproveTestPolicy lets aa-user's nodes see each other and
// auto-approve subnets under 10.55.0.0/16 and exit routes; aa-other's
// node only reaches the internet, so it sees an aa-user node only once
// that node is an approved exit node.
const autoApproveTestPolicy = `{
	"acls": [
		{"action": "accept", "src": ["aa-user@"], "dst": ["aa-user@:*", "10.55.0.0/16:*"]},
		{"action": "accept", "src": ["aa-other@"], "dst": ["autogroup:internet:*"]}
	],
	"autoApprovers": {
		"routes": {"10.55.0.0/16": ["aa-user@"]},
		"exitNode": ["aa-user@"]
	}}`

// newAutoApproveTestState returns a State under autoApproveTestPolicy with
// three aa-user nodes followed by one aa-other node. prepare edits the
// database rows before the State loads them, and the returned counter
// counts peer builds from then on.
func newAutoApproveTestState(
	t *testing.T,
	prepare func(nodes []*types.Node),
) (*State, []types.NodeID, *atomic.Int64) {
	t.Helper()

	dbPath := t.TempDir() + "/slopscale.db"
	cfg := persistTestConfig(dbPath)

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	user := database.CreateUserForTest("aa-user")
	other := database.CreateUserForTest("aa-other")
	nodes := database.CreateRegisteredNodesForTest(user, 3, "aa-node")
	nodes = append(nodes, database.CreateRegisteredNodeForTest(other, "aa-other-node"))

	if prepare != nil {
		prepare(nodes)

		for _, n := range nodes {
			require.NoError(t, db.SaveNode(database, n))
		}
	}

	require.NoError(t, database.Close())

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.SetPolicy([]byte(autoApproveTestPolicy))
	require.NoError(t, err)

	ids := make([]types.NodeID, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}

	return s, ids, countStatePeerBuilds(t, s)
}

func routeMapRequest(t *testing.T, s *State, id types.NodeID, routes ...netip.Prefix) tailcfg.MapRequest {
	t.Helper()

	nv, ok := s.GetNodeByID(id)
	require.True(t, ok)

	return tailcfg.MapRequest{
		NodeKey:  nv.NodeKey(),
		DiscoKey: nv.DiscoKey(),
		Hostinfo: &tailcfg.Hostinfo{
			Hostname:    nv.Hostname(),
			RoutableIPs: routes,
			NetInfo:     &tailcfg.NetInfo{PreferredDERP: 1},
		},
	}
}

// countNodeRowUpdates counts UPDATEs of the nodes table from now on.
func countNodeRowUpdates(t *testing.T, s *State) *atomic.Int64 {
	t.Helper()

	var n atomic.Int64

	s.DB().SetQueryHook(func(query string) error {
		if strings.HasPrefix(query, "UPDATE nodes") {
			n.Add(1)
		}

		return nil
	})
	t.Cleanup(func() { s.DB().SetQueryHook(nil) })

	return &n
}

func checkAutoApproveAdjacency(t *testing.T, s *State) {
	t.Helper()

	users, err := s.ListAllUsers()
	require.NoError(t, err)

	checkAdjacencyMatchesFullBuild(t, s.nodeStore, autoApproveTestPolicy, users, s.access.Load())
}

// TestMapRequestAutoApprovalIsOneWrite pins that a map request whose
// announced route the policy auto-approves lands in one route-changing
// NodeStore write (one peer build) and one row update, persists the
// approval, and reports the policy change the new route causes.
func TestMapRequestAutoApprovalIsOneWrite(t *testing.T) {
	s, ids, builds := newAutoApproveTestState(t, nil)
	route := netip.MustParsePrefix("10.55.1.0/24")

	req := routeMapRequest(t, s, ids[0], route)
	rowUpdates := countNodeRowUpdates(t, s)

	c, err := s.UpdateNodeFromMapRequest(ids[0], req)
	require.NoError(t, err)

	assert.Equal(t, int64(1), builds.Load(), "peer builds for one map request")
	assert.Equal(t, int64(1), rowUpdates.Load(), "node row updates for one map request")
	assert.Equal(t, "policy", c.Type())

	row, err := s.DB().GetNodeByID(ids[0])
	require.NoError(t, err)
	assert.Contains(t, row.ApprovedRoutes, route, "the approval must be persisted")

	nv, ok := s.GetNodeByID(ids[0])
	require.True(t, ok)
	assert.Contains(t, nv.SubnetRoutes(), route)

	checkAutoApproveAdjacency(t, s)
}

// TestMapRequestWithdrawingRoutesClearsUnhealthy pins that a node whose
// announced set shrinks to empty stops being an unhealthy HA candidate.
func TestMapRequestWithdrawingRoutesClearsUnhealthy(t *testing.T) {
	route := netip.MustParsePrefix("10.55.1.0/24")
	s, ids, _ := newAutoApproveTestState(t, func(nodes []*types.Node) {
		nodes[0].Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{route}}
		nodes[0].ApprovedRoutes = []netip.Prefix{route}
	})

	_, ok := s.nodeStore.UpdateNode(ids[0], func(n *types.Node) { n.Unhealthy = true })
	require.True(t, ok)

	_, err := s.UpdateNodeFromMapRequest(ids[0], routeMapRequest(t, s, ids[0]))
	require.NoError(t, err)

	nv, ok := s.GetNodeByID(ids[0])
	require.True(t, ok)
	assert.Empty(t, nv.AllApprovedRoutes())
	assert.False(t, nv.Unhealthy(), "a node with no approved routes is no HA candidate")
}

// TestSetApprovedRoutesReportsVisibility pins which change an admin route
// approval reports: a whole-peer update when nothing peers or the policy
// read moved, a policy change otherwise.
func TestSetApprovedRoutesReportsVisibility(t *testing.T) {
	subnetA := netip.MustParsePrefix("10.55.1.0/24")
	subnetB := netip.MustParsePrefix("10.55.2.0/24")
	exit := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}

	tests := []struct {
		name    string
		prepare func(nodes []*types.Node)
		approve []netip.Prefix
		// wantType is the returned change's [change.Change.Type].
		wantType string
		// wantNewPeer is the index of a node that must become a peer.
		wantNewPeer int
	}{
		{
			name: "unannounced route moves nothing peers see",
			prepare: func(nodes []*types.Node) {
				nodes[0].Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnetA}}
				nodes[0].ApprovedRoutes = []netip.Prefix{subnetA}
			},
			approve:     []netip.Prefix{subnetA, netip.MustParsePrefix("10.99.0.0/24")},
			wantType:    "peers",
			wantNewPeer: -1,
		},
		{
			// No primary moves and no peer is gained, but the node's
			// approved subnets are a policy input (wildcard sources, via
			// grants and its own reduced filter read them), so the
			// policy manager reports it.
			name: "second subnet as HA standby is a policy input",
			prepare: func(nodes []*types.Node) {
				nodes[1].Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnetB}}
				nodes[1].ApprovedRoutes = []netip.Prefix{subnetB}
				nodes[0].Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnetA, subnetB}}
				nodes[0].ApprovedRoutes = []netip.Prefix{subnetA}
			},
			approve:     []netip.Prefix{subnetA, subnetB},
			wantType:    "policy",
			wantNewPeer: -1,
		},
		{
			name: "first exit approval makes the node visible to a new peer",
			prepare: func(nodes []*types.Node) {
				nodes[0].Hostinfo = &tailcfg.Hostinfo{RoutableIPs: exit}
			},
			approve:     exit,
			wantType:    "policy",
			wantNewPeer: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ids, _ := newAutoApproveTestState(t, tt.prepare)

			primariesBefore := s.nodeStore.PrimaryRoutes()
			peersBefore := s.nodeStore.ListPeerIDs(ids[0])

			_, c, err := s.SetApprovedRoutes(ids[0], tt.approve)
			require.NoError(t, err)

			assert.Equal(t, tt.wantType, c.Type())

			if c.Type() == "peers" {
				assert.Equal(t, ids[0], c.OriginNode)
				assert.Contains(t, c.PeersChanged, ids[0])
			}

			peersAfter := s.nodeStore.ListPeerIDs(ids[0])

			if tt.wantNewPeer < 0 {
				assert.Equal(t, peersBefore, peersAfter, "scenario must keep the node's peers")
				assert.Equal(t, primariesBefore, s.nodeStore.PrimaryRoutes(), "scenario must keep every primary")
			} else {
				assert.NotContains(t, peersBefore, ids[tt.wantNewPeer])
				assert.Contains(t, peersAfter, ids[tt.wantNewPeer])
			}

			checkAutoApproveAdjacency(t, s)
		})
	}
}

// benchMapRequestSetup pre-creates a sqlite database with n registered
// nodes spread across 10 users (~10% tagged tag:srv, half of those also
// carrying an approved and announced 10.x.0.0/24 route), then constructs a State
// that loads them, at benchmark scale the same way persistTestSetup
// does for a single node. Returns the State and the ID of node 0, a
// plain node with neither tag nor route, to drive requests against.
func benchMapRequestSetup(b *testing.B, n int) (*State, types.NodeID) {
	b.Helper()

	dbPath := b.TempDir() + "/slopscale.db"
	cfg := persistTestConfig(dbPath)

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(b, err)

	users := make([]*types.User, 10)
	for i := range users {
		users[i] = database.CreateUserForTest(fmt.Sprintf("u%d", i+1))
	}

	var targetID types.NodeID

	for i := range n {
		node := database.CreateRegisteredNodeForTest(users[i%len(users)], fmt.Sprintf("n%d", i))

		if i == 0 {
			targetID = node.ID

			continue
		}

		if i%10 != 0 {
			continue
		}

		node.Tags = []string{"tag:srv"}

		if i%20 == 0 {
			subnet := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte((i / 20) % 256), 0, 0}), 24)
			node.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnet}}
			node.ApprovedRoutes = []netip.Prefix{subnet}
		}

		require.NoError(b, db.SaveNode(database, node))
	}

	require.NoError(b, database.Close())

	s, err := NewState(cfg)
	require.NoError(b, err)
	b.Cleanup(func() { _ = s.Close() })

	target, ok := s.GetNodeByID(targetID)
	require.True(b, ok)
	require.False(b, target.IsTagged(), "target must be plain")
	require.Empty(b, target.AnnouncedRoutes(), "target must be plain")

	routers := 0

	for _, nv := range s.ListNodes().All() {
		if len(nv.AnnouncedRoutes()) > 0 {
			routers++
		}
	}

	require.Positive(b, routers, "setup must create subnet routers")

	return s, targetID
}

// BenchmarkUpdateNodeFromMapRequest measures the no-op path
// TestNoOpMapRequestSkipsPersist and TestNoOpMapRequestEmitsNoPeerChange
// pin the behaviour of: a MapRequest that is value-identical to the
// node's current state, against a realistic node count. The baseline
// later NodeStore write-path changes are compared against.
func BenchmarkUpdateNodeFromMapRequest(b *testing.B) {
	b.Run("identical/n=617", func(b *testing.B) {
		s, nodeID := benchMapRequestSetup(b, 617)

		nv, ok := s.GetNodeByID(nodeID)
		require.True(b, ok, "target node should exist in NodeStore")

		req := tailcfg.MapRequest{
			NodeKey:  nv.NodeKey(),
			DiscoKey: nv.DiscoKey(),
			Hostinfo: &tailcfg.Hostinfo{
				Hostname: nv.Hostname(),
				NetInfo:  &tailcfg.NetInfo{PreferredDERP: 1},
			},
		}

		// Establish the Hostinfo/DERP state once so every request timed
		// below is a genuine no-op.
		_, err := s.UpdateNodeFromMapRequest(nodeID, req)
		require.NoError(b, err)

		b.ReportAllocs()

		for b.Loop() {
			_, err := s.UpdateNodeFromMapRequest(nodeID, req)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
