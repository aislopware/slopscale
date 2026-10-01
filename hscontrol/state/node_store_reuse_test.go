package state

import (
	"net/netip"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// countingPeers is a [PeersFunc] that makes every node a peer of every
// other and counts how often the store asked for it.
func countingPeers(calls *atomic.Int64) PeersFunc {
	return func(nodes []types.NodeView) map[types.NodeID][]types.NodeView {
		calls.Add(1)

		out := make(map[types.NodeID][]types.NodeView, len(nodes))

		for _, n := range nodes {
			for _, p := range nodes {
				if p.ID() != n.ID() {
					out[n.ID()] = append(out[n.ID()], p)
				}
			}
		}

		return out
	}
}

// TestNodeStoreKeepsPeerMapAcrossUnrelatedWrites pins that a write which
// changes no input of the peer relationship carries the previous peer map
// forward, with the peers' views pointing at the new copies, and that a
// write which does change one recomputes it.
func TestNodeStoreKeepsPeerMapAcrossUnrelatedWrites(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	nodes := benchNodes(3)
	initial := make(types.Nodes, 0, len(nodes))

	for _, n := range nodes {
		n.ApprovedAt = new(time.Now())
		initial = append(initial, new(n))
	}

	store := NewNodeStore(initial, countingPeers(&calls), 1, time.Millisecond)
	store.Start()

	defer store.Stop()

	require.Equal(t, int64(1), calls.Load(), "the initial snapshot builds the peer map")
	require.Equal(t, 2, store.ListPeers(1).Len())

	seen := time.Now().Add(-time.Minute)

	_, ok := store.UpdateNode(2, func(n *types.Node) { n.LastSeen = &seen })
	require.True(t, ok)
	assert.Equal(t, int64(1), calls.Load(), "a LastSeen update keeps the peer map")

	peers := store.ListPeers(1)
	require.Equal(t, 2, peers.Len())

	for _, p := range peers.All() {
		if p.ID() == 2 {
			require.True(t, p.LastSeen().Valid(), "the peer view must point at the updated copy")
			assert.WithinDuration(t, seen, p.LastSeen().Get(), 0)
		}
	}

	_, ok = store.UpdateNode(2, func(n *types.Node) { n.Tags = []string{"tag:server"} })
	require.True(t, ok)
	assert.Equal(t, int64(2), calls.Load(), "a tag change recomputes the peer map")

	_, ok = store.UpdateNode(3, func(n *types.Node) { n.SuspendedAt = new(time.Now()) })
	require.True(t, ok)
	assert.Equal(t, int64(3), calls.Load(), "a suspension recomputes the peer map")

	extra := benchNodes(4)[4]
	store.PutNode(extra)
	assert.Equal(t, int64(4), calls.Load(), "a new node recomputes the peer map")
	assert.Equal(t, 3, store.ListPeers(1).Len())

	// DeleteNode does not wait for its batch.
	store.DeleteNode(4)
	require.Eventually(t, func() bool { return calls.Load() == 5 }, time.Second, time.Millisecond,
		"a deleted node recomputes the peer map")
	assert.Equal(t, 2, store.ListPeers(1).Len())

	store.RebuildPeerMaps()
	assert.Equal(t, int64(6), calls.Load(), "an explicit rebuild recomputes the peer map")
}

// BenchmarkUpdateNodePeerMap compares a write that keeps the peer map
// with one that recomputes it, over a tailnet where every node peers
// with every other, so the recompute is the O(n²) scan production pays
// through the policy.
func BenchmarkUpdateNodePeerMap(b *testing.B) {
	var calls atomic.Int64

	nodes := benchNodes(benchNodeCount)
	initial := make(types.Nodes, 0, len(nodes))

	for _, n := range nodes {
		n.ApprovedAt = new(time.Now())
		initial = append(initial, new(n))
	}

	store := NewNodeStore(initial, countingPeers(&calls), 1, time.Millisecond)
	store.Start()

	defer store.Stop()

	b.Run("last_seen_only", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			seen := time.Now()

			store.UpdateNode(1, func(n *types.Node) { n.LastSeen = &seen })
		}
	})

	b.Run("tags_changed", func(b *testing.B) {
		b.ReportAllocs()

		i := 0
		for b.Loop() {
			i++
			tag := "tag:" + strconv.Itoa(i)

			store.UpdateNode(1, func(n *types.Node) { n.Tags = []string{tag} })
		}
	})
}

// TestUpdateNodeRecomputesPeersOnlyForRelationInputs pins which fields make
// a write recompute the peer relationship (see peerInputsChanged): what the
// policy reads, admission, exit-node status, routes and the user identity.
// An announced but unapproved route is included on purpose. Everything
// else carries the previous peer map forward.
func TestUpdateNodeRecomputesPeersOnlyForRelationInputs(t *testing.T) {
	t.Parallel()

	subnet := netip.MustParsePrefix("10.77.0.0/24")

	tests := []struct {
		name          string
		mutate        func(*types.Node)
		wantRecompute bool
	}{
		{name: "last seen", mutate: func(n *types.Node) { n.LastSeen = new(time.Now()) }},
		{name: "node key", mutate: func(n *types.Node) { n.NodeKey = key.NewNode().Public() }},
		{name: "expiry", mutate: func(n *types.Node) { n.Expiry = new(time.Now()) }},
		{name: "online", mutate: func(n *types.Node) { n.IsOnline = new(true) }},
		{name: "unhealthy", mutate: func(n *types.Node) { n.Unhealthy = true }},
		{
			name: "endpoints",
			mutate: func(n *types.Node) {
				n.Endpoints = []netip.AddrPort{netip.MustParseAddrPort("203.0.113.1:41641")}
			},
		},
		{name: "tags", mutate: func(n *types.Node) { n.Tags = []string{"tag:x"} }, wantRecompute: true},
		{
			name: "ipv4",
			mutate: func(n *types.Node) {
				ip := netip.MustParseAddr("100.64.9.9")
				n.IPv4 = &ip
			},
			wantRecompute: true,
		},
		{
			name: "announced route",
			mutate: func(n *types.Node) {
				n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnet}}
			},
			wantRecompute: true,
		},
		{
			name:          "approved route",
			mutate:        func(n *types.Node) { n.ApprovedRoutes = []netip.Prefix{subnet} },
			wantRecompute: true,
		},
		{name: "approval", mutate: func(n *types.Node) { n.ApprovedAt = new(time.Now()) }, wantRecompute: true},
		{name: "user association", mutate: func(n *types.Node) { n.User = nil }, wantRecompute: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int64

			node1 := createTestNode(1, 1, "user1", "node1")
			node2 := createTestNode(2, 2, "user2", "node2")

			store := NewNodeStore(types.Nodes{&node1, &node2}, countingPeers(&calls), TestBatchSize, TestBatchTimeout)
			store.Start()

			defer store.Stop()

			calls.Store(0)

			_, ok := store.UpdateNode(1, tt.mutate)
			require.True(t, ok)

			var want int64
			if tt.wantRecompute {
				want = 1
			}

			assert.Equal(t, want, calls.Load())
		})
	}
}

// TestHealthOnlyWriteReusesPeerMap ensures a health flip re-elects routes
// without recomputing the peer relationship.
func TestHealthOnlyWriteReusesPeerMap(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	// Two HA candidates for the same prefix.
	node1 := createTestNode(1, 1, "user1", "router1")
	node2 := createTestNode(2, 1, "user1", "router2")

	pfx := netip.MustParsePrefix("10.99.0.0/24")
	node1.Hostinfo = &tailcfg.Hostinfo{Hostname: "router1", RoutableIPs: []netip.Prefix{pfx}}
	node2.Hostinfo = &tailcfg.Hostinfo{Hostname: "router2", RoutableIPs: []netip.Prefix{pfx}}
	node1.ApprovedRoutes = []netip.Prefix{pfx}
	node2.ApprovedRoutes = []netip.Prefix{pfx}
	node1.IsOnline = new(true)
	node2.IsOnline = new(true)

	store := NewNodeStore(types.Nodes{&node1, &node2}, countingPeers(&calls), TestBatchSize, TestBatchTimeout)
	store.Start()

	defer store.Stop()

	primary, ok := store.PrimaryRouteFor(pfx)
	require.True(t, ok)
	require.Equal(t, types.NodeID(1), primary)

	calls.Store(0)

	// Healthy -> healthy is a no-op: healthSetter(true) clears an
	// Unhealthy bit the node never had.
	_, ok = store.UpdateNode(1, healthSetter(true))
	require.True(t, ok)

	// Healthy -> unhealthy moves the primary, but Unhealthy is an
	// election input, not a relation input.
	_, ok = store.UpdateNode(1, healthSetter(false))
	require.True(t, ok)

	primary, ok = store.PrimaryRouteFor(pfx)
	require.True(t, ok)
	require.Equal(t, types.NodeID(2), primary)

	// Unhealthy -> unhealthy is a no-op again.
	_, ok = store.UpdateNode(1, healthSetter(false))
	require.True(t, ok)

	assert.Equal(t, int64(0), calls.Load(), "no health-only write may recompute the peer map")
}

// TestRebuildPeerMapsAfterStopReturns ensures a rebuild requested after the
// writer has exited does not block the caller forever.
func TestRebuildPeerMapsAfterStopReturns(t *testing.T) {
	t.Parallel()

	node := createTestNode(1, 1, "user1", "node1")
	store := NewNodeStore(types.Nodes{&node}, allowAllPeersFunc, TestBatchSize, TestBatchTimeout)
	store.Start()
	store.Stop()

	done := make(chan struct{})

	go func() {
		store.RebuildPeerMaps()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RebuildPeerMaps hung after Stop")
	}
}

// TestPeerInputsChangedClassifiesEveryNodeField makes every exported
// [types.Node] field carry a decision about whether a write to it must
// rebuild the peer map, checked against peerInputsChanged with a real
// before/after pair. A new field fails here until someone decides; left
// undecided, a relation input is exactly what goes silently unrebuilt.
func TestPeerInputsChangedClassifiesEveryNodeField(t *testing.T) {
	t.Parallel()

	ip := netip.MustParseAddr("100.64.9.9")
	subnet := netip.MustParsePrefix("10.44.0.0/24")

	fields := map[string]struct {
		// mutate is nil for a field no NodeStore write changes.
		mutate        func(*types.Node)
		wantRecompute bool
	}{
		"ID":         {},
		"MachineKey": {mutate: func(n *types.Node) { n.MachineKey = key.NewMachine().Public() }},
		"NodeKey":    {mutate: func(n *types.Node) { n.NodeKey = key.NewNode().Public() }},
		"DiscoKey":   {mutate: func(n *types.Node) { n.DiscoKey = key.NewDisco().Public() }},
		"Endpoints": {mutate: func(n *types.Node) {
			n.Endpoints = []netip.AddrPort{netip.MustParseAddrPort("192.0.2.1:1")}
		}},
		"Hostinfo":       {mutate: func(n *types.Node) { n.Hostinfo.OS = "linux" }, wantRecompute: true},
		"IPv4":           {mutate: func(n *types.Node) { n.IPv4 = &ip }, wantRecompute: true},
		"IPv6":           {mutate: func(n *types.Node) { n.IPv6 = nil }, wantRecompute: true},
		"Hostname":       {mutate: func(n *types.Node) { n.Hostname = "other" }},
		"GivenName":      {mutate: func(n *types.Node) { n.GivenName = "other" }},
		"UserID":         {mutate: func(n *types.Node) { n.UserID = new(uint(99)) }, wantRecompute: true},
		"User":           {mutate: func(n *types.Node) { n.User.Name = "renamed" }, wantRecompute: true},
		"RegisterMethod": {mutate: func(n *types.Node) { n.RegisterMethod = "oidc" }},
		"Tags":           {mutate: func(n *types.Node) { n.Tags = []string{"tag:x"} }, wantRecompute: true},
		"AuthKeyID":      {mutate: func(n *types.Node) { n.AuthKeyID = new(uint64(5)) }},
		"AuthKey":        {mutate: func(n *types.Node) { n.AuthKey = &types.PreAuthKey{ID: 5} }},
		"Expiry":         {mutate: func(n *types.Node) { n.Expiry = new(time.Now()) }},
		"LastSeen":       {mutate: func(n *types.Node) { n.LastSeen = new(time.Now()) }},
		"ApprovedRoutes": {
			mutate:        func(n *types.Node) { n.ApprovedRoutes = []netip.Prefix{subnet} },
			wantRecompute: true,
		},
		"ApprovedAt":  {mutate: func(n *types.Node) { n.ApprovedAt = nil }, wantRecompute: true},
		"SuspendedAt": {mutate: func(n *types.Node) { n.SuspendedAt = new(time.Now()) }, wantRecompute: true},
		"Posture": {mutate: func(n *types.Node) {
			n.Posture = &types.PostureIdentity{SerialNumbers: []string{"S1"}}
		}, wantRecompute: true},
		"HardwareAttestation": {mutate: func(n *types.Node) {
			n.HardwareAttestation = &types.HardwareAttestation{Attested: true}
		}, wantRecompute: true},
		"Attributes": {mutate: func(n *types.Node) {
			n.Attributes = []types.NodeAttribute{
				{Key: "custom:x", Value: types.AttributeValue{Kind: types.AttributeBool, Bool: true}},
			}
		}, wantRecompute: true},
		"Services": {mutate: func(n *types.Node) {
			n.Services = &types.NodeServices{Services: []tailcfg.VIPService{{Name: "svc:web"}}}
		}, wantRecompute: true},
		"ApprovedServices": {
			mutate:        func(n *types.Node) { n.ApprovedServices = []string{"svc:web"} },
			wantRecompute: true,
		},
		"KeySignature":     {mutate: func(n *types.Node) { n.KeySignature = []byte{1} }},
		"NLKey":            {mutate: func(n *types.Node) { n.NLKey = key.NewNLPrivate().Public() }},
		"SourceAddr":       {mutate: func(n *types.Node) { n.SourceAddr = netip.MustParseAddr("192.0.2.7") }},
		"SharedWith":       {mutate: func(n *types.Node) { n.SharedWith = []types.UserID{9} }, wantRecompute: true},
		"GlobalExitNode":   {mutate: func(n *types.Node) { n.GlobalExitNode = true }, wantRecompute: true},
		"ExitNodePriority": {mutate: func(n *types.Node) { n.ExitNodePriority = 10 }, wantRecompute: true},
		"Ephemeral":        {mutate: func(n *types.Node) { n.Ephemeral = true }},
		"CreatedAt":        {mutate: func(n *types.Node) { n.CreatedAt = n.CreatedAt.Add(time.Hour) }},
		"UpdatedAt":        {mutate: func(n *types.Node) { n.UpdatedAt = n.UpdatedAt.Add(time.Hour) }},
		"DeletedAt":        {mutate: func(n *types.Node) { n.DeletedAt = new(time.Now()) }},
		"IsOnline":         {mutate: func(n *types.Node) { n.IsOnline = new(true) }},
		"Unhealthy":        {mutate: func(n *types.Node) { n.Unhealthy = true }},
		"ActiveSessions":   {mutate: func(n *types.Node) { n.ActiveSessions = 2 }},
		"SessionEpoch":     {mutate: func(n *types.Node) { n.SessionEpoch = 3 }},
		"CapVer":           {mutate: func(n *types.Node) { n.CapVer = 1 }},
		"ClientWarnings":   {mutate: func(n *types.Node) { n.ClientWarnings = []string{"w"} }},
	}

	for f := range reflect.TypeFor[types.Node]().Fields() {
		if !f.IsExported() {
			continue
		}

		tt, ok := fields[f.Name]
		if !ok {
			t.Errorf("types.Node.%s has no peer input decision here", f.Name)

			continue
		}

		if tt.mutate == nil {
			continue
		}

		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()

			pre := createTestNode(1, 1, "u", "n")
			pre.User.ID = 1
			pre.Hostinfo = &tailcfg.Hostinfo{OS: "macOS"}
			pre.ApprovedAt = new(time.Now())

			post := *pre.Clone()
			tt.mutate(&post)

			assert.Equal(t, tt.wantRecompute, peerInputsChanged(&pre, &post))
		})
	}
}
