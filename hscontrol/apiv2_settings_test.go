package hscontrol

import (
	"encoding/json"
	"maps"
	"net/http"
	"testing"
	"time"

	apiv2 "github.com/aislopware/slopscale/hscontrol/api/v2"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settingsAPIWithConfig registers the v2 API over a Backend carrying cfg and a
// zero State — the settings GET reads only b.Cfg, so the other handlers (which
// would need State) are simply never hit.
func settingsAPIWithConfig(t *testing.T, cfg *types.Config) humatest.TestAPI {
	t.Helper()

	_, api := humatest.New(t, apiv2.Config())
	apiv2.Register(api, apiv2.Backend{Cfg: cfg})

	return api
}

func getSettings(t *testing.T, api humatest.TestAPI) apiv2.TailnetSettings {
	t.Helper()

	resp := api.Get("/api/v2/tailnet/-/settings")
	require.Equalf(t, http.StatusOK, resp.Code, "body: %s", resp.Body)

	var s apiv2.TailnetSettings
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &s))

	return s
}

// TestAPIv2SettingsComputedFields validates the GET mapping for each field that
// is computed from config — the branches the default-config roundtrip never
// exercises. Expectations live in struct fields, not name branches.
func TestAPIv2SettingsComputedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		cfg                 types.Config
		wantHTTPS           bool
		wantKeyDurationDays int
		wantACLsExtManaged  bool
	}{
		{
			name: "defaults",
			cfg:  types.Config{},
		},
		{
			name:      "https via machine certificates",
			cfg:       types.Config{HTTPSCerts: types.HTTPSCertsConfig{Enabled: true}},
			wantHTTPS: true,
		},
		{
			// httpsEnabled is the machine certificate toggle; the server's
			// own TLS does not let machines get certificates.
			name: "server tls is not https",
			cfg:  types.Config{TLS: types.TLSConfig{CertPath: "/x/cert.pem"}},
		},
		{
			name:                "key duration 7d",
			cfg:                 types.Config{Node: types.NodeConfig{Expiry: 7 * 24 * time.Hour}},
			wantKeyDurationDays: 7,
		},
		{
			name:                "key duration 90d",
			cfg:                 types.Config{Node: types.NodeConfig{Expiry: 90 * 24 * time.Hour}},
			wantKeyDurationDays: 90,
		},
		{
			name:                "key duration truncates",
			cfg:                 types.Config{Node: types.NodeConfig{Expiry: 36 * time.Hour}},
			wantKeyDurationDays: 1,
		},
		{
			name:               "acls externally managed in file mode",
			cfg:                types.Config{Policy: types.PolicyConfig{Mode: types.PolicyModeFile}},
			wantACLsExtManaged: true,
		},
		{
			name: "acls db mode",
			cfg:  types.Config{Policy: types.PolicyConfig{Mode: types.PolicyModeDB}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := getSettings(t, settingsAPIWithConfig(t, &tt.cfg))

			assert.Equal(t, tt.wantHTTPS, s.HTTPSEnabled)
			assert.Equal(t, tt.wantKeyDurationDays, s.DevicesKeyDurationDays)
			assert.Equal(t, tt.wantACLsExtManaged, s.ACLsExternallyManagedOn)
			assert.Equal(t, "none", s.UsersRoleAllowedToJoinExternalTailnets)
		})
	}
}

// TestAPIv2SettingsConstantOffFields pins the hardcoded-off fields:
// even with every config knob set, they must not pick up signal.
func TestAPIv2SettingsConstantOffFields(t *testing.T) {
	t.Parallel()

	cfg := types.Config{
		TLS:        types.TLSConfig{CertPath: "/x/cert.pem"},
		HTTPSCerts: types.HTTPSCertsConfig{Enabled: true},
		Node:       types.NodeConfig{Expiry: 90 * 24 * time.Hour},
		Policy:     types.PolicyConfig{Mode: types.PolicyModeFile},
	}

	s := getSettings(t, settingsAPIWithConfig(t, &cfg))

	// Only the four computed fields reflect cfg; route selection is always
	// regional; everything else stays off.
	want := apiv2.TailnetSettings{
		ACLsExternallyManagedOn:                true,
		DevicesKeyDurationDays:                 90,
		HTTPSEnabled:                           true,
		UsersRoleAllowedToJoinExternalTailnets: "none",
		RegionalRoutingOn:                      true,
		RouteSelection:                         "regional-routing",
	}
	if diff := cmp.Diff(want, s); diff != "" {
		t.Errorf("settings mismatch (-want +got):\n%s", diff)
	}
}

// TestAPIv2SettingsPatchUnsupported confirms writes are rejected and inert.
func TestAPIv2SettingsPatch(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	api := registerAPIV2(t, app)

	require.False(t, getSettings(t, api).DevicesApprovalOn)
	require.False(t, getSettings(t, api).UsersApprovalOn)

	patch := api.Patch("/api/v2/tailnet/-/settings", map[string]any{"devicesApprovalOn": true})
	require.Equalf(t, http.StatusOK, patch.Code, "body: %s", patch.Body)

	got := getSettings(t, api)
	assert.True(t, got.DevicesApprovalOn)
	assert.False(t, got.UsersApprovalOn, "absent fields keep their value")

	patch = api.Patch("/api/v2/tailnet/-/settings", map[string]any{"usersApprovalOn": true, "devicesApprovalOn": false})
	require.Equalf(t, http.StatusOK, patch.Code, "body: %s", patch.Body)

	got = getSettings(t, api)
	assert.False(t, got.DevicesApprovalOn)
	assert.True(t, got.UsersApprovalOn)
}

// TestAPIv2SettingsPatchFixedFields pins how a PATCH treats the settings the
// API cannot change: the Tailscale Go client sends aclsExternallyManagedOn and
// aclsExternalLink as null on every write, and Terraform writes what it was
// configured with, so null and the reported value must pass; another value is
// a 400 that names the field, and nothing is applied.
func TestAPIv2SettingsPatchFixedFields(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	api := registerAPIV2(t, app)
	current := getSettings(t, api)

	accepted := []map[string]any{
		{"aclsExternallyManagedOn": nil, "aclsExternalLink": nil, "devicesApprovalOn": true},
		{
			"aclsExternallyManagedOn":                current.ACLsExternallyManagedOn,
			"aclsExternalLink":                       current.ACLsExternalLink,
			"devicesAutoUpdatesOn":                   current.DevicesAutoUpdatesOn,
			"usersRoleAllowedToJoinExternalTailnets": current.UsersRoleAllowedToJoinExternalTailnets,
			"networkFlowLoggingOn":                   current.NetworkFlowLoggingOn,
			"httpsEnabled":                           current.HTTPSEnabled,
			"regionalRoutingOn":                      current.RegionalRoutingOn,
		},
		{"routeSelection": current.RouteSelection},
	}

	for _, body := range accepted {
		resp := api.Patch("/api/v2/tailnet/-/settings", body)
		assert.Equalf(t, http.StatusOK, resp.Code, "%v: %s", body, resp.Body)
	}

	assert.True(t, getSettings(t, api).DevicesApprovalOn, "the null fields did not stop the real change")

	refused := []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"aclsExternallyManagedOn": !current.ACLsExternallyManagedOn}, "aclsExternallyManagedOn"},
		{map[string]any{"aclsExternalLink": "https://example.com/acl"}, "aclsExternalLink"},
		{map[string]any{"devicesAutoUpdatesOn": true}, "devicesAutoUpdatesOn"},
		{map[string]any{"usersRoleAllowedToJoinExternalTailnets": "admin"}, "usersRoleAllowedToJoinExternalTailnets"},
		{map[string]any{"networkFlowLoggingOn": true}, "networkFlowLoggingOn"},
		{map[string]any{"httpsEnabled": !current.HTTPSEnabled}, "httpsEnabled"},
		{
			map[string]any{"regionalRoutingOn": true, "routeSelection": "regional-routing"},
			"regionalRoutingOn and routeSelection",
		},
	}

	for _, tc := range refused {
		body := maps.Clone(tc.body)
		body["devicesApprovalOn"] = false

		resp := api.Patch("/api/v2/tailnet/-/settings", body)
		require.Equalf(t, http.StatusBadRequest, resp.Code, "%v: %s", tc.body, resp.Body)

		var apiErr struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &apiErr))
		assert.Contains(t, apiErr.Message, tc.field)
	}

	assert.True(t, getSettings(t, api).DevicesApprovalOn, "a refused request changes nothing")

	unknown := api.Patch("/api/v2/tailnet/-/settings", map[string]any{"routeSelection": "nearest"})
	assert.Equal(t, http.StatusUnprocessableEntity, unknown.Code, "an unknown route selection is not a value")
}

// TestAPIv2SettingsNonDefaultTailnet404 — the tailnet check runs first, so a
// bad tailnet is 404 on both verbs.
func TestAPIv2SettingsNonDefaultTailnet404(t *testing.T) {
	t.Parallel()

	api := settingsAPIWithConfig(t, &types.Config{})

	assert.Equal(t, http.StatusNotFound, api.Get("/api/v2/tailnet/example.com/settings").Code)
	assert.Equal(t, http.StatusNotFound,
		api.Patch("/api/v2/tailnet/example.com/settings", map[string]any{}).Code)
}
