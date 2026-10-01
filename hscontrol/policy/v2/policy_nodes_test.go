package v2

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
)

// benchSetNodesPolicies are the shapes BenchmarkSetNodes measures: a
// global ACL with a route-owning rule, and autogroup:self, whose filter is
// per node.
var benchSetNodesPolicies = []struct {
	name   string
	policy string
}{
	{name: "global", policy: `{
		"groups": {"group:a": ["u1@"]},
		"tagOwners": {"tag:srv": ["u1@"]},
		"acls": [
			{"action": "accept", "src": ["group:a"], "dst": ["tag:srv:*"]},
			{"action": "accept", "src": ["u2@"], "dst": ["10.0.0.0/8:*"]}
		]}`},
	{name: "self", policy: `{
		"acls": [{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"]}]}`},
}

// benchSetNodesNodes builds n nodes spread across 10 users, ~10% of them
// tagged tag:srv, each with a unique IPv4.
func benchSetNodesNodes(n int) (types.Users, types.Nodes) {
	users := make(types.Users, 10)
	for i := range users {
		users[i] = types.User{ID: uint(i + 1), Name: fmt.Sprintf("u%d", i+1)}
	}

	nodes := make(types.Nodes, 0, n)

	for i := range n {
		u := users[i%len(users)]
		ip := netip.AddrFrom4([4]byte{100, 64, byte(i / 256), byte(i % 256)})

		nd := &types.Node{
			ID:       types.NodeID(i + 1),
			Hostname: fmt.Sprintf("n%d", i+1),
			IPv4:     &ip,
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

// BenchmarkSetNodes measures PolicyManager.SetNodes when one node's route
// changes on every call: the recompile the NodeStore writer runs before a
// peer map build whenever a write moved a policy input.
func BenchmarkSetNodes(b *testing.B) {
	for _, pol := range benchSetNodesPolicies {
		b.Run(fmt.Sprintf("%s/n=%d", pol.name, 617), func(b *testing.B) {
			users, nodes := benchSetNodesNodes(617)
			pm, err := NewPolicyManager([]byte(pol.policy), users, nodes.ViewSlice())
			require.NoError(b, err)

			b.ReportAllocs()

			i := 0
			for b.Loop() {
				i++
				subnet := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(200 + i%50), 0, 0}), 24)
				// The policy manager holds views of the previous node; mutating
				// it in place would change both sides of SetNodes' comparison.
				nd := nodes[0].Clone()
				nd.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{subnet}}
				nd.ApprovedRoutes = []netip.Prefix{subnet}
				nodes[0] = nd

				changed, err := pm.SetNodes(nodes.ViewSlice())
				if err != nil {
					b.Fatal(err)
				}

				if !changed {
					b.Fatal("SetNodes must see the route change and recompile")
				}
			}
		})
	}
}

// TestNodesGenerationCountsChangingSetNodes pins that NodesGeneration
// moves exactly when SetNodes reports a change, so a caller that did not
// run the SetNodes itself can still tell its write moved the policy.
func TestNodesGenerationCountsChangingSetNodes(t *testing.T) {
	users := types.Users{{ID: 1, Name: "user1"}}

	nodes := types.Nodes{node("n1", "100.64.0.1", "fd7a:115c:a1e0::1", users[0])}
	nodes[0].ID = 1

	pm, err := NewPolicyManager([]byte(`{
		"acls": [{"action": "accept", "src": ["user1@"], "dst": ["user1@:*"]}]
	}`), users, nodes.ViewSlice())
	require.NoError(t, err)

	gen := pm.NodesGeneration()

	changed, err := pm.SetNodes(nodes.ViewSlice())
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, gen, pm.NodesGeneration(), "unchanged nodes must not advance the generation")

	added := node("n2", "100.64.0.2", "fd7a:115c:a1e0::2", users[0])
	added.ID = 2

	changed, err = pm.SetNodes(append(nodes, added).ViewSlice())
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, gen+1, pm.NodesGeneration(), "a changing SetNodes must advance the generation once")
}

// TestSetNodesRetriesAfterFailedRecompile pins that a SetNodes whose
// recompile fails keeps the previous node list, like SetUsers. Keeping the
// new list would make an identical retry look unchanged, so the recompile
// would never be retried and the caller would never learn the policy moved.
func TestSetNodesRetriesAfterFailedRecompile(t *testing.T) {
	users := types.Users{{ID: 1, Name: "user1"}}

	nodes := make(types.Nodes, 0, 2)
	nodes = append(nodes, node("n1", "100.64.0.1", "fd7a:115c:a1e0::1", users[0]))
	nodes[0].ID = 1

	pm, err := NewPolicyManager([]byte(`{
		"tagOwners": {"tag:a": ["user1@"]},
		"acls": [{"action": "accept", "src": ["user1@"], "dst": ["user1@:*"]}]
	}`), users, nodes.ViewSlice())
	require.NoError(t, err)

	added := node("n2", "100.64.0.2", "fd7a:115c:a1e0::2", users[0])
	added.ID = 2
	grown := append(slices.Clone(nodes), added)

	// Break tag owner resolution so the recompile fails.
	good := pm.pol.TagOwners
	missing := Tag("tag:missing")
	pm.pol.TagOwners = TagOwners{"tag:a": Owners{&missing}}

	_, err = pm.SetNodes(grown.ViewSlice())
	require.Error(t, err)

	pm.pol.TagOwners = good
	gen := pm.NodesGeneration()

	changed, err := pm.SetNodes(grown.ViewSlice())
	require.NoError(t, err)
	require.True(t, changed, "the retry must recompile, not see the failed input as current")
	require.Equal(t, gen+1, pm.NodesGeneration())
}

// TestSetNodesCachedResultsMatchFresh drives random node writes through
// one PolicyManager, reading every node between writes so its caches
// fill, and checks each read against a PolicyManager built fresh from the
// same nodes. A cache entry that a write should have dropped shows up as
// a difference.
func TestSetNodesCachedResultsMatchFresh(t *testing.T) {
	users := types.Users{{ID: 1, Name: "u1"}, {ID: 2, Name: "u2"}, {ID: 3, Name: "u3"}}

	policies := []struct {
		name   string
		policy string
	}{
		{name: "global", policy: `{
			"groups": {"group:a": ["u1@", "u2@"]},
			"tagOwners": {"tag:srv": ["u1@"], "tag:router": ["u1@"], "tag:other": ["u1@"]},
			"acls": [
				{"action": "accept", "src": ["group:a"], "dst": ["tag:srv:*"]},
				{"action": "accept", "src": ["u3@"], "dst": ["10.33.0.0/16:*", "u1@:22"]}
			],
			"ssh": [{"action": "accept", "src": ["group:a"], "dst": ["tag:srv"], "users": ["root"]}]}`},
		{name: "autogroup-self", policy: `{
			"groups": {"group:a": ["u1@"]},
			"tagOwners": {"tag:srv": ["u1@"], "tag:router": ["u1@"], "tag:other": ["u1@"]},
			"acls": [
				{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"]},
				{"action": "accept", "src": ["group:a"], "dst": ["tag:srv:*"]}
			],
			"ssh": [
				{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self"],
				 "users": ["autogroup:nonroot"]},
				{"action": "accept", "src": ["group:a"], "dst": ["tag:srv"], "users": ["root"]}
			]}`},
		{name: "via", policy: `{
			"tagOwners": {"tag:srv": ["u1@"], "tag:router": ["u1@"], "tag:other": ["u1@"]},
			"grants": [
				{"src": ["u2@"], "dst": ["10.33.0.0/16"], "ip": ["*"], "via": ["tag:router"]},
				{"src": ["u3@", "tag:srv"], "dst": ["autogroup:internet"], "ip": ["*"], "via": ["tag:router"]},
				{"src": ["u1@"], "dst": ["tag:srv"], "ip": ["*"]}
			]}`},
		{name: "autogroup-self-and-via", policy: `{
			"groups": {"group:a": ["u1@", "u2@"]},
			"tagOwners": {"tag:srv": ["u1@"], "tag:router": ["u1@"], "tag:other": ["u1@"]},
			"grants": [
				{"src": ["group:a"], "dst": ["autogroup:self"], "ip": ["*"]},
				{"src": ["u2@", "tag:srv"], "dst": ["10.33.0.0/16"], "ip": ["*"], "via": ["tag:router"]},
				{"src": ["u3@"], "dst": ["tag:srv"], "ip": ["*"]}
			]}`},
	}

	subnets := []netip.Prefix{
		netip.MustParsePrefix("10.33.0.0/24"),
		netip.MustParsePrefix("10.33.1.0/24"),
	}
	exits := []netip.Prefix{tsaddr.AllIPv4(), tsaddr.AllIPv6()}

	for _, pc := range policies {
		t.Run(pc.name, func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				nodes := make(types.Nodes, 0, 8)
				nextIP := 1

				newNode := func(id types.NodeID, u types.User) *types.Node {
					n := node(
						fmt.Sprintf("n%d", id),
						fmt.Sprintf("100.64.0.%d", nextIP),
						fmt.Sprintf("fd7a:115c:a1e0::%d", nextIP),
						u,
					)
					n.ID = id
					nextIP++

					return n
				}

				for i := range 6 {
					nodes = append(
						nodes,
						newNode(types.NodeID(i+1), users[i%len(users)]),
					)
				}

				nextID := types.NodeID(len(nodes) + 1)

				pm, err := NewPolicyManager([]byte(pc.policy), users, nodes.ViewSlice())
				if err != nil {
					rt.Fatalf("new policy manager: %v", err)
				}

				check := func() {
					fresh, err := NewPolicyManager([]byte(pc.policy), users, nodes.ViewSlice())
					if err != nil {
						rt.Fatalf("fresh policy manager: %v", err)
					}

					for _, n := range nodes {
						nv := n.View()

						got, _ := pm.FilterForNode(nv)

						want, _ := fresh.FilterForNode(nv)
						if diff := cmp.Diff(want, got); diff != "" {
							rt.Fatalf("node %d FilterForNode (-fresh +cached):\n%s", n.ID, diff)
						}

						gotM, _ := pm.MatchersForNode(nv)

						wantM, _ := fresh.MatchersForNode(nv)
						if diff := cmp.Diff(matcherStrings(wantM), matcherStrings(gotM)); diff != "" {
							rt.Fatalf("node %d MatchersForNode (-fresh +cached):\n%s", n.ID, diff)
						}

						gotS, err := pm.SSHPolicy("", nv)
						if err != nil {
							rt.Fatalf("node %d SSHPolicy: %v", n.ID, err)
						}

						wantS, _ := fresh.SSHPolicy("", nv)
						if diff := cmp.Diff(wantS, gotS); diff != "" {
							rt.Fatalf("node %d SSHPolicy (-fresh +cached):\n%s", n.ID, diff)
						}
					}
				}

				check()

				steps := rapid.IntRange(1, 25).Draw(rt, "steps")
				for range steps {
					// Several writes per SetNodes, as a NodeStore batch applies them.
					writes := rapid.IntRange(1, 3).Draw(rt, "writes")
					for range writes {
						i := rapid.IntRange(0, len(nodes)-1).Draw(rt, "node")
						// Mutate a copy: pm still holds views of the old node.
						n := nodes[i].View().AsStruct()

						switch rapid.IntRange(0, 9).Draw(rt, "op") {
						case 0: // tag
							n.Tags = []string{
								rapid.SampledFrom([]string{"tag:srv", "tag:router", "tag:other"}).Draw(rt, "tag"),
							}
							n.UserID, n.User = nil, nil
						case 1: // (re)assign user, untagging
							u := rapid.SampledFrom(users).Draw(rt, "user")
							n.Tags = nil
							n.UserID, n.User = new(u.ID), new(u)
						case 2: // new IPs
							ip4 := netip.MustParseAddr(fmt.Sprintf("100.64.1.%d", nextIP))
							ip6 := netip.MustParseAddr(fmt.Sprintf("fd7a:115c:a1e0::1:%d", nextIP))
							n.IPv4, n.IPv6 = &ip4, &ip6
							nextIP++
						case 3: // announce and approve a subnet
							p := rapid.SampledFrom(subnets).Draw(rt, "subnet")
							n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{p}}
							n.ApprovedRoutes = []netip.Prefix{p}
						case 4: // drop approvals
							n.ApprovedRoutes = nil
						case 5: // exit node
							n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: exits}
							n.ApprovedRoutes = exits
						case 6: // add a node
							nodes = append(nodes, newNode(nextID, rapid.SampledFrom(users).Draw(rt, "user")))
							nextID++

							n = nil
						case 7: // remove the node
							if len(nodes) > 1 {
								nodes = slices.Delete(slices.Clone(nodes), i, i+1)
							}

							n = nil
						case 8: // owner association not loaded
							if !n.IsTagged() {
								n.User = nil
							}
						case 9: // payload only
							n.Hostname += "x"
						}

						if n != nil {
							nodes = slices.Clone(nodes)
							nodes[i] = n
						}
					}

					_, err := pm.SetNodes(nodes.ViewSlice())
					if err != nil {
						rt.Fatalf("SetNodes: %v", err)
					}

					check()
				}
			})
		})
	}
}
