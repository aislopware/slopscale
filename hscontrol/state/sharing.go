package state

import (
	"errors"
	"fmt"
	"slices"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
)

// ErrShareWithOwner is returned when a node is shared with its own owner.
var ErrShareWithOwner = errors.New("node cannot be shared with its owner")

// ShareNode gives the user's personal devices access to the node wherever
// the policy names autogroup:shared, and marks the node as shared in
// those devices' netmaps. createdBy records who shared it, nil when an
// administrator credential without a user did. Sharing is one way: the
// node gets no access back.
func (s *State) ShareNode(
	nodeID types.NodeID,
	userID types.UserID,
	createdBy *types.UserID,
) (types.NodeView, change.Change, error) {
	node, ok := s.nodeStore.GetNode(nodeID)
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	if !node.IsTagged() && node.UserID().Valid() && types.UserID(node.UserID().Get()) == userID {
		return types.NodeView{}, change.Change{}, ErrShareWithOwner
	}

	_, err := s.db.GetUserByID(userID)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("looking up sharee: %w", err)
	}

	err = s.db.ShareNode(nodeID, userID, createdBy)
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	n, _ := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		if !node.IsSharedWith(userID) {
			node.SharedWith = append(node.SharedWith, userID)
			slices.Sort(node.SharedWith)
		}
	})

	c, err := s.policyChangeAfterShare()
	if err != nil {
		return n, change.Change{}, err
	}

	return n, c, nil
}

// UnshareNode withdraws a share made by [State.ShareNode].
func (s *State) UnshareNode(nodeID types.NodeID, userID types.UserID) (types.NodeView, change.Change, error) {
	if _, ok := s.nodeStore.GetNode(nodeID); !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	err := s.db.UnshareNode(nodeID, userID)
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	n, _ := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		node.SharedWith = slices.DeleteFunc(node.SharedWith, func(uid types.UserID) bool { return uid == userID })
	})

	c, err := s.policyChangeAfterShare()
	if err != nil {
		return n, change.Change{}, err
	}

	return n, c, nil
}

// policyChangeAfterShare refreshes the policy manager's node list and
// returns a tailnet-wide recompute: a share changes the sharees'
// devices' peer lists and filters and the Sharer marker they see on
// the node, and the policy manager only notices when the policy names
// autogroup:shared.
func (s *State) policyChangeAfterShare() (change.Change, error) {
	_, err := s.updatePolicyManagerNodes()
	if err != nil {
		return change.Change{}, fmt.Errorf("updating policy manager after share change: %w", err)
	}

	c := change.PolicyChange()
	c.Reason = "node sharing"

	return c, nil
}

// dropSharesWithUser removes a deleted user from every node's share
// list in the NodeStore; the database drops the rows by cascade.
func (s *State) dropSharesWithUser(userID types.UserID) {
	updates := make(map[types.NodeID]UpdateNodeFunc)

	for _, node := range s.nodeStore.ListNodes().All() {
		if node.IsSharedWith(userID) {
			updates[node.ID()] = func(node *types.Node) {
				node.SharedWith = slices.DeleteFunc(
					node.SharedWith,
					func(uid types.UserID) bool { return uid == userID },
				)
			}
		}
	}

	s.nodeStore.UpdateNodes(updates)
}
