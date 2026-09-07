package apiv1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/scope"
)

func init() {
	registrations = append(registrations, registerGlobalExitNode)
}

// SetGlobalExitNodeRequestBody is the body of setGlobalExitNode. An absent
// body marks the node.
type SetGlobalExitNodeRequestBody struct {
	Enabled *bool `doc:"false clears the mark." json:"enabled,omitempty"`
}

type setGlobalExitNodeInput struct {
	NodeID string                        `format:"uint64"  path:"nodeId"`
	Body   *SetGlobalExitNodeRequestBody `required:"false"`
}

func (b *SetGlobalExitNodeRequestBody) enabled() bool {
	return b == nil || b.Enabled == nil || *b.Enabled
}

func registerGlobalExitNode(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "setGlobalExitNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/global-exit-node",
		Summary:     "Mark node as global exit node",
		Description: "Marks an exit node every client is told to prefer, or clears the mark. " +
			"Marking approves the node's exit routes; the node then carries suggest-exit-node " +
			"and every node auto-exit-node, so clients that use an exit node automatically " +
			"(`tailscale set --exit-node=auto:any`) pick it.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesRoutes), func(_ context.Context, in *setGlobalExitNodeInput) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		node, nodeChange, err := b.State.SetGlobalExitNode(nodeID, in.Body.enabled())
		if err != nil {
			return nil, mapError("setting global exit node", err)
		}

		b.Change(nodeChange)

		out := &nodeOutput{}
		out.Body.Node = nodeFromView(node)

		return out, nil
	})
}
