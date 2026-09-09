package types

import (
	"net/netip"

	"tailscale.com/tailcfg"
)

// DebugRoutes is the JSON-shaped snapshot of the slopscale primary
// route ledger exposed by the /debug/routes endpoint and consumed by
// the integration test harness. It used to live in hscontrol/routes,
// but the algorithm now runs inside hscontrol/state and that package
// must not be imported from integration code.
type DebugRoutes struct {
	// AvailableRoutes maps node IDs to their advertised routes
	// (intersection of announced and approved). Only nodes currently
	// connected to slopscale are listed.
	AvailableRoutes map[NodeID][]netip.Prefix `json:"available_routes"`

	// PrimaryRoutes maps route prefixes to the node currently elected
	// primary for that prefix.
	PrimaryRoutes map[string]NodeID `json:"primary_routes"`

	// RegionalPrimaryRoutes maps a DERP region to the primary elected
	// among that region's own advertisers, which viewers homed there are
	// steered to instead of the tailnet-wide primary.
	RegionalPrimaryRoutes map[tailcfg.DERPRegionID]map[string]NodeID `json:"regional_primary_routes,omitempty"`

	// UnhealthyNodes lists nodes that have failed health probes.
	UnhealthyNodes []NodeID `json:"unhealthy_nodes,omitempty"`
}
