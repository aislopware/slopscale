package mapper

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

const benchNodesPerUser = 10

// benchVisibilityPolicies are the policy shapes the peer lists are
// decided under: one matcher for everyone, a global filter with a rule
// per user (every pair check walks all of them), and a per-node filter.
func benchVisibilityPolicies(users int) []struct{ name, policy string } {
	var grants strings.Builder

	grants.WriteString(`{"groups":{"group:admins":["u-0@"]},"acls":[`)
	grants.WriteString(`{"action":"accept","src":["group:admins"],"dst":["*:*"]}`)

	for i := range users {
		fmt.Fprintf(&grants,
			`,{"action":"accept","src":["u-%d@"],"dst":["u-%d@:*","u-%d@:22,443"]}`,
			i, i, (i+1)%users)
	}

	grants.WriteString(`]}`)

	return []struct{ name, policy string }{
		{"allow_all", `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`},
		{"many_grants", grants.String()},
		{
			"autogroup_self",
			`{"acls":[{"action":"accept","src":["autogroup:member"],"dst":["autogroup:self:*"]}]}`,
		},
	}
}

// newVisibilityBenchState builds a tailnet of n admitted nodes, ten per
// user, and returns a mapper over it.
func newVisibilityBenchState(b *testing.B, n int) (*mapper, []types.NodeID) {
	b.Helper()

	tmp := b.TempDir()
	p4 := netip.MustParsePrefix("100.64.0.0/10")
	p6 := netip.MustParsePrefix("fd7a:115c:a1e0::/48")
	cfg := &types.Config{
		Database: types.DatabaseConfig{
			Type:   types.DatabaseSqlite,
			Sqlite: types.SqliteConfig{Path: tmp + "/h.db"},
		},
		PrefixV4:     &p4,
		PrefixV6:     &p6,
		IPAllocation: types.IPAllocationStrategySequential,
		BaseDomain:   "slopscale.test",
		Policy:       types.PolicyConfig{Mode: types.PolicyModeDB},
		DERP: types.DERPConfig{
			DERPMap: &tailcfg.DERPMap{
				Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{999: {RegionID: 999}},
			},
		},
		Tuning: types.Tuning{
			NodeStoreBatchSize:    state.TestBatchSize,
			NodeStoreBatchTimeout: state.TestBatchTimeout,
		},
	}

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(b, err)

	for _, user := range database.CreateUsersForTest(n/benchNodesPerUser, "u") {
		database.CreateRegisteredNodesForTest(user, benchNodesPerUser, "n")
	}

	require.NoError(b, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(b, err)
	b.Cleanup(func() { _ = s.Close() })

	ids := make([]types.NodeID, 0, n)
	for _, nv := range s.ListNodes().All() {
		ids = append(ids, nv.ID())
	}

	return &mapper{state: s, cfg: cfg}, ids
}

// BenchmarkPeerVisibility measures the map paths that decide which peers
// a node sees: a patch and a changed peer fanned out to every node of
// the tailnet (one op is the whole fan-out), and one node's full map.
// It is the evidence for where peer visibility is decided: the NodeStore
// peer map alone, or that map narrowed again by the policy per response.
func BenchmarkPeerVisibility(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping peer visibility benchmark in short mode")
	}

	capVer := tailcfg.CurrentCapabilityVersion

	for _, n := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("nodes=%d", n), func(b *testing.B) {
			m, ids := newVisibilityBenchState(b, n)
			target := ids[len(ids)/2]

			for _, pol := range benchVisibilityPolicies(n / benchNodesPerUser) {
				_, err := m.state.SetPolicyInDB(pol.policy)
				require.NoError(b, err)

				_, err = m.state.ReloadPolicy()
				require.NoError(b, err)

				fanOut := func(b *testing.B, c change.Change) {
					b.Helper()
					b.ReportAllocs()

					for b.Loop() {
						for _, id := range ids {
							_, err := m.buildFromChange(id, capVer, &c)
							if err != nil {
								b.Fatal(err)
							}
						}
					}
				}

				b.Run("policy="+pol.name+"/op=patch", func(b *testing.B) {
					fanOut(b, change.NodeOnline(target, time.Now()))
				})

				b.Run("policy="+pol.name+"/op=peer_changed", func(b *testing.B) {
					fanOut(b, change.PeersChanged("bench", target))
				})

				b.Run("policy="+pol.name+"/op=full_map", func(b *testing.B) {
					b.ReportAllocs()

					i := 0
					for b.Loop() {
						_, err := m.fullMapResponse(ids[i%len(ids)], capVer)
						if err != nil {
							b.Fatal(err)
						}

						i++
					}
				})
			}
		})
	}
}
