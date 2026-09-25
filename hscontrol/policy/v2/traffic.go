package v2

import (
	"net/netip"
	"slices"

	"tailscale.com/tailcfg"
)

// dnsPort is where the traffic monitor's gateway resolvers answer.
const dnsPort = 53

// SetTrafficResolvers replaces the traffic monitor's gateway resolvers
// and recompiles, so the grant to them follows the set.
func (pm *PolicyManager) SetTrafficResolvers(addrs []netip.Addr) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if slices.Equal(pm.trafficResolvers, addrs) {
		return false, nil
	}

	pm.trafficResolvers = slices.Clone(addrs)

	return pm.updateLocked()
}

// trafficResolverGrants is the grant that lets every node ask the
// traffic monitor's gateway resolvers, UDP and TCP on port 53: the server
// points every client at them, and a policy that did not admit the
// queries would leave the clients without DNS. Nil when there are none.
func (pol *Policy) trafficResolverGrants() []Grant {
	if pol == nil || len(pol.trafficResolvers) == 0 {
		return nil
	}

	destinations := make(Aliases, 0, len(pol.trafficResolvers))

	for _, addr := range pol.trafficResolvers {
		prefix := Prefix(netip.PrefixFrom(addr, addr.BitLen()))
		destinations = append(destinations, &prefix)
	}

	ports := []tailcfg.PortRange{{First: dnsPort, Last: dnsPort}}

	return []Grant{{
		Sources:      Aliases{Wildcard},
		Destinations: destinations,
		InternetProtocols: []ProtocolPort{
			{Protocol: ProtocolNameUDP, Ports: ports},
			{Protocol: ProtocolNameTCP, Ports: ports},
		},
	}}
}
