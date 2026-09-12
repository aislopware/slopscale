package derp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// derpNode returns a [tailcfg.DERPNode] test fixture named name in regionID,
// with a host name derived from name.
func derpNode(name string, regionID tailcfg.DERPRegionID) *tailcfg.DERPNode {
	return &tailcfg.DERPNode{Name: name, RegionID: regionID, HostName: "derp" + name + ".tailscale.com"}
}

// derpMapOf builds a single-region [tailcfg.DERPMap] test fixture, with one
// node per name in nodeNames, in the given order.
func derpMapOf(id tailcfg.DERPRegionID, code, name string, nodeNames ...string) *tailcfg.DERPMap {
	nodes := make([]*tailcfg.DERPNode, len(nodeNames))
	for i, n := range nodeNames {
		nodes[i] = derpNode(n, id)
	}

	return &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			id: {
				RegionID:   id,
				RegionCode: code,
				RegionName: name,
				Nodes:      nodes,
			},
		},
	}
}

func TestShuffleDERPMapDeterministic(t *testing.T) {
	tests := []struct {
		name       string
		baseDomain string
		derpMap    *tailcfg.DERPMap
		expected   *tailcfg.DERPMap
	}{
		{
			name:       "single region with 4 nodes",
			baseDomain: "test1.example.com",
			derpMap:    derpMapOf(1, "nyc", "New York City", "1f", "1g", "1h", "1i"),
			expected:   derpMapOf(1, "nyc", "New York City", "1h", "1f", "1g", "1i"),
		},
		{
			name:       "multiple regions with nodes",
			baseDomain: "test2.example.com",
			derpMap: &tailcfg.DERPMap{
				Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
					10: {
						RegionID:   10,
						RegionCode: "sea",
						RegionName: "Seattle",
						Nodes: []*tailcfg.DERPNode{
							{Name: "10b", RegionID: 10, HostName: "derp10b.tailscale.com"},
							{Name: "10c", RegionID: 10, HostName: "derp10c.tailscale.com"},
							{Name: "10d", RegionID: 10, HostName: "derp10d.tailscale.com"},
						},
					},
					2: {
						RegionID:   2,
						RegionCode: "sfo",
						RegionName: "San Francisco",
						Nodes: []*tailcfg.DERPNode{
							{Name: "2d", RegionID: 2, HostName: "derp2d.tailscale.com"},
							{Name: "2e", RegionID: 2, HostName: "derp2e.tailscale.com"},
							{Name: "2f", RegionID: 2, HostName: "derp2f.tailscale.com"},
						},
					},
				},
			},
			expected: &tailcfg.DERPMap{
				Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
					10: {
						RegionID:   10,
						RegionCode: "sea",
						RegionName: "Seattle",
						Nodes: []*tailcfg.DERPNode{
							{Name: "10c", RegionID: 10, HostName: "derp10c.tailscale.com"},
							{Name: "10b", RegionID: 10, HostName: "derp10b.tailscale.com"},
							{Name: "10d", RegionID: 10, HostName: "derp10d.tailscale.com"},
						},
					},
					2: {
						RegionID:   2,
						RegionCode: "sfo",
						RegionName: "San Francisco",
						Nodes: []*tailcfg.DERPNode{
							{Name: "2f", RegionID: 2, HostName: "derp2f.tailscale.com"},
							{Name: "2d", RegionID: 2, HostName: "derp2d.tailscale.com"},
							{Name: "2e", RegionID: 2, HostName: "derp2e.tailscale.com"},
						},
					},
				},
			},
		},
		{
			name:       "large region with many nodes",
			baseDomain: "test3.example.com",
			derpMap:    derpMapOf(4, "fra", "Frankfurt", "4f", "4g", "4h", "4i"),
			expected:   derpMapOf(4, "fra", "Frankfurt", "4f", "4g", "4h", "4i"),
		},
		{
			name:       "same region different base domain",
			baseDomain: "different.example.com",
			derpMap:    derpMapOf(4, "fra", "Frankfurt", "4f", "4g", "4h", "4i"),
			expected:   derpMapOf(4, "fra", "Frankfurt", "4h", "4f", "4g", "4i"),
		},
		{
			name:       "same dataset with another base domain",
			baseDomain: "another.example.com",
			derpMap:    derpMapOf(4, "fra", "Frankfurt", "4f", "4g", "4h", "4i"),
			expected:   derpMapOf(4, "fra", "Frankfurt", "4h", "4i", "4g", "4f"),
		},
		{
			name:       "same dataset with yet another base domain",
			baseDomain: "yetanother.example.com",
			derpMap:    derpMapOf(4, "fra", "Frankfurt", "4f", "4g", "4h", "4i"),
			expected:   derpMapOf(4, "fra", "Frankfurt", "4i", "4h", "4g", "4f"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf.Set("dns.base_domain", tt.baseDomain)

			defer conf.Reset()

			resetDerpRandomForTesting()

			testMap := tt.derpMap.View().AsStruct()
			shuffleDERPMap(testMap)

			if diff := cmp.Diff(tt.expected, testMap); diff != "" {
				t.Errorf("Shuffled DERP map doesn't match expected (-expected +actual):\n%s", diff)
			}
		})
	}
}

func TestShuffleDERPMapEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		derpMap *tailcfg.DERPMap
	}{
		{
			name:    "nil derp map",
			derpMap: nil,
		},
		{
			name: "empty derp map",
			derpMap: &tailcfg.DERPMap{
				Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{},
			},
		},
		{
			name: "region with no nodes",
			derpMap: &tailcfg.DERPMap{
				Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
					1: {
						RegionID:   1,
						RegionCode: "empty",
						RegionName: "Empty Region",
						Nodes:      []*tailcfg.DERPNode{},
					},
				},
			},
		},
		{
			name: "region with single node",
			derpMap: &tailcfg.DERPMap{
				Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
					1: {
						RegionID:   1,
						RegionCode: "single",
						RegionName: "Single Node Region",
						Nodes: []*tailcfg.DERPNode{
							{Name: "1a", RegionID: 1, HostName: "derp1a.tailscale.com"},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			shuffleDERPMap(tt.derpMap)
		})
	}
}

func TestShuffleDERPMapWithoutBaseDomain(t *testing.T) {
	conf.Reset()
	resetDerpRandomForTesting()

	derpMap := &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			1: {
				RegionID:   1,
				RegionCode: "test",
				RegionName: "Test Region",
				Nodes: []*tailcfg.DERPNode{
					{Name: "1a", RegionID: 1, HostName: "derp1a.test.com"},
					{Name: "1b", RegionID: 1, HostName: "derp1b.test.com"},
					{Name: "1c", RegionID: 1, HostName: "derp1c.test.com"},
					{Name: "1d", RegionID: 1, HostName: "derp1d.test.com"},
				},
			},
		},
	}

	original := derpMap.View().AsStruct()
	shuffleDERPMap(derpMap)

	if len(derpMap.Regions) != 1 || len(derpMap.Regions[1].Nodes) != 4 {
		t.Error("Shuffle corrupted DERP map structure")
	}

	originalNodes := make(map[string]bool)
	for _, node := range original.Regions[1].Nodes {
		originalNodes[node.Name] = true
	}

	shuffledNodes := make(map[string]bool)
	for _, node := range derpMap.Regions[1].Nodes {
		shuffledNodes[node.Name] = true
	}

	if diff := cmp.Diff(originalNodes, shuffledNodes); diff != "" {
		t.Errorf("Shuffle changed node set (-original +shuffled):\n%s", diff)
	}
}

// TestBuildSanitizesRegions proves a fetched map with a null relay, a
// relay under the wrong region id, a duplicate name and a null region
// builds into one the clients can rely on.
// TestEmbeddedRegionSTUN proves the embedded region carries the STUN port
// while STUN is on and a negative port, which tells clients not to ask,
// while it is off.
func TestEmbeddedRegionSTUN(t *testing.T) {
	t.Parallel()

	settings := types.DERPServerSettings{
		Enabled:     true,
		RegionID:    999,
		RegionCode:  "slopscale",
		STUNEnabled: true,
		STUNAddr:    "0.0.0.0:3479",
	}

	region, err := EmbeddedRegion(t.Context(), "https://derp.example", settings)
	require.NoError(t, err)
	require.Len(t, region.Nodes, 1)
	assert.Equal(t, 3479, region.Nodes[0].STUNPort)

	settings.STUNEnabled = false
	settings.STUNAddr = ""

	region, err = EmbeddedRegion(t.Context(), "https://derp.example", settings)
	require.NoError(t, err)
	require.Len(t, region.Nodes, 1)
	assert.Equal(t, -1, region.Nodes[0].STUNPort, "no STUN port is published while STUN is off")
	assert.Equal(t, 443, region.Nodes[0].DERPPort)
}

func TestBuildSanitizesRegions(t *testing.T) {
	t.Parallel()

	built := Build(&tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
		10: {RegionID: 99, RegionCode: "nyc", Nodes: []*tailcfg.DERPNode{
			nil,
			{Name: "a", RegionID: 99, HostName: "a.example"},
			{Name: "a", HostName: "dup.example"},
			{Name: "", HostName: "unnamed.example"},
		}},
		11: {RegionID: 11, RegionCode: "empty", Nodes: []*tailcfg.DERPNode{nil}},
		12: nil,
	}})

	require.Len(t, built.Regions, 2, "the null region is dropped, the one without relays stays")
	assert.Empty(t, built.Regions[11].Nodes)

	region := built.Regions[10]
	require.NotNil(t, region)
	assert.Equal(t, tailcfg.DERPRegionID(10), region.RegionID, "the region carries the map's id")
	require.Len(t, region.Nodes, 1)
	assert.Equal(t, "a.example", region.Nodes[0].HostName, "the first relay of a name stays")
	assert.Equal(t, tailcfg.DERPRegionID(10), region.Nodes[0].RegionID)
}

func TestHasRelay(t *testing.T) {
	t.Parallel()

	assert.False(t, HasRelay(nil))
	assert.False(t, HasRelay(&tailcfg.DERPMap{}))
	assert.False(t, HasRelay(&tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
		1: {Nodes: []*tailcfg.DERPNode{{Name: "s", STUNOnly: true}}},
	}}), "STUN-only relays carry no traffic")
	assert.True(t, HasRelay(&tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
		1: {Nodes: []*tailcfg.DERPNode{{Name: "s", STUNOnly: true}, {Name: "a"}}},
	}}))
}

// TestLoadDERPMapFromURL proves the fetch is bounded: a redirect is refused
// so the map comes from the URL the operator configured, a non-2xx answer is
// an error naming the status, and a body past the limit is cut off instead of
// read forever.
func TestLoadDERPMapFromURL(t *testing.T) {
	// Not parallel: the egress policy is process-wide and the test servers
	// are on loopback, which it refuses by default.
	egress.SetDefault(egress.Policy{AllowLoopback: true})
	t.Cleanup(func() { egress.SetDefault(egress.Policy{}) })

	t.Run("ok", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"Regions":{"900":{"RegionID":900,"RegionCode":"test"}}}`))
		}))
		t.Cleanup(srv.Close)

		derpMap, err := loadDERPMapFromURL(t.Context(), *mustURL(t, srv.URL))
		require.NoError(t, err)
		require.NotNil(t, derpMap.Regions[900])
		assert.Equal(t, "test", derpMap.Regions[900].RegionCode)
	})

	t.Run("refuses a redirect", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"Regions":{}}`))
		}))
		t.Cleanup(target.Close)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusFound)
		}))
		t.Cleanup(srv.Close)

		_, err := loadDERPMapFromURL(t.Context(), *mustURL(t, srv.URL))
		require.ErrorIs(t, err, ErrRedirected)
	})

	t.Run("refuses a non-2xx answer", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		_, err := loadDERPMapFromURL(t.Context(), *mustURL(t, srv.URL))
		require.ErrorIs(t, err, ErrFetchFailed)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("bounds the body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"Padding":"`))

			chunk := strings.Repeat("a", 1<<20)
			for range 6 {
				_, _ = w.Write([]byte(chunk))
			}
		}))
		t.Cleanup(srv.Close)

		_, err := loadDERPMapFromURL(t.Context(), *mustURL(t, srv.URL))
		require.Error(t, err, "the truncated body is not a map")
	})

	t.Run("refuses a blocked address", func(t *testing.T) {
		egress.SetDefault(egress.Policy{})
		t.Cleanup(func() { egress.SetDefault(egress.Policy{AllowLoopback: true}) })

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"Regions":{}}`))
		}))
		t.Cleanup(srv.Close)

		_, err := loadDERPMapFromURL(t.Context(), *mustURL(t, srv.URL))
		require.ErrorIs(t, err, egress.ErrBlocked)
	})
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	require.NoError(t, err)

	return parsed
}
