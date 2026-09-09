package posture

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expr    string
		wantErr error
	}{
		{expr: "node:os == 'macos'"},
		{expr: `node:os == "macos"`},
		{expr: "node:tsVersion >= '1.40'"},
		{expr: "node:os IN ['macos', 'linux']"},
		{expr: "node:os not in ['windows']"},
		{expr: "custom:oncall == true"},
		{expr: "custom:tier > 2"},
		{expr: "custom:attr IS SET"},
		{expr: "custom:attr NOT SET"},
		{expr: "ip:country IN ['VN','SG']"},
		{expr: "ip:address IN ['203.0.113.0/24']"},
		{expr: "", wantErr: ErrEmpty},
		{expr: "os == 'macos'", wantErr: ErrAttribute},
		{expr: "user:name == 'x'", wantErr: ErrUnknownPrefix},
		{expr: "node:os", wantErr: ErrOperator},
		{expr: "node:os ==", wantErr: ErrValue},
		{expr: "node:os == 'macos' extra", wantErr: ErrTrailing},
		{expr: "node:os IN 'macos'", wantErr: ErrListExpected},
		{expr: "node:os == ['macos']", wantErr: ErrScalarWanted},
		{expr: "node:os == 'macos", wantErr: ErrUnterminated},
		{expr: "custom:x > true", wantErr: ErrOrderedValue},
		{expr: "custom:x == macos", wantErr: ErrValue},
		{expr: "custom:x IS SET now", wantErr: ErrTrailing},
		{expr: "falcon:ztaScore >= 80"},
		{expr: "sentinelOne:operationalState == 'unlocked'"},
		{expr: "intune:complianceState == 'compliant'"},
		{expr: "jamfPro:SIPEnabled == 'ENABLED'"},
		{expr: "kandji:mdmEnabled == true"},
		{expr: "kolide:authState == 'Will Block'"},
		{expr: "foo:bar == 1", wantErr: ErrUnknownPrefix},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(tt.expr)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestEval(t *testing.T) {
	t.Parallel()

	attrs := map[string]any{
		"node:os":                      "macos",
		"node:tsVersion":               "1.86.2",
		"node:serialNumber":            []string{"C02ABC", "C02DEF"},
		"custom:oncall":                true,
		"custom:tier":                  float64(3),
		"ip:address":                   "203.0.113.7",
		"ip:country":                   "VN",
		"falcon:ztaScore":              float64(85),
		"sentinelOne:operationalState": "unlocked",
		"intune:complianceState":       "compliant",
		"jamfPro:SIPEnabled":           "ENABLED",
		"kandji:mdmEnabled":            true,
		"kolide:authState":             "Will Block",
	}

	tests := []struct {
		expr string
		want bool
	}{
		{"node:os == 'macos'", true},
		{"node:os == 'linux'", false},
		{"node:os != 'linux'", true},
		{"node:os IN ['linux', 'macos']", true},
		{"node:os NOT IN ['linux', 'macos']", false},
		{"node:tsVersion >= '1.40'", true},
		{"node:tsVersion >= '1.86.2'", true},
		{"node:tsVersion > '1.86.2'", false},
		{"node:tsVersion < '1.100'", true},
		{"node:tsVersion >= '2'", false},
		{"node:serialNumber == 'C02DEF'", true},
		{"node:serialNumber IN ['C02DEF']", true},
		{"node:serialNumber != 'C02DEF'", false},
		{"node:serialNumber NOT IN ['X']", true},
		{"custom:oncall == true", true},
		{"custom:oncall == false", false},
		{"custom:oncall != false", true},
		{"custom:tier > 2", true},
		{"custom:tier >= 4", false},
		{"custom:tier == 3", true},
		{"custom:missing IS SET", false},
		{"custom:missing NOT SET", true},
		{"custom:missing == 'x'", false},
		{"custom:missing != 'x'", false},
		{"custom:oncall IS SET", true},
		{"ip:address IN ['203.0.113.0/24']", true},
		{"ip:address IN ['198.51.100.0/24', '203.0.113.7']", true},
		{"ip:address NOT IN ['203.0.113.0/24']", false},
		{"ip:country IN ['VN', 'SG']", true},
		{"ip:country == 'US'", false},
		{"node:os > 'a'", true},
		{"custom:tier == 'three'", false},
		{"falcon:ztaScore >= 80", true},
		{"falcon:ztaScore < 50", false},
		{"sentinelOne:operationalState == 'unlocked'", true},
		{"intune:complianceState == 'compliant'", true},
		{"jamfPro:SIPEnabled == 'ENABLED'", true},
		{"kandji:mdmEnabled == true", true},
		{"kolide:authState == 'Will Block'", true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()

			e, err := Parse(tt.expr)
			require.NoError(t, err)
			assert.Equal(t, tt.want, e.Eval(attrs))
		})
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, CompareVersions("1.40", "1.40"))
	assert.Equal(t, -1, CompareVersions("1.40", "1.40.1"))
	assert.Equal(t, 1, CompareVersions("1.86.2", "1.9"))
	assert.Equal(t, 1, CompareVersions("1.40.0", "1.40.rc1"))
	assert.Equal(t, 0, CompareVersions("v1.2", "1.2"))
	assert.Equal(t, -1, CompareVersions("13.4", "14"))
}

func TestSchedule(t *testing.T) {
	t.Parallel()

	office := Schedule{
		Days:     []string{"mon", "tue", "wed", "thu", "fri"},
		Start:    "09:00",
		End:      "17:30",
		Timezone: "Asia/Ho_Chi_Minh",
	}
	require.NoError(t, office.Validate())

	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)

	// 2026-09-07 is a Monday.
	monday := time.Date(2026, time.September, 7, 10, 0, 0, 0, hcm)
	assert.True(t, office.Active(monday))
	assert.False(t, office.Active(monday.Add(8*time.Hour)), "after closing")
	assert.False(t, office.Active(monday.AddDate(0, 0, 5)), "saturday")
	assert.Equal(t, time.Date(2026, time.September, 7, 17, 30, 0, 0, hcm), office.NextBoundary(monday))
	assert.Equal(t,
		time.Date(2026, time.September, 8, 0, 0, 0, 0, hcm),
		office.NextBoundary(time.Date(2026, time.September, 7, 18, 0, 0, 0, hcm)))

	night := Schedule{Days: []string{"fri"}, Start: "22:00", End: "02:00"}
	require.NoError(t, night.Validate())
	// 2026-09-11 is a Friday.
	assert.True(t, night.Active(time.Date(2026, time.September, 11, 23, 0, 0, 0, time.UTC)))
	assert.True(
		t,
		night.Active(time.Date(2026, time.September, 12, 1, 0, 0, 0, time.UTC)),
		"saturday morning belongs to friday's window",
	)
	assert.False(t, night.Active(time.Date(2026, time.September, 12, 3, 0, 0, 0, time.UTC)))
	assert.False(
		t,
		night.Active(time.Date(2026, time.September, 11, 1, 0, 0, 0, time.UTC)),
		"friday morning is thursday's window",
	)

	require.ErrorIs(t, Schedule{Start: "09:00", End: "17:00"}.Validate(), ErrScheduleDays)
	require.ErrorIs(t, Schedule{Days: []string{"mon"}, Start: "9am", End: "17:00"}.Validate(), ErrScheduleTime)
	require.ErrorIs(
		t,
		Schedule{Days: []string{"mon"}, Start: "09:00", End: "17:00", Timezone: "Mars/Olympus"}.Validate(),
		ErrScheduleTimezone,
	)
}
