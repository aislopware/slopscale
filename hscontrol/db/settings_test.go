package db

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestDNSSettingsRoundTrip proves the DNS row is stored as JSON beside
// the boolean switches without upsetting LoadSettings, and that deleting
// it returns to "no override".
func TestDNSSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	got, err := db.LoadDNSSettings()
	require.NoError(t, err)
	assert.Nil(t, got, "a fresh database has no override")

	want := types.DNSSettings{
		Nameservers:      []string{"9.9.9.9"},
		OverrideLocalDNS: true,
		SplitNameservers: map[string][]string{"corp.example": {"10.0.0.1"}},
		SearchDomains:    []string{"lab.example"},
		ExtraRecords:     []tailcfg.DNSRecord{{Name: "a.corp", Type: "A", Value: "10.0.0.5"}},
	}
	require.NoError(t, db.SaveDNSSettings(want))
	require.NoError(t, db.SaveSetting(types.SettingDevicesApprovalOn, true))

	got, err = db.LoadDNSSettings()
	require.NoError(t, err)
	assert.Equal(t, &want, got)

	settings, err := db.LoadSettings()
	require.NoError(t, err, "the JSON row must not be parsed as a switch")
	assert.True(t, settings.DevicesApprovalOn)

	want.Nameservers = []string{"1.1.1.1"}
	require.NoError(t, db.SaveDNSSettings(want), "saving again updates the row")

	got, err = db.LoadDNSSettings()
	require.NoError(t, err)
	assert.Equal(t, []string{"1.1.1.1"}, got.Nameservers)

	require.NoError(t, db.DeleteDNSSettings())
	require.NoError(t, db.DeleteDNSSettings(), "deleting twice is fine")

	got, err = db.LoadDNSSettings()
	require.NoError(t, err)
	assert.Nil(t, got)
}
