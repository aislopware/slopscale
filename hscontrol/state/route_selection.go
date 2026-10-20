package state

import (
	"fmt"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
)

// RouteSelection returns the route selection in force.
func (s *State) RouteSelection() types.RouteSelection {
	return s.Settings().RouteSelection.OrDefault()
}

// SetRouteSelection stores how machines choose among the routers, app
// connectors and service hosts that serve the same prefix, and re-elects
// with it before returning.
//
// The returned change is a [change.PolicyChange] without IncludeSelf: the
// selection only moves which peer carries a prefix in a viewer's
// AllowedIPs and the exit node priority on a viewer's peer view, so every
// node needs its peers recomputed, the same change a moved primary sends.
// A node's own view is untouched: its self routes are the tailnet-wide
// primaries and service hosts, which every selection elects the same
// way, and the traffic-steering cap does not depend on the selection.
func (s *State) SetRouteSelection(selection types.RouteSelection) (change.Change, error) {
	selection, err := types.ParseRouteSelection(string(selection))
	if err != nil {
		return change.Change{}, err
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	settings := s.Settings()
	if settings.RouteSelection == selection {
		return change.Change{}, nil
	}

	err = s.db.SaveRouteSelection(selection)
	if err != nil {
		return change.Change{}, fmt.Errorf("storing route selection: %w", err)
	}

	settings.RouteSelection = selection
	s.settings.Store(&settings)

	if !s.nodeStore.SetRouteSelection(selection) {
		// Only the stored name moved, e.g. from no row to the default.
		return change.Change{}, nil
	}

	log.Info().Str("route_selection", string(selection)).Msg("route selection changed")

	c := change.PolicyChange()
	c.Reason = "route selection"

	return c, nil
}
