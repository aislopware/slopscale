package state

import (
	"net/netip"
	"testing"
	"time"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
)

// The mapper takes every peer list, full or incremental, from the
// NodeStore's peer map and never asks the policy again, so the map must
// match the live policy by the time a change is published. These tests
// cover the policy manager inputs that once changed without a rebuild.

// requirePeerMapCurrent fails when a node's peers in the NodeStore differ
// from a fresh build under the live policy.
func requirePeerMapCurrent(t *testing.T, s *State) {
	t.Helper()

	nodes := s.nodeStore.ListNodes().AsSlice()
	fresh := peerPositionsFunc(s.polMan)(nodes)

	for i, n := range nodes {
		var want []types.NodeID

		if fresh != nil {
			for _, p := range fresh[i] {
				want = append(want, nodes[p].ID())
			}
		}

		var got []types.NodeID
		for _, p := range s.ListPeers(n.ID()).All() {
			got = append(got, p.ID())
		}

		require.ElementsMatch(t, want, got, "peers of node %d", n.ID())
	}
}

func peerIDs(s *State, id types.NodeID) []types.NodeID {
	var out []types.NodeID
	for _, p := range s.ListPeers(id).All() {
		out = append(out, p.ID())
	}

	return out
}

func TestPeerMapFollowsSetPolicy(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	alice := createUserWithRole(t, s, "alice", "")
	bob := createUserWithRole(t, s, "bob", "")

	a := s.CreateRegisteredNodeForTest(alice, "alice-1")
	s.PutNodeInStoreForTest(*a)
	b := s.CreateRegisteredNodeForTest(bob, "bob-1")
	s.PutNodeInStoreForTest(*b)

	_, err := s.updatePolicyManagerNodes()
	require.NoError(t, err)

	// A fresh tailnet only reaches its own machines.
	require.Empty(t, peerIDs(s, a.ID))

	changed, err := s.SetPolicy([]byte(`{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`))
	require.NoError(t, err)
	require.True(t, changed)

	assert.Equal(t, []types.NodeID{b.ID}, peerIDs(s, a.ID))
	requirePeerMapCurrent(t, s)
}

// TestPeerMapCurrentAfterBoot starts a server whose SSH recorder is the
// only thing that connects two users. The NodeStore builds its peer map
// before the recorders are loaded, and applying them does not rebuild
// it; the access model loaded next does, as its builtin groups always
// change the compile at boot.
func TestPeerMapCurrentAfterBoot(t *testing.T) {
	t.Parallel()

	cfg := persistTestConfig(t.TempDir() + "/slopscale.db")

	database, err := hsdb.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	alice := database.CreateUserForTest("alice")
	bob := database.CreateUserForTest("bob")
	recorder := database.CreateRegisteredNodeForTest(alice, "recorder")
	b := database.CreateRegisteredNodeForTest(bob, "bob-1")

	_, err = database.SetPolicy(`{"acls":[
		{"action":"accept","src":["alice@"],"dst":["alice@:*"]},
		{"action":"accept","src":["bob@"],"dst":["bob@:*"]}
	]}`)
	require.NoError(t, err)
	require.NoError(t, database.SaveSSHRecording([]string{recorder.IPv4.String() + "/32"}, false))
	require.NoError(t, database.Close())

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, []types.NodeID{recorder.ID}, peerIDs(s, b.ID), "bob uploads to the recorder")
	requirePeerMapCurrent(t, s)
}

// TestPeerMapFollowsTrafficResolvers turns a gateway's resolver on: its
// grant admits every node to the gateway's port 53.
func TestPeerMapFollowsTrafficResolvers(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	owner := createUserWithRole(t, s, "owner", "")
	bob := createUserWithRole(t, s, "bob", "")

	_, err := s.SetPolicy([]byte(`{
		"tagOwners": {"tag:gateway": ["owner@"]},
		"grants": [{"src": ["owner@"], "dst": ["autogroup:internet"], "ip": ["*"]}]
	}`))
	require.NoError(t, err)

	gw := s.CreateRegisteredNodeForTest(owner, "gateway")
	gw.Tags = []string{"tag:gateway"}
	gw.Hostinfo = &tailcfg.Hostinfo{Hostname: "gateway", RoutableIPs: tsaddr.ExitRoutes()}
	gw.ApprovedRoutes = tsaddr.ExitRoutes()
	s.PutNodeInStoreForTest(*gw)

	b := s.CreateRegisteredNodeForTest(bob, "bob-1")
	s.PutNodeInStoreForTest(*b)

	_, err = s.updatePolicyManagerNodes()
	require.NoError(t, err)
	require.Empty(t, peerIDs(s, b.ID), "bob has no grant to the gateway")

	_, _, err = s.PatchTrafficSettings(func(ts *types.TrafficSettings) { ts.DNSLogging = true })
	require.NoError(t, err)

	now := time.Now()

	s.trafficMu.Lock()
	s.trafficReporters[gw.ID] = types.TrafficReporter{
		NodeID:             gw.ID,
		Status:             traffic.Status{DNS: traffic.Collector{Enabled: true}},
		DNSListen:          []netip.AddrPort{netip.AddrPortFrom(*gw.IPv4, 53)},
		LastReportAt:       now,
		ResolverApprovedAt: now,
	}
	s.trafficMu.Unlock()

	c, err := s.TrafficTick(now)
	require.NoError(t, err)
	require.False(t, c.IsEmpty(), "the resolver comes into use")

	assert.Equal(t, []types.NodeID{gw.ID}, peerIDs(s, b.ID))
	requirePeerMapCurrent(t, s)
}
