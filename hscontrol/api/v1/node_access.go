package apiv1

import (
	"context"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"tailscale.com/types/views"
)

// A member holds no scope, yet the console is where they look after their
// own machines. The node reads and the writes a member may make on a
// machine of their own therefore authorize here instead of through a
// static scope (see selfEnforcedOps): a credential that reads devices
// sees every node; any other credential owned by a user sees the nodes
// that user owns and the ones shared with them, and changes only the ones
// they own. Nothing else is visible, and an invisible node is reported as
// not found so a member cannot enumerate the tailnet through it.

// actsAsUser reports whether p stands for its user: a console session or
// an API key minted without scopes. A credential minted with a scope list
// (a scoped key, an OAuth token) is bounded to exactly that list, so it
// gets none of what the user may do for themselves.
func actsAsUser(p principal.Principal) bool {
	return p.HasUser() && !p.Scoped
}

// ownsNode reports whether the node is a personal machine of p's user.
// A tagged node belongs to its tags: the user id it may carry is who
// created it, not an owner.
func ownsNode(p principal.Principal, node types.NodeView) bool {
	return actsAsUser(p) && !node.IsTagged() && node.UserID().Valid() &&
		types.UserID(node.UserID().Get()) == p.UserID
}

// canSeeNode reports whether p may read the node.
func canSeeNode(p principal.Principal, node types.NodeView) bool {
	if p.Allows(scope.DevicesCoreRead) {
		return true
	}

	return ownsNode(p, node) || (actsAsUser(p) && node.IsSharedWith(p.UserID))
}

// visibleNodes is every node p may read, in store order.
func (b Backend) visibleNodes(p principal.Principal) views.Slice[types.NodeView] {
	all := b.State.ListNodes()
	if p.Allows(scope.DevicesCoreRead) {
		return all
	}

	if !actsAsUser(p) {
		return views.SliceOf[types.NodeView](nil)
	}

	var visible []types.NodeView

	for _, node := range all.All() {
		if canSeeNode(p, node) {
			visible = append(visible, node)
		}
	}

	return views.SliceOf(visible)
}

// requireNodeVisible parses the id and returns the node when the caller
// may read it; any other node, including one that does not exist, is not
// found.
func requireNodeVisible(ctx context.Context, b Backend, id string) (types.NodeView, error) {
	nodeID, err := parseNodeID(id)
	if err != nil {
		return types.NodeView{}, err
	}

	node, ok := b.State.GetNodeByID(nodeID)
	if !ok || !canSeeNode(caller(ctx), node) {
		return types.NodeView{}, huma.Error404NotFound("node not found")
	}

	return node, nil
}

// requireOwnNodeAccess lets a caller holding the devices scope change any
// node and a member change only the nodes they own: a node shared with
// them is theirs to see, not to rename, expire or remove.
func requireOwnNodeAccess(ctx context.Context, b Backend, id string) (types.NodeView, error) {
	node, err := requireNodeVisible(ctx, b, id)
	if err != nil {
		return types.NodeView{}, err
	}

	p := caller(ctx)
	if p.Allows(scope.DevicesCore) || ownsNode(p, node) {
		return node, nil
	}

	return types.NodeView{}, huma.Error403Forbidden("only the node's owner or an administrator may change it")
}
