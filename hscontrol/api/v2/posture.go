package apiv2

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/api/principal"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerPosture)
}

// DeviceAttributes is Tailscale's device posture attributes response:
// every attribute the policy can check, custom ones included.
type DeviceAttributes struct {
	Attributes map[string]any `json:"attributes"`
}

// SetDeviceAttribute is Tailscale's request body for a custom attribute.
type SetDeviceAttribute struct {
	Value   json.RawMessage `json:"value"`
	Expiry  *time.Time      `json:"expiry,omitempty"`
	Comment string          `json:"comment,omitempty" maxLength:"200"`
}

type (
	deviceAttributesOutput struct {
		Body DeviceAttributes
	}
	setDeviceAttributeInput struct {
		DeviceID string             `path:"id"`
		Key      string             `path:"attributeKey"`
		Body     SetDeviceAttribute `required:"true"`
	}
	deleteDeviceAttributeInput struct {
		DeviceID string `path:"id"`
		Key      string `path:"attributeKey"`
	}
)

func mapAttributeError(err error) error {
	switch {
	case errors.Is(err, types.ErrAttributeKeyInvalid), errors.Is(err, types.ErrAttributeValueInvalid),
		errors.Is(err, types.ErrAttributeExpiryPast):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, types.ErrAttributeNotFound):
		return huma.Error404NotFound(err.Error())
	}

	return mapError("device attributes", err)
}

func registerPosture(api huma.API, b Backend) {
	deviceTags := []string{"Devices", tagTailscaleCompat}

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getDeviceAttributes",
		Method:      http.MethodGet,
		Path:        "/api/v2/device/{id}/attributes",
		Summary:     "Get a device's posture attributes",
		Description: "Returns every posture attribute of the device: the node:... attributes " +
			"derived from what the client reports and the custom:... attributes set through " +
			"this API, the way Tailscale's endpoint does.",
		Tags:          deviceTags,
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DevicesPostureAttributesRead), func(
		_ context.Context, in *deviceByIDInput,
	) (*deviceAttributesOutput, error) {
		node, err := lookupNode(b, in.DeviceID)
		if err != nil {
			return nil, err
		}

		return &deviceAttributesOutput{
			Body: DeviceAttributes{Attributes: map[string]any(node.PostureAttributes(time.Now()))},
		}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "setDeviceAttribute",
		Method:      http.MethodPost,
		Path:        "/api/v2/device/{id}/attributes/{attributeKey}",
		Summary:     "Set a custom posture attribute",
		Description: "Stores a custom:... attribute with a string, number or boolean value and an " +
			"optional expiry, replacing one with the same key.",
		Tags:          deviceTags,
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DevicesPostureAttributes), "node.attribute.set", "node", "id"), func(
		ctx context.Context, in *setDeviceAttributeInput,
	) (*emptyOutput, error) {
		node, err := lookupNode(b, in.DeviceID)
		if err != nil {
			return nil, err
		}

		value, err := types.AttributeValueFromJSON(in.Body.Value)
		if err != nil {
			return nil, mapAttributeError(err)
		}

		attr := types.NodeAttribute{Key: in.Key, Value: value, Comment: in.Body.Comment}
		if in.Body.Expiry != nil {
			attr.ExpiresAt = *in.Body.Expiry
		}

		audit.Target(ctx, "", "", node.GivenName())
		audit.Detail(ctx, "key", in.Key)
		audit.Detail(ctx, "value", value.Any())

		_, c, err := b.State.SetNodeAttribute(node.ID(), attr)
		if err != nil {
			return nil, mapAttributeError(err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID:   "deleteDeviceAttribute",
		Method:        http.MethodDelete,
		Path:          "/api/v2/device/{id}/attributes/{attributeKey}",
		Summary:       "Delete a custom posture attribute",
		Tags:          deviceTags,
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DevicesPostureAttributes), "node.attribute.delete", "node", "id"), func(
		ctx context.Context, in *deleteDeviceAttributeInput,
	) (*emptyOutput, error) {
		node, err := lookupNode(b, in.DeviceID)
		if err != nil {
			return nil, err
		}

		audit.Target(ctx, "", "", node.GivenName())
		audit.Detail(ctx, "key", in.Key)

		_, c, err := b.State.DeleteNodeAttribute(node.ID(), in.Key)
		if err != nil {
			return nil, mapAttributeError(err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	})
}
