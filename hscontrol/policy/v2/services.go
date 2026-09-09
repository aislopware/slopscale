package v2

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/policy/matcher"
	"github.com/aislopware/slopscale/hscontrol/types"
	"go4.org/netipx"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/views"
)

// Tailscale Services in the policy: a `svc:name` alias resolves to the
// service's own addresses, so a rule can open a service without naming
// its hosts, and autoApprovers.services lets nodes host one without an
// operator approving each. See docs/ref/services.md.

// Service is a `svc:name` alias, valid as a destination.
type Service string

// Validate enforces the svc: prefix and a DNS label after it.
func (s *Service) Validate() error {
	if !isService(string(*s)) {
		return fmt.Errorf("%w: %q", ErrInvalidServiceFormat, *s)
	}

	err := tailcfg.ServiceName(*s).Validate()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidServiceFormat, err)
	}

	return nil
}

func (s *Service) UnmarshalJSON(b []byte) error {
	*s = Service(strings.Trim(string(b), `"`))

	return s.Validate()
}

// MarshalJSON marshals the service alias to JSON.
func (s *Service) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(string(*s))
	if err != nil {
		return nil, fmt.Errorf("marshaling service: %w", err)
	}

	return b, nil
}

func (s *Service) String() string {
	return string(*s)
}

// Name returns the alias as a service name.
func (s *Service) Name() tailcfg.ServiceName {
	return tailcfg.ServiceName(*s)
}

// CanBeAutoApprover is false: a service cannot approve anything.
func (s *Service) CanBeAutoApprover() bool {
	return false
}

func (s *Service) Resolve(p *Policy, users types.Users, nodes views.Slice[types.NodeView]) (ResolvedAddresses, error) {
	return newResolvedAddresses(s.resolve(p, users, nodes))
}

// resolve returns the service's addresses, or nothing while the tailnet
// has no service of that name: the rule waits for the service the way a
// tag rule waits for its first node.
func (s *Service) resolve(p *Policy, _ types.Users, _ views.Slice[types.NodeView]) (*netipx.IPSet, error) {
	var ips netipx.IPSetBuilder

	if p != nil {
		if svc, ok := p.services[s.Name()]; ok {
			for _, addr := range svc.Addrs() {
				ips.Add(addr)
			}
		}
	}

	set, err := ips.IPSet()
	if err != nil {
		return nil, fmt.Errorf("building service address set: %w", err)
	}

	return set, nil
}

func isService(str string) bool {
	return strings.HasPrefix(str, "svc:")
}

// servicesByName indexes the tailnet's services for the resolver.
func servicesByName(services []types.VIPService) map[tailcfg.ServiceName]types.VIPService {
	if len(services) == 0 {
		return nil
	}

	out := make(map[tailcfg.ServiceName]types.VIPService, len(services))
	for _, svc := range services {
		out[svc.Name] = svc
	}

	return out
}

// resolveServiceAutoApprovers resolves autoApprovers.services to the
// addresses of the nodes each entry lets host the service.
func resolveServiceAutoApprovers(
	p *Policy,
	users types.Users,
	nodes views.Slice[types.NodeView],
) (map[tailcfg.ServiceName]*netipx.IPSet, error) {
	if p == nil || len(p.AutoApprovers.Services) == 0 {
		return map[tailcfg.ServiceName]*netipx.IPSet{}, nil
	}

	out := make(map[tailcfg.ServiceName]*netipx.IPSet, len(p.AutoApprovers.Services))

	for name, approvers := range p.AutoApprovers.Services {
		var b netipx.IPSetBuilder

		for _, approver := range approvers {
			aa, ok := approver.(Alias)
			if !ok {
				return nil, fmt.Errorf("%w: %v", ErrAutoApproverNotAlias, approver)
			}

			// An approver that resolves to nothing approves nobody.
			ips, _ := aa.resolve(p, users, nodes)
			b.AddSet(ips)
		}

		set, err := b.IPSet()
		if err != nil {
			return nil, fmt.Errorf("building service approver set for %s: %w", name, err)
		}

		out[name] = set
	}

	return out, nil
}

// SetVIPServices replaces the tailnet's services and recompiles: svc:
// aliases resolve against them and the service caps are stamped from
// them.
func (pm *PolicyManager) SetVIPServices(services []types.VIPService) (bool, error) {
	if pm == nil {
		return false, nil
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.vipServices = slices.Clone(services)

	return pm.updateLocked()
}

// NodeCanApproveService reports whether autoApprovers.services lets the
// node host the service without an operator.
func (pm *PolicyManager) NodeCanApproveService(node types.NodeView, name tailcfg.ServiceName) bool {
	if pm == nil || !node.Valid() {
		return false
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	approvers, ok := pm.autoApproveServices[name]
	if !ok || approvers == nil {
		return false
	}

	return slices.ContainsFunc(node.IPs(), approvers.Contains)
}

// stampServiceCaps adds to capMaps what Tailscale Services put on the
// self view: services-in-desktop-clients on every node while the tailnet
// has a service, service-host with the addresses of the services a node
// hosts, and one services/<name> entry per service the node can reach
// and somebody advertises.
func stampServiceCaps(
	services []types.VIPService,
	nodes views.Slice[types.NodeView],
	reachable func(node types.NodeView, svc types.VIPService) bool,
	capMaps map[types.NodeID]tailcfg.NodeCapMap,
) {
	if len(services) == 0 {
		return
	}

	capMapOf := func(id types.NodeID) tailcfg.NodeCapMap {
		capMap, ok := capMaps[id]
		if !ok {
			capMap = tailcfg.NodeCapMap{}
			capMaps[id] = capMap
		}

		return capMap
	}

	byName := servicesByName(services)
	advertised := advertisedPorts(byName, nodes)

	for _, node := range nodes.All() {
		capMap := capMapOf(node.ID())
		capMap[nodecap.ServicesInDesktopClients] = nil

		if mapping := serviceHostMapping(byName, node); mapping != nil {
			capMap[nodecap.ServiceHost] = mapping
		}

		for _, name := range slices.Sorted(maps.Keys(advertised)) {
			svc := byName[name]
			if !reachable(node, svc) {
				continue
			}

			raw, err := json.Marshal(svc.Details(advertised[name]))
			if err != nil {
				continue
			}

			capMap[nodecap.Cap(string(nodecap.ServicesPrefix)+svc.Name.WithoutPrefix())] = []tailcfg.RawMessage{
				tailcfg.RawMessage(raw),
			}
		}
	}
}

// serviceHostMapping is the service-host value for a node: the addresses
// of every service it is approved for and reports, whether or not it
// advertises it yet, because the client waits for the mapping before it
// lets the user advertise.
func serviceHostMapping(
	byName map[tailcfg.ServiceName]types.VIPService, node types.NodeView,
) []tailcfg.RawMessage {
	mapping := tailcfg.ServiceIPMappings{}

	for _, name := range node.HostedServices() {
		svc, ok := byName[name]
		if !ok {
			continue
		}

		mapping[name] = svc.Addrs()
	}

	if len(mapping) == 0 {
		return nil
	}

	raw, err := json.Marshal(mapping)
	if err != nil {
		return nil
	}

	return []tailcfg.RawMessage{tailcfg.RawMessage(raw)}
}

// advertisedPorts lists the services at least one approved host
// advertises, with the union of the ports those hosts serve.
func advertisedPorts(
	byName map[tailcfg.ServiceName]types.VIPService, nodes views.Slice[types.NodeView],
) map[tailcfg.ServiceName][]tailcfg.ProtoPortRange {
	out := make(map[tailcfg.ServiceName][]tailcfg.ProtoPortRange)

	for _, node := range nodes.All() {
		if !node.IsAdmitted() || node.IsExpired() {
			continue
		}

		for _, svc := range node.AnnouncedServices() {
			if _, known := byName[svc.Name]; !known || !svc.Active {
				continue
			}

			if !node.ApprovedServices().ContainsFunc(func(s string) bool { return s == string(svc.Name) }) {
				continue
			}

			ports := out[svc.Name]

			for _, p := range svc.Ports {
				if !slices.Contains(ports, p) {
					ports = append(ports, p)
				}
			}

			out[svc.Name] = ports
		}
	}

	for name, ports := range out {
		slices.SortFunc(ports, func(a, b tailcfg.ProtoPortRange) int {
			return strings.Compare(a.String(), b.String())
		})
		out[name] = ports
	}

	return out
}

// serviceReachable is the reachability rule for services/<name> caps: an
// open tailnet reaches everything; otherwise some rule must let the node
// reach one of the service's addresses.
func serviceReachable(enforces bool, matchers []matcher.Match) func(types.NodeView, types.VIPService) bool {
	return func(node types.NodeView, svc types.VIPService) bool {
		if !enforces {
			return true
		}

		ips := node.IPs()
		addrs := svc.Addrs()

		for i := range matchers {
			if matchers[i].SrcsContainsIPs(ips...) && matchers[i].DestsContainsIP(addrs...) {
				return true
			}
		}

		return false
	}
}

// ServicePrefixes returns the addresses of the named services as
// single-address prefixes, in name order; unknown names are skipped.
func ServicePrefixes(services []types.VIPService, names []tailcfg.ServiceName) []netip.Prefix {
	byName := servicesByName(services)

	var out []netip.Prefix

	for _, name := range names {
		if svc, ok := byName[name]; ok {
			out = append(out, svc.Prefixes()...)
		}
	}

	return out
}
