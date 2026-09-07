package apiv2

import (
	"cmp"
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/api/principal"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerSettings)
}

// hoursPerDay converts Node.Expiry (a duration) into days for
// devicesKeyDurationDays.
const hoursPerDay = 24

// TailnetSettings is the Tailscale tailnet-settings response. Headscale's config
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

	NetworkFlowLoggingOn        bool `json:"networkFlowLoggingOn"`
	RegionalRoutingOn           bool `json:"regionalRoutingOn"`
	PostureIdentityCollectionOn bool `json:"postureIdentityCollectionOn"`
	HTTPSEnabled                bool `json:"httpsEnabled"`
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
// that was waiting. The other Tailscale settings are file-based here and
// are rejected.
type UpdateTailnetSettings struct {
	DevicesApprovalOn           *bool `json:"devicesApprovalOn,omitempty"`
	DevicesKeyDurationDays      *int  `doc:"0 leaves the config file and the client in charge." json:"devicesKeyDurationDays,omitempty" maximum:"365" minimum:"0"` //nolint:lll // struct tag
	UsersApprovalOn             *bool `json:"usersApprovalOn,omitempty"`
	PostureIdentityCollectionOn *bool `json:"postureIdentityCollectionOn,omitempty"`
}

func tailnetSettings(b Backend) TailnetSettings {
	cfg := b.Cfg
	current := b.State.Settings()
	keyDuration := cmp.Or(current.KeyExpiry, cfg.Node.Expiry)

	return TailnetSettings{
		// File-mode policy is genuinely externally managed (read-only via API).
		ACLsExternallyManagedOn:                cfg.Policy.Mode == types.PolicyModeFile,
		DevicesApprovalOn:                      current.DevicesApprovalOn,
		DevicesKeyDurationDays:                 int(keyDuration / (hoursPerDay * time.Hour)),
		HTTPSEnabled:                           cfg.TLS.CertPath != "" || cfg.TLS.LetsEncrypt.Hostname != "",
		PostureIdentityCollectionOn:            current.PostureIdentityOn,
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
		err := requireDefaultTailnet(in.Tailnet)
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
		Description: "Changes devicesApprovalOn, usersApprovalOn and devicesKeyDurationDays; the " +
			"other settings are file-based in Headscale and cannot be changed here.",
		Tags:     settingsTags,
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.FeatureSettings), "settings.set", "", ""), func(
		ctx context.Context, in *patchSettingsInput,
	) (*settingsOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
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

		return &settingsOutput{Body: tailnetSettings(b)}, nil
	})
}
