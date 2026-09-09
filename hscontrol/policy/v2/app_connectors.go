package v2

import (
	"encoding/json"
	"net/netip"
	"slices"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/views"
)

// App connectors in the policy: the apps table is stamped on the
// connector nodes as the tailscale.com/app-connectors capability the
// client reads, next to whatever nodeAttrs.app in the file adds, and a
// connector may approve its own routes: a host route for an address it
// learned from a domain, or one of the app's static routes. See
// docs/ref/apps.md.

// AppConnectorsCap is the capability carrying the app definitions; the
// client reads it in ipn/ipnlocal reconfigAppConnectorLocked.
const AppConnectorsCap nodecap.Cap = "tailscale.com/app-connectors"

// SetAppConnectors replaces the tailnet's apps and recompiles.
func (pm *PolicyManager) SetAppConnectors(apps []types.AppConnector) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.appConnectors = slices.Clone(apps)

	return pm.updateLocked()
}

// appsForNode returns the apps whose connectors include the node: a
// tagged node one of the app's tags selects, or any node running the
// connector service when an app takes every connector.
func appsForNode(apps []types.AppConnector, node types.NodeView) []types.AppConnector {
	if len(apps) == 0 {
		return nil
	}

	tags := node.Tags().AsSlice()
	runsConnector := node.Hostinfo().Valid() && node.Hostinfo().AppConnector().EqualBool(true)

	var out []types.AppConnector

	for _, app := range apps {
		if app.Selects(tags, runsConnector) {
			out = append(out, app)
		}
	}

	return out
}

// stampAppConnectorCaps gives every connector node the app definitions
// it serves and tells it to keep the routes it learns across restarts.
func stampAppConnectorCaps(
	apps []types.AppConnector,
	nodes views.Slice[types.NodeView],
	capMaps map[types.NodeID]tailcfg.NodeCapMap,
) {
	if len(apps) == 0 {
		return
	}

	for _, node := range nodes.All() {
		mine := appsForNode(apps, node)
		if len(mine) == 0 {
			continue
		}

		capMap, ok := capMaps[node.ID()]
		if !ok {
			capMap = tailcfg.NodeCapMap{}
			capMaps[node.ID()] = capMap
		}

		for _, app := range mine {
			raw, err := json.Marshal(app.Attr())
			if err != nil {
				continue
			}

			capMap[AppConnectorsCap] = append(capMap[AppConnectorsCap], tailcfg.RawMessage(raw))
		}

		if _, exists := capMap[nodecap.StoreAppCRoutes]; !exists {
			capMap[nodecap.StoreAppCRoutes] = nil
		}
	}
}

// connectorCanApproveRoute reports whether the node is a connector of an
// app that covers the route: a single address, which a connector learns
// from a domain, or a prefix inside the app's static routes. Exit routes
// are never approved this way. Callers hold pm.mu.
func (pm *PolicyManager) connectorCanApproveRoute(node types.NodeView, route netip.Prefix) bool {
	apps := appsForNode(pm.appConnectors, node)
	if len(apps) == 0 {
		return false
	}

	if route.IsSingleIP() {
		return true
	}

	for _, app := range apps {
		for _, r := range app.Routes {
			if r.Bits() <= route.Bits() && r.Overlaps(route) {
				return true
			}
		}
	}

	return false
}
