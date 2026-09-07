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
	huma.Register(api, audited(withScope(huma.Operation{
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
	}, scope.DevicesRoutes), "node.global_exit_node.set", "node", "nodeId"), func(
		ctx context.Context, in *setGlobalExitNodeInput,
	) (*nodeOutput, error) {
		return switchNode(ctx, b, in.NodeID, "enabled", in.Body.enabled(), "setting global exit node",
			b.State.SetGlobalExitNode)
	})
}
