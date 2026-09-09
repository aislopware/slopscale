package apiv1

import (
	"context"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerSharing)
}

// ShareNodeRequestBody names the user a node is shared with.
type ShareNodeRequestBody struct {
	UserID string `doc:"ID of the user to share the node with." format:"uint64" json:"userId"`
}

type (
	shareNodeInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
		Body   ShareNodeRequestBody
	}

	unshareNodeInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
		UserID string `format:"uint64" path:"userId"`
	}
)

// requireShareAccess lets an all-access caller or one holding the devices
// scope share any node and a member share only nodes they own. The
// operations are self-enforcing (see selfEnforcedOps) because members
// hold no scope at all.
func requireShareAccess(ctx context.Context, b Backend, nodeID types.NodeID) error {
	node, ok := b.State.GetNodeByID(nodeID)
	if !ok {
		return huma.Error404NotFound("node not found")
	}

	p := caller(ctx)
	if p.Allows(scope.DevicesCore) {
		return nil
	}

	if node.IsTagged() || !node.UserID().Valid() || types.UserID(node.UserID().Get()) != p.UserID {
		return huma.Error403Forbidden("only the node's owner or an administrator may share it")
	}

	return nil
}

func registerSharing(api huma.API, b Backend) {
	huma.Register(api, audited(huma.Operation{
		OperationID: "shareNode",
		Method:      http.MethodPost,
		Path:        "/api/v1/node/{nodeId}/share",
		Summary:     "Share node with a user",
		Description: "Gives the user's personal devices access to the node wherever the policy " +
			"names autogroup:shared, and marks the node as shared in their netmaps. The node " +
			"gets no access back. A member may share the nodes they own; sharing any node " +
			"needs the devices scope.",
		Tags:     []string{"Nodes"},
		Security: bearerAuth,
	}, "node.share", "node", "nodeId"), func(ctx context.Context, in *shareNodeInput) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		userID, err := parseUserID(in.Body.UserID)
		if err != nil {
			return nil, err
		}

		err = requireShareAccess(ctx, b, nodeID)
		if err != nil {
			return nil, err
		}

		var createdBy *types.UserID

		if p := caller(ctx); p.HasUser() {
			uid := p.UserID
			createdBy = &uid
		}

		node, nodeChange, err := b.State.ShareNode(nodeID, userID, createdBy)
		if err != nil {
			return nil, mapError("sharing node", err)
		}

		audit.Target(ctx, "", "", node.GivenName())
		audit.Detail(ctx, "userId", in.Body.UserID)

		b.Change(nodeChange)

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)

		return out, nil
	})

	huma.Register(api, audited(huma.Operation{
		OperationID: "unshareNode",
		Method:      http.MethodDelete,
		Path:        "/api/v1/node/{nodeId}/share/{userId}",
		Summary:     "Stop sharing node with a user",
		Tags:        []string{"Nodes"},
		Security:    bearerAuth,
	}, "node.unshare", "node", "nodeId"), func(ctx context.Context, in *unshareNodeInput) (*nodeOutput, error) {
		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		userID, err := parseUserID(in.UserID)
		if err != nil {
			return nil, err
		}

		err = requireShareAccess(ctx, b, nodeID)
		if err != nil {
			return nil, err
		}

		node, nodeChange, err := b.State.UnshareNode(nodeID, userID)
		if err != nil {
			return nil, mapError("unsharing node", err)
		}

		audit.Target(ctx, "", "", node.GivenName())
		audit.Detail(ctx, "userId", in.UserID)

		b.Change(nodeChange)

		out := &nodeOutput{}
		out.Body.Node = b.nodeFromView(node)

		return out, nil
	})
}
