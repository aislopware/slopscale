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

// GroupBuiltinSelf marks the group that, as a rule's destination, holds
// the machines owned by the same user as the source. It is Tailscale's
// autogroup:self: relational, so it has no members of its own, cannot be
// a source and is never joined.
const GroupBuiltinSelf = "self"

// GroupSelfName is the name of the builtin self group.
const GroupSelfName = "Own machines"

// DefaultRuleName names the rule the server seeds once per database: every
// machine reaches the other machines of its own user, and nothing else
// until an operator adds rules.
const DefaultRuleName = "Own machines"

// AccessGroup is a named set of nodes. Nodes belong to it directly or
// through their owner: every user-owned node of a user in UserIDs is a
// member. Tagged nodes have no owner and join directly.
type AccessGroup struct {
	ID          GroupID
	Name        string
	Description string
	// Builtin is empty for groups an operator made and [GroupBuiltinAll]
	// for the group that holds every node.
	Builtin string
	// Requestable lets members ask to join the group for a while; see
	// [AccessRequest].
	Requestable bool
	NodeIDs     []NodeID
	UserIDs     []UserID
	// NodeExpiries and UserExpiries hold the end of the temporary
	// memberships; a member absent from them is permanent.
	NodeExpiries map[NodeID]time.Time
	UserExpiries map[UserID]time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NodeExpiry returns when the node's direct membership ends, or zero
// for a permanent one.
func (g AccessGroup) NodeExpiry(id NodeID) time.Time {
	return g.NodeExpiries[id]
}

// UserExpiry returns when the user's membership ends, or zero for a
// permanent one.
func (g AccessGroup) UserExpiry(id UserID) time.Time {
	return g.UserExpiries[id]
}

// NextExpiry returns the earliest membership expiry after the instant,
// or zero when none is due.
func (g AccessGroup) NextExpiry(after time.Time) time.Time {
	var next time.Time

	for _, t := range g.NodeExpiries {
		next = earliestAfter(next, t, after)
	}

	for _, t := range g.UserExpiries {
		next = earliestAfter(next, t, after)
	}

	return next
}

// earliestAfter returns the earlier of next and t among those after the
// instant; zero counts as unset.
func earliestAfter(next, t, after time.Time) time.Time {
	if t.IsZero() || !t.After(after) {
		return next
	}

	if next.IsZero() || t.Before(next) {
		return t
	}

	return next
}

// memberAt reports whether a membership with the expiry counts at the
// instant.
func memberAt(expiry, now time.Time) bool {
	return expiry.IsZero() || now.Before(expiry)
}

// IsBuiltin reports whether the server owns the group.
func (g AccessGroup) IsBuiltin() bool {
	return g.Builtin != ""
}

// IsSelf reports whether the group is the builtin self group, which
// resolves per destination rather than to a member list.
func (g AccessGroup) IsSelf() bool {
	return g.Builtin == GroupBuiltinSelf
}

// Contains reports whether the node is a member now, through its owner
// or directly. The builtin all group contains every node.
func (g AccessGroup) Contains(node NodeView) bool {
	return g.ContainsAt(node, time.Now())
}

// ContainsAt is [AccessGroup.Contains] at an instant: a temporary
// membership counts until its expiry.
func (g AccessGroup) ContainsAt(node NodeView, now time.Time) bool {
	if g.Builtin == GroupBuiltinAll {
		return true
	}

	if slices.Contains(g.NodeIDs, node.ID()) && memberAt(g.NodeExpiry(node.ID()), now) {
		return true
	}

	if node.IsTagged() || !node.UserID().Valid() {
		return false
	}

	uid := UserID(node.UserID().Get())

	return slices.Contains(g.UserIDs, uid) && memberAt(g.UserExpiry(uid), now)
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
	// PostureIDs are the postures a source must satisfy, any one of
	// them; empty means no posture check.
	PostureIDs []PostureID
	// ExpiresAt is when the rule stops applying; nil never does. An
	// expired rule is kept, shown as expired, until it is extended or
	// deleted.
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Active reports whether the rule applies at the instant: enabled and
// not expired.
func (r AccessRule) Active(now time.Time) bool {
	return r.Enabled && (r.ExpiresAt == nil || now.Before(*r.ExpiresAt))
}

// Expired reports whether the rule's expiry has passed.
func (r AccessRule) Expired(now time.Time) bool {
	return r.ExpiresAt != nil && !now.Before(*r.ExpiresAt)
}

// AccessModel is every group, rule and network, loaded together because
// the policy compiles them together.
type AccessModel struct {
	Groups   []AccessGroup
	Rules    []AccessRule
	Networks []Network
	Postures []Posture
	DNSRules []GroupDNSRule
}

// NextExpiry returns the earliest rule or membership expiry after the
// instant, or zero when nothing is due: the moment the policy has to be
// recompiled without anyone touching it.
func (m AccessModel) NextExpiry(after time.Time) time.Time {
	var next time.Time

	for _, r := range m.Rules {
		if r.ExpiresAt != nil && r.Enabled {
			next = earliestAfter(next, *r.ExpiresAt, after)
		}
	}

	for _, g := range m.Groups {
		next = earliestAfter(next, g.NextExpiry(after), after)
	}

	return next
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
	ErrRuleSelfSource     = errors.New("the builtin self group can only be a destination")
	ErrRuleSelfBoth       = errors.New("a rule with the builtin self group cannot run both directions")
	ErrGroupSelfMembers   = errors.New("the builtin self group has no members of its own")
	ErrRuleNotFound       = errors.New("access rule not found")
	ErrRulePortsWithout   = errors.New("ports apply only to tcp and udp")
	ErrRulePortsInvalid   = errors.New("ports must be a comma-separated list of ports or ranges between 1 and 65535")
	ErrRuleExpiryPast     = errors.New("rule expiry must be in the future")
	ErrMemberExpiryPast   = errors.New("membership expiry must be in the future")
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
