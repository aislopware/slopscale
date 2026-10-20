package state

import (
	"maps"
	"net/netip"
	"slices"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
)

// Service hosts are elected by the machinery that elects subnet route
// primaries, keyed by service name instead of prefix: a host must be
// admitted, unexpired, approved for the service and advertising it, and
// an online one is chosen over an offline one; the host that holds a
// service keeps it while it stays online and healthy, so a host that
// returns does not take the traffic back; an unhealthy host is passed
// over while a healthy one is left. Per viewer, the route selection
// applies as it does to routes, except that viewers equally near several
// hosts are always spread over them: the host terminates the connection,
// so there is no return path to keep on one machine.

// serviceElection is a snapshot's choice of host per service.
type serviceElection struct {
	// prefixes are each service's addresses, from the services table.
	prefixes map[tailcfg.ServiceName][]netip.Prefix
	// names lists the services with a host, in name order.
	names []tailcfg.ServiceName
	// hosts is each service's tailnet-wide host. When no host is online
	// and healthy the previous one is kept, else the first candidate, so
	// the service's name keeps resolving while its hosts are away.
	hosts map[tailcfg.ServiceName]types.NodeID
	// regional and contested are steering's inputs, as for routes.
	regional  map[tailcfg.DERPRegionID]map[tailcfg.ServiceName]types.NodeID
	contested map[tailcfg.ServiceName][]site
	// served is the addresses of the services each node is the
	// tailnet-wide host of, in service name order.
	served map[types.NodeID][]netip.Prefix
}

// ServiceHosts returns the tailnet-wide host of every service that has
// one. The map is the snapshot's and must not be mutated.
func (s *NodeStore) ServiceHosts() map[tailcfg.ServiceName]types.NodeID {
	return s.data.Load().services.hosts
}

// ServiceRoutesForNode returns the addresses of the services id is the
// tailnet-wide host of, in service name order. The slice is the
// snapshot's and must not be mutated.
func (s *NodeStore) ServiceRoutesForNode(id types.NodeID) []netip.Prefix {
	return s.data.Load().services.served[id]
}

// ServiceRoutesByHost returns, by host, the addresses of the services
// the viewer reaches through that host, in service name order. It is
// worked out once per map response; the result must not be mutated.
func (s *NodeStore) ServiceRoutesByHost(viewer viewerSite) map[types.NodeID][]netip.Prefix {
	return s.data.Load().services.routesFor(viewer)
}

// serviceCandidates maps each service in prefixes to the nodes that may
// carry its traffic, in ID order: all of them, and the online ones.
func serviceCandidates(
	nodes map[types.NodeID]types.Node,
	prefixes map[tailcfg.ServiceName][]netip.Prefix,
) (map[tailcfg.ServiceName][]types.NodeID, map[tailcfg.ServiceName][]types.NodeID) {
	if len(prefixes) == 0 {
		return nil, nil
	}

	var all, online map[tailcfg.ServiceName][]types.NodeID

	for id, n := range nodes {
		// Most nodes host nothing; this keeps them off the slow path.
		if len(n.ApprovedServices) == 0 || n.Services == nil || !n.IsAdmitted() || n.IsExpired() {
			continue
		}

		nv := n.View()

		for _, raw := range n.ApprovedServices {
			name := tailcfg.ServiceName(raw)
			if _, known := prefixes[name]; !known {
				continue
			}

			if _, active := nv.HostsService(name); !active {
				continue
			}

			if all == nil {
				all = make(map[tailcfg.ServiceName][]types.NodeID)
				online = make(map[tailcfg.ServiceName][]types.NodeID)
			}

			all[name] = append(all[name], id)

			if n.Online() {
				online[name] = append(online[name], id)
			}
		}
	}

	for _, ids := range all {
		slices.Sort(ids)
	}

	for _, ids := range online {
		slices.Sort(ids)
	}

	return all, online
}

// electServices elects the host of every service for one snapshot.
func electServices(
	nodes map[types.NodeID]types.Node,
	in routeInputs,
	prev PrimaryRouteLedger,
) serviceElection {
	el := serviceElection{prefixes: in.services}

	all, online := serviceCandidates(nodes, in.services)
	if len(all) == 0 {
		return el
	}

	el.hosts = electPrefixes(nodes, online, prev.serviceHosts)

	for name, ids := range all {
		if _, ok := el.hosts[name]; ok {
			continue
		}

		switch cur, held := prev.serviceHosts[name]; {
		case held && slices.Contains(ids, cur):
			el.hosts[name] = cur
		case len(online[name]) > 0:
			el.hosts[name] = online[name][0]
		default:
			el.hosts[name] = ids[0]
		}
	}

	if in.selection.Regional() {
		el.regional = electRegionalRoutes(nodes, online, prev.serviceRegional)
	}

	if in.selection.Nearest() {
		el.contested = contestedRoutes(nodes, online)
	}

	el.names = slices.SortedFunc(maps.Keys(el.hosts), func(a, b tailcfg.ServiceName) int {
		return strings.Compare(string(a), string(b))
	})

	el.served = make(map[types.NodeID][]netip.Prefix, len(el.hosts))
	for _, name := range el.names {
		id := el.hosts[name]
		el.served[id] = append(el.served[id], el.prefixes[name]...)
	}

	return el
}

// hostFor is the host the viewer uses for a service.
func (el *serviceElection) hostFor(
	name tailcfg.ServiceName,
	viewer viewerSite,
	near map[*site][]site,
) types.NodeID {
	return pickFor(name, el.hosts[name], el.regional, el.contested, viewer, near, true)
}

// routesFor returns, by host, the addresses of the services the viewer
// reaches through that host, in service name order. Without steering it
// is the tailnet-wide assignment, which the caller must not mutate.
func (el *serviceElection) routesFor(viewer viewerSite) map[types.NodeID][]netip.Prefix {
	if len(el.contested) == 0 && len(el.regional) == 0 {
		return el.served
	}

	out := make(map[types.NodeID][]netip.Prefix, len(el.served))
	near := make(map[*site][]site)

	for _, name := range el.names {
		id := el.hostFor(name, viewer, near)
		out[id] = append(out[id], el.prefixes[name]...)
	}

	return out
}

// steeringDiffers reports whether a viewer that moved from before to
// after now uses another host for any service.
func (el *serviceElection) steeringDiffers(before, after viewerSite) bool {
	if len(el.contested) == 0 && len(el.regional) == 0 {
		return false
	}

	nearBefore, nearAfter := make(map[*site][]site), make(map[*site][]site)

	for _, name := range el.names {
		if el.hostFor(name, before, nearBefore) != el.hostFor(name, after, nearAfter) {
			return true
		}
	}

	return false
}

// servicePrefixes is the services table the election reads: each
// service's addresses by name.
func servicePrefixes(services []types.VIPService) map[tailcfg.ServiceName][]netip.Prefix {
	if len(services) == 0 {
		return nil
	}

	out := make(map[tailcfg.ServiceName][]netip.Prefix, len(services))
	for _, svc := range services {
		out[svc.Name] = svc.Prefixes()
	}

	return out
}
