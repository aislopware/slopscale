package apiv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerPosture)
}

// NodePosture is everything the policy can check about a node: the
// attributes derived from what the client reports, the identity the
// server collected and the custom attributes an operator set.
type NodePosture struct {
	// Attributes is the attribute map the policy evaluates, keyed by
	// name (node:os, node:tsVersion, custom:...). Values are strings,
	// numbers, booleans or, for node:serialNumber, a list of strings.
	Attributes map[string]any `json:"attributes"`
	// Identity is what the client reported when asked; absent until the
	// server asked, which needs the postureIdentityOn setting.
	Identity *PostureIdentity `json:"identity,omitempty"`
	// Custom lists the custom attributes with their expiry and comment.
	Custom []CustomAttribute `json:"custom" nullable:"false"`
	// IdentityCollectionOn mirrors the tailnet setting.
	IdentityCollectionOn bool `json:"identityCollectionOn"`
}

// PostureIdentity is the client's answer to the server's identity
// request.
type PostureIdentity struct {
	SerialNumbers []string `json:"serialNumbers" nullable:"false"`
	// Disabled is true when the client has posture checking off and so
	// reported nothing.
	Disabled    bool      `json:"disabled"`
	CollectedAt time.Time `json:"collectedAt"`
}

// CustomAttribute is one custom posture attribute.
type CustomAttribute struct {
	Key       string     `json:"key"`
	Value     any        `json:"value"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Comment   string     `json:"comment,omitempty"`
}

// SetAttributeRequestBody sets a custom attribute, Tailscale style.
type SetAttributeRequestBody struct {
	// Value is a string, a number or a boolean.
	Value json.RawMessage `json:"value"`
	// Expiry, when set, removes the attribute at that time.
	Expiry  *time.Time `json:"expiry,omitempty"`
	Comment string     `json:"comment,omitempty" maxLength:"200"`
}

type (
	nodePostureInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
	}
	nodePostureOutput struct {
		Body NodePosture
	}
	setAttributeInput struct {
		NodeID string                  `format:"uint64" path:"nodeId"`
		Key    string                  `path:"key"`
		Body   SetAttributeRequestBody `required:"true"`
	}
	deleteAttributeInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
		Key    string `path:"key"`
	}
)

func postureFromView(view types.NodeView, settings types.Settings) NodePosture {
	now := time.Now()
	out := NodePosture{
		Attributes:           map[string]any(view.PostureAttributes(now)),
		Custom:               []CustomAttribute{},
		IdentityCollectionOn: settings.PostureIdentityOn,
	}

	if view.Posture().Valid() {
		p := view.Posture()
		out.Identity = &PostureIdentity{
			SerialNumbers: nonNilStrings(p.SerialNumbers().AsSlice()),
			Disabled:      p.Disabled(),
			CollectedAt:   p.CollectedAt(),
		}
	}

	for _, a := range view.Attributes().All() {
		c := CustomAttribute{Key: a.Key, Value: a.Value.Any(), Comment: a.Comment}

		if !a.ExpiresAt.IsZero() {
			at := a.ExpiresAt
			c.ExpiresAt = &at
		}

		out.Custom = append(out.Custom, c)
	}

	return out
}

// mapPostureError maps collection failures to HTTP statuses.
func mapPostureError(err error) error {
	switch {
	case errors.Is(err, state.ErrPostureCollectionOff):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, state.ErrNodeNotConnected), errors.Is(err, state.ErrC2NTimeout):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, state.ErrC2NFailed):
		return huma.Error502BadGateway(err.Error())
	case errors.Is(err, types.ErrAttributeKeyInvalid), errors.Is(err, types.ErrAttributeValueInvalid),
		errors.Is(err, types.ErrAttributeExpiryPast):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, types.ErrAttributeNotFound):
		return huma.Error404NotFound(err.Error())
	}

	return mapError("node posture", err)
}

func registerPosture(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getNodePosture",
		Method:      http.MethodGet,
		Path:        "/api/v1/node/{nodeId}/posture",
		Summary:     "Get node posture",
		Description: "Returns the attribute map the policy evaluates for the node, the identity the " +
			"server collected and the custom attributes. See docs/ref/device-trust.md.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributesRead), func(_ context.Context, in *nodePostureInput) (*nodePostureOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		view, ok := b.State.GetNodeByID(nodeID)
		if !ok {
			return nil, huma.Error404NotFound("node not found")
		}

		return &nodePostureOutput{Body: postureFromView(view, b.State.Settings())}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "collectNodePosture",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/posture/collect",
		Summary:     "Ask the node for its identity now",
		Description: "Sends the connected node a control-to-node request for its hardware serial " +
			"numbers and waits for the answer. Needs the postureIdentityOn setting; a client " +
			"with posture checking off answers with an empty, disabled report.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributes), "node.posture.collect", "node", "nodeId"), func(
		ctx context.Context, in *nodePostureInput,
	) (*nodePostureOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		view, ok := b.State.GetNodeByID(nodeID)
		if !ok {
			return nil, huma.Error404NotFound("node not found")
		}

		audit.Target(ctx, "", "", view.GivenName())

		online := view.IsOnline().Valid() && view.IsOnline().Get()

		_, c, err := b.State.CollectPosture(ctx, nodeID, online, b.Change)
		if err != nil {
			return nil, mapPostureError(err)
		}

		if !c.IsEmpty() {
			b.Change(c)
		}

		view, _ = b.State.GetNodeByID(nodeID)

		return &nodePostureOutput{Body: postureFromView(view, b.State.Settings())}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setNodeAttribute",
		Method:      http.MethodPut,
		Path:        "/api/v1/node/{nodeId}/attributes/{key}",
		Summary:     "Set a custom posture attribute",
		Description: "Stores a custom:... attribute on the node, replacing one with the same key. " +
			"The value is a string, a number or a boolean; an expiry removes it at that " +
			"time, which is how a temporary grant such as an on-call marker is made.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributes), "node.attribute.set", "node", "nodeId"), func(
		ctx context.Context, in *setAttributeInput,
	) (*nodePostureOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		value, err := types.AttributeValueFromJSON(in.Body.Value)
		if err != nil {
			return nil, mapPostureError(err)
		}

		attr := types.NodeAttribute{Key: in.Key, Value: value, Comment: in.Body.Comment}
		if in.Body.Expiry != nil {
			attr.ExpiresAt = *in.Body.Expiry
		}

		audit.Detail(ctx, "key", in.Key)
		audit.Detail(ctx, "value", value.Any())

		if in.Body.Expiry != nil {
			audit.Detail(ctx, "expiry", in.Body.Expiry.UTC().Format(time.RFC3339))
		}

		view, c, err := b.State.SetNodeAttribute(nodeID, attr)
		if err != nil {
			return nil, mapPostureError(err)
		}

		audit.Target(ctx, "", "", view.GivenName())
		b.Change(c)

		return &nodePostureOutput{Body: postureFromView(view, b.State.Settings())}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteNodeAttribute",
		Method:      http.MethodDelete,
		Path:        "/api/v1/node/{nodeId}/attributes/{key}",
		Summary:     "Delete a custom posture attribute",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, scope.DevicesPostureAttributes), "node.attribute.delete", "node", "nodeId"), func(
		ctx context.Context, in *deleteAttributeInput,
	) (*nodePostureOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "key", in.Key)

		view, c, err := b.State.DeleteNodeAttribute(nodeID, in.Key)
		if err != nil {
			return nil, mapPostureError(err)
		}

		audit.Target(ctx, "", "", view.GivenName())
		b.Change(c)

		return &nodePostureOutput{Body: postureFromView(view, b.State.Settings())}, nil
	})
}
