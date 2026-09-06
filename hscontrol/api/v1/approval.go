package apiv1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
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
}

// UpdateSettingsRequestBody carries the switches to change; absent ones
// keep their value. Switching an approval off approves everything waiting.
type UpdateSettingsRequestBody struct {
	DevicesApprovalOn *bool `json:"devicesApprovalOn,omitempty"`
	UsersApprovalOn   *bool `json:"usersApprovalOn,omitempty"`
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

func settingsFrom(s types.Settings) Settings {
	return Settings{DevicesApprovalOn: s.DevicesApprovalOn, UsersApprovalOn: s.UsersApprovalOn}
}

func registerApproval(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "approveNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/approve",
		Summary:     "Approve node",
		Description: "Admits a node that registered while device approval was on, or withdraws " +
			"the approval again. A node waiting for approval stays registered but has no peers " +
			"and is not visible to any.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCore), func(_ context.Context, in *approveNodeInput) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		node, nodeChange, err := b.State.SetNodeApproval(nodeID, in.Body.approved())
		if err != nil {
			return nil, mapError("approving node", err)
		}

		b.Change(nodeChange)

		out := &nodeOutput{}
		out.Body.Node = nodeFromView(node)

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "approveUser",
		Method:      http.MethodPost,
		Path:        "/api/v1/user/{id}/approve",
		Summary:     "Approve user",
		Description: "Admits a user created by OIDC login while users approval was on, or " +
			"withdraws the approval again, which also withdraws every node the user owns.",
		Tags:     []string{"Users"},
		Security: bearerAuth,
	}, scope.Users), func(_ context.Context, in *approveUserInput) (*userOutput, error) {
		id, err := parseUserID(in.ID)
		if err != nil {
			return nil, err
		}

		user, userChange, err := b.State.SetUserApproval(id, in.Body.approved())
		if err != nil {
			return nil, mapError("approving user", err)
		}

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
		return &settingsOutput{Body: settingsFrom(b.State.Settings())}, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "updateSettings",
		Method:      http.MethodPost,
		Path:        "/api/v1/settings",
		Summary:     "Update settings",
		Description: "Changes the given switches. Switching device or users approval off " +
			"approves every node or user that was waiting.",
		Tags:     []string{"Settings"},
		Security: bearerAuth,
	}, scope.FeatureSettings), func(_ context.Context, in *updateSettingsInput) (*settingsOutput, error) {
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

			b.Change(c)
		}

		return &settingsOutput{Body: settingsFrom(b.State.Settings())}, nil
	})
}
