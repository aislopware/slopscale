package v2

import (
	"errors"
	"fmt"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"go4.org/netipx"
	"tailscale.com/net/tsaddr"
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

// accessGrants turns the enabled rules and networks of the model into
// grants.
func accessGrants(model types.AccessModel) []Grant {
	return append(ruleGrants(model), networkGrants(model)...)
}

// ruleGrants turns the active rules of the model into grants: enabled
// and not expired. A bidirectional rule is two grants, one per direction.
// A rule that names a group the model no longer has skips that side
// entry.
func ruleGrants(model types.AccessModel) []Grant {
	grants := make([]Grant, 0, len(model.Rules))
	now := time.Now()

	for _, rule := range model.Rules {
		if !rule.Active(now) {
			continue
		}

		sources := groupAliases(model, rule.SourceGroupIDs)
		destinations := groupAliases(model, rule.DestinationGroupIDs)

		if len(sources) == 0 || len(destinations) == 0 {
			continue
		}

		protocols := accessProtocolPorts(rule)
		postures := rulePostureNames(model, rule)

		grants = append(grants, Grant{
			Sources: sources, Destinations: destinations, InternetProtocols: protocols, SrcPosture: postures,
		})

		if rule.Bidirectional {
			grants = append(grants, Grant{
				Sources: destinations, Destinations: sources, InternetProtocols: protocols, SrcPosture: postures,
			})
		}
	}

	return grants
}

// rulePostureNames names the rule's postures the way [grantPostures]
// resolves database postures; one the model no longer has is skipped. A
// rule whose postures were all deleted gets an empty, non-nil list so
// the file's defaultSrcPosture does not apply to it.
func rulePostureNames(model types.AccessModel, rule types.AccessRule) []string {
	if len(rule.PostureIDs) == 0 {
		return []string{}
	}

	names := make([]string, 0, len(rule.PostureIDs))

	for _, id := range rule.PostureIDs {
		if _, ok := model.Posture(id); ok {
			names = append(names, dbPostureName(id))
		}
	}

	return names
}

// groupAliases turns the group IDs into aliases. The builtin self group
// is autogroup:self, which the compiler resolves per destination node;
// the rule validation keeps it out of sources.
func groupAliases(model types.AccessModel, ids []types.GroupID) Aliases {
	aliases := make(Aliases, 0, len(ids))

	for _, id := range ids {
		group, ok := model.Group(id)
		if !ok {
			continue
		}

		if group.IsSelf() {
			self := AutoGroupSelf
			aliases = append(aliases, &self)

			continue
		}

		aliases = append(aliases, &accessGroup{group: group})
	}

	return aliases
}

// accessProtocolPorts renders the rule's protocol and ports the way a
// grant's "ip" field parses. ICMP covers both IP versions.
func accessProtocolPorts(rule types.AccessRule) []ProtocolPort {
	return protocolPorts(rule.Protocol, rule.Ports)
}

// protocolPorts renders a protocol and port list the way a grant
// carries them. An empty port list is every port.
func protocolPorts(protocol types.AccessProtocol, portList string) []ProtocolPort {
	ports := []tailcfg.PortRange{tailcfg.PortRangeAny}

	if protocol.HasPorts() {
		ranges, err := types.ParsePortList(portList)
		if err == nil && len(ranges) > 0 {
			ports = make([]tailcfg.PortRange, 0, len(ranges))
			for _, r := range ranges {
				ports = append(ports, tailcfg.PortRange{First: r.First, Last: r.Last})
			}
		}
	}

	switch protocol {
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

// networkGrants lets the groups of every enabled network reach its
// prefixes on every port. They only matter once something else makes
// the tailnet enforce; a network alone keeps it open, and the routes
// themselves are handed out by membership, not by the filter.
func networkGrants(model types.AccessModel) []Grant {
	grants := make([]Grant, 0, len(model.Networks))

	for _, network := range model.Networks {
		if !network.Enabled {
			continue
		}

		sources := groupAliases(model, network.GroupIDs)
		if len(sources) == 0 || len(network.Prefixes) == 0 {
			continue
		}

		destinations := make(Aliases, 0, len(network.Prefixes))
		internet := false

		for _, p := range network.Prefixes {
			// An exit route as a literal 0.0.0.0/0 would also grant every
			// tailnet address; autogroup:internet is the Internet alone.
			if tsaddr.IsExitRoute(p) {
				internet = true

				continue
			}

			prefix := Prefix(p)
			destinations = append(destinations, &prefix)
		}

		if internet {
			autogroup := AutoGroupInternet
			destinations = append(destinations, &autogroup)
		}

		grants = append(grants, Grant{
			Sources:           sources,
			Destinations:      destinations,
			InternetProtocols: protocolPorts(network.ProtocolOrAll(), network.Ports),
		})
	}

	return grants
}

// hasAccessGrants reports whether the model enforces anything: one
// enabled rule with both sides makes the tailnet default-deny. Networks
// do not count; see [networkGrants].
func hasAccessGrants(model types.AccessModel) bool {
	return len(ruleGrants(model)) > 0
}
