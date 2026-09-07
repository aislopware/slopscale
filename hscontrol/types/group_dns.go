package types

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// GroupDNSRuleID identifies a rule in the group_dns_rules table.
type GroupDNSRuleID uint64

// String renders the ID in base 10.
func (id GroupDNSRuleID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// GroupDNSRule is split DNS for the machines of some groups only: queries
// for the domains go to the nameservers on those machines, the way the
// global split DNS works for everyone. A domain several rules name gets
// every rule's nameservers.
type GroupDNSRule struct {
	ID          GroupDNSRuleID
	Name        string
	Description string
	Enabled     bool
	// Domains are the zones the nameservers answer for, normalized.
	Domains []string
	// Nameservers are resolver addresses, the forms DNSSettings accept.
	Nameservers []string
	// GroupIDs are the groups whose machines receive the rule.
	GroupIDs  []GroupID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Errors returned by the group DNS rule validation.
var (
	ErrGroupDNSRuleNameEmpty     = errors.New("dns rule name must not be empty")
	ErrGroupDNSRuleNameTooLong   = errors.New("dns rule name must be at most 64 characters")
	ErrGroupDNSRuleNameTaken     = errors.New("dns rule name already exists")
	ErrGroupDNSRuleNoDomains     = errors.New("dns rule needs at least one domain")
	ErrGroupDNSRuleNoNameservers = errors.New("dns rule needs at least one nameserver")
	ErrGroupDNSRuleNoGroups      = errors.New("dns rule needs at least one group")
	ErrGroupDNSRuleNotFound      = errors.New("dns rule not found")
	ErrGroupDNSRuleReservedZone  = errors.New("dns rule domain is resolved by MagicDNS")
)

// Normalize trims and lowercases the domains and drops blanks and
// duplicates from every list.
func (r GroupDNSRule) Normalize() GroupDNSRule {
	r.Name = strings.TrimSpace(r.Name)
	r.Domains = normalizeList(r.Domains, normalizeDomain)
	r.Nameservers = normalizeList(r.Nameservers, strings.TrimSpace)
	r.GroupIDs = slices.Compact(slices.Clone(r.GroupIDs))

	return r
}

// ValidateGroupDNSRule checks the fields the operator controls. Group
// existence is checked by the caller against the model.
func ValidateGroupDNSRule(r GroupDNSRule) error {
	switch {
	case r.Name == "":
		return ErrGroupDNSRuleNameEmpty
	case len([]rune(r.Name)) > maxAccessNameLength:
		return ErrGroupDNSRuleNameTooLong
	case len(r.Domains) == 0:
		return ErrGroupDNSRuleNoDomains
	case len(r.Nameservers) == 0:
		return ErrGroupDNSRuleNoNameservers
	case len(r.GroupIDs) == 0:
		return ErrGroupDNSRuleNoGroups
	}

	for _, d := range r.Domains {
		err := validateDomain(d)
		if err != nil {
			return err
		}
	}

	for _, ns := range r.Nameservers {
		err := validateNameserver(ns)
		if err != nil {
			return fmt.Errorf("%w for %q", err, r.Name)
		}
	}

	return nil
}

// DNSRulesUsingGroup returns the rules that hand their domains to the group.
func (m AccessModel) DNSRulesUsingGroup(id GroupID) []GroupDNSRule {
	var rules []GroupDNSRule

	for _, r := range m.DNSRules {
		if slices.Contains(r.GroupIDs, id) {
			rules = append(rules, r)
		}
	}

	return rules
}

// DNSRoutes returns the split DNS the node gets from the enabled rules
// of its groups: domain to nameservers, each list in rule order without
// duplicates. Nil when no rule applies.
func (m AccessModel) DNSRoutes(node NodeView) map[string][]string {
	var routes map[string][]string

	for _, r := range m.DNSRules {
		if !r.Enabled || !m.MemberOfAny(node, r.GroupIDs) {
			continue
		}

		if routes == nil {
			routes = make(map[string][]string)
		}

		for _, d := range r.Domains {
			for _, ns := range r.Nameservers {
				if !slices.Contains(routes[d], ns) {
					routes[d] = append(routes[d], ns)
				}
			}
		}
	}

	return routes
}
