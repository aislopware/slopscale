package apiv1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
)

func init() {
	registrations = append(registrations, registerSuspension)
}

// SetSuspensionRequestBody is the body of suspendNode. An absent body
// suspends.
type SetSuspensionRequestBody struct {
	Suspended *bool `doc:"false lifts the suspension." json:"suspended,omitempty"`
}

type suspendNodeInput struct {
	NodeID string                    `format:"uint64"  path:"nodeId"`
	Body   *SetSuspensionRequestBody `required:"false"`
}

func (b *SetSuspensionRequestBody) suspended() bool {
	return b == nil || b.Suspended == nil || *b.Suspended
}

// nodeSwitch is a state operation that turns something about a node on
// or off and returns the node with the change to publish.
type nodeSwitch func(types.NodeID, bool) (types.NodeView, change.Change, error)

// switchNode runs a [nodeSwitch] for an API call: it parses the id,
// records the value and the node in the audit entry, publishes the change
// and returns the node. The approve, suspend and global exit node
// endpoints share it.
func switchNode(
	ctx context.Context, b Backend, id, detail string, on bool, what string, op nodeSwitch,
) (*nodeOutput, error) {
	nodeID, err := parseNodeID(id)
	if err != nil {
		return nil, err
	}

	audit.Detail(ctx, detail, on)

	node, nodeChange, err := op(nodeID, on)
	if err != nil {
		return nil, mapError(what, err)
	}

	audit.Target(ctx, "", "", node.GivenName())

	b.Change(nodeChange)

	out := &nodeOutput{}
	out.Body.Node = nodeFromView(node)

	return out, nil
}

func registerSuspension(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "suspendNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/suspend",
		Summary:     "Suspend node",
		Description: "Suspends a node or lifts the suspension. A suspended node stays registered " +
			"and keeps its addresses, but it has no peers, no peer sees it and its client is " +
			"told it is not authorized until the suspension is lifted. Unlike expiring the key, " +
			"lifting a suspension needs no login on the device.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesCore), "node.suspension.set", "node", "nodeId"), func(
		ctx context.Context, in *suspendNodeInput,
	) (*nodeOutput, error) {
		return switchNode(ctx, b, in.NodeID, "suspended", in.Body.suspended(), "suspending node",
			b.State.SetNodeSuspension)
	})
}
