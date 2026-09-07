package types

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// GroupID identifies a group in the groups table.
type GroupID uint64

// String renders the ID in base 10.
func (id GroupID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// AccessRuleID identifies a rule in the access_rules table.
type AccessRuleID uint64

// String renders the ID in base 10.
func (id AccessRuleID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// GroupBuiltinAll marks the group that holds every node. It exists once,
// is created by the server and cannot be edited or deleted.
const GroupBuiltinAll = "all"

// GroupAllName is the name of the builtin group that holds every node.
const GroupAllName = "All"

// AccessGroup is a named set of nodes. Nodes belong to it directly or
// through their owner: every user-owned node of a user in UserIDs is a
// member. Tagged nodes have no owner and join directly.
type AccessGroup struct {
	ID          GroupID
	Name        string
	Description string
	// Builtin is empty for groups an operator made and [GroupBuiltinAll]
	// for the group that holds every node.
	Builtin   string
	NodeIDs   []NodeID
	UserIDs   []UserID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsBuiltin reports whether the server owns the group.
func (g AccessGroup) IsBuiltin() bool {
	return g.Builtin != ""
}

// Contains reports whether the node is a member, through its owner or
// directly. The builtin all group contains every node.
func (g AccessGroup) Contains(node NodeView) bool {
	if g.Builtin == GroupBuiltinAll {
		return true
	}

	if slices.Contains(g.NodeIDs, node.ID()) {
		return true
	}

	if node.IsTagged() || !node.UserID().Valid() {
		return false
	}

	return slices.Contains(g.UserIDs, UserID(node.UserID().Get()))
}

// AccessProtocol is the protocol an access rule matches.
type AccessProtocol string

const (
	AccessProtocolAll  AccessProtocol = "all"
	AccessProtocolTCP  AccessProtocol = "tcp"
	AccessProtocolUDP  AccessProtocol = "udp"
	AccessProtocolICMP AccessProtocol = "icmp"
)

// ErrInvalidAccessProtocol is returned for a protocol outside the four
// the rules accept.
var ErrInvalidAccessProtocol = errors.New("protocol must be one of all, tcp, udp, icmp")

// ParseAccessProtocol validates a protocol name.
func ParseAccessProtocol(s string) (AccessProtocol, error) {
	switch p := AccessProtocol(strings.ToLower(s)); p {
	case AccessProtocolAll, AccessProtocolTCP, AccessProtocolUDP, AccessProtocolICMP:
		return p, nil
	default:
		return "", fmt.Errorf("%w, got %q", ErrInvalidAccessProtocol, s)
	}
}

// HasPorts reports whether the protocol carries ports.
func (p AccessProtocol) HasPorts() bool {
	return p == AccessProtocolTCP || p == AccessProtocolUDP
}

// AccessRule lets the source groups reach the destination groups on the
// protocol and ports. Rules only allow; there is no deny. Bidirectional
// rules let both sides start connections.
type AccessRule struct {
	ID          AccessRuleID
	Name        string
	Description string
	Enabled     bool
	Protocol    AccessProtocol
	// Ports is the comma-separated port list ("22,80,8000-8100"); empty
	// means every port. Only tcp and udp carry ports.
	Ports               string
	Bidirectional       bool
	SourceGroupIDs      []GroupID
	DestinationGroupIDs []GroupID
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// AccessModel is every group, rule and network, loaded together because
// the policy compiles them together.
type AccessModel struct {
	Groups   []AccessGroup
	Rules    []AccessRule
	Networks []Network
}

// Network returns the network with the ID.
func (m AccessModel) Network(id NetworkID) (Network, bool) {
	for _, n := range m.Networks {
		if n.ID == id {
			return n, true
		}
	}

	return Network{}, false
}

// NetworksUsingGroup lists the networks that distribute to the group.
func (m AccessModel) NetworksUsingGroup(id GroupID) []Network {
	var networks []Network

	for _, n := range m.Networks {
		if slices.Contains(n.GroupIDs, id) {
			networks = append(networks, n)
		}
	}

	return networks
}

// NetworkRoutes returns the prefixes the enabled networks assign to the
// node as a router, without duplicates.
func (m AccessModel) NetworkRoutes(nodeID NodeID) []netip.Prefix {
	var routes []netip.Prefix

	for _, n := range m.Networks {
		if !n.Routes(nodeID) {
			continue
		}

		for _, p := range n.Prefixes {
			if !slices.Contains(routes, p) {
				routes = append(routes, p)
			}
		}
	}

	return routes
}

// NetworksRouting lists the enabled networks in which the node routes
// the prefix.
func (m AccessModel) NetworksRouting(nodeID NodeID, prefix netip.Prefix) []Network {
	var networks []Network

	for _, n := range m.Networks {
		if n.Routes(nodeID) && n.Covers(prefix) {
			networks = append(networks, n)
		}
	}

	return networks
}

// MemberOfAny reports whether the node is in one of the groups.
func (m AccessModel) MemberOfAny(node NodeView, ids []GroupID) bool {
	for _, id := range ids {
		if g, ok := m.Group(id); ok && g.Contains(node) {
			return true
		}
	}

	return false
}

// Group returns the group with the ID.
func (m AccessModel) Group(id GroupID) (AccessGroup, bool) {
	for _, g := range m.Groups {
		if g.ID == id {
			return g, true
		}
	}

	return AccessGroup{}, false
}

// RulesUsingGroup lists the rules that name the group on either side.
func (m AccessModel) RulesUsingGroup(id GroupID) []AccessRule {
	var rules []AccessRule

	for _, r := range m.Rules {
		if slices.Contains(r.SourceGroupIDs, id) || slices.Contains(r.DestinationGroupIDs, id) {
			rules = append(rules, r)
		}
	}

	return rules
}

// Errors returned by the validation helpers.
var (
	ErrGroupNameEmpty   = errors.New("group name must not be empty")
	ErrGroupNameInvalid = errors.New(
		"group name may only contain letters, digits, spaces, dots, dashes and underscores",
	)
	ErrGroupNameTooLong   = errors.New("group name must be at most 64 characters")
	ErrGroupBuiltin       = errors.New("builtin group cannot be changed")
	ErrGroupInUse         = errors.New("group is used by an access rule")
	ErrGroupNotFound      = errors.New("group not found")
	ErrGroupNameTaken     = errors.New("group name already exists")
	ErrGroupMemberExists  = errors.New("already a member of the group")
	ErrGroupMemberMissing = errors.New("not a member of the group")
	ErrRuleNameEmpty      = errors.New("rule name must not be empty")
	ErrRuleNameTooLong    = errors.New("rule name must be at most 64 characters")
	ErrRuleNoSources      = errors.New("rule needs at least one source group")
	ErrRuleNoDestinations = errors.New("rule needs at least one destination group")
	ErrRuleNotFound       = errors.New("access rule not found")
	ErrRulePortsWithout   = errors.New("ports apply only to tcp and udp")
	ErrRulePortsInvalid   = errors.New("ports must be a comma-separated list of ports or ranges between 1 and 65535")
)

const maxAccessNameLength = 64

var groupNameRe = regexp.MustCompile(`^[\pL\pN][\pL\pN ._-]*$`)

// ValidateGroupName checks a group name an operator typed.
func ValidateGroupName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return ErrGroupNameEmpty
	case len([]rune(name)) > maxAccessNameLength:
		return ErrGroupNameTooLong
	case !groupNameRe.MatchString(name):
		return ErrGroupNameInvalid
	}

	return nil
}

// ValidateAccessRule checks the fields the operator controls. Group
// existence is checked by the caller against the model.
func ValidateAccessRule(rule AccessRule) error {
	switch {
	case strings.TrimSpace(rule.Name) == "":
		return ErrRuleNameEmpty
	case len([]rune(rule.Name)) > maxAccessNameLength:
		return ErrRuleNameTooLong
	case len(rule.SourceGroupIDs) == 0:
		return ErrRuleNoSources
	case len(rule.DestinationGroupIDs) == 0:
		return ErrRuleNoDestinations
	}

	_, err := ParseAccessProtocol(string(rule.Protocol))
	if err != nil {
		return err
	}

	if rule.Ports != "" && !rule.Protocol.HasPorts() {
		return ErrRulePortsWithout
	}

	_, err = ParsePortList(rule.Ports)

	return err
}

// PortRange is an inclusive port range; First == Last for a single port.
type PortRange struct {
	First uint16
	Last  uint16
}

// ParsePortList parses "22,80,8000-8100" into ranges. Empty means every
// port and returns nil.
func ParsePortList(s string) ([]PortRange, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	ranges := make([]PortRange, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		first, last, ok := strings.Cut(part, "-")

		lo, err := parsePort(first)
		if err != nil {
			return nil, err
		}

		hi := lo

		if ok {
			hi, err = parsePort(last)
			if err != nil {
				return nil, err
			}
		}

		if hi < lo {
			return nil, fmt.Errorf("%w: %q", ErrRulePortsInvalid, part)
		}

		ranges = append(ranges, PortRange{First: lo, Last: hi})
	}

	return ranges, nil
}

func parsePort(s string) (uint16, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 16)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("%w: %q", ErrRulePortsInvalid, s)
	}

	return uint16(n), nil
}

// NormalizePortList renders the ranges back in canonical form so the
// stored value is what the console shows.
func NormalizePortList(ranges []PortRange) string {
	parts := make([]string, 0, len(ranges))

	for _, r := range ranges {
		if r.First == r.Last {
			parts = append(parts, strconv.FormatUint(uint64(r.First), 10))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", r.First, r.Last))
		}
	}

	return strings.Join(parts, ",")
}
