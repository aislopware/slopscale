package types

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"tailscale.com/tailcfg"
)

// A VIP service is a name with a pair of addresses of its own that one or
// more nodes host, following Tailscale Services: clients reach it by
// name or by address and the server routes them to a host that
// advertises it. See docs/ref/services.md.

// VIPServiceID identifies a service row.
type VIPServiceID uint64

// Uint64 returns the ID as a plain integer.
func (id VIPServiceID) Uint64() uint64 { return uint64(id) }

// VIPService is a service the tailnet knows: its name, its addresses and
// what an operator wrote about it. Which nodes host it is recorded on the
// nodes, as their announced and approved services.
type VIPService struct {
	ID VIPServiceID
	// Name is the service's name with its svc: prefix; the part after it
	// is the MagicDNS label under the base domain.
	Name tailcfg.ServiceName
	// DisplayName is the label clients show; empty falls back to Name.
	DisplayName string
	// Comment is the operator's note.
	Comment string
	// Ports are the protocol and port ranges clients are told the
	// service listens on. Empty means the union of what the hosts serve.
	Ports []tailcfg.ProtoPortRange
	// IPv4 and IPv6 are the service's own addresses, allocated from the
	// tailnet's prefixes when the service is created.
	IPv4 *netip.Addr
	IPv6 *netip.Addr

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Addrs returns the service's addresses.
func (s VIPService) Addrs() []netip.Addr {
	addrs := make([]netip.Addr, 0, 2)

	if s.IPv4 != nil {
		addrs = append(addrs, *s.IPv4)
	}

	if s.IPv6 != nil {
		addrs = append(addrs, *s.IPv6)
	}

	return addrs
}

// Prefixes returns the service's addresses as single-address prefixes,
// the shape AllowedIPs carries.
func (s VIPService) Prefixes() []netip.Prefix {
	addrs := s.Addrs()
	prefixes := make([]netip.Prefix, 0, len(addrs))

	for _, addr := range addrs {
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}

	return prefixes
}

// DNSName is the service's MagicDNS name under the base domain, without
// a trailing dot.
func (s VIPService) DNSName(baseDomain string) string {
	return strings.ToLower(s.Name.WithoutPrefix() + "." + strings.TrimSuffix(baseDomain, "."))
}

// Details is what a client that can reach the service is told about it.
func (s VIPService) Details(ports []tailcfg.ProtoPortRange) tailcfg.ServiceDetails {
	if len(s.Ports) > 0 {
		ports = s.Ports
	}

	return tailcfg.ServiceDetails{
		Name:        s.Name,
		DisplayName: s.DisplayName,
		Addrs:       s.Addrs(),
		Ports:       ports,
	}
}

// ErrVIPServiceNotFound is returned for a service name the tailnet does
// not have.
var ErrVIPServiceNotFound = errors.New("service not found")

// ErrVIPServiceNameTaken is returned when a service with the name exists.
var ErrVIPServiceNameTaken = errors.New("a service with that name exists")

// ErrVIPServiceName wraps what is wrong with a service name.
var ErrVIPServiceName = errors.New("invalid service name")

// ErrVIPServicePorts wraps what is wrong with a port specification.
var ErrVIPServicePorts = errors.New("invalid service ports")

// ErrVIPServiceHostTagged is returned when a user-owned node is approved
// to host a service; Tailscale Services are hosted by tagged nodes only,
// because a host answers for a name that outlives any one person.
var ErrVIPServiceHostTagged = errors.New("only a tagged node can host a service")

// ParseServiceName accepts a service name with or without its svc:
// prefix and returns it with the prefix, validated as a DNS label.
func ParseServiceName(name string) (tailcfg.ServiceName, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(name, "svc:") {
		name = "svc:" + name
	}

	sn := tailcfg.ServiceName(name)

	err := sn.Validate()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrVIPServiceName, err)
	}

	return sn, nil
}

// ParseServicePorts parses port specifications such as "tcp:443",
// "udp:53-60" or "tcp:*".
func ParseServicePorts(ports []string) ([]tailcfg.ProtoPortRange, error) {
	if len(ports) == 0 {
		return nil, nil
	}

	parsed, err := tailcfg.ParseProtoPortRanges(ports)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrVIPServicePorts, err)
	}

	return parsed, nil
}

// ServicePortsStrings renders port ranges the way [ParseServicePorts]
// reads them.
func ServicePortsStrings(ports []tailcfg.ProtoPortRange) []string {
	out := make([]string, 0, len(ports))

	for _, p := range ports {
		out = append(out, p.String())
	}

	return out
}

// NodeServices is what a node last reported hosting over its control
// connection: the services in its serve configuration, with the ports
// each listens on and whether the node is advertising it, and the hash
// the client stamps in its Hostinfo so the server knows when to ask
// again.
type NodeServices struct {
	// Hash is the client's own hash of the list, as in
	// [tailcfg.Hostinfo.ServicesHash].
	Hash string
	// Services are the reported services, sorted by name.
	Services []tailcfg.VIPService
}

// Announced returns the reported service with the name, if any.
func (ns *NodeServices) Announced(name tailcfg.ServiceName) (tailcfg.VIPService, bool) {
	if ns == nil {
		return tailcfg.VIPService{}, false
	}

	for _, svc := range ns.Services {
		if svc.Name == name {
			return svc, true
		}
	}

	return tailcfg.VIPService{}, false
}

// AnnouncedServices lists the services the node reported hosting, in
// name order.
func (nv NodeView) AnnouncedServices() []tailcfg.VIPService {
	if !nv.Valid() || nv.ж.Services == nil {
		return nil
	}

	return slices.Clone(nv.ж.Services.Services)
}

// ServicesHash is the client's hash of its last report; empty until it
// reported.
func (nv NodeView) ServicesHash() string {
	if !nv.Valid() || nv.ж.Services == nil {
		return ""
	}

	return nv.ж.Services.Hash
}

// HostsService reports whether the node is approved for the service and
// reports it: the node gets the service's addresses to listen on. The
// second return is whether it also advertises it, which is what makes it
// a candidate for traffic.
func (nv NodeView) HostsService(name tailcfg.ServiceName) (bool, bool) {
	if !nv.Valid() || !nv.ApprovedServices().ContainsFunc(func(s string) bool { return s == string(name) }) {
		return false, false
	}

	svc, ok := nv.ж.Services.Announced(name)
	if !ok {
		return false, false
	}

	return true, svc.Active
}

// HostedServices lists the services the node is approved for and
// reports, in name order.
func (nv NodeView) HostedServices() []tailcfg.ServiceName {
	var out []tailcfg.ServiceName

	for _, svc := range nv.AnnouncedServices() {
		if nv.ApprovedServices().ContainsFunc(func(s string) bool { return s == string(svc.Name) }) {
			out = append(out, svc.Name)
		}
	}

	return out
}

// ServicesEqualForPeers reports whether two views of a node host the
// same services with the same ports and advertisement, which is what
// the policy and the peers read.
func (nv NodeView) ServicesEqualForPeers(other NodeView) bool {
	if !nv.Valid() || !other.Valid() {
		return nv.Valid() == other.Valid()
	}

	return nv.ж.servicesEqual(other.ж)
}

// servicesEqual is [NodeView.ServicesEqualForPeers] on the structs.
func (node *Node) servicesEqual(other *Node) bool {
	if !slices.Equal(node.ApprovedServices, other.ApprovedServices) {
		return false
	}

	var a, b []tailcfg.VIPService

	if node.Services != nil {
		a = node.Services.Services
	}

	if other.Services != nil {
		b = other.Services.Services
	}

	return slices.EqualFunc(a, b, func(x, y tailcfg.VIPService) bool {
		return x.Name == y.Name && x.Active == y.Active && slices.Equal(x.Ports, y.Ports)
	})
}

// IsServiceAddress reports whether a prefix in a node's routes is a
// single address from the tailnet's own prefixes, which is the shape
// only a service address has: a subnet route never lies inside them.
func (c *Config) IsServiceAddress(p netip.Prefix) bool {
	if !p.IsSingleIP() {
		return false
	}

	return (c.PrefixV4 != nil && c.PrefixV4.Contains(p.Addr())) ||
		(c.PrefixV6 != nil && c.PrefixV6.Contains(p.Addr()))
}
