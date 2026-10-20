package apiv2

import (
	"cmp"
	"context"
	"net/http"
	"time"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerSettings)
}

// hoursPerDay converts Node.Expiry (a duration) into days for
// devicesKeyDurationDays.
const hoursPerDay = 24

// TailnetSettings is the Tailscale tailnet-settings response. Slopscale's config
// is file-based and mostly not runtime-mutable, so only a few fields carry a
// real value; the rest report the default "off".
type TailnetSettings struct {
	ACLsExternallyManagedOn bool   `json:"aclsExternallyManagedOn"`
	ACLsExternalLink        string `json:"aclsExternalLink"`

	DevicesApprovalOn      bool `json:"devicesApprovalOn"`
	DevicesAutoUpdatesOn   bool `json:"devicesAutoUpdatesOn"`
	DevicesKeyDurationDays int  `json:"devicesKeyDurationDays"`

	UsersApprovalOn                        bool   `json:"usersApprovalOn"`
	UsersRoleAllowedToJoinExternalTailnets string `json:"usersRoleAllowedToJoinExternalTailnets"`

	NetworkFlowLoggingOn bool `json:"networkFlowLoggingOn"`
	// RegionalRoutingOn is the older form of RouteSelection, which the
	// hosted API still returns: true only for regional-routing.
	RegionalRoutingOn           bool `json:"regionalRoutingOn"`
	PostureIdentityCollectionOn bool `json:"postureIdentityCollectionOn"`
	HTTPSEnabled                bool `json:"httpsEnabled"`
	// RouteSelection is how machines choose among routers, app connectors
	// and service hosts that serve the same prefix.
	RouteSelection string `enum:"active-passive-failover,regional-routing,regional-routing-failover,magicroute" json:"routeSelection"` //nolint:lll // struct tag
}

type (
	getSettingsInput struct {
		Tailnet string `path:"tailnet"`
	}
	settingsOutput struct {
		Body TailnetSettings
	}
	patchSettingsInput struct {
		Tailnet string `path:"tailnet"`
		Body    UpdateTailnetSettings
	}
)

// UpdateTailnetSettings carries the settings a PATCH may change; absent
// fields keep their value. Switching an approval off approves everything
// that was waiting.
//
// It takes every field of Tailscale's request, because the Tailscale Go
// client sends aclsExternallyManagedOn and aclsExternalLink on every write
// (null when unset) and the Terraform provider writes whatever its
// configuration names. The settings slopscale does not let the API change
// accept null or the value GET reports; anything else is a 400 naming the
// field.
type UpdateTailnetSettings struct {
	ACLsExternallyManagedOn *bool   `json:"aclsExternallyManagedOn,omitempty" nullable:"true"`
	ACLsExternalLink        *string `json:"aclsExternalLink,omitempty"        nullable:"true"`

	DevicesApprovalOn      *bool `json:"devicesApprovalOn,omitempty"`
	DevicesAutoUpdatesOn   *bool `json:"devicesAutoUpdatesOn,omitempty"`
	DevicesKeyDurationDays *int  `doc:"0 leaves the config file and the client in charge." json:"devicesKeyDurationDays,omitempty" maximum:"365" minimum:"0"` //nolint:lll // struct tag

	UsersApprovalOn                        *bool   `json:"usersApprovalOn,omitempty"`
	UsersRoleAllowedToJoinExternalTailnets *string `enum:"none,member,admin"         json:"usersRoleAllowedToJoinExternalTailnets,omitempty"` //nolint:lll // struct tag

	NetworkFlowLoggingOn *bool `json:"networkFlowLoggingOn,omitempty"`
	// RegionalRoutingOn is the older form of RouteSelection; a request
	// carries one of the two, never both.
	RegionalRoutingOn           *bool `json:"regionalRoutingOn,omitempty"`
	PostureIdentityCollectionOn *bool `json:"postureIdentityCollectionOn,omitempty"`
	HTTPSEnabled                *bool `json:"httpsEnabled,omitempty"`
	// RouteSelection is how machines choose among routers that serve the
	// same prefix.
	RouteSelection *string `enum:"active-passive-failover,regional-routing,regional-routing-failover,magicroute" json:"routeSelection,omitempty"` //nolint:lll // struct tag
}

// errBothRouteSelections is the hosted API's rule: regionalRoutingOn and
// routeSelection set the same thing, so a request names one of them.
var errBothRouteSelections = settingsBadRequest(
	"regionalRoutingOn and routeSelection cannot both be set; send routeSelection",
)

// settingsBadRequest is a 400 whose message is detail. It is built as a
// value rather than through huma's helpers so the status error is ours
// to return.
func settingsBadRequest(detail string) *huma.ErrorModel {
	return &huma.ErrorModel{
		Status: http.StatusBadRequest,
		Title:  http.StatusText(http.StatusBadRequest),
		Detail: detail,
	}
}

// fixedSetting is a setting the API cannot change, with whether the
// request leaves it as it is and why it is fixed.
type fixedSetting struct {
	field string
	same  bool
	why   string
}

// checkFixedSettings refuses a request that asks for a value of a setting
// slopscale does not let the API change. A field that is absent, null or
// equal to what GET reports passes, so a client that writes back what it
// read, or sends every field, is accepted.
func checkFixedSettings(body UpdateTailnetSettings, current TailnetSettings) error {
	if body.RegionalRoutingOn != nil && body.RouteSelection != nil {
		return errBothRouteSelections
	}

	fixed := []fixedSetting{
		{
			"aclsExternallyManagedOn",
			unchanged(body.ACLsExternallyManagedOn, current.ACLsExternallyManagedOn),
			"it follows policy.mode in the config file",
		},
		{"aclsExternalLink", unchanged(body.ACLsExternalLink, current.ACLsExternalLink), "Slopscale has no such link"},
		{
			"devicesAutoUpdatesOn",
			unchanged(body.DevicesAutoUpdatesOn, current.DevicesAutoUpdatesOn),
			"Slopscale does not push client updates",
		},
		{
			"usersRoleAllowedToJoinExternalTailnets",
			unchanged(body.UsersRoleAllowedToJoinExternalTailnets, current.UsersRoleAllowedToJoinExternalTailnets),
			"Slopscale serves a single tailnet",
		},
		{
			"networkFlowLoggingOn",
			unchanged(body.NetworkFlowLoggingOn, current.NetworkFlowLoggingOn),
			"Slopscale has no network flow logging to switch on",
		},
		{
			"httpsEnabled",
			unchanged(body.HTTPSEnabled, current.HTTPSEnabled),
			"certificates follow the tls settings in the config file",
		},
	}

	for _, f := range fixed {
		if !f.same {
			return settingsBadRequest(f.field + " cannot be changed: " + f.why)
		}
	}

	return nil
}

// unchanged reports whether a request field leaves a setting as it is:
// absent, null, or the current value.
func unchanged[T comparable](requested *T, current T) bool {
	return requested == nil || *requested == current
}

// requestedRouteSelection is the route selection a request asks for, nil
// when it asks for none. The legacy regionalRoutingOn maps as on the
// hosted API: true is regional-routing, false active-passive-failover.
func requestedRouteSelection(body UpdateTailnetSettings) *types.RouteSelection {
	switch {
	case body.RouteSelection != nil:
		return new(types.RouteSelection(*body.RouteSelection))
	case body.RegionalRoutingOn == nil:
		return nil
	case *body.RegionalRoutingOn:
		return new(types.RouteSelectionRegional)
	default:
		return new(types.RouteSelectionActivePassive)
	}
}

func tailnetSettings(b Backend) TailnetSettings {
	cfg := b.Cfg
	current := b.State.Settings()
	keyDuration := cmp.Or(current.KeyExpiry, cfg.Node.Expiry)
	selection := current.RouteSelection.OrDefault()

	return TailnetSettings{
		// File-mode policy is genuinely externally managed (read-only via API).
		ACLsExternallyManagedOn: cfg.Policy.Mode == types.PolicyModeFile,
		DevicesApprovalOn:       current.DevicesApprovalOn,
		DevicesKeyDurationDays:  int(keyDuration / (hoursPerDay * time.Hour)),
		// Tailscale's toggle for machine certificates, not the server's own TLS.
		HTTPSEnabled:                           cfg.HTTPSCerts.Enabled,
		PostureIdentityCollectionOn:            current.PostureIdentityOn,
		RegionalRoutingOn:                      selection == types.RouteSelectionRegional,
		RouteSelection:                         string(selection),
		UsersApprovalOn:                        current.UsersApprovalOn,
		UsersRoleAllowedToJoinExternalTailnets: "none",
	}
}

func registerSettings(api huma.API, b Backend) {
	settingsTags := []string{"TailnetSettings", "Tailscale compat"}

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getTailnetSettings",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/settings",
		Summary:     "Get tailnet settings",
		Tags:        settingsTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.FeatureSettingsRead), func(_ context.Context, in *getSettingsInput) (*settingsOutput, error) {
		err := b.requireTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		return &settingsOutput{Body: tailnetSettings(b)}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "updateTailnetSettings",
		Method:      http.MethodPatch,
		Path:        "/api/v2/tailnet/{tailnet}/settings",
		Summary:     "Update tailnet settings",
		Description: "Changes devicesApprovalOn, usersApprovalOn, postureIdentityCollectionOn, " +
			"devicesKeyDurationDays and routeSelection (or the older regionalRoutingOn: true is " +
			"regional-routing, false active-passive-failover; a request carries one of the two). The " +
			"other settings are file-based in Slopscale: a request may carry them as null or with the " +
			"value GET reports, anything else is a 400 naming the field.",
		Tags:     settingsTags,
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.FeatureSettings), "settings.set", "", ""), func(
		ctx context.Context, in *patchSettingsInput,
	) (*settingsOutput, error) {
		err := b.requireTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		err = checkFixedSettings(in.Body, tailnetSettings(b))
		if err != nil {
			return nil, err
		}

		updates := []struct {
			key   types.SettingKey
			value *bool
		}{
			{types.SettingDevicesApprovalOn, in.Body.DevicesApprovalOn},
			{types.SettingUsersApprovalOn, in.Body.UsersApprovalOn},
			{types.SettingPostureIdentityOn, in.Body.PostureIdentityCollectionOn},
		}

		for _, u := range updates {
			if u.value == nil {
				continue
			}

			c, err := b.State.SetSetting(u.key, *u.value)
			if err != nil {
				return nil, mapError("updating tailnet settings", err)
			}

			audit.Detail(ctx, string(u.key), *u.value)

			b.Change(c)
		}

		if days := in.Body.DevicesKeyDurationDays; days != nil {
			err := b.State.SetKeyExpiry(time.Duration(*days) * hoursPerDay * time.Hour)
			if err != nil {
				return nil, mapError("updating tailnet settings", err)
			}

			audit.Detail(ctx, string(types.SettingKeyExpiry), *days)
		}

		if selection := requestedRouteSelection(in.Body); selection != nil {
			c, err := b.State.SetRouteSelection(*selection)
			if err != nil {
				return nil, mapError("updating tailnet settings", err)
			}

			audit.Detail(ctx, string(types.SettingRouteSelection), *selection)

			b.Change(c)
		}

		return &settingsOutput{Body: tailnetSettings(b)}, nil
	})
}
