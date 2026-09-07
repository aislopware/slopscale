package v2

import (
	"errors"
	"fmt"

	"github.com/juanfont/headscale/hscontrol/types"
	"go4.org/netipx"
	"tailscale.com/tailcfg"
	"tailscale.com/types/views"
)

// errAccessGroupNotInPolicyFile is returned when the policy file names
// an access group, which it cannot: groups are addressed through the
// API and compile into grants next to the file's own.
var errAccessGroupNotInPolicyFile = errors.New("access groups cannot be written in the policy file")

// accessGroup is the [Alias] an access rule's sides compile to. It
// resolves to the addresses of the group's members: nodes listed
// directly and the user-owned nodes of its users, or every node for the
// builtin all group.
type accessGroup struct {
	group types.AccessGroup
}

func (g *accessGroup) Validate() error { return nil }

func (g *accessGroup) UnmarshalJSON([]byte) error { return errAccessGroupNotInPolicyFile }

func (g *accessGroup) String() string { return "accessgroup:" + g.group.ID.String() }

func (g *accessGroup) Resolve(
	p *Policy,
	users types.Users,
	nodes views.Slice[types.NodeView],
) (ResolvedAddresses, error) {
	return newResolvedAddresses(g.resolve(p, users, nodes))
}

func (g *accessGroup) resolve(_ *Policy, _ types.Users, nodes views.Slice[types.NodeView]) (*netipx.IPSet, error) {
	var ips netipx.IPSetBuilder

	for _, node := range nodes.All() {
		if g.group.Contains(node) {
			node.AppendToIPSet(&ips)
		}
	}

	ipset, err := ips.IPSet()
	if err != nil {
		return nil, fmt.Errorf("building IP set for %s: %w", g, err)
	}

	return ipset, nil
}

// accessGrants turns the enabled rules of the model into grants. A
// bidirectional rule is two grants, one per direction. A rule that
// names a group the model no longer has skips that side entry.
func accessGrants(model types.AccessModel) []Grant {
	grants := make([]Grant, 0, len(model.Rules))

	for _, rule := range model.Rules {
		if !rule.Enabled {
			continue
		}

		sources := groupAliases(model, rule.SourceGroupIDs)
		destinations := groupAliases(model, rule.DestinationGroupIDs)

		if len(sources) == 0 || len(destinations) == 0 {
			continue
		}

		protocols := accessProtocolPorts(rule)

		grants = append(grants, Grant{Sources: sources, Destinations: destinations, InternetProtocols: protocols})

		if rule.Bidirectional {
			grants = append(grants, Grant{Sources: destinations, Destinations: sources, InternetProtocols: protocols})
		}
	}

	return grants
}

func groupAliases(model types.AccessModel, ids []types.GroupID) Aliases {
	aliases := make(Aliases, 0, len(ids))

	for _, id := range ids {
		group, ok := model.Group(id)
		if !ok {
			continue
		}

		aliases = append(aliases, &accessGroup{group: group})
	}

	return aliases
}

// accessProtocolPorts renders the rule's protocol and ports the way a
// grant's "ip" field parses. ICMP covers both IP versions.
func accessProtocolPorts(rule types.AccessRule) []ProtocolPort {
	ports := []tailcfg.PortRange{tailcfg.PortRangeAny}

	if rule.Protocol.HasPorts() {
		ranges, err := types.ParsePortList(rule.Ports)
		if err == nil && len(ranges) > 0 {
			ports = make([]tailcfg.PortRange, 0, len(ranges))
			for _, r := range ranges {
				ports = append(ports, tailcfg.PortRange{First: r.First, Last: r.Last})
			}
		}
	}

	switch rule.Protocol {
	case types.AccessProtocolTCP:
		return []ProtocolPort{{Protocol: ProtocolNameTCP, Ports: ports}}
	case types.AccessProtocolUDP:
		return []ProtocolPort{{Protocol: ProtocolNameUDP, Ports: ports}}
	case types.AccessProtocolICMP:
		return []ProtocolPort{
			{Protocol: ProtocolNameICMP, Ports: ports},
			{Protocol: ProtocolNameIPv6ICMP, Ports: ports},
		}
	case types.AccessProtocolAll:
		return []ProtocolPort{{Protocol: ProtocolNameWildcard, Ports: ports}}
	default:
		return nil
	}
}

// hasAccessGrants reports whether the model enforces anything: one
// enabled rule with both sides makes the tailnet default-deny.
func hasAccessGrants(model types.AccessModel) bool {
	return len(accessGrants(model)) > 0
}
