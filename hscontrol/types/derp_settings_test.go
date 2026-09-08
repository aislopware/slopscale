package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

func validDERPSettings() DERPSettings {
	return DERPSettings{
		URLs:            []string{TailscaleDERPMapURL},
		AutoUpdate:      true,
		UpdateFrequency: time.Hour,
		Regions: []DERPCustomRegion{{
			ID:   20,
			Code: "sgp",
			Nodes: []DERPCustomNode{{
				HostName: "sgp.derp.example",
				IPv4:     "203.0.113.5",
				IPv6:     "none",
			}},
		}},
		Server: DERPServerSettings{
			Enabled:    true,
			RegionID:   999,
			RegionCode: "headscale",
			STUNAddr:   "0.0.0.0:3478",
		},
	}
}

func TestDERPSettingsNormalize(t *testing.T) {
	t.Parallel()

	s := DERPSettings{
		URLs: []string{" https://a.example/map ", "", "  "},
		Regions: []DERPCustomRegion{{
			ID:    1,
			Code:  " sgp ",
			Nodes: []DERPCustomNode{{HostName: " sgp.derp.example "}},
		}},
		Server: DERPServerSettings{RegionCode: " hs "},
	}

	n := s.Normalize()

	assert.Equal(t, []string{"https://a.example/map"}, n.URLs)
	assert.Equal(t, "sgp", n.Regions[0].Code)
	assert.Equal(t, "sgp", n.Regions[0].Name, "an empty name takes the code")
	assert.Equal(t, "sgp.derp.example", n.Regions[0].Nodes[0].HostName)
	assert.Equal(t, "sgp.derp.example", n.Regions[0].Nodes[0].Name, "an empty relay name takes the host")
	assert.Equal(t, "hs", n.Server.RegionCode)
	assert.Equal(t, "hs", n.Server.RegionName)

	assert.Equal(t, []string{" https://a.example/map ", "", "  "}, s.URLs, "the input is not changed")
	assert.Empty(t, DERPSettings{}.Normalize().URLs)
	assert.NotNil(t, DERPSettings{}.Normalize().URLs)
}

func TestDERPSettingsValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, validDERPSettings().Normalize().Validate())

	tests := []struct {
		name   string
		mutate func(*DERPSettings)
		want   error
	}{
		{"ftp url", func(s *DERPSettings) { s.URLs = []string{"ftp://x"} }, ErrDERPURLInvalid},
		{"url without host", func(s *DERPSettings) { s.URLs = []string{"https://"} }, ErrDERPURLInvalid},
		{
			"too frequent",
			func(s *DERPSettings) { s.UpdateFrequency = 10 * time.Second },
			ErrDERPUpdateFrequencyTooShort,
		},
		{
			"frequency ignored while auto update is off",
			func(s *DERPSettings) { s.AutoUpdate = false; s.UpdateFrequency = 0 },
			nil,
		},
		{"region id zero", func(s *DERPSettings) { s.Regions[0].ID = 0 }, ErrDERPRegionIDInvalid},
		{"region without code", func(s *DERPSettings) { s.Regions[0].Code = "" }, ErrDERPRegionCodeEmpty},
		{"region without relays", func(s *DERPSettings) { s.Regions[0].Nodes = nil }, ErrDERPRegionNoNodes},
		{
			"relay without host",
			func(s *DERPSettings) { s.Regions[0].Nodes[0].HostName = "" },
			ErrDERPNodeHostEmpty,
		},
		{
			"two relays share a name",
			func(s *DERPSettings) {
				s.Regions[0].Nodes = append(s.Regions[0].Nodes, DERPCustomNode{HostName: "sgp.derp.example"})
			},
			ErrDERPNodeNameTaken,
		},
		{
			"relay ipv4 not v4",
			func(s *DERPSettings) { s.Regions[0].Nodes[0].IPv4 = "2001:db8::1" },
			ErrDERPNodeIPInvalid,
		},
		{"relay ipv6 garbage", func(s *DERPSettings) { s.Regions[0].Nodes[0].IPv6 = "nope" }, ErrDERPNodeIPInvalid},
		{"relay port too big", func(s *DERPSettings) { s.Regions[0].Nodes[0].DERPPort = 70000 }, ErrDERPPortInvalid},
		{
			"two regions share an id",
			func(s *DERPSettings) { s.Regions = append(s.Regions, s.Regions[0]) },
			ErrDERPRegionIDTaken,
		},
		{"embedded shares a custom id", func(s *DERPSettings) { s.Server.RegionID = 20 }, ErrDERPRegionIDTaken},
		{"embedded without code", func(s *DERPSettings) { s.Server.RegionCode = "" }, ErrDERPRegionCodeEmpty},
		{"embedded region id zero", func(s *DERPSettings) { s.Server.RegionID = 0 }, ErrDERPRegionIDInvalid},
		{"embedded without stun", func(s *DERPSettings) { s.Server.STUNAddr = "" }, ErrDERPSTUNAddrInvalid},
		{"embedded stun without port", func(s *DERPSettings) { s.Server.STUNAddr = "0.0.0.0" }, ErrDERPSTUNAddrInvalid},
		{"embedded stun port 0 is allowed", func(s *DERPSettings) { s.Server.STUNAddr = "127.0.0.1:0" }, nil},
		{"embedded ipv4 not v4", func(s *DERPSettings) { s.Server.IPv4 = "::1" }, ErrDERPServerIPInvalid},
		{"embedded ipv6 not v6", func(s *DERPSettings) { s.Server.IPv6 = "1.2.3.4" }, ErrDERPServerIPInvalid},
		{
			"embedded off skips its checks",
			func(s *DERPSettings) { s.Server = DERPServerSettings{Enabled: false} },
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := validDERPSettings()
			tt.mutate(&s)

			err := s.Normalize().Validate()
			if tt.want == nil {
				assert.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.want)
			assert.ErrorIs(t, err, ErrDERPSettingsInvalid)
		})
	}
}

func TestDERPSettingsRegionsMap(t *testing.T) {
	t.Parallel()

	s := validDERPSettings().Normalize()
	s.Regions[0].Nodes[0].DERPPort = 8443
	s.Regions[0].Nodes[0].STUNOnly = true

	dm := s.RegionsMap()
	require.Len(t, dm.Regions, 1)

	region := dm.Regions[20]
	require.NotNil(t, region)
	assert.Equal(t, tailcfg.DERPRegionID(20), region.RegionID)
	assert.Equal(t, "sgp", region.RegionCode)
	require.Len(t, region.Nodes, 1)
	assert.Equal(t, tailcfg.DERPRegionID(20), region.Nodes[0].RegionID)
	assert.Equal(t, "sgp.derp.example", region.Nodes[0].Name)
	assert.Equal(t, 8443, region.Nodes[0].DERPPort)
	assert.True(t, region.Nodes[0].STUNOnly)
	assert.Equal(t, "none", region.Nodes[0].IPv6)
}

func TestDERPConfigSettings(t *testing.T) {
	t.Parallel()

	cfg := DERPConfig{
		ServerEnabled:    true,
		ServerRegionID:   999,
		ServerRegionCode: "hs",
		STUNAddr:         "0.0.0.0:3478",
		AutoUpdate:       true,
		UpdateFrequency:  3 * time.Hour,
	}

	s := cfg.Settings()
	assert.Empty(t, s.URLs)
	assert.NotNil(t, s.URLs)
	assert.NotNil(t, s.Regions)
	assert.True(t, s.Server.Enabled)
	assert.Equal(t, tailcfg.DERPRegionID(999), s.Server.RegionID)
	assert.Equal(t, 3*time.Hour, s.UpdateFrequency)

	clone := s.Clone()
	clone.Server.Enabled = false
	assert.True(t, s.Server.Enabled, "Clone is a copy")
}
