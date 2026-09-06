package state

import (
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

func TestNodeStoreDebugString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setupFn  func() *NodeStore
		contains []string
	}{
		{
			name: "empty nodestore",
			setupFn: func() *NodeStore {
				return NewNodeStore(nil, allowAllPeersFunc, TestBatchSize, TestBatchTimeout)
			},
			contains: []string{
				"=== NodeStore Debug Information ===",
				"Total Nodes: 0",
				"Users with Nodes: 0",
				"NodeKey Index: 0 entries",
			},
		},
		{
			name: "nodestore with data",
			setupFn: func() *NodeStore {
				node1 := createTestNode(1, 1, "user1", "node1")
				node2 := createTestNode(2, 2, "user2", "node2")

				store := NewNodeStore(nil, allowAllPeersFunc, TestBatchSize, TestBatchTimeout)
				store.Start()

				_ = store.PutNode(node1)
				_ = store.PutNode(node2)

				return store
			},
			contains: []string{
				"Total Nodes: 2",
				"Users with Nodes: 2",
				"Peer Relationships:",
				"NodeKey Index: 2 entries",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := tt.setupFn()
			if store.writeQueue != nil {
				defer store.Stop()
			}

			debugStr := store.DebugString()

			for _, expected := range tt.contains {
				assert.Contains(t, debugStr, expected,
					"Debug string should contain: %s\nActual debug:\n%s", expected, debugStr)
			}
		})
	}
}

func TestDebugRegistrationCache(t *testing.T) {
	t.Parallel()

	// Create a minimal NodeStore for testing debug methods
	store := NewNodeStore(nil, allowAllPeersFunc, TestBatchSize, TestBatchTimeout)

	debugStr := store.DebugString()

	// Should contain basic debug information
	assert.Contains(t, debugStr, "=== NodeStore Debug Information ===")
	assert.Contains(t, debugStr, "Total Nodes: 0")
	assert.Contains(t, debugStr, "Users with Nodes: 0")
	assert.Contains(t, debugStr, "NodeKey Index: 0 entries")
}

// debugTestState is persistTestSetup with the State closed at the end of
// the test; the node it returns is registered and loaded into the NodeStore.
func debugTestState(t *testing.T) (*State, types.NodeID) {
	t.Helper()

	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	return s, nodeID
}

// announceRoute makes the node advertise and hold approval for route, then
// brings it online so it becomes a primary-route candidate.
func announceRoute(t *testing.T, s *State, nodeID types.NodeID, route netip.Prefix) {
	t.Helper()

	_, ok := s.nodeStore.UpdateNode(nodeID, func(n *types.Node) {
		n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{route}}
		n.ApprovedRoutes = []netip.Prefix{route}
	})
	require.True(t, ok)

	_, epoch := s.Connect(nodeID)
	require.NotZero(t, epoch)
}

func TestStateDebugRegistrationCache(t *testing.T) {
	t.Parallel()

	s, _ := debugTestState(t)

	assert.Equal(t, map[string]any{
		"type":        "expirable-lru",
		"expiration":  registerCacheExpiration.String(),
		"max_entries": defaultRegisterCacheMaxEntries,
		"current_len": 0,
		"status":      "active",
	}, s.DebugRegistrationCache())
}

func TestStateDebugRoutes(t *testing.T) {
	t.Parallel()

	s, nodeID := debugTestState(t)
	route := netip.MustParsePrefix("10.44.0.0/24")

	// Offline node: nothing is available or elected, and the text form is
	// empty rather than a header with no rows.
	debug := s.DebugRoutes()
	assert.Empty(t, debug.AvailableRoutes)
	assert.Empty(t, debug.PrimaryRoutes)
	assert.Empty(t, debug.UnhealthyNodes)
	assert.Empty(t, s.DebugRoutesString())

	announceRoute(t, s, nodeID, route)

	debug = s.DebugRoutes()
	assert.Equal(t, map[types.NodeID][]netip.Prefix{nodeID: {route}}, debug.AvailableRoutes)
	assert.Equal(t, map[string]types.NodeID{route.String(): nodeID}, debug.PrimaryRoutes)
	assert.Empty(t, debug.UnhealthyNodes)
	assert.Equal(t, route.String()+": "+strconv.FormatUint(nodeID.Uint64(), 10)+"\n", s.DebugRoutesString())

	// An unhealthy mark only sticks on an online node with approved routes,
	// which this one now is.
	s.SetNodeHealth(nodeID, false)

	debug = s.DebugRoutes()
	assert.Equal(t, []types.NodeID{nodeID}, debug.UnhealthyNodes)
}

func TestStateDebugSSHPolicies(t *testing.T) {
	t.Parallel()

	s, nodeID := debugTestState(t)

	node, ok := s.GetNodeByID(nodeID)
	require.True(t, ok)

	// Without SSH rules every node still gets an entry; the policy is empty.
	policies := s.DebugSSHPolicies()
	key := "id:" + strconv.FormatUint(nodeID.Uint64(), 10) +
		" hostname:" + node.Hostname() + " givenname:" + node.GivenName()
	require.Contains(t, policies, key)

	_, err := s.SetPolicy([]byte(`{
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}],
		"ssh": [{
			"action": "accept",
			"src": ["persist-user@"],
			"dst": ["persist-user@"],
			"users": ["autogroup:nonroot"]
		}]
	}`))
	require.NoError(t, err)

	policies = s.DebugSSHPolicies()
	require.Contains(t, policies, key)
	require.NotNil(t, policies[key])
	require.Len(t, policies[key].Rules, 1)
	assert.True(t, policies[key].Rules[0].Action.Accept)
}

func TestStateDebugPolicy(t *testing.T) {
	t.Parallel()

	t.Run("db mode without policy fails", func(t *testing.T) {
		t.Parallel()

		s, _ := debugTestState(t)

		_, err := s.DebugPolicy()
		require.ErrorIs(t, err, types.ErrPolicyNotFound)
	})

	t.Run("db mode returns stored policy", func(t *testing.T) {
		t.Parallel()

		s, _ := debugTestState(t)

		const pol = `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`

		_, err := s.SetPolicyInDB(pol)
		require.NoError(t, err)

		got, err := s.DebugPolicy()
		require.NoError(t, err)
		assert.JSONEq(t, pol, got)
	})

	t.Run("file mode returns file and overview reports path", func(t *testing.T) {
		t.Parallel()

		const pol = `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`

		dir := t.TempDir()
		polPath := filepath.Join(dir, "policy.hujson")
		require.NoError(t, os.WriteFile(polPath, []byte(pol), 0o600))

		cfg := persistTestConfig(filepath.Join(dir, "headscale.db"))
		cfg.Policy = types.PolicyConfig{Mode: types.PolicyModeFile, Path: polPath}

		s, err := NewState(cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })

		got, err := s.DebugPolicy()
		require.NoError(t, err)
		assert.JSONEq(t, pol, got)

		info := s.DebugOverviewJSON()
		assert.Equal(t, string(types.PolicyModeFile), info.Policy.Mode)
		assert.Equal(t, polPath, info.Policy.Path)

		text := s.DebugOverview()
		assert.Contains(t, text, "  - Mode: "+string(types.PolicyModeFile))
		assert.Contains(t, text, "  - Path: "+polPath)
	})

	t.Run("unsupported mode is rejected", func(t *testing.T) {
		t.Parallel()

		s, _ := debugTestState(t)
		s.cfg.Policy.Mode = "carrier-pigeon"

		_, err := s.DebugPolicy()
		require.ErrorIs(t, err, ErrUnsupportedPolicyMode)
		assert.Contains(t, err.Error(), "carrier-pigeon")
	})
}

func TestStateDebugOverviewCounts(t *testing.T) {
	t.Parallel()

	dbPath := t.TempDir() + "/headscale.db"
	cfg := persistTestConfig(dbPath)

	database, err := db.NewHeadscaleDatabase(cfg)
	require.NoError(t, err)

	user := database.CreateUserForTest("overview-user")
	online := database.CreateRegisteredNodeForTest(user, "online-node")
	expired := database.CreateRegisteredNodeForTest(user, "expired-node")

	require.NoError(t, database.Close())

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, epoch := s.Connect(online.ID)
	require.NotZero(t, epoch)

	_, ok := s.nodeStore.UpdateNode(expired.ID, func(n *types.Node) {
		n.Expiry = new(time.Now().Add(-time.Hour))
	})
	require.True(t, ok)

	info := s.DebugOverviewJSON()
	assert.Equal(t, 2, info.Nodes.Total)
	assert.Equal(t, 1, info.Nodes.Online)
	assert.Equal(t, 1, info.Nodes.Expired)
	assert.Equal(t, 0, info.Nodes.Ephemeral)
	assert.Equal(t, 1, info.TotalUsers)
	assert.Equal(t, map[string]int{"overview-user": 2}, info.Users)
	assert.False(t, info.DERP.Configured)
	assert.Equal(t, 0, info.PrimaryRoutes)

	text := s.DebugOverview()
	assert.Contains(t, text, "Nodes: 2 total")
	assert.Contains(t, text, "  - Online: 1")
	assert.Contains(t, text, "  - Expired: 1")
	assert.Contains(t, text, "  - overview-user: 2 nodes")
	assert.Contains(t, text, "DERP: not configured")
	assert.Contains(t, text, "Primary Routes: 0 active")

	s.SetDERPMap(&tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			1: {RegionID: 1, RegionName: "one"},
			2: {RegionID: 2, RegionName: "two"},
		},
	})

	info = s.DebugOverviewJSON()
	assert.True(t, info.DERP.Configured)
	assert.Equal(t, 2, info.DERP.Regions)
	assert.Contains(t, s.DebugOverview(), "DERP: 2 regions configured")
}

// TestStateDebugPassthroughs pins the debug accessors that only forward to
// a subsystem, so a renamed field or a swapped receiver shows up here
// rather than as a blank debug page.
func TestStateDebugPassthroughs(t *testing.T) {
	t.Parallel()

	s, nodeID := debugTestState(t)

	assert.Same(t, s.cfg, s.DebugConfig())

	assert.Contains(t, s.DebugNodeStore(), "=== NodeStore Debug Information ===")
	assert.Contains(t, s.DebugNodeStore(), "Total Nodes: 1")

	nodes := s.DebugNodeStoreJSON()
	require.Contains(t, nodes, nodeID)
	assert.Equal(t, "persist-node", nodes[nodeID].Hostname)

	pm := s.DebugPolicyManager()
	assert.Contains(t, pm, "PolicyManager (v")
	assert.Equal(t, pm, s.PolicyDebugString())
	assert.Equal(t, DebugStringInfo{Content: pm}, s.DebugPolicyManagerJSON())

	filter, err := s.DebugFilter()
	require.NoError(t, err)

	wantFilter, _ := s.Filter()
	assert.Equal(t, wantFilter, filter)

	s.SetDERPMap(nil)
	assert.Equal(t, "DERP Map: not configured\n", s.DebugDERPMap())
	assert.Equal(t, DebugDERPInfo{Regions: map[tailcfg.DERPRegionID]*DebugDERPRegion{}}, s.DebugDERPJSON())

	s.SetDERPMap(&tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			7: {
				RegionID:   7,
				RegionName: "seven",
				Nodes: []*tailcfg.DERPNode{
					{Name: "7a", RegionID: 7, HostName: "derp7.headscale.test", DERPPort: 443, STUNPort: 3478},
					{Name: "7b", RegionID: 7, HostName: "derp7b.headscale.test", DERPPort: 8443},
				},
			},
		},
	})

	text := s.DebugDERPMap()
	assert.Contains(t, text, "Total Regions: 1")
	assert.Contains(t, text, "Region 7: seven")
	assert.Contains(t, text, "  - Nodes: 2")
	assert.Contains(t, text, "    - 7a (derp7.headscale.test:443)\n      STUN: 3478\n")
	assert.Contains(t, text, "    - 7b (derp7b.headscale.test:8443)\n")
	assert.NotContains(t, text, "STUN: 0")

	info := s.DebugDERPJSON()
	assert.True(t, info.Configured)
	assert.Equal(t, 1, info.TotalRegions)
	require.Contains(t, info.Regions, tailcfg.DERPRegionID(7))
	assert.Equal(t, &DebugDERPRegion{
		RegionID:   7,
		RegionName: "seven",
		Nodes: []*DebugDERPNode{
			{Name: "7a", HostName: "derp7.headscale.test", DERPPort: 443, STUNPort: 3478},
			{Name: "7b", HostName: "derp7b.headscale.test", DERPPort: 8443},
		},
	}, info.Regions[7])
}
