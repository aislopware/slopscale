package derp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
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

// TestMergeDERPMapsNullRemovesRegion pins the docs' recipe: a later map
// setting a region to null drops it from the result.
func TestMergeDERPMapsNullRemovesRegion(t *testing.T) {
	base := &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			1: {RegionID: 1, RegionCode: "nyc"},
			2: {RegionID: 2, RegionCode: "sfo"},
		},
	}
	drop := &tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{1: nil}}

	merged := mergeDERPMaps([]*tailcfg.DERPMap{base, drop})

	assert.NotContains(t, merged.Regions, tailcfg.DERPRegionID(1))
	assert.Contains(t, merged.Regions, tailcfg.DERPRegionID(2))
}

// TestLoadDERPMapFromPath covers each file format and the silent-empty trap:
// keys the decoder doesn't know decode to nothing.
func TestLoadDERPMapFromPath(t *testing.T) {
	want := &tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			999: {
				RegionID:   999,
				RegionCode: "test",
				Nodes: []*tailcfg.DERPNode{
					{Name: "999a", RegionID: 999, HostName: "derp.test", DERPPort: 443, InsecureForTests: true},
				},
			},
		},
	}

	tailcfgJSON, err := json.Marshal(want)
	require.NoError(t, err)

	yamlMap := `regions:
  999:
    regionid: 999
    regioncode: test
    nodes:
      - name: 999a
        regionid: 999
        hostname: derp.test
        derpport: 443
        insecurefortests: true
`

	tests := []struct {
		name    string
		file    string
		content string
		wantErr bool
	}{
		{name: "yaml", file: "derp.yaml", content: yamlMap},
		{name: "yml", file: "derp.yml", content: yamlMap},
		{
			name: "flow-style yaml",
			file: "derp.yaml",
			content: "{regions: {999: {regionid: 999, regioncode: test, nodes: [{name: 999a, regionid: 999, " +
				"hostname: derp.test, derpport: 443, insecurefortests: true}]}}}",
		},
		{name: "tailcfg json", file: "derp.json", content: string(tailcfgJSON)},
		{name: "tailcfg hujson", file: "derp.hujson", content: "// test map\n" + string(tailcfgJSON)},
		{name: "json as yaml decodes to nothing", file: "derp.yaml", content: string(tailcfgJSON), wantErr: true},
		{name: "empty", file: "derp.yaml", content: "", wantErr: true},
		{name: "no extension", file: "derp", content: yamlMap, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.file)
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))

			got, err := loadDERPMapFromPath(path)
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("loadDERPMapFromPath() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestLoadDERPMapFromPathOnlyScoresOrRemovals keeps the two documented
// files that add no region loading: scores alone, and a null region alone.
func TestLoadDERPMapFromPathOnlyScoresOrRemovals(t *testing.T) {
	for name, content := range map[string]string{
		"scores.yaml": "homeparams:\n  regionscore:\n    1: 3\n",
		"drop.yaml":   "regions:\n  1: null\n",
	} {
		path := filepath.Join(t.TempDir(), name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		_, err := loadDERPMapFromPath(path)
		require.NoError(t, err, name)
	}
}
