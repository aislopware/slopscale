package state

import "github.com/aislopware/slopscale/hscontrol/types"

// routeInputs is what a snapshot's route election reads besides the
// nodes. The zero value is the default route selection.
type routeInputs struct {
	selection types.RouteSelection
}

// SetRouteSelection changes how viewers choose among routers and
// returns once a snapshot elected with it is published. It reports
// whether the selection in force changed.
func (s *NodeStore) SetRouteSelection(selection types.RouteSelection) bool {
	return s.setRouteInputs(func(in *routeInputs) bool {
		if in.selection.OrDefault() == selection.OrDefault() {
			return false
		}

		in.selection = selection

		return true
	})
}

// routeInputs returns the inputs in force.
func (s *NodeStore) routeInputs() routeInputs {
	if in := s.inputs.Load(); in != nil {
		return *in
	}

	return routeInputs{}
}

// setRouteInputs applies change to a copy of the inputs and, when it
// reports a change, publishes the copy and rebuilds the snapshot. The
// rebuild keeps the peer relationship: no input of it moved.
func (s *NodeStore) setRouteInputs(change func(*routeInputs) bool) bool {
	s.inputsMu.Lock()
	defer s.inputsMu.Unlock()

	in := s.routeInputs()
	if !change(&in) {
		return false
	}

	s.inputs.Store(&in)

	w := work{op: rebuildRoutes, result: make(chan struct{})}

	select {
	case s.writeQueue <- w:
	case <-s.stopped:
		return true
	}

	<-w.result

	return true
}
