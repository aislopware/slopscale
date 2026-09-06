package derp

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/viper"
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
			viper.Set("dns.base_domain", tt.baseDomain)

			defer viper.Reset()

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
	viper.Reset()
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
