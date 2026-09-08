package derp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestMergeDERPMapsClonesRegions ensures merged DERP maps own their regions
// rather than aliasing the source pointers, so a later in-place node shuffle
// cannot mutate a shared or previously served map.
func TestMergeDERPMapsClonesRegions(t *testing.T) {
	src := &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			1: {RegionID: 1, Nodes: []*tailcfg.DERPNode{{Name: "a"}, {Name: "b"}}},
		},
	}

	merged := mergeDERPMaps([]*tailcfg.DERPMap{src})

	assert.NotSame(t, src.Regions[1], merged.Regions[1],
		"merged region must not alias the source region pointer")

	merged.Regions[1].Nodes[0] = &tailcfg.DERPNode{Name: "mutated"}
	assert.Equal(t, "a", src.Regions[1].Nodes[0].Name,
		"source region was mutated through a shared pointer")
}

// TestMergeDERPMapsHomeParams covers the region scores: they survive the
// merge, the last map wins per region, and scores the client would ignore
// are dropped so a map without any keeps HomeParams nil.
func TestMergeDERPMapsHomeParams(t *testing.T) {
	first := &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{1: {RegionID: 1}, 2: {RegionID: 2}},
		HomeParams: &tailcfg.DERPHomeParams{
			RegionScore: map[tailcfg.DERPRegionID]float64{1: 0.5, 2: 2},
		},
	}
	second := &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{900: {RegionID: 900}},
		HomeParams: &tailcfg.DERPHomeParams{
			RegionScore: map[tailcfg.DERPRegionID]float64{2: 1.5, 900: 0.25, 901: 0},
		},
	}

	merged := mergeDERPMaps([]*tailcfg.DERPMap{first, second})

	if assert.NotNil(t, merged.HomeParams) {
		assert.Equal(t, map[tailcfg.DERPRegionID]float64{1: 0.5, 2: 1.5, 900: 0.25}, merged.HomeParams.RegionScore)
	}

	merged.HomeParams.RegionScore[1] = 9
	assert.InDelta(t, 0.5, first.HomeParams.RegionScore[1], 0, "the source map is not shared")

	assert.Nil(t, mergeDERPMaps([]*tailcfg.DERPMap{{Regions: first.Regions}}).HomeParams)
	assert.Nil(t, mergeDERPMaps([]*tailcfg.DERPMap{{
		HomeParams: &tailcfg.DERPHomeParams{RegionScore: map[tailcfg.DERPRegionID]float64{1: -1}},
	}}).HomeParams, "an ignored score alone does not add HomeParams")
}

// TestLoadDERPMapFromPathHomeParams pins the YAML keys an operator writes
// for the scores, lowercase field names like the rest of the file.
func TestLoadDERPMapFromPathHomeParams(t *testing.T) {
	path := filepath.Join(t.TempDir(), "derp.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
regions:
  900:
    regionid: 900
    regioncode: custom
homeparams:
  regionscore:
    900: 0.5
    1: 3
`), 0o600))

	dm, err := loadDERPMapFromPath(path)
	require.NoError(t, err)
	require.NotNil(t, dm.HomeParams)
	assert.Equal(t, map[tailcfg.DERPRegionID]float64{900: 0.5, 1: 3}, dm.HomeParams.RegionScore)
}
