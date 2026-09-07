package apiv1

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

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
	// KeyExpiryDays caps how long a node key stays valid after a login;
	// 0 leaves the config file's node.expiry and the client in charge.
	KeyExpiryDays int `json:"keyExpiryDays"`
	// DefaultKeyExpiryDays is the config file's node.expiry, applied when
	// a client asks for nothing and no cap is set; 0 means never.
	DefaultKeyExpiryDays int `json:"defaultKeyExpiryDays"`
}

// UpdateSettingsRequestBody carries the switches to change; absent ones
// keep their value. Switching an approval off approves everything waiting.
type UpdateSettingsRequestBody struct {
	DevicesApprovalOn *bool `json:"devicesApprovalOn,omitempty"`
	UsersApprovalOn   *bool `json:"usersApprovalOn,omitempty"`
	// KeyExpiryDays 0 switches the cap off.
	KeyExpiryDays *int `json:"keyExpiryDays,omitempty" maximum:"365" minimum:"0"`
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
		DevicesApprovalOn: s.DevicesApprovalOn,
		UsersApprovalOn:   s.UsersApprovalOn,
		KeyExpiryDays:     int(s.KeyExpiry / day),
	}

	if cfg != nil {
		out.DefaultKeyExpiryDays = int(cfg.Node.Expiry / day)
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
		Tags:        []string{"Settings"},
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
			"approves every node or user that was waiting.",
		Tags:     []string{"Settings"},
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

		return &settingsOutput{Body: settingsFrom(b.State.Settings(), b.Cfg)}, nil
	})
}
