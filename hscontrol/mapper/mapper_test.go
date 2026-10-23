package mapper

import (
	stdcmp "cmp"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/db"
	policyv2 "github.com/aislopware/slopscale/hscontrol/policy/v2"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/dnstype"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
)

var iap = func(ipStr string) *netip.Addr {
	ip := netip.MustParseAddr(ipStr)
	return &ip
}

func TestDNSConfigMapResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		magicDNS bool
		want     *tailcfg.DNSConfig
	}{
		{
			magicDNS: true,
			want: &tailcfg.DNSConfig{
				Routes: map[string][]*dnstype.Resolver{},
				Domains: []string{
					"foobar.slopscale.net",
				},
				Proxied: true,
			},
		},
		{
			magicDNS: false,
			want: &tailcfg.DNSConfig{
				Domains: []string{"foobar.slopscale.net"},
				Proxied: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("with-magicdns-%v", tt.magicDNS), func(t *testing.T) {
			t.Parallel()

			mach := func(hostname, username string, userid uint) *types.Node {
				return &types.Node{
					Hostname: hostname,
					UserID:   new(userid),
					User: &types.User{
						Name: username,
					},
				}
			}

			baseDomain := "foobar.slopscale.net"

			dnsConfigOrig := tailcfg.DNSConfig{
				Routes:  make(map[string][]*dnstype.Resolver),
				Domains: []string{baseDomain},
				Proxied: tt.magicDNS,
			}

			nodeInShared1 := mach("test_get_shared_nodes_1", "shared1", 1)

			got := generateDNSConfig(
				&types.Config{
					TailcfgDNSConfig: &dnsConfigOrig,
				},
				nodeInShared1.View(),
				nil,
				nil,
				nil,
			)

			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("expandAlias() unexpected result (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNextDNSCapMapRendering(t *testing.T) {
	t.Parallel()

	mkConfig := func(addrs ...string) *types.Config {
		resolvers := make([]*dnstype.Resolver, len(addrs))
		for i, a := range addrs {
			resolvers[i] = &dnstype.Resolver{Addr: a}
		}

		return &types.Config{
			TailcfgDNSConfig: &tailcfg.DNSConfig{
				Resolvers: resolvers,
			},
		}
	}

	mkNode := func() types.NodeView {
		return (&types.Node{
			ID:       1,
			Hostname: "node1",
			IPv4:     iap("100.64.0.1"),
			Hostinfo: &tailcfg.Hostinfo{OS: "linux"},
		}).View()
	}

	// resolverAddr extracts the first resolver's address with a
	// bounds check. Without it, a regression that drops the
	// resolver list would nil-panic instead of failing cleanly.
	resolverAddr := func(t *testing.T, got *tailcfg.DNSConfig) string {
		t.Helper()

		if got == nil {
			t.Fatalf("generateDNSConfig returned nil")
		}

		if len(got.Resolvers) == 0 {
			t.Fatalf("generateDNSConfig returned no Resolvers")
		}

		return got.Resolvers[0].Addr
	}

	t.Run("no_capmap_metadata_appended", func(t *testing.T) {
		t.Parallel()

		got := generateDNSConfig(
			mkConfig("https://dns.nextdns.io/abc"),
			mkNode(),
			nil,
			nil,
			nil,
		)

		want := "https://dns.nextdns.io/abc?device_ip=100.64.0.1&device_model=linux&device_name=node1"
		if addr := resolverAddr(t, got); addr != want {
			t.Errorf("addr = %q, want %q", addr, want)
		}
	})

	t.Run("profile_overrides_global", func(t *testing.T) {
		t.Parallel()

		capMap := tailcfg.NodeCapMap{
			"nextdns:override": []tailcfg.RawMessage{},
		}

		got := generateDNSConfig(
			mkConfig("https://dns.nextdns.io/global"),
			mkNode(),
			capMap,
			nil,
			nil,
		)

		want := "https://dns.nextdns.io/override?device_ip=100.64.0.1&device_model=linux&device_name=node1"
		if addr := resolverAddr(t, got); addr != want {
			t.Errorf("addr = %q, want %q", addr, want)
		}
	})

	t.Run("no_device_info_skips_metadata", func(t *testing.T) {
		t.Parallel()

		capMap := tailcfg.NodeCapMap{
			"nextdns:abc":            []tailcfg.RawMessage{},
			"nextdns:no-device-info": []tailcfg.RawMessage{},
		}

		got := generateDNSConfig(
			mkConfig("https://dns.nextdns.io/global"),
			mkNode(),
			capMap,
			nil,
			nil,
		)

		want := "https://dns.nextdns.io/abc"
		if addr := resolverAddr(t, got); addr != want {
			t.Errorf("addr = %q, want %q", addr, want)
		}
	})

	t.Run("non_nextdns_resolver_untouched", func(t *testing.T) {
		t.Parallel()

		capMap := tailcfg.NodeCapMap{
			"nextdns:abc": []tailcfg.RawMessage{},
		}

		got := generateDNSConfig(
			mkConfig("https://dns.example.org/dns-query"),
			mkNode(),
			capMap,
			nil,
			nil,
		)

		want := "https://dns.example.org/dns-query"
		if addr := resolverAddr(t, got); addr != want {
			t.Errorf("non-nextdns resolver was rewritten: %q", addr)
		}
	})
}

// TestBuildFromChangeFiltersPeerPatchesByVisibility proves that incremental
// peer-change patches (online/offline, endpoint, key-expiry) are restricted to
// the recipient's ACL-visible peer set, the NodeStore peer map buildTailPeers
// is fed from. Without it, a node receives the
// existence, presence, and addresses of peers its policy forbids accessing.
func TestBuildFromChangeFiltersPeerPatchesByVisibility(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

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
	require.NoError(t, err)

	user1 := database.CreateUserForTest("u1")
	user2 := database.CreateUserForTest("u2")
	n1 := database.CreateRegisteredNodeForTest(user1, "n1")
	n1b := database.CreateRegisteredNodeForTest(user1, "n1b")
	n2 := database.CreateRegisteredNodeForTest(user2, "n2")
	require.NoError(t, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// Each user may reach only its own devices, so n1 cannot access n2.
	policy := `{"acls":[
		{"action":"accept","src":["u1@"],"dst":["u1@:*"]},
		{"action":"accept","src":["u2@"],"dst":["u2@:*"]}
	]}`
	_, err = s.SetPolicy([]byte(policy))
	require.NoError(t, err)

	m := &mapper{state: s, cfg: cfg}

	// n2 (user2) comes online; n1 (user1) must NOT receive its patch, and
	// with nothing else in the change it must receive no frame at all.
	leakChange := change.NodeOnline(n2.ID, time.Now())
	resp, err := m.buildFromChange(n1.ID, tailcfg.CurrentCapabilityVersion, &leakChange)
	require.NoError(t, err)
	assert.Nil(t, resp, "a change holding only a patch n1 cannot see must not produce a frame for n1")

	// A node's own online patch is not a peer patch for it either.
	selfChange := change.NodeOnline(n1.ID, time.Now())
	resp, err = m.buildFromChange(n1.ID, tailcfg.CurrentCapabilityVersion, &selfChange)
	require.NoError(t, err)
	assert.Nil(t, resp, "n1's own online patch must not produce a frame for n1")

	// The hidden patch is dropped but the rest of the change is still sent.
	mixedChange := leakChange.Merge(change.PeersChanged("test", n1b.ID))
	resp, err = m.buildFromChange(n1.ID, tailcfg.CurrentCapabilityVersion, &mixedChange)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.PeersChanged, 1, "the visible changed peer must survive the dropped patch")
	assert.Equal(t, n1b.ID.NodeID(), resp.PeersChanged[0].ID)
	assert.Empty(t, resp.PeersChangedPatch,
		"n1 must not receive an online patch for n2, which its policy forbids accessing")

	// Control: n1b (same user) coming online IS visible to n1.
	okChange := change.NodeOnline(n1b.ID, time.Now())
	resp2, err := m.buildFromChange(n1.ID, tailcfg.CurrentCapabilityVersion, &okChange)
	require.NoError(t, err)
	require.NotNil(t, resp2)

	var gotVisible bool

	for _, p := range resp2.PeersChangedPatch {
		if p.NodeID == n1b.ID.NodeID() {
			gotVisible = true
		}
	}

	assert.True(t, gotVisible,
		"n1 must receive the online patch for visible same-user peer n1b")
}

// TestBuildFromChangeFiltersUserProfilesByVisibility proves the incremental
// PeersChanged path restricts UserProfiles to the recipient's ACL-visible
// peers, from the same BuildPeerMap-filtered set the full-map path reads.
// Without it, a changed node broadcast to all
// nodes leaks its owner's identity (login name, display name, avatar) to
// recipients whose policy forbids accessing that node.
func TestBuildFromChangeFiltersUserProfilesByVisibility(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

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
	require.NoError(t, err)

	user1 := database.CreateUserForTest("u1")
	user2 := database.CreateUserForTest("u2")
	n1 := database.CreateRegisteredNodeForTest(user1, "n1")
	n2 := database.CreateRegisteredNodeForTest(user2, "n2")
	require.NoError(t, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// Each user may reach only its own devices, so n1 cannot access n2.
	policy := `{"acls":[
		{"action":"accept","src":["u1@"],"dst":["u1@:*"]},
		{"action":"accept","src":["u2@"],"dst":["u2@:*"]}
	]}`
	_, err = s.SetPolicy([]byte(policy))
	require.NoError(t, err)

	m := &mapper{state: s, cfg: cfg}

	// n2 (user2) is added and broadcast. n1 (user1) cannot access it, so n1
	// must NOT receive user2's profile.
	c := change.NodeAdded(n2.ID)
	resp, err := m.buildFromChange(n1.ID, tailcfg.CurrentCapabilityVersion, &c)
	require.NoError(t, err)
	require.NotNil(t, resp)

	for _, up := range resp.UserProfiles {
		assert.NotEqual(t, user2.TailscaleUserProfile().ID, up.ID,
			"n1 must not receive user2's profile; n2 is not ACL-visible to n1")
	}
}

// TestBuildFromChangeVisibilityMatchesFullMap is the consolidation guard for
// PR #3304: the incremental change paths (peer patches via NodeOnline, changed
// peers via NodeAdded) must expose exactly the same ACL-visible peer set as the
// full-map path under every policy shape, and a cross-user UserProfile must not
// leak. If a future refactor lets one path drift from another, this fails.
//
// It pins two behaviours of the snapshot peer map every path reads: deny-all
// (empty matchers) must hide every peer on the incremental path rather than
// fall open to "no matchers => all visible", and per-node policies
// (autogroup:self) must agree across paths.
//
//nolint:tparallel // subtests install different policies on one shared State
func TestBuildFromChangeVisibilityMatchesFullMap(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
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
	require.NoError(t, err)

	user1 := database.CreateUserForTest("u1")
	user2 := database.CreateUserForTest("u2")
	n1 := database.CreateRegisteredNodeForTest(user1, "n1")
	n1b := database.CreateRegisteredNodeForTest(user1, "n1b")
	n2 := database.CreateRegisteredNodeForTest(user2, "n2")
	require.NoError(t, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	m := &mapper{state: s, cfg: cfg}
	capVer := tailcfg.CurrentCapabilityVersion

	// fullVisible returns the peer IDs n1 sees in the full map.
	fullVisible := func(t *testing.T) map[tailcfg.NodeID]bool {
		t.Helper()

		resp, err := m.fullMapResponse(n1.ID, capVer)
		require.NoError(t, err)

		got := map[tailcfg.NodeID]bool{}
		for _, p := range resp.Peers {
			got[p.ID] = true
		}

		return got
	}
	// patchReaches reports whether a NodeOnline patch for id is delivered to n1.
	patchReaches := func(t *testing.T, id types.NodeID) bool {
		t.Helper()

		c := change.NodeOnline(id, time.Now())
		resp, err := m.buildFromChange(n1.ID, capVer, &c)
		require.NoError(t, err)

		if resp == nil {
			return false
		}

		for _, p := range resp.PeersChangedPatch {
			if p.NodeID == id.NodeID() {
				return true
			}
		}

		return false
	}
	// changedReaches reports whether a NodeAdded changed-peer for id reaches n1.
	changedReaches := func(t *testing.T, id types.NodeID) bool {
		t.Helper()

		c := change.NodeAdded(id)
		resp, err := m.buildFromChange(n1.ID, capVer, &c)
		require.NoError(t, err)

		if resp == nil {
			return false
		}

		for _, p := range resp.PeersChanged {
			if p.ID == id.NodeID() {
				return true
			}
		}

		return false
	}
	// profileReaches reports whether want's profile is delivered to n1 when n
	// is added. Use a cross-user node so the result is not masked by n1's own
	// always-present user profile.
	profileReaches := func(t *testing.T, n *types.Node, want tailcfg.UserID) bool {
		t.Helper()

		c := change.NodeAdded(n.ID)
		resp, err := m.buildFromChange(n1.ID, capVer, &c)
		require.NoError(t, err)

		if resp == nil {
			return false
		}

		for _, up := range resp.UserProfiles {
			if up.ID == want {
				return true
			}
		}

		return false
	}

	// wantFull pins the actual peer-visibility semantics so the invariant below
	// cannot pass vacuously (e.g. if every path broke to zero identically).
	// deny_all is an ACL set with no rules: the policy enforces, the filter
	// is empty, and nobody sees anybody, on the full map and on every
	// incremental path alike. user_isolation and autogroup_self are the
	// discriminating cases that prove filtering works.
	tests := []struct {
		name     string
		policy   string
		wantFull int
	}{
		{"allow_all", `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`, 2},
		{
			"user_isolation",
			`{"acls":[
				{"action":"accept","src":["u1@"],"dst":["u1@:*"]},
				{"action":"accept","src":["u2@"],"dst":["u2@:*"]}
			]}`,
			1,
		},
		{"deny_all", `{"acls":[]}`, 0},
		{
			"autogroup_self",
			`{"acls":[{"action":"accept","src":["autogroup:member"],"dst":["autogroup:self:*"]}]}`,
			1,
		},
	}

	// The subtests share one State and each installs its own policy, so
	// they must run one after another. The policy goes in the way the API
	// puts it there, through the database and a reload, so the NodeStore's
	// peer map is rebuilt for it as in production.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.SetPolicyInDB(tt.policy)
			require.NoError(t, err)

			_, err = s.ReloadPolicy()
			require.NoError(t, err)

			full := fullVisible(t)
			require.Lenf(t, full, tt.wantFull,
				"%s: unexpected full-map visible peer count", tt.name)

			for _, peer := range []*types.Node{n1b, n2} {
				want := full[peer.ID.NodeID()]
				assert.Equalf(t, want, patchReaches(t, peer.ID),
					"%s: NodeOnline patch for %s must reach n1 iff full-map shows it",
					tt.name, peer.Hostname)
				assert.Equalf(t, want, changedReaches(t, peer.ID),
					"%s: NodeAdded changed-peer for %s must reach n1 iff full-map shows it",
					tt.name, peer.Hostname)
			}
			// Cross-user profile (user2) must appear iff n2 is visible to n1.
			assert.Equalf(t, full[n2.ID.NodeID()], profileReaches(t, n2, user2.TailscaleUserProfile().ID),
				"%s: user2 profile must be sent iff n2 is visible to n1", tt.name)
		})
	}
}

// TestFullMapResponseSurvivesPeerWithInvalidName proves a single node with an
// FQDN-invalid GivenName must not break map generation for its peers.
//
// A node whose stored GivenName is empty (ErrNodeHasNoGivenName) or yields an
// FQDN longer than MaxHostnameLength (ErrHostnameTooLong) makes GetFQDN, and
// therefore TailNode, return an error. buildTailPeers used to abort the entire
// peer list on the first such error, so MapResponseBuilder.Build() failed for
// every node that could see the bad peer; on the initial-connection path that
// surfaced as "PollNetMap: ... unexpected EOF" and the "Unable to connect to
// the Tailscale coordination server" health warning. A legacy DB row loads
// verbatim (NewNodeStore reads db.ListNodes() without re-sanitising names), so
// the bad peer persists across restart. The build for an unaffected viewer
// must succeed: the bad peer is dropped, valid peers and self survive.
func TestFullMapResponseSurvivesPeerWithInvalidName(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		badName string
	}{
		{"empty given name", ""},
		{"over-long fqdn", strings.Repeat("a", types.MaxHostnameLength+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
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
			require.NoError(t, err)

			user := database.CreateUserForTest("u1")
			n1 := database.CreateRegisteredNodeForTest(user, "n1")     // viewer, valid
			bad := database.CreateRegisteredNodeForTest(user, "bad")   // peer, name corrupted below
			good := database.CreateRegisteredNodeForTest(user, "good") // peer, valid control

			// Simulate a legacy/corrupt row that v29 loads verbatim.
			_, err = database.DB.ExecContext(t.Context(),
				"UPDATE nodes SET given_name = $1 WHERE id = $2", tt.badName, bad.ID)
			require.NoError(t, err)
			require.NoError(t, database.Close())

			s, err := state.NewState(cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })

			// Allow-all so n1 sees both peers; the bad one must still be dropped.
			_, err = s.SetPolicy([]byte(`{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`))
			require.NoError(t, err)

			m := &mapper{state: s, cfg: cfg}
			capVer := tailcfg.CurrentCapabilityVersion

			resp, err := m.fullMapResponse(n1.ID, capVer)
			require.NoError(t, err, "n1's map must build despite a peer with an invalid name")
			require.NotNil(t, resp)
			require.NotNil(t, resp.Node, "n1 must receive its own self node")

			peers := map[tailcfg.NodeID]bool{}
			for _, p := range resp.Peers {
				peers[p.ID] = true
			}

			assert.False(t, peers[bad.ID.NodeID()], "the peer with an invalid name must be dropped")
			assert.True(t, peers[good.ID.NodeID()], "valid peers must remain in the map")
		})
	}
}

// TestGenerateDNSConfigNilHostinfoNoPanic proves generateDNSConfig does not
// panic when a node's Hostinfo is nil (e.g. a legacy DB row with a NULL
// host_info column). addNextDNSMetadata dereferenced node.Hostinfo().OS()
// without the .Valid() guard its siblings (RequestTags, TailNode) apply, so
// building such a node's map crashed the server whenever a NextDNS resolver
// was configured.
func TestGenerateDNSConfigNilHostinfoNoPanic(t *testing.T) {
	t.Parallel()

	node := (&types.Node{
		Hostname: "legacy-node",
		IPv4:     iap("100.64.0.1"),
		// Hostinfo intentionally nil, as a legacy NULL host_info row loads.
	}).View()

	cfg := &types.Config{
		TailcfgDNSConfig: &tailcfg.DNSConfig{
			Resolvers: []*dnstype.Resolver{{Addr: "https://dns.nextdns.io/abc"}},
		},
	}

	require.NotPanics(t, func() {
		generateDNSConfig(cfg, node, nil, nil, nil)
	}, "generateDNSConfig must not panic when a node has nil Hostinfo")
}

// policyShapes covers the paths that decide how the mapper filters peers: a
// global filter with matchers, a per-node (autogroup:self) filter, a policy
// that leaves every node with zero matchers, and no rules at all. The
// zero-matcher shape is the interesting one: it must hide every peer, not
// fall open to "no matchers => all visible".
var policyShapes = []struct {
	name   string
	policy string
}{
	{
		name:   "allow all",
		policy: `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`,
	},
	{
		name:   "autogroup self",
		policy: `{"acls":[{"action":"accept","src":["autogroup:member"],"dst":["autogroup:self:*"]}]}`,
	},
	{
		name:   "no rules",
		policy: `{"acls":[]}`,
	},
	{
		name:   "empty policy",
		policy: `{}`,
	},
}

// assertSelfNotAPeer fails when a [tailcfg.MapResponse] addressed to nodeID
// mentions nodeID in any peer-carrying field.
//
// The Tailscale client merges [tailcfg.MapResponse.PeersChanged] straight into
// its peer map (controlclient updatePeersStateFromResponse) and keeps the self
// node in a separate field, so a node present in its own peer list is rendered
// twice by clients that concatenate peers with self.
func assertSelfNotAPeer(t *testing.T, nodeID types.NodeID, resp *tailcfg.MapResponse, what string) {
	t.Helper()

	if resp == nil {
		return
	}

	self := nodeID.NodeID()

	for _, p := range resp.Peers {
		assert.NotEqualf(t, self, p.ID, "%s: node %d listed in its own Peers", what, nodeID)
	}

	for _, p := range resp.PeersChanged {
		assert.NotEqualf(t, self, p.ID, "%s: node %d listed in its own PeersChanged", what, nodeID)
	}

	for _, p := range resp.PeersChangedPatch {
		assert.NotEqualf(t, self, p.NodeID, "%s: node %d patched in its own PeersChangedPatch", what, nodeID)
	}

	for _, id := range resp.PeersRemoved {
		assert.NotEqualf(t, self, id, "%s: node %d listed in its own PeersRemoved", what, nodeID)
	}
}

// TestMapResponseNeverContainsSelfAsPeer drives the [change.Change] shapes the
// server emits through the response builder for every node, under each policy
// shape, and asserts the recipient is never present in its own peer fields.
func TestMapResponseNeverContainsSelfAsPeer(t *testing.T) {
	t.Parallel()

	for _, tt := range policyShapes {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testData, cleanup := setupBatcherWithTestData(t, NewBatcherAndMapper, 2, 3, largeBufferSize)
			defer cleanup()

			_, err := testData.State.SetPolicy([]byte(tt.policy))
			require.NoError(t, err)

			batcher := unwrapBatcher(testData.Batcher)

			allIDs := make([]types.NodeID, 0, len(testData.Nodes))

			for i := range testData.Nodes {
				tn := &testData.Nodes[i]
				require.NoError(t, testData.Batcher.AddNode(tn.n.ID, tn.ch, 100, nil))
				allIDs = append(allIDs, tn.n.ID)
			}

			seen := time.Now()

			for _, recipient := range allIDs {
				changes := map[string]change.Change{
					"full self":     change.FullSelf(recipient),
					"full update":   change.FullUpdate(),
					"policy change": change.PolicyChange(),
					"self added":    change.NodeAdded(recipient),
					"self online":   change.NodeOnline(recipient, seen),
					"self offline":  change.NodeOffline(recipient, seen),
					// A batch naming every node, the recipient included.
					// change.PeersChanged carries no OriginNode, so the
					// self-update short circuit in buildFromChange never fires
					// and the peer lookup is the only thing left to drop self.
					"all peers changed": change.PeersChanged("all peers", allIDs...),
				}

				for name, ch := range changes {
					resp, err := batcher.MapResponseFromChange(recipient, ch)
					require.NoError(t, err, "%s for node %d", name, recipient)
					assertSelfNotAPeer(t, recipient, resp, name)
				}
			}
		})
	}
}

// TestNoSelfAsPeerDuringRealNodeChurn exercises the change flow poll.go
// drives (connect, disconnect, reconnect, policy reload, key expiry) and
// scans every delivered [tailcfg.MapResponse] for the recipient's own node.
func TestNoSelfAsPeerDuringRealNodeChurn(t *testing.T) {
	t.Parallel()

	// How long a node's stream must stay silent before the churn counts
	// as settled and the scan moves on to the next node. The batcher
	// flushes every few milliseconds, so this is many batches.
	const quietPeriod = 500 * time.Millisecond

	for _, tt := range policyShapes {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testData, cleanup := setupBatcherWithTestData(t, NewBatcherAndMapper, 2, 3, largeBufferSize)
			defer cleanup()

			_, err := testData.State.SetPolicy([]byte(tt.policy))
			require.NoError(t, err)

			batcher := testData.Batcher

			for i := range testData.Nodes {
				tn := &testData.Nodes[i]
				require.NoError(t, batcher.AddNode(tn.n.ID, tn.ch, 100, nil))
			}

			// Drop and re-add every node but the first, then reload the
			// policy and expire the one node that never reconnected.
			for i := 1; i < len(testData.Nodes); i++ {
				tn := &testData.Nodes[i]
				batcher.RemoveNode(tn.n.ID, tn.ch)
				require.NoError(t, batcher.AddNode(tn.n.ID, tn.ch, 100, nil))
			}

			_, err = testData.State.SetPolicy([]byte(tt.policy))
			require.NoError(t, err)

			expiry := time.Now().Add(time.Hour)

			_, c, err := testData.State.SetNodeExpiry(testData.Nodes[0].n.ID, &expiry)
			require.NoError(t, err)
			batcher.AddWork(c)

			for i := range testData.Nodes {
				tn := &testData.Nodes[i]

				for quiet := false; !quiet; {
					select {
					case resp := <-tn.ch:
						assertSelfNotAPeer(t, tn.n.ID, resp, "churn")
					case <-time.After(quietPeriod):
						quiet = true
					}
				}
			}
		})
	}
}

// TestGenerateMapResponseDebugOnlyOnFullMaps pins where [tailcfg.Debug] rides:
// on every full map (the initial map and FullUpdate renders), so a client that
// connects or reconnects is told to stop log uploads, and on nothing else. The
// renders run through [generateMapResponse], the path the batcher uses, so a
// branch that bypasses buildFromChange cannot silently drop or add it.
func TestGenerateMapResponseDebugOnlyOnFullMaps(t *testing.T) {
	tmp := t.TempDir()
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
	require.NoError(t, err)

	user := database.CreateUserForTest("u1")
	n1 := database.CreateRegisteredNodeForTest(user, "n1")
	n2 := database.CreateRegisteredNodeForTest(user, "n2")
	require.NoError(t, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, err = s.SetPolicy([]byte(`{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`))
	require.NoError(t, err)

	m := &mapper{state: s, cfg: cfg}

	kinds := []struct {
		name string
		c    change.Change
		full bool
	}{
		{"full_update", change.FullUpdate(), true},
		{"full_self", change.FullSelf(n1.ID), true},
		{"user_removed", change.UserRemoved(), true},
		{"self_update", change.SelfUpdate(n1.ID), false},
		{"self_node_added", change.NodeAdded(n1.ID), false},
		{"policy_change", change.PolicyChange(), false},
		{"derp_map", change.DERPMap(), false},
		{"dns_config", change.DNSConfig(), false},
		{"peer_added", change.NodeAdded(n2.ID), false},
		{"peer_online", change.NodeOnline(n2.ID, time.Now()), false},
	}

	for _, logTail := range []bool{false, true} {
		for _, k := range kinds {
			t.Run(fmt.Sprintf("logtail=%t/%s", logTail, k.name), func(t *testing.T) {
				cfg.LogTail.Enabled = logTail

				resps, err := generateMapResponse(newMockNodeConnection(n1.ID), m, k.c)
				require.NoError(t, err)
				require.Len(t, resps, 1)

				var want *tailcfg.Debug
				if k.full && !logTail {
					want = &tailcfg.Debug{DisableLogTail: true}
				}

				assert.Equal(t, want, resps[0].Debug)
			})
		}
	}
}

// generateUserProfilesPointerMap freezes generateUserProfiles before the
// profile map held views by value.
func generateUserProfilesPointerMap(
	node types.NodeView,
	peers views.Slice[types.NodeView],
) []tailcfg.UserProfile {
	userMap := make(map[uint]*types.UserView)

	user := node.Owner()
	if !user.Valid() {
		return nil
	}

	userMap[user.ID()] = &user

	for _, peer := range peers.All() {
		peerUser := peer.Owner()
		if !peerUser.Valid() {
			continue
		}

		userMap[peerUser.ID()] = &peerUser
	}

	var profiles []tailcfg.UserProfile

	for _, id := range slices.Sorted(maps.Keys(userMap)) {
		profiles = append(profiles, userMap[id].TailscaleUserProfile())
	}

	return profiles
}

// userProfilesOrPanic reports gen's result, or that it panicked.
func userProfilesOrPanic(
	gen func(types.NodeView, views.Slice[types.NodeView]) []tailcfg.UserProfile,
	node types.NodeView,
	peers views.Slice[types.NodeView],
) ([]tailcfg.UserProfile, bool) {
	var profiles []tailcfg.UserProfile

	panicked := true

	func() {
		defer func() { _ = recover() }()

		profiles = gen(node, peers)
		panicked = false
	}()

	return profiles, panicked
}

func checkUserProfilesMatchPointerMap(
	t *testing.T,
	node types.NodeView,
	peers views.Slice[types.NodeView],
) {
	t.Helper()

	want, wantPanic := userProfilesOrPanic(generateUserProfilesPointerMap, node, peers)
	got, gotPanic := userProfilesOrPanic(generateUserProfiles, node, peers)
	require.Equal(t, wantPanic, gotPanic)
	require.Equal(t, want, got)
}

func TestGenerateUserProfilesMatchesPointerMap(t *testing.T) {
	t.Parallel()

	first := &types.User{
		ID: 7, Name: "first", DisplayName: "First",
		Email: "first@example.com", ProfilePicURL: "https://example.com/first",
	}
	last := &types.User{
		ID: 7, Name: "last", DisplayName: "Last",
		Email: "last@example.com", ProfilePicURL: "https://example.com/last",
	}
	other := &types.User{ID: 100}
	node := (&types.Node{ID: 1, UserID: new(first.ID), User: first}).View()
	duplicate := (&types.Node{ID: 2, UserID: new(last.ID), User: last}).View()
	peer := (&types.Node{ID: 3, UserID: new(other.ID), User: other}).View()
	orphan := (&types.Node{ID: 4}).View()
	missingUser := (&types.Node{ID: 5, UserID: new(first.ID)}).View()
	tagged := (&types.Node{ID: 6, Tags: []string{"tag:server"}}).View()
	taggedCreator := (&types.Node{
		ID: 7, Tags: []string{"tag:server"}, UserID: new(first.ID), User: first,
	}).View()
	zeroUser := (&types.Node{ID: 8, User: &types.User{}}).View()

	for _, tt := range []struct {
		name  string
		node  types.NodeView
		peers []types.NodeView
	}{
		{name: "nil_peers", node: node},
		{name: "empty_peers", node: node, peers: []types.NodeView{}},
		{name: "unowned_requester", node: orphan, peers: []types.NodeView{peer}},
		{name: "missing_requester_user", node: missingUser, peers: []types.NodeView{peer}},
		{name: "invalid_peer_after_unowned_requester", node: orphan, peers: []types.NodeView{{}}},
		{name: "invalid_owner_peers", node: node, peers: []types.NodeView{orphan, missingUser}},
		{name: "same_owner", node: node, peers: []types.NodeView{node, node, node}},
		{name: "last_peer_wins", node: node, peers: []types.NodeView{peer, node, duplicate}},
		{name: "reverse_duplicate_order", node: node, peers: []types.NodeView{duplicate, peer, node}},
		{name: "zero_id_and_no_user_id", node: zeroUser, peers: []types.NodeView{peer, node}},
		{name: "tagged_requester", node: tagged},
		{name: "tagged_requester_with_creator", node: taggedCreator},
		{name: "tagged_peers", node: node, peers: []types.NodeView{tagged, taggedCreator}},
		{
			name: "mixed_unsorted_owners", node: node,
			peers: []types.NodeView{taggedCreator, peer, orphan, zeroUser, tagged, duplicate},
		},
		{name: "invalid_requester"},
		{name: "invalid_peer", node: node, peers: []types.NodeView{{}}},
		{name: "invalid_peer_after_valid", node: node, peers: []types.NodeView{peer, {}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checkUserProfilesMatchPointerMap(t, tt.node, views.SliceOf(tt.peers))
		})
	}

	require.Equal(t, []tailcfg.UserProfile{
		{
			ID:            7,
			LoginName:     last.Email,
			DisplayName:   last.DisplayName,
			ProfilePicURL: last.ProfilePicURL,
		},
		{ID: 100, LoginName: "100", DisplayName: "100"},
	}, generateUserProfiles(node, views.SliceOf([]types.NodeView{peer, node, duplicate})))
	require.Equal(t, []tailcfg.UserProfile{
		{
			ID:          types.TaggedDevicesUserID,
			LoginName:   types.TaggedDevices.Name,
			DisplayName: types.TaggedDevices.DisplayName,
		},
	}, generateUserProfiles(taggedCreator, views.SliceOf([]types.NodeView{tagged})))
	require.Nil(t, generateUserProfiles(orphan, views.SliceOf([]types.NodeView{peer})))
}

func TestGenerateUserProfilesFallbacksMatchPointerMap(t *testing.T) {
	t.Parallel()

	for _, id := range []uint{0, 99, 100, 101, ^uint(0)} {
		for mask := range 32 {
			t.Run(fmt.Sprintf("ID=%d/fields=%02d", id, mask), func(t *testing.T) {
				t.Parallel()

				u := &types.User{
					ID:                 id,
					ProfilePicURL:      "https://example.com/avatar",
					ProviderIdentifier: sql.NullString{Valid: mask&8 != 0},
				}

				if mask&1 != 0 {
					u.Email = "email@example.com"
				}

				if mask&2 != 0 {
					u.Name = "username"
				}

				if mask&4 != 0 {
					u.ProviderIdentifier.String = "https://issuer.example.com/subject"
				}

				if mask&16 != 0 {
					u.DisplayName = "Display Name"
				}

				node := (&types.Node{User: u}).View()
				checkUserProfilesMatchPointerMap(t, node, views.Slice[types.NodeView]{})

				tagged := (&types.Node{Tags: []string{"tag:server"}}).View()
				checkUserProfilesMatchPointerMap(t, tagged, views.SliceOf([]types.NodeView{node}))
			})
		}
	}
}

func userProfilesFixture(
	peerCount, ownerCount int,
	names string,
) (types.NodeView, views.Slice[types.NodeView]) {
	users := make([]types.User, ownerCount)
	for i := range users {
		users[i].ID = uint(i + 1000)
		if names == "named" {
			users[i].Name = fmt.Sprintf("user-%d", i)
			users[i].Email = fmt.Sprintf("user-%d@example.com", i)
			users[i].DisplayName = fmt.Sprintf("User %d", i)
		}

		users[i].ProfilePicURL = "https://example.com/avatar"
	}

	peers := make([]types.NodeView, peerCount)
	for i := range peers {
		u := &users[i%ownerCount]
		peers[i] = (&types.Node{UserID: new(u.ID), User: u}).View()
	}

	return (&types.Node{UserID: new(users[0].ID), User: &users[0]}).View(), views.SliceOf(peers)
}

func TestGenerateUserProfilesTailnetsMatchPointerMap(t *testing.T) {
	t.Parallel()

	for _, peerCount := range []int{8, 128, 1024, 8192} {
		for _, ownerCount := range []int{1, peerCount} {
			for _, names := range []string{"named", "numeric"} {
				t.Run(fmt.Sprintf("peers=%d/owners=%d/%s", peerCount, ownerCount, names), func(t *testing.T) {
					t.Parallel()

					node, peers := userProfilesFixture(peerCount, ownerCount, names)
					checkUserProfilesMatchPointerMap(t, node, peers)
				})
			}
		}
	}
}

func BenchmarkGenerateUserProfiles(b *testing.B) {
	for _, peerCount := range []int{8, 128, 1024, 8192} {
		for _, ownerCount := range []int{1, peerCount} {
			for _, names := range []string{"named", "numeric"} {
				b.Run(fmt.Sprintf("peers=%d/owners=%d/%s", peerCount, ownerCount, names), func(b *testing.B) {
					node, peers := userProfilesFixture(peerCount, ownerCount, names)
					require.Equal(b, generateUserProfilesPointerMap(node, peers), generateUserProfiles(node, peers))

					b.Run("pointer-map", func(b *testing.B) {
						b.ReportAllocs()

						for b.Loop() {
							_ = generateUserProfilesPointerMap(node, peers)
						}
					})
					b.Run("value-map", func(b *testing.B) {
						b.ReportAllocs()

						for b.Loop() {
							_ = generateUserProfiles(node, peers)
						}
					})
				})
			}
		}
	}
}

// buildTailPeersCapMapsReference freezes buildTailPeers before it
// snapshotted only the requested peers' caps.
func (b *MapResponseBuilder) buildTailPeersCapMapsReference(
	peers views.Slice[types.NodeView],
) ([]*tailcfg.Node, error) {
	node, ok := b.mapper.state.GetNodeByID(b.nodeID)
	if !ok {
		return nil, ErrNodeNotFoundMapper
	}

	b.visiblePeers = peers

	matchers, err := b.mapper.state.MatchersForNode(node)
	if err != nil {
		return nil, err
	}

	allCapMaps := b.mapper.state.NodeCapMaps()
	globalExitNodes := b.mapper.state.HasGlobalExitNode()
	exitPriorities := b.mapper.state.ExitNodePrioritiesFor(node)
	serviceRoutes := b.mapper.state.ServiceRoutesByHost(node)

	tailPeers := make([]*tailcfg.Node, 0, peers.Len())

	for _, peer := range peers.All() {
		tn, err := peer.PeerTailNode(func(id types.NodeID) []netip.Prefix {
			return slices.Concat(
				b.mapper.state.RoutesForPeer(node, peer, matchers),
				serviceRoutes[id],
			)
		}, b.mapper.cfg, allCapMaps[peer.ID()])
		if err != nil {
			continue
		}

		markShared(tn, node, peer)

		tn.CapMap = policyv2.PeerCapMap(peer, allCapMaps[peer.ID()], globalExitNodes)

		if priority, ok := exitPriorities[peer.ID()]; ok {
			setExitNodePriority(tn, priority)
		}

		tailPeers = append(tailPeers, tn)
	}

	slices.SortStableFunc(tailPeers, func(a, b *tailcfg.Node) int {
		return stdcmp.Compare(a.ID, b.ID)
	})

	return tailPeers, nil
}

func (b *MapResponseBuilder) withPeersCapMapsReference(peers views.Slice[types.NodeView]) *MapResponseBuilder {
	tailPeers, err := b.buildTailPeersCapMapsReference(peers)
	if err != nil {
		b.addError(err)
		return b
	}

	b.resp.Peers = tailPeers

	return b
}

func (m *mapper) fullMapResponseCapMapsReference(
	nodeID types.NodeID,
	capVer tailcfg.CapabilityVersion,
) (*tailcfg.MapResponse, error) {
	peers := m.state.ListPeers(nodeID)

	return m.NewMapResponseBuilder(nodeID).
		WithDebugType(fullResponseDebug).
		WithCapabilityVersion(capVer).
		WithSelfNode().
		WithDERPMap().
		WithDomain().
		WithCollectServicesDisabled().
		WithDebugConfig().
		WithSSHPolicy().
		WithDNSConfig().
		WithPacketFilters().
		withPeersCapMapsReference(peers).
		WithUserProfilesOfPeers().
		Build()
}

// capMapsMapperFixture builds n nodes of one user, every eighth from the
// second on an exit node. A star policy gives the first node every peer.
func capMapsMapperFixture(tb testing.TB, n int, attrs string) (*mapper, []types.NodeView) {
	tb.Helper()

	p4 := netip.MustParsePrefix("100.64.0.0/10")
	p6 := netip.MustParsePrefix("fd7a:115c:a1e0::/48")
	cfg := &types.Config{
		Database: types.DatabaseConfig{
			Type:   types.DatabaseSqlite,
			Sqlite: types.SqliteConfig{Path: tb.TempDir() + "/caps.db"},
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
	require.NoError(tb, err)

	user := database.CreateUserForTest("capuser")
	ip4, ip6 := netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("fd7a:115c:a1e0::1")
	exitRoutes := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}

	err = database.Write(func(tx *db.Tx) error {
		for i := range n {
			name := fmt.Sprintf("capnode%d", i+1)

			node := &types.Node{
				MachineKey:     key.NewMachine().Public(),
				NodeKey:        key.NewNode().Public(),
				DiscoKey:       key.NewDisco().Public(),
				Hostname:       name,
				GivenName:      name,
				UserID:         &user.ID,
				IPv4:           new(ip4),
				IPv6:           new(ip6),
				Hostinfo:       &tailcfg.Hostinfo{},
				RegisterMethod: util.RegisterMethodAuthKey,
				ApprovedAt:     new(time.Now().UTC()),
			}
			if i%8 == 1 {
				node.ApprovedRoutes = exitRoutes
				node.Hostinfo.RoutableIPs = exitRoutes
			}

			createErr := db.CreateNode(tx, node)
			if createErr != nil {
				return createErr
			}

			ip4, ip6 = ip4.Next(), ip6.Next()
		}

		return nil
	})
	require.NoError(tb, err)

	_, err = database.SetPolicy(`{
		"acls":[{"action":"accept","src":["100.64.0.1"],"dst":["*:*"]}],
		"nodeAttrs":` + attrs + `}`)
	require.NoError(tb, err)
	require.NoError(tb, database.Close())

	s, err := state.NewState(cfg)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = s.Close() })

	stored := s.ListNodes().AsSlice()
	slices.SortFunc(stored, func(a, b types.NodeView) int { return stdcmp.Compare(a.ID(), b.ID()) })
	require.Len(tb, stored, n)

	return &mapper{state: s, cfg: cfg}, stored
}

// checkTailPeersMatchReference builds peers both ways for the viewer and
// checks the narrowed cap snapshot changes nothing, leaves the manager's
// maps alone, and hands out maps a caller may mutate.
func checkTailPeersMatchReference(
	t *testing.T,
	m *mapper,
	viewer types.NodeID,
	peers views.Slice[types.NodeView],
) []*tailcfg.Node {
	t.Helper()

	before, err := json.Marshal(m.state.NodeCapMaps())
	require.NoError(t, err)

	builder := m.NewMapResponseBuilder(viewer).WithCapabilityVersion(tailcfg.CurrentCapabilityVersion)
	want, wantErr := builder.buildTailPeersCapMapsReference(peers)
	got, gotErr := builder.buildTailPeers(peers)
	require.Equal(t, wantErr, gotErr)
	require.Equal(t, want, got)

	for _, peer := range got {
		if peer.CapMap != nil {
			peer.CapMap["caller-only"] = nil
		}
	}

	after, err := json.Marshal(m.state.NodeCapMaps())
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "rendering must not mutate manager-owned inner maps")

	again, err := builder.buildTailPeers(peers)
	require.NoError(t, err)
	require.Equal(t, want, again, "output mutations must not affect later builds")

	return again
}

func peerByID(peers []*tailcfg.Node, id types.NodeID) *tailcfg.Node {
	for _, p := range peers {
		if p.ID == id.NodeID() {
			return p
		}
	}

	return nil
}

func TestBuildTailPeersCapMapsMatchReference(t *testing.T) {
	t.Parallel()

	for _, attrs := range []struct {
		name, policy string
	}{
		{"empty", `[]`},
		{"partial", `[{"target":["100.64.0.2"],"attr":["disable-ipv4","suggest-exit-node"]}]`},
		{"all", `[{"target":["*"],"attr":["disable-ipv4","suggest-exit-node"]}]`},
	} {
		t.Run(attrs.name, func(t *testing.T) {
			t.Parallel()

			m, nodes := capMapsMapperFixture(t, 12, attrs.policy)
			viewer := nodes[0].ID()
			exit, otherExit := nodes[1], nodes[9]

			duplicate := exit.AsStruct()
			duplicate.GivenName = "same-id-different-name"
			missing := nodes[2].AsStruct()
			missing.ID = 999
			invalid := nodes[3].AsStruct()
			invalid.GivenName = ""

			for _, selection := range []struct {
				name  string
				peers []types.NodeView
			}{
				{"nil", nil},
				{"empty", []types.NodeView{}},
				{"one", []types.NodeView{exit}},
				{"full", nodes[1:]},
				{"missing", []types.NodeView{missing.View()}},
				{"duplicates", []types.NodeView{nodes[3], duplicate.View(), exit, nodes[2], exit}},
				{"invalid-name", []types.NodeView{invalid.View(), exit}},
			} {
				got := checkTailPeersMatchReference(t, m, viewer, views.SliceOf(selection.peers))
				require.NotNil(t, got, selection.name)

				if p := peerByID(got, exit.ID()); p != nil {
					require.Contains(t, p.CapMap, nodecap.SuggestExitNode, selection.name)

					if attrs.name != "empty" {
						require.Equal(t,
							[]netip.Prefix{netip.PrefixFrom(exit.IPv6().Get(), 128)}, p.Addresses, selection.name)
						require.NotContains(t, p.CapMap, nodecap.Cap("disable-ipv4"),
							"self caps must not leak into the peer view")
					}
				}
			}

			_, err := m.NewMapResponseBuilder(999).buildTailPeers(views.SliceOf(nodes))
			require.ErrorIs(t, err, ErrNodeNotFoundMapper)

			// A global exit node narrows suggest-exit-node to the marked
			// nodes; the narrowed snapshot must still carry the mark.
			_, _, err = m.state.SetGlobalExitNode(exit.ID(), true, new(20))
			require.NoError(t, err)

			marked, ok := m.state.GetNodeByID(exit.ID())
			require.True(t, ok)

			unmarked, ok := m.state.GetNodeByID(otherExit.ID())
			require.True(t, ok)

			got := checkTailPeersMatchReference(t, m, viewer, views.SliceOf([]types.NodeView{marked}))
			require.Len(t, got, 1)
			require.Contains(t, got[0].CapMap, nodecap.SuggestExitNode)

			got = checkTailPeersMatchReference(t, m, viewer, views.SliceOf([]types.NodeView{unmarked}))
			require.Len(t, got, 1)

			if attrs.name != "all" {
				require.NotContains(t, got[0].CapMap, nodecap.SuggestExitNode)
			}

			checkTailPeersMatchReference(t, m, viewer, m.state.ListPeers(viewer))

			for _, pol := range []string{
				"",
				`{"nodeAttrs":[{"target":["*"],"attr":["randomize-client-port"]}]}`,
				`{"randomizeClientPort":true}`,
			} {
				if pol != "" {
					_, err := m.state.SetPolicy([]byte(pol))
					require.NoError(t, err)
				}

				want, err := m.fullMapResponseCapMapsReference(viewer, tailcfg.CurrentCapabilityVersion)
				require.NoError(t, err)
				got, err := m.fullMapResponse(viewer, tailcfg.CurrentCapabilityVersion)
				require.NoError(t, err)

				want.ControlTime, got.ControlTime = nil, nil
				require.Equal(t, want, got)
			}
		})
	}
}

var (
	capMapsBuilderBenchmarkSink  []*tailcfg.Node
	capMapsResponseBenchmarkSink *tailcfg.MapResponse
)

func BenchmarkBuildTailPeersCapMaps(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		for _, attrs := range []struct{ name, policy string }{
			{"empty", `[]`},
			{"partial", `[{"target":["100.64.0.2"],"attr":["disable-ipv4"]}]`},
			{"all", `[{"target":["*"],"attr":["disable-ipv4"]}]`},
		} {
			b.Run(fmt.Sprintf("N%d/%s", n, attrs.name), func(b *testing.B) {
				m, nodes := capMapsMapperFixture(b, n, attrs.policy)
				builder := m.NewMapResponseBuilder(nodes[0].ID()).
					WithCapabilityVersion(tailcfg.CurrentCapabilityVersion)

				allPeers := views.SliceOf(nodes[1:])
				for _, k := range []int{0, 1, 8, allPeers.Len()} {
					peers := allPeers.Slice(0, k)

					b.Run(fmt.Sprintf("K%d/reference", k), func(b *testing.B) {
						b.ReportAllocs()

						for b.Loop() {
							var err error

							capMapsBuilderBenchmarkSink, err = builder.buildTailPeersCapMapsReference(peers)
							if err != nil {
								b.Fatal(err)
							}
						}
					})
					b.Run(fmt.Sprintf("K%d/selected", k), func(b *testing.B) {
						b.ReportAllocs()

						for b.Loop() {
							var err error

							capMapsBuilderBenchmarkSink, err = builder.buildTailPeers(peers)
							if err != nil {
								b.Fatal(err)
							}
						}
					})
				}

				b.Run("fullresponse/reference", func(b *testing.B) {
					b.ReportAllocs()

					for b.Loop() {
						var err error

						capMapsResponseBenchmarkSink, err = m.fullMapResponseCapMapsReference(
							nodes[0].ID(), tailcfg.CurrentCapabilityVersion)
						if err != nil {
							b.Fatal(err)
						}
					}
				})
				b.Run("fullresponse/selected", func(b *testing.B) {
					b.ReportAllocs()

					for b.Loop() {
						var err error

						capMapsResponseBenchmarkSink, err = m.fullMapResponse(
							nodes[0].ID(), tailcfg.CurrentCapabilityVersion)
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			})
		}
	}
}
