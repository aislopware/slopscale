package state

import (
	"fmt"
	"net/netip"
	"slices"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"tailscale.com/net/tsaddr"
)

// SetGlobalExitNode marks a node as a global exit node or clears the
// mark. Marking approves the node's exit routes, so it serves as an exit
// node as soon as it advertises them; clearing leaves the routes as they
// are. Every node's capabilities change (auto-exit-node comes and goes
// with the first and last global exit node), so the result is a
// tailnet-wide recompute.
func (s *State) SetGlobalExitNode(nodeID types.NodeID, on bool) (types.NodeView, change.Change, error) {
	node, ok := s.nodeStore.GetNode(nodeID)
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	var c change.Change

	if on {
		routes := node.ApprovedRoutes().AsSlice()

		var missing []netip.Prefix

		for _, exit := range tsaddr.ExitRoutes() {
			if !slices.Contains(routes, exit) {
				missing = append(missing, exit)
			}
		}

		if len(missing) > 0 {
			var err error

			_, c, err = s.SetApprovedRoutes(nodeID, slices.Concat(routes, missing))
			if err != nil {
				return types.NodeView{}, change.Change{}, fmt.Errorf("approving exit routes: %w", err)
			}
		}
	}

	n, _ := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		node.GlobalExitNode = on
	})

	// persistNodeToDB leaves global_exit_node alone, as it does expiry.
	err := s.db.NodeSetGlobalExitNode(nodeID, on)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("setting global exit node in database: %w", err)
	}

	_, err = s.updatePolicyManagerNodes()
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf(
			"updating policy manager after global exit node change: %w",
			err,
		)
	}

	full := change.PolicyChange()
	full.Reason = "global exit node"
	full.IncludeSelf = true

	if !c.IsEmpty() {
		full = c.Merge(full)
	}

	return n, full, nil
}
