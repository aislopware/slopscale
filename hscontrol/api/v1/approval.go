package apiv1

import (
	"context"
	"net/http"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

// tagSettings groups the settings operations in the OpenAPI document.
const tagSettings = "Settings"

func init() {
	registrations = append(registrations, registerApproval)
}

// SetApprovalRequestBody is the body of approveNode and approveUser. An
// absent body approves.
type SetApprovalRequestBody struct {
	Approved *bool `doc:"false withdraws the approval." json:"approved,omitempty"`
}

// Settings is the tailnet-wide switches; see UpdateSettingsRequestBody.
type Settings struct {
	DevicesApprovalOn bool `doc:"New nodes wait for an administrator unless they register with a preauthorized key." json:"devicesApprovalOn"` //nolint:lll // struct tag
	UsersApprovalOn   bool `doc:"Users created by OIDC login wait for an administrator before registering nodes."    json:"usersApprovalOn"`   //nolint:lll // struct tag
	// PostureIdentityOn lets the server ask clients for their hardware
	// serial numbers, which the policy can then check as node:serialNumber.
	PostureIdentityOn bool `json:"postureIdentityOn"`
	// DeviceAttributesOn lets a machine set its own custom posture
	// attributes over its control connection (the client's
	// alpha-set-device-attrs local API).
	DeviceAttributesOn bool `json:"deviceAttributesOn"`
	// KeyExpiryDays caps how long a node key stays valid after a login;
	// 0 leaves the config file's node.expiry and the client in charge.
	KeyExpiryDays int `json:"keyExpiryDays"`
	// DefaultKeyExpiryDays is the config file's node.expiry, applied when
	// a client asks for nothing and no cap is set; 0 means never.
	DefaultKeyExpiryDays int `json:"defaultKeyExpiryDays"`
	// SSHRecorders are the session recorders every SSH rule without its
	// own streams to, as policy aliases: tags, hosts or addresses.
	SSHRecorders []string `json:"sshRecorders" nullable:"false"`
	// SSHRecordingEnforce refuses a session no default recorder can take.
	SSHRecordingEnforce bool `json:"sshRecordingEnforce"`
	// EmbeddedRecorder reports whether the server runs its own recorder
	// node, which is always a default recorder; see ssh_recording in the
	// configuration.
	EmbeddedRecorder bool `json:"embeddedRecorder"`
	// RouteSelection is how machines choose among routers, app connectors
	// and service hosts that serve the same prefix, by Tailscale's names.
	RouteSelection string `enum:"active-passive-failover,regional-routing,regional-routing-failover,magicroute" json:"routeSelection"` //nolint:lll // struct tag
}

// UpdateSettingsRequestBody carries the switches to change; absent ones
// keep their value. Switching an approval off approves everything waiting.
type UpdateSettingsRequestBody struct {
	DevicesApprovalOn  *bool `json:"devicesApprovalOn,omitempty"`
	UsersApprovalOn    *bool `json:"usersApprovalOn,omitempty"`
	PostureIdentityOn  *bool `json:"postureIdentityOn,omitempty"`
	DeviceAttributesOn *bool `json:"deviceAttributesOn,omitempty"`
	// KeyExpiryDays 0 switches the cap off.
	KeyExpiryDays *int `json:"keyExpiryDays,omitempty" maximum:"365" minimum:"0"`
	// SSHRecorders replaces the default recorders; an empty list clears
	// them. SSHRecordingEnforce is applied alongside when given.
	SSHRecorders        *[]string `json:"sshRecorders,omitempty"`
	SSHRecordingEnforce *bool     `json:"sshRecordingEnforce,omitempty"`
	// RouteSelection changes how machines choose among routers; every
	// machine is sent its peers again.
	RouteSelection *string `enum:"active-passive-failover,regional-routing,regional-routing-failover,magicroute" json:"routeSelection,omitempty"` //nolint:lll // struct tag
}

type (
	approveNodeInput struct {
		NodeID string                  `format:"uint64"  path:"nodeId"`
		Body   *SetApprovalRequestBody `required:"false"`
	}

	approveUserInput struct {
		ID   string                  `format:"uint64"  path:"id"`
		Body *SetApprovalRequestBody `required:"false"`
	}

	settingsOutput struct {
		Body Settings
	}

	updateSettingsInput struct {
		Body UpdateSettingsRequestBody
	}
)

func (b *SetApprovalRequestBody) approved() bool {
	return b == nil || b.Approved == nil || *b.Approved
}

const day = 24 * time.Hour

func settingsFrom(s types.Settings, cfg *types.Config) Settings {
	out := Settings{
		DevicesApprovalOn:   s.DevicesApprovalOn,
		UsersApprovalOn:     s.UsersApprovalOn,
		PostureIdentityOn:   s.PostureIdentityOn,
		DeviceAttributesOn:  s.DeviceAttributesOn,
		KeyExpiryDays:       int(s.KeyExpiry / day),
		SSHRecorders:        append([]string{}, s.SSHRecorders...),
		SSHRecordingEnforce: s.SSHRecordingEnforce,
		RouteSelection:      string(s.RouteSelection.OrDefault()),
	}

	if cfg != nil {
		out.DefaultKeyExpiryDays = int(cfg.Node.Expiry / day)
		out.EmbeddedRecorder = cfg.SSHRecording.Enabled
	}

	return out
}

func registerApproval(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "approveNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/approve",
		Summary:     "Approve node",
		Description: "Admits a node that registered while device approval was on, or withdraws " +
			"the approval again. A node waiting for approval stays registered but has no peers " +
			"and is not visible to any.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCore), "node.approval.set", "node", "nodeId"), func(
		ctx context.Context, in *approveNodeInput,
	) (*nodeOutput, error) {
		return switchNode(ctx, b, in.NodeID, "approved", in.Body.approved(), "approving node",
			b.State.SetNodeApproval)
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "approveUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/user/{id}/approve",
		Summary:     "Approve user",
		Description: "Admits a user created by OIDC login while users approval was on, or " +
			"withdraws the approval again, which also withdraws every node the user owns.",
		Tags:     []string{"Users"},
		Security: bearerAuth,
	}, scope.Users), "user.approval.set", "user", "id"), func(
		ctx context.Context, in *approveUserInput,
	) (*userOutput, error) {
		id, err := parseUserID(in.ID)
		if err != nil {
			return nil, err
		}

		approved := in.Body.approved()
		audit.Detail(ctx, "approved", approved)

		user, userChange, err := b.State.SetUserApproval(id, approved)
		if err != nil {
			return nil, mapError("approving user", err)
		}

		audit.Target(ctx, "", "", user.Name)

		b.Change(userChange)

		out := &userOutput{}
		out.Body.User = userFromView(user.View())

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getSettings",
		Method:      http.MethodGet,
		Path:        "/api/v1/settings",
		Summary:     "Get settings",
		Tags:        []string{tagSettings},
		Security:    bearerAuth,
	}, scope.FeatureSettingsRead), func(_ context.Context, _ *struct{}) (*settingsOutput, error) {
		return &settingsOutput{Body: settingsFrom(b.State.Settings(), b.Cfg)}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateSettings",
		Method:      http.MethodPost,
		Path:        "/api/v1/settings",
		Summary:     "Update settings",
		Description: "Changes the given switches. Switching device or users approval off " +
			"approves every node or user that was waiting; changing the route selection sends " +
			"every machine its peers again.",
		Tags:     []string{tagSettings},
		Security: bearerAuth,
	}, scope.FeatureSettings), "settings.set", "", ""), func(
		ctx context.Context, in *updateSettingsInput,
	) (*settingsOutput, error) {
		updates := []struct {
			key   types.SettingKey
			value *bool
		}{
			{types.SettingDevicesApprovalOn, in.Body.DevicesApprovalOn},
			{types.SettingUsersApprovalOn, in.Body.UsersApprovalOn},
			{types.SettingPostureIdentityOn, in.Body.PostureIdentityOn},
			{types.SettingDeviceAttributesOn, in.Body.DeviceAttributesOn},
		}

		for _, u := range updates {
			if u.value == nil {
				continue
			}

			c, err := b.State.SetSetting(u.key, *u.value)
			if err != nil {
				return nil, mapError("updating settings", err)
			}

			audit.Detail(ctx, string(u.key), *u.value)

			b.Change(c)
		}

		if in.Body.KeyExpiryDays != nil {
			err := b.State.SetKeyExpiry(time.Duration(*in.Body.KeyExpiryDays) * day)
			if err != nil {
				return nil, mapError("updating settings", err)
			}

			audit.Detail(ctx, string(types.SettingKeyExpiry), *in.Body.KeyExpiryDays)
		}

		err := setSSHRecording(ctx, b, in.Body.SSHRecorders, in.Body.SSHRecordingEnforce)
		if err != nil {
			return nil, err
		}

		err = setRouteSelection(ctx, b, in.Body.RouteSelection)
		if err != nil {
			return nil, err
		}

		return &settingsOutput{Body: settingsFrom(b.State.Settings(), b.Cfg)}, nil
	})
}

// setSSHRecording applies the requested recorders and enforcement, when
// either is given, keeping the current value of the other.
func setSSHRecording(ctx context.Context, b Backend, recorders *[]string, enforce *bool) error {
	if recorders == nil && enforce == nil {
		return nil
	}

	current := b.State.Settings()
	nextRecorders, nextEnforce := current.SSHRecorders, current.SSHRecordingEnforce

	if recorders != nil {
		nextRecorders = *recorders
	}

	if enforce != nil {
		nextEnforce = *enforce
	}

	c, err := b.State.SetSSHRecording(nextRecorders, nextEnforce)
	if err != nil {
		return mapError("updating settings", err)
	}

	audit.Detail(ctx, string(types.SettingSSHRecorders), nextRecorders)
	audit.Detail(ctx, string(types.SettingSSHRecordingEnforce), nextEnforce)

	b.Change(c)

	return nil
}

// setRouteSelection applies a requested route selection, when there is
// one, and dispatches the change it makes.
func setRouteSelection(ctx context.Context, b Backend, selection *string) error {
	if selection == nil {
		return nil
	}

	c, err := b.State.SetRouteSelection(types.RouteSelection(*selection))
	if err != nil {
		return mapError("updating settings", err)
	}

	audit.Detail(ctx, string(types.SettingRouteSelection), *selection)

	b.Change(c)

	return nil
}
