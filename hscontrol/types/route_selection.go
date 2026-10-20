package types

import (
	"errors"
	"fmt"
)

// SettingRouteSelection holds the tailnet's [RouteSelection]; no row means
// [RouteSelectionRegional].
const SettingRouteSelection SettingKey = "route_selection"

// RouteSelection is how a machine chooses among the routers that serve the
// same prefix: subnet routers, app connectors and the hosts of a service.
// The values are the hosted control plane's routeSelection names; see
// docs/content/ref/route-selection.mdx for what each does here.
type RouteSelection string

const (
	// RouteSelectionActivePassive gives every machine the tailnet-wide
	// primary: the first healthy router, kept while it stays healthy.
	// Global exit nodes keep the operator's priority for everyone.
	RouteSelectionActivePassive RouteSelection = "active-passive-failover"
	// RouteSelectionRegional is the default: a machine uses the primary
	// of its home DERP region, or where several routers are healthy, the
	// one nearest it by measured round trip; the nearest global exit
	// nodes are lifted above the others.
	RouteSelectionRegional RouteSelection = "regional-routing"
	// RouteSelectionRegionalFailover groups routers by DERP region and
	// keeps the oldest healthy one per region, without the per-machine
	// latency steering or exit node lifting of [RouteSelectionRegional].
	RouteSelectionRegionalFailover RouteSelection = "regional-routing-failover"
	// RouteSelectionMagicRoute is accepted for the hosted control plane's
	// latency-scored selection and steers as [RouteSelectionRegional]
	// does, which already scores routers per machine by round trip.
	RouteSelectionMagicRoute RouteSelection = "magicroute"
)

// RouteSelections lists every value in the order the hosted control plane
// documents them.
var RouteSelections = []RouteSelection{
	RouteSelectionActivePassive,
	RouteSelectionRegional,
	RouteSelectionRegionalFailover,
	RouteSelectionMagicRoute,
}

// ErrRouteSelectionUnknown is returned for a value outside RouteSelections.
var ErrRouteSelectionUnknown = errors.New("unknown route selection")

// ParseRouteSelection checks a route selection name.
func ParseRouteSelection(s string) (RouteSelection, error) {
	for _, r := range RouteSelections {
		if string(r) == s {
			return r, nil
		}
	}

	return "", fmt.Errorf("%w: %q", ErrRouteSelectionUnknown, s)
}

// OrDefault returns the selection in force: the zero value, which a
// database without the row holds, is [RouteSelectionRegional].
func (r RouteSelection) OrDefault() RouteSelection {
	if r == "" {
		return RouteSelectionRegional
	}

	return r
}

// Regional reports whether a machine uses the primary of its home DERP
// region before the tailnet-wide one.
func (r RouteSelection) Regional() bool {
	return r.OrDefault() != RouteSelectionActivePassive
}

// Nearest reports whether a machine is steered per its own round trips:
// to the nearest of several healthy routers, and to the nearest global
// exit nodes.
func (r RouteSelection) Nearest() bool {
	switch r.OrDefault() {
	case RouteSelectionRegional, RouteSelectionMagicRoute:
		return true
	case RouteSelectionActivePassive, RouteSelectionRegionalFailover:
		return false
	default:
		// A value a newer server stored steers as the default does.
		return true
	}
}
