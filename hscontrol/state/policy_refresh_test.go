package state

import (
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/db"
	policyv2 "github.com/aislopware/slopscale/hscontrol/policy/v2"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
	"tailscale.com/tailcfg"
	"tailscale.com/types/views"
)

// fatalfer is the subset of *testing.T / *testing.B / *rapid.T that the
// NodeStore-against-policy helpers need, so property tests and
// benchmarks can share them.
type fatalfer interface {
	Fatalf(format string, args ...any)
}

// Policy shapes shared by TestNodeStoreAdjacencyMatchesFullBuild and
// BenchmarkNodeStoreWrite: a global ACL, autogroup:self, and a via grant.
const (
	policyGlobal = `{
		"groups": {"group:a": ["u1@"]},
		"tagOwners": {"tag:srv": ["u1@"], "tag:web": ["u1@"]},
		"acls": [
			{"action": "accept", "src": ["group:a"], "dst": ["tag:srv:*"]},
			{"action": "accept", "src": ["u2@"], "dst": ["10.33.0.0/24:*"]}
		]}`

	policyAutogroupSelf = `{
		"acls": [{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"]}]}`

	policyVia = `{
		"tagOwners": {"tag:router": ["u1@"]},
		"grants": [{"src": ["u2@"], "dst": ["10.33.0.0/24"], "ip": ["*"], "via": ["tag:router"]}]}`
)

// nodeStoreWithPolicy builds a [NodeStore] whose peers come from a real
// [policyv2.PolicyManager], wired the way [NewState] wires the two.
func nodeStoreWithPolicy(
	t fatalfer, pol string, users []types.User, nodes types.Nodes,
) (*NodeStore, *policyv2.PolicyManager) {
	pm, err := policyv2.NewPolicyManager([]byte(pol), users, nodes.ViewSlice())
	if err != nil {
		t.Fatalf("policy: %v", err)
	}

	store := NewNodeStorePositional(nodes, peerPositionsFunc(pm), TestBatchSize, TestBatchTimeout)
	store.Start()

	return store, pm
}

// syncPolicy does what [State.updatePolicyManagerNodes] does after a
// NodeStore write: feed the policy manager the current nodes, and rebuild
// the peer map only if it reports a policy-affecting change.
func syncPolicy(t fatalfer, store *NodeStore, pm *policyv2.PolicyManager) {
	changed, err := pm.SetNodes(store.ListNodes())
	if err != nil {
		t.Fatalf("SetNodes: %v", err)
	}

	if changed {
		store.RebuildPeerMaps()
	}
}

// checkAdjacencyMatchesFullBuild compares the NodeStore's current peer
// map, however it got there, including the reused-from-previous-snapshot
// path a write that changes no peer input takes, against a build from a
// fresh policy manager over the same nodes. The fresh manager keeps the
// oracle independent of the one the NodeStore writer updates; access is
// the database access model the live one compiles next to the policy.
// Divergence means the store served stale adjacency.
func checkAdjacencyMatchesFullBuild(
	t fatalfer, store *NodeStore, pol string, users []types.User, access *types.AccessModel,
) {
	snap := store.data.Load()

	fresh, err := policyv2.NewPolicyManager([]byte(pol), users, views.SliceOf(snap.allNodes))
	if err != nil {
		t.Fatalf("fresh policy: %v", err)
	}

	if access != nil {
		_, err = fresh.SetAccessModel(*access)
		if err != nil {
			t.Fatalf("fresh access model: %v", err)
		}
	}

	want := peerPositionsFunc(fresh)(snap.allNodes)
	got := snap.peersByNode()

	for i, n := range snap.allNodes {
		var exp []types.NodeID

		if want != nil {
			for _, p := range want[i] {
				exp = append(exp, snap.allNodes[p].ID())
			}
		}

		var have []types.NodeID
		for _, p := range got[n.ID()] {
			have = append(have, p.ID())
		}

		slices.Sort(exp)
		slices.Sort(have)

		if !slices.Equal(have, exp) {
			t.Fatalf("node %d: adjacency %v, full build %v", n.ID(), have, exp)
		}
	}
}

// TestNodeStoreAdjacencyMatchesFullBuild drives random node mutations
// through a real [NodeStore] wired to a real [policyv2.PolicyManager] and
// checks, after every step, that the resulting adjacency, including
// whatever the store served from its reused-peers path, matches a fresh
// build over the same nodes.
func TestNodeStoreAdjacencyMatchesFullBuild(t *testing.T) {
	users := []types.User{
		{ID: 1, Name: "u1"},
		{ID: 2, Name: "u2"},
	}
	subnet := netip.MustParsePrefix("10.33.0.0/24")

	for _, tc := range []struct {
		name string
		pol  string
	}{
		{name: "global", pol: policyGlobal},
		{name: "autogroup-self", pol: policyAutogroupSelf},
		{name: "via", pol: policyVia},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				nodes := make(types.Nodes, 0, 6)

				for i := 1; i <= 6; i++ {
					n := createTestNode(types.NodeID(i), uint(1+i%2), fmt.Sprintf("u%d", 1+i%2), fmt.Sprintf("n%d", i))
					ip4 := netip.AddrFrom4([4]byte{100, 64, 0, byte(i)})
					n.IPv4 = &ip4
					n.IPv6 = nil
					n.User = &users[i%2]
					n.ApprovedAt = new(time.Now())
					nodes = append(nodes, &n)
				}

				store, pm := nodeStoreWithPolicy(rt, tc.pol, users, nodes)
				defer store.Stop()

				steps := rapid.IntRange(1, 20).Draw(rt, "steps")
				for range steps {
					id := types.NodeID(rapid.IntRange(1, 6).Draw(rt, "id"))
					switch rapid.IntRange(0, 6).Draw(rt, "op") {
					case 0: // payload only
						store.UpdateNode(id, func(n *types.Node) { n.LastSeen = new(time.Now()) })
					case 1: // tag
						tag := rapid.SampledFrom([]string{"tag:srv", "tag:router"}).Draw(rt, "tag")

						store.UpdateNode(id, func(n *types.Node) {
							n.Tags = []string{tag}
							n.UserID, n.User = nil, nil
						})
					case 2: // announce + approve subnet
						store.UpdateNode(id, func(n *types.Node) {
							n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnet}}
							n.ApprovedRoutes = []netip.Prefix{subnet}
						})
					case 3: // drop routes
						store.UpdateNode(id, func(n *types.Node) { n.ApprovedRoutes = nil })
					case 4: // online flip
						store.UpdateNode(id, func(n *types.Node) { n.IsOnline = new(!n.Online()) })
					case 5: // endpoint
						store.UpdateNode(id, func(n *types.Node) {
							n.Endpoints = []netip.AddrPort{netip.MustParseAddrPort("192.0.2.1:41641")}
						})
					case 6: // suspension flip
						store.UpdateNode(id, func(n *types.Node) {
							if n.SuspendedAt == nil {
								n.SuspendedAt = new(time.Now())
							} else {
								n.SuspendedAt = nil
							}
						})
					}

					// Check before syncPolicy: the write's own snapshot must
					// already be right, both on the reuse path and on the
					// recompute path, where the peers func refreshed the
					// policy manager before building. Checking only after
					// syncPolicy would let either mistake hide behind a
					// RebuildPeerMaps.
					checkAdjacencyMatchesFullBuild(rt, store, tc.pol, users, nil)

					syncPolicy(rt, store, pm)
					checkAdjacencyMatchesFullBuild(rt, store, tc.pol, users, nil)
				}
			})
		})
	}
}

// peerBuildTestPolicy is the policy newPeerBuildTestState installs.
const peerBuildTestPolicy = `{
	"tagOwners": {"tag:a": ["pb-user@"], "tag:b": ["pb-user@"]},
	"acls": [
		{"action": "accept", "src": ["tag:a"], "dst": ["tag:b:*"]},
		{"action": "accept", "src": ["pb-user@"], "dst": ["10.55.0.0/24:*"]}
	]}`

// countStatePeerBuilds swaps s's NodeStore for one wired the same way
// but counting peer builds, so a test can see how many O(n^2) builds a
// State write costs. Call before anything else uses s.
func countStatePeerBuilds(t *testing.T, s *State) *atomic.Int64 {
	t.Helper()

	var calls atomic.Int64

	swapStatePeersFunc(t, s, func(inner PeerPositionsFunc) PeerPositionsFunc {
		return func(ns []types.NodeView) [][]int32 {
			calls.Add(1)

			return inner(ns)
		}
	})

	calls.Store(0)

	return &calls
}

// swapStatePeersFunc replaces s's NodeStore with one over the same nodes
// whose peers func is wrap applied to the one [NewState] installs.
func swapStatePeersFunc(t *testing.T, s *State, wrap func(PeerPositionsFunc) PeerPositionsFunc) {
	t.Helper()

	nodes := make(types.Nodes, 0, s.nodeStore.ListNodes().Len())
	for _, nv := range s.nodeStore.ListNodes().All() {
		nodes = append(nodes, nv.AsStruct())
	}

	s.nodeStore.Stop()

	s.nodeStore = NewNodeStorePositional(nodes, wrap(peerPositionsFunc(s.polMan)), TestBatchSize, TestBatchTimeout)
	s.nodeStore.Start()
}

// newPeerBuildTestState returns a State over three user-owned nodes, the
// first announcing a subnet, under a policy where both a tag and that
// subnet decide who sees whom.
func newPeerBuildTestState(t *testing.T) (*State, []types.NodeID, *atomic.Int64) {
	t.Helper()

	dbPath := t.TempDir() + "/slopscale.db"
	cfg := persistTestConfig(dbPath)

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	user := database.CreateUserForTest("pb-user")
	nodes := database.CreateRegisteredNodesForTest(user, 3, "pb-node")
	require.NoError(t, database.Close())

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.SetPolicy([]byte(peerBuildTestPolicy))
	require.NoError(t, err)

	ids := make([]types.NodeID, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}

	_, ok := s.nodeStore.UpdateNode(ids[0], func(n *types.Node) {
		n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{netip.MustParsePrefix("10.55.0.0/24")}}
	})
	require.True(t, ok)

	return s, ids, countStatePeerBuilds(t, s)
}

// checkStateAdjacencyMatchesFullBuild is checkAdjacencyMatchesFullBuild
// for a State from newPeerBuildTestState.
func checkStateAdjacencyMatchesFullBuild(t *testing.T, s *State) {
	t.Helper()

	users, err := s.ListAllUsers()
	require.NoError(t, err)

	checkAdjacencyMatchesFullBuild(t, s.nodeStore, peerBuildTestPolicy, users, s.access.Load())
}

// TestStatePolicyWriteBuildsPeersOnce pins that a policy-relevant State
// write costs one peer build: the NodeStore writer's own build must
// already use the matchers the written node implies, not the old ones
// followed by a second rebuild once the policy manager catches up.
func TestStatePolicyWriteBuildsPeersOnce(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, s *State, id types.NodeID) change.Change
	}{
		{name: "tag", write: func(t *testing.T, s *State, id types.NodeID) change.Change {
			t.Helper()

			_, c, err := s.SetNodeTags(id, []string{"tag:a"})
			require.NoError(t, err)

			return c
		}},
		{name: "route", write: func(t *testing.T, s *State, id types.NodeID) change.Change {
			t.Helper()

			_, c, err := s.SetApprovedRoutes(id, []netip.Prefix{netip.MustParsePrefix("10.55.0.0/24")})
			require.NoError(t, err)

			return c
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ids, builds := newPeerBuildTestState(t)

			c := tt.write(t, s, ids[0])

			assert.Equal(t, "policy", c.Type(), "a policy-relevant write must still report a policy change")
			assert.Equal(t, int64(1), builds.Load(), "peer builds for one policy-relevant write")

			checkStateAdjacencyMatchesFullBuild(t, s)
		})
	}
}

// TestStateConcurrentTagWritesEachReportPolicyChange runs two SetNodeTags
// on different nodes at once. The NodeStore may apply both in one batch,
// so the policy manager sees both tags in a single SetNodes; each caller
// must still report a policy change for its own write, and adjacency
// must end up matching a full build.
func TestStateConcurrentTagWritesEachReportPolicyChange(t *testing.T) {
	s, ids, _ := newPeerBuildTestState(t)

	tags := [2]string{"tag:a", "tag:b"}

	for round := range 20 {
		var (
			wg      sync.WaitGroup
			changes [2]change.Change
			errs    [2]error
		)

		for i := range 2 {
			wg.Go(func() {
				tag := tags[(i+round)%2]
				_, changes[i], errs[i] = s.SetNodeTags(ids[1+i], []string{tag})
			})
		}

		wg.Wait()

		for i := range 2 {
			require.NoError(t, errs[i])
			require.True(t, changes[i].RequiresRuntimePeerComputation,
				"round %d writer %d: %s must report a policy change", round, i, changes[i].Type())
			require.Equal(t, ids[1+i], changes[i].OriginNode, "round %d writer %d", round, i)
		}

		checkStateAdjacencyMatchesFullBuild(t, s)
	}
}

// TestPolicyWriteReportsAfterPublish holds the NodeStore writer between the
// policy manager's SetNodes and the snapshot swap, and lets another caller
// report in that window. The writer's own caller must still report a policy
// change: one reported in the window is computed against the old snapshot,
// so peers would keep the adjacency the write replaced.
func TestPolicyWriteReportsAfterPublish(t *testing.T) {
	s, ids, _ := newPeerBuildTestState(t)

	var (
		armed   atomic.Bool
		reached = make(chan struct{})
		release = make(chan struct{})
	)

	swapStatePeersFunc(t, s, func(inner PeerPositionsFunc) PeerPositionsFunc {
		return func(ns []types.NodeView) [][]int32 {
			if armed.CompareAndSwap(true, false) {
				_, err := s.polMan.SetNodes(views.SliceOf(ns))
				assert.NoError(t, err)
				close(reached)
				<-release
			}

			return inner(ns)
		}
	})

	other := s.polMan.NodesGeneration()

	armed.Store(true)

	var (
		wg   sync.WaitGroup
		tagC change.Change
		err  error
	)

	wg.Go(func() {
		_, tagC, err = s.SetNodeTags(ids[1], []string{"tag:a"})
	})

	<-reached

	published, ok := s.GetNodeByID(ids[1])
	require.True(t, ok)
	require.False(t, published.IsTagged(), "the tag must not be published yet")

	otherC := s.policyChangeSince(other)

	close(release)
	wg.Wait()

	require.NoError(t, err)
	assert.True(t, otherC.IncludePolicy, "a caller whose window saw the move may report it early")
	assert.True(t, tagC.IncludePolicy,
		"the writer must report the policy change once its snapshot is published")
	assert.Equal(t, ids[1], tagC.OriginNode)
}

// TestBackfillNodeIPsReportsPolicyChange pins that assigning a missing
// address reports a policy change and tells the readdressed node: the
// address is a policy input, and without the change clients only learned
// it from whichever unrelated write next refreshed the policy.
func TestBackfillNodeIPsReportsPolicyChange(t *testing.T) {
	dbPath := t.TempDir() + "/slopscale.db"
	cfg := persistTestConfig(dbPath)

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	user := database.CreateUserForTest("bf-user")
	nodes := database.CreateRegisteredNodesForTest(user, 2, "bf-node")

	_, err = database.DB.Exec(`UPDATE nodes SET ipv4 = NULL WHERE id = ?`, nodes[0].ID)
	require.NoError(t, err)
	// Backfill copies the stored Hostinfo, which a registered client always has.
	_, err = database.DB.Exec(`UPDATE nodes SET host_info = '{}'`)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	backfilled, cs, err := s.BackfillNodeIPs()
	require.NoError(t, err)
	require.NotEmpty(t, backfilled)
	assert.True(t, slices.ContainsFunc(cs, change.Change.IsBroadcastPolicyChange),
		"backfill must report a policy change: %v", cs)
	assert.True(t, slices.ContainsFunc(cs, func(c change.Change) bool { return c.OriginNode == nodes[0].ID }),
		"backfill must resend the readdressed node: %v", cs)
}

// TestPolicyCachesSurviveOldViewDuringBuild covers the window between the
// writer's SetNodes and the snapshot swap: a mapper still holding the
// written node's old view can ask for its filter or SSH policy then. The
// answer for that old view must not be cached under the node's ID, or
// the node keeps it after the swap, since nothing invalidates it again.
func TestPolicyCachesSurviveOldViewDuringBuild(t *testing.T) {
	users := []types.User{{ID: 1, Name: "u1"}, {ID: 2, Name: "u2"}}
	subnet := netip.MustParsePrefix("10.33.0.0/24")

	pol := `{
		"tagOwners": {"tag:srv": ["u1@"]},
		"acls": [{"action": "accept", "src": ["u2@"], "dst": ["10.33.0.0/24:*"]}],
		"ssh": [{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self"], "users": ["root"]}]
	}`

	tests := []struct {
		name   string
		mutate func(n *types.Node)
		// probe reports a property of node 1's cached artefact that the
		// write flips from !want to want. It takes no *testing.T because it
		// also runs on the NodeStore writer, where FailNow would hang.
		probe func(pm *policyv2.PolicyManager, view types.NodeView) (bool, error)
		want  bool
	}{
		{
			name: "filter after route approval",
			mutate: func(n *types.Node) {
				n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnet}}
				n.ApprovedRoutes = []netip.Prefix{subnet}
			},
			probe: func(pm *policyv2.PolicyManager, view types.NodeView) (bool, error) {
				rules, err := pm.FilterForNode(view)
				if err != nil {
					return false, err
				}

				for _, r := range rules {
					for _, d := range r.DstPorts {
						if d.IP == subnet.String() {
							return true, nil
						}
					}
				}

				return false, nil
			},
			want: true,
		},
		{
			// A tagged node is outside autogroup:self, so tagging it must
			// drop its SSH rules.
			name: "ssh after tagging",
			mutate: func(n *types.Node) {
				n.Tags = []string{"tag:srv"}
				n.UserID, n.User = nil, nil
			},
			probe: func(pm *policyv2.PolicyManager, view types.NodeView) (bool, error) {
				sshPol, err := pm.SSHPolicy("", view)
				if err != nil {
					return false, err
				}

				return sshPol != nil && len(sshPol.Rules) > 0, nil
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Node 3 shares node 1's user so autogroup:self has a source
			// for node 1 once node 1 itself is tagged away.
			owners := []int{0, 1, 0}
			nodes := make(types.Nodes, 0, len(owners))

			for i, o := range owners {
				id := i + 1
				n := createTestNode(types.NodeID(id), users[o].ID, users[o].Name, fmt.Sprintf("n%d", id))
				ip4 := netip.AddrFrom4([4]byte{100, 64, 0, byte(id)})
				n.IPv4, n.IPv6 = &ip4, nil
				n.User = &users[o]
				n.ApprovedAt = new(time.Now())
				nodes = append(nodes, &n)
			}

			pm, err := policyv2.NewPolicyManager([]byte(pol), users, nodes.ViewSlice())
			require.NoError(t, err)

			var (
				oldView  atomic.Pointer[types.NodeView]
				buildErr atomic.Pointer[error]
			)

			inner := peerPositionsFunc(pm)
			store := NewNodeStorePositional(nodes, func(ns []types.NodeView) [][]int32 {
				// Stand in for a mapper that read the snapshot just before
				// this write and asks between SetNodes and the swap.
				if v := oldView.Load(); v != nil {
					_, probeErr := pm.SetNodes(views.SliceOf(ns))
					if probeErr == nil {
						_, probeErr = tt.probe(pm, *v)
					}

					if probeErr != nil {
						buildErr.CompareAndSwap(nil, &probeErr)
					}
				}

				return inner(ns)
			}, TestBatchSize, TestBatchTimeout)
			store.Start()

			defer store.Stop()

			before, ok := store.GetNode(1)
			require.True(t, ok)

			got, err := tt.probe(pm, before)
			require.NoError(t, err)
			require.NotEqual(t, tt.want, got, "precondition: the write must flip the probed artefact")

			oldView.Store(&before)

			after, ok := store.UpdateNode(1, tt.mutate)
			require.True(t, ok)
			oldView.Store(nil)

			if e := buildErr.Load(); e != nil {
				require.NoError(t, *e, "probe during peer map build")
			}

			got, err = tt.probe(pm, after)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got, "cached artefact must reflect the written node")
		})
	}
}

// writeBenchNodes builds n approved nodes across 10 users: ~10% tagged
// tag:srv, each with a unique IPv4.
func writeBenchNodes(n int) ([]types.User, types.Nodes) {
	users := make([]types.User, 10)
	for i := range users {
		users[i] = types.User{ID: uint(i + 1), Name: fmt.Sprintf("u%d", i+1)}
	}

	nodes := make(types.Nodes, 0, n)

	for i := range n {
		u := users[i%len(users)]
		ip := netip.AddrFrom4([4]byte{100, 64, byte(i / 256), byte(i % 256)})

		nd := &types.Node{
			ID:         types.NodeID(i + 1),
			Hostname:   fmt.Sprintf("n%d", i+1),
			GivenName:  fmt.Sprintf("n%d", i+1),
			IPv4:       &ip,
			ApprovedAt: new(time.Now()),
		}

		if i%10 == 0 {
			nd.Tags = []string{"tag:srv"}
		} else {
			nd.UserID, nd.User = &u.ID, &u
		}

		nodes = append(nodes, nd)
	}

	return users, nodes
}

// BenchmarkNodeStoreWrite drives one UpdateNode of the named kind followed
// by syncPolicy against a started NodeStore wired to a real policy
// manager, over a realistic node count. peer-builds/op counts peer map
// builds: a write that changes no peer input (lastseen) should cost none,
// one that does (route, tag) one.
func BenchmarkNodeStoreWrite(b *testing.B) {
	users, nodes := writeBenchNodes(617)

	for _, kind := range []struct {
		name   string
		mutate func(i int, n *types.Node)
	}{
		{name: "lastseen", mutate: func(_ int, n *types.Node) {
			n.LastSeen = new(time.Now())
		}},
		{name: "route", mutate: func(i int, n *types.Node) {
			subnet := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(200 + i%50), 0, 0}), 24)
			n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnet}}
			n.ApprovedRoutes = []netip.Prefix{subnet}
		}},
		{name: "tag", mutate: func(i int, n *types.Node) {
			tag := "tag:srv"
			if i%2 == 0 {
				tag = "tag:web"
			}

			n.Tags = []string{tag}
			n.UserID, n.User = nil, nil
		}},
	} {
		b.Run(fmt.Sprintf("%s/n=%d", kind.name, len(nodes)), func(b *testing.B) {
			var calls atomic.Int64

			pm, err := policyv2.NewPolicyManager([]byte(policyGlobal), users, nodes.ViewSlice())
			require.NoError(b, err)

			inner := peerPositionsFunc(pm)
			store := NewNodeStorePositional(nodes, func(ns []types.NodeView) [][]int32 {
				calls.Add(1)

				return inner(ns)
			}, TestBatchSize, TestBatchTimeout)

			store.Start()
			defer store.Stop()

			targetID := nodes[0].ID

			// The store's own initial build is setup, not a per-write cost.
			calls.Store(0)

			b.ReportAllocs()

			i := 0
			for b.Loop() {
				i++

				store.UpdateNode(targetID, func(n *types.Node) { kind.mutate(i, n) })
				syncPolicy(b, store, pm)
			}

			b.ReportMetric(float64(calls.Load())/float64(b.N), "peer-builds/op")
		})
	}
}
