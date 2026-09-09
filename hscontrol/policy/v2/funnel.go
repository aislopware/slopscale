package v2

import (
	"slices"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tailcfg/peercap"
	"tailscale.com/types/views"
)

// funnelGrants is the grant that lets the Funnel ingress nodes reach the
// nodes the policy grants the `funnel` attribute: every port, because a
// node's peer API listens on a port of its choosing, and the ingress
// peer capability, which is what a node checks before it takes an
// ingress connection. The nodes still admit only the host:port pairs
// their own serve config allows Funnel for. Nil when nothing grants
// Funnel.
func (pol *Policy) funnelGrants() []Grant {
	if pol == nil {
		return nil
	}

	var targets Aliases

	for _, na := range pol.NodeAttrs {
		if slices.Contains(na.Attrs, nodecap.Funnel) {
			targets = append(targets, na.Targets...)
		}
	}

	if len(targets) == 0 {
		return nil
	}

	ingress := Tag(types.FunnelIngressTag)

	return []Grant{{
		Sources:      Aliases{&ingress},
		Destinations: targets,
		InternetProtocols: []ProtocolPort{{
			Protocol: ProtocolNameWildcard,
			Ports:    []tailcfg.PortRange{tailcfg.PortRangeAny},
		}},
		App: tailcfg.PeerCapMap{peercap.Ingress: nil},
	}}
}

// funnelFilterRules compiles the funnel grants on their own, for a
// policy that does not enforce: the tailnet is open, so the rules add
// nothing but the ingress capability, which an allow-all filter does not
// carry and without which no node takes an ingress connection.
func (pol *Policy) funnelFilterRules(users types.Users, nodes views.Slice[types.NodeView]) []tailcfg.FilterRule {
	var compiled []compiledGrant

	for _, grant := range pol.funnelGrants() {
		cg, err := pol.compileOneGrant(grant, users, nodes)
		if err != nil || cg == nil {
			continue
		}

		compiled = append(compiled, *cg)
	}

	return globalFilterRules(compiled)
}
