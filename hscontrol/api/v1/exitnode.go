package apiv1

import (
	"context"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerGlobalExitNode)
}

// SetGlobalExitNodeRequestBody is the body of setGlobalExitNode. An absent
// body marks the node and keeps its priority.
type SetGlobalExitNodeRequestBody struct {
	Enabled  *bool `doc:"false clears the mark."                                                                                                                                                      json:"enabled,omitempty"`              //nolint:lll // struct tag
	Priority *int  `doc:"Order among the global exit nodes for clients that pick one automatically: the highest wins, 0 is no preference. Absent keeps the current one; clearing the mark resets it." json:"priority,omitempty" minimum:"0"` //nolint:lll // struct tag
}

type setGlobalExitNodeInput struct {
	NodeID string                        `format:"uint64"  path:"nodeId"`
	Body   *SetGlobalExitNodeRequestBody `required:"false"`
}

func (b *SetGlobalExitNodeRequestBody) enabled() bool {
	return b == nil || b.Enabled == nil || *b.Enabled
}

func (b *SetGlobalExitNodeRequestBody) priority() *int {
	if b == nil {
		return nil
	}

	return b.Priority
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
			"(`tailscale set --exit-node=auto:any`) pick it. A priority orders several marked " +
			"nodes: while one has a priority, every node carries traffic-steering and the " +
			"clients pick the highest one that is online, returning to it when it comes back.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, scope.DevicesRoutes), "node.global_exit_node.set", "node", "nodeId"), func(
		ctx context.Context, in *setGlobalExitNodeInput,
	) (*nodeOutput, error) {
		if p := in.Body.priority(); p != nil {
			audit.Detail(ctx, "priority", *p)
		}

		return switchNode(ctx, b, in.NodeID, "enabled", in.Body.enabled(), "setting global exit node",
			func(id types.NodeID, on bool) (types.NodeView, change.Change, error) {
				return b.State.SetGlobalExitNode(id, on, in.Body.priority())
			})
	})
}
