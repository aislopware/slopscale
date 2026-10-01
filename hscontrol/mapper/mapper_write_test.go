package mapper

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

var errInjectedExpiry = errors.New("injected expiry failure")

// writeTestConfig is a sqlite-backed State config in a test's temp dir.
func writeTestConfig(t *testing.T) *types.Config {
	t.Helper()

	p4 := netip.MustParsePrefix("100.64.0.0/10")
	p6 := netip.MustParsePrefix("fd7a:115c:a1e0::/48")

	return &types.Config{
		Database: types.DatabaseConfig{
			Type:   types.DatabaseSqlite,
			Sqlite: types.SqliteConfig{Path: t.TempDir() + "/h.db"},
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
}

// TestBackfillNodeIPsReachesBackfilledNode proves the node that receives a
// backfilled address learns it too. Peers pick it up from the policy
// refresh, but that refresh carries no self node, so the node itself kept
// serving its old addresses until an unrelated self update.
func TestBackfillNodeIPsReachesBackfilledNode(t *testing.T) {
	cfg := writeTestConfig(t)

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	user := database.CreateUserForTest("u1")
	nodes := database.CreateRegisteredNodesForTest(user, 2, "bf")
	target := nodes[0].ID

	_, err = database.DB.Exec(`UPDATE nodes SET ipv6 = NULL WHERE id = ?`, target)
	require.NoError(t, err)
	// Backfill copies the stored Hostinfo, which a registered client always has.
	_, err = database.DB.Exec(`UPDATE nodes SET host_info = '{}'`)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	backfilled, cs, err := s.BackfillNodeIPs()
	require.NoError(t, err)
	require.NotEmpty(t, backfilled)

	stored, ok := s.GetNodeByID(target)
	require.True(t, ok)
	require.True(t, stored.IPv6().Valid(), "backfill must assign an IPv6 address")

	m := &mapper{state: s, cfg: cfg}
	nc := newMockNodeConnection(target)

	var self *tailcfg.Node

	for _, ch := range change.FilterForNode(target, cs) {
		resps, err := generateMapResponse(nc, m, ch)
		require.NoError(t, err)

		for _, resp := range resps {
			if resp.Node != nil {
				self = resp.Node
			}
		}
	}

	require.NotNil(t, self, "the backfilled node must receive its own node")
	assert.Contains(t, self.Addresses, netip.PrefixFrom(stored.IPv6().Get(), 128))
}

// TestFailedExpiryOfPrimaryAnnouncesBackup expires the primary of an HA
// route while the database write fails. The NodeStore already moved the
// route to the backup, so the change returned with the error must still
// tell a client about the new primary, as the successful path does;
// otherwise the client drops the old primary's route with no replacement.
func TestFailedExpiryOfPrimaryAnnouncesBackup(t *testing.T) {
	cfg := writeTestConfig(t)
	route := netip.MustParsePrefix("10.77.0.0/24")

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	user := database.CreateUserForTest("u1")
	nodes := database.CreateRegisteredNodesForTest(user, 3, "ha")

	for _, n := range nodes[:2] {
		n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{route}}
		n.ApprovedRoutes = []netip.Prefix{route}
		require.NoError(t, db.SaveNode(database, n))
	}

	require.NoError(t, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	primary, backup, client := nodes[0].ID, nodes[1].ID, nodes[2].ID
	for _, id := range []types.NodeID{primary, backup, client} {
		s.Connect(id)
	}

	require.Equal(t, []netip.Prefix{route}, s.GetNodePrimaryRoutes(primary))

	s.DB().SetQueryHook(func(query string) error {
		if strings.HasPrefix(query, "UPDATE nodes") {
			return errInjectedExpiry
		}

		return nil
	})
	t.Cleanup(func() { s.DB().SetQueryHook(nil) })

	past := time.Now().Add(-time.Hour)
	_, c, err := s.SetNodeExpiry(primary, &past)
	require.ErrorIs(t, err, errInjectedExpiry)
	require.Equal(t, []netip.Prefix{route}, s.GetNodePrimaryRoutes(backup),
		"the NodeStore moved the route to the backup")

	m := &mapper{state: s, cfg: cfg}
	nc := newMockNodeConnection(client)

	var backupRoutes []netip.Prefix

	for _, ch := range change.FilterForNode(client, []change.Change{c}) {
		resps, err := generateMapResponse(nc, m, ch)
		require.NoError(t, err)

		for _, resp := range resps {
			for _, p := range append(resp.Peers, resp.PeersChanged...) {
				if p.ID == backup.NodeID() {
					backupRoutes = p.PrimaryRoutes
				}
			}
		}
	}

	assert.Contains(t, backupRoutes, route, "the client must learn the backup is primary: %s", c.Type())
}
