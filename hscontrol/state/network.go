package state

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
)

// GetNetwork returns one network.
func (s *State) GetNetwork(id types.NetworkID) (types.Network, error) {
	network, ok := s.AccessModel().Network(id)
	if !ok {
		return types.Network{}, types.ErrNetworkNotFound
	}

	return network, nil
}

// CreateNetwork stores a network and approves its prefixes on its
// routers when it is on.
func (s *State) CreateNetwork(network types.Network) (types.Network, change.Change, error) {
	err := s.validateNetwork(network)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	before := s.networkAssignments()

	created, err := s.db.CreateNetwork(network)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	c, err := s.applyNetworkChange(before)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	log.Info().Uint64("network.id", uint64(created.ID)).Str("network.name", created.Name).Msg("Network created")

	return created, c, nil
}

// UpdateNetwork replaces every field of the network and moves the route
// approvals along.
func (s *State) UpdateNetwork(network types.Network) (types.Network, change.Change, error) {
	if _, ok := s.AccessModel().Network(network.ID); !ok {
		return types.Network{}, change.Change{}, types.ErrNetworkNotFound
	}

	err := s.validateNetwork(network)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	before := s.networkAssignments()

	updated, err := s.db.UpdateNetwork(network)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	c, err := s.applyNetworkChange(before)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	return updated, c, nil
}

// SetNetworkEnabled switches the network on or off; off withdraws the
// route approvals it made.
func (s *State) SetNetworkEnabled(id types.NetworkID, enabled bool) (types.Network, change.Change, error) {
	before := s.networkAssignments()

	updated, err := s.db.SetNetworkEnabled(id, enabled)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	c, err := s.applyNetworkChange(before)
	if err != nil {
		return types.Network{}, change.Change{}, err
	}

	return updated, c, nil
}

// DeleteNetwork removes the network and withdraws the route approvals it
// made.
func (s *State) DeleteNetwork(id types.NetworkID) (change.Change, error) {
	network, ok := s.AccessModel().Network(id)
	if !ok {
		return change.Change{}, types.ErrNetworkNotFound
	}

	before := s.networkAssignments()

	err := s.db.DeleteNetwork(id)
	if err != nil {
		return change.Change{}, err
	}

	c, err := s.applyNetworkChange(before)
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Uint64("network.id", uint64(id)).Str("network.name", network.Name).Msg("Network deleted")

	return c, nil
}

// validateNetwork checks the fields and that every group and router
// exists.
func (s *State) validateNetwork(network types.Network) error {
	err := types.ValidateNetwork(network)
	if err != nil {
		return err
	}

	model := s.AccessModel()

	for _, id := range network.GroupIDs {
		if _, ok := model.Group(id); !ok {
			return fmt.Errorf("%w: %d", types.ErrGroupNotFound, id)
		}
	}

	for _, id := range network.RouterNodeIDs {
		if _, ok := s.nodeStore.GetNode(id); !ok {
			return fmt.Errorf("%w: %d", ErrNodeNotFound, id)
		}
	}

	return nil
}

// networkAssignments maps every router to the prefixes the enabled
// networks assign it.
func (s *State) networkAssignments() map[types.NodeID][]netip.Prefix {
	out := make(map[types.NodeID][]netip.Prefix)

	for _, n := range s.AccessModel().Networks {
		if !n.Enabled {
			continue
		}

		for _, id := range n.RouterNodeIDs {
			for _, p := range n.Prefixes {
				if !slices.Contains(out[id], p) {
					out[id] = append(out[id], p)
				}
			}
		}
	}

	return out
}

// applyNetworkChange reloads the model after a network write, then moves
// the route approvals of every router the change touched: prefixes a
// network now assigns are approved, prefixes no enabled network assigns
// any more are withdrawn, and approvals made by hand are left alone.
func (s *State) applyNetworkChange(before map[types.NodeID][]netip.Prefix) (change.Change, error) {
	_, err := s.loadAccessModel()
	if err != nil {
		return change.Change{}, err
	}

	after := s.networkAssignments()

	nodes := make([]types.NodeID, 0, len(before)+len(after))
	for id := range before {
		nodes = append(nodes, id)
	}

	for id := range after {
		if _, ok := before[id]; !ok {
			nodes = append(nodes, id)
		}
	}

	slices.Sort(nodes)

	for _, id := range nodes {
		node, ok := s.nodeStore.GetNode(id)
		if !ok {
			continue
		}

		current := node.ApprovedRoutes().AsSlice()
		desired := make([]netip.Prefix, 0, len(current)+len(after[id]))

		for _, p := range current {
			if slices.Contains(before[id], p) && !slices.Contains(after[id], p) {
				continue
			}

			desired = append(desired, p)
		}

		for _, p := range after[id] {
			if !slices.Contains(desired, p) {
				desired = append(desired, p)
			}
		}

		if slices.Equal(current, desired) {
			continue
		}

		_, _, err := s.SetApprovedRoutes(id, desired)
		if err != nil {
			return change.Change{}, fmt.Errorf("moving route approvals of node %d: %w", id, err)
		}
	}

	// Every peer's routes may have moved, whether the policy compiled
	// differently or not, so every client gets a fresh netmap.
	c := change.PolicyChange()
	c.Reason = "network change"

	return c, nil
}

// networkRoutesFor keeps only the peer's routes the viewer should get:
// a prefix an enabled network assigns to the peer reaches the members of
// the network's groups and the network's other routers, nobody else. A
// prefix outside every network is left as it was.
func (s *State) networkRoutesFor(viewer, peer types.NodeView, routes []netip.Prefix) []netip.Prefix {
	model := s.AccessModel()
	if len(model.Networks) == 0 {
		return routes
	}

	out := make([]netip.Prefix, 0, len(routes))

	for _, p := range routes {
		networks := model.NetworksRouting(peer.ID(), p)
		if len(networks) == 0 || slices.ContainsFunc(networks, func(n types.Network) bool {
			return slices.Contains(n.RouterNodeIDs, viewer.ID()) || model.MemberOfAny(viewer, n.GroupIDs)
		}) {
			out = append(out, p)
		}
	}

	return out
}

// networkNames renders the networks' names for error messages.
func networkNames(networks []types.Network) string {
	names := make([]string, 0, len(networks))
	for _, n := range networks {
		names = append(names, n.Name)
	}

	return strings.Join(names, ", ")
}
