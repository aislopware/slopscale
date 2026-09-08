package state

import (
	"fmt"
	"strings"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
	"tailscale.com/types/dnstype"
)

// ListGroupDNSRules returns every group DNS rule in ID order.
func (s *State) ListGroupDNSRules() []types.GroupDNSRule {
	return s.AccessModel().DNSRules
}

// GetGroupDNSRule returns the rule with the ID.
func (s *State) GetGroupDNSRule(id types.GroupDNSRuleID) (types.GroupDNSRule, bool) {
	for _, r := range s.AccessModel().DNSRules {
		if r.ID == id {
			return r, true
		}
	}

	return types.GroupDNSRule{}, false
}

// CreateGroupDNSRule stores a rule and pushes the DNS configuration to
// every client.
func (s *State) CreateGroupDNSRule(rule types.GroupDNSRule) (types.GroupDNSRule, change.Change, error) {
	rule = rule.Normalize()

	err := s.validateGroupDNSRule(rule)
	if err != nil {
		return types.GroupDNSRule{}, change.Change{}, err
	}

	created, err := s.db.CreateGroupDNSRule(rule)
	if err != nil {
		return types.GroupDNSRule{}, change.Change{}, err
	}

	c, err := s.applyGroupDNSChange()
	if err != nil {
		return types.GroupDNSRule{}, change.Change{}, err
	}

	log.Info().Uint64("dnsrule.id", uint64(created.ID)).Str("dnsrule.name", created.Name).Msg("DNS rule created")

	return created, c, nil
}

// UpdateGroupDNSRule replaces every field of the rule.
func (s *State) UpdateGroupDNSRule(rule types.GroupDNSRule) (types.GroupDNSRule, change.Change, error) {
	if _, ok := s.GetGroupDNSRule(rule.ID); !ok {
		return types.GroupDNSRule{}, change.Change{}, types.ErrGroupDNSRuleNotFound
	}

	rule = rule.Normalize()

	err := s.validateGroupDNSRule(rule)
	if err != nil {
		return types.GroupDNSRule{}, change.Change{}, err
	}

	updated, err := s.db.UpdateGroupDNSRule(rule)
	if err != nil {
		return types.GroupDNSRule{}, change.Change{}, err
	}

	c, err := s.applyGroupDNSChange()
	if err != nil {
		return types.GroupDNSRule{}, change.Change{}, err
	}

	return updated, c, nil
}

// DeleteGroupDNSRule removes the rule.
func (s *State) DeleteGroupDNSRule(id types.GroupDNSRuleID) (change.Change, error) {
	rule, ok := s.GetGroupDNSRule(id)
	if !ok {
		return change.Change{}, types.ErrGroupDNSRuleNotFound
	}

	err := s.db.DeleteGroupDNSRule(id)
	if err != nil {
		return change.Change{}, err
	}

	c, err := s.applyGroupDNSChange()
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Uint64("dnsrule.id", uint64(id)).Str("dnsrule.name", rule.Name).Msg("DNS rule deleted")

	return c, nil
}

// GroupDNSRoutes returns the split DNS the node's groups give it, as the
// map response carries it. Nil when no rule applies.
func (s *State) GroupDNSRoutes(node types.NodeView) map[string][]*dnstype.Resolver {
	routes := s.AccessModel().DNSRoutes(node)
	if len(routes) == 0 {
		return nil
	}

	out := make(map[string][]*dnstype.Resolver, len(routes))

	for domain, servers := range routes {
		for _, ns := range servers {
			resolver, err := types.ParseResolver(ns)
			if err != nil {
				// Validation refused it on the way in; the record cannot
				// hold one, so there is nothing to log.
				continue
			}

			out[domain] = append(out[domain], resolver)
		}
	}

	return out
}

// validateGroupDNSRule checks the fields and that every group exists.
func (s *State) validateGroupDNSRule(rule types.GroupDNSRule) error {
	err := types.ValidateGroupDNSRule(rule)
	if err != nil {
		return err
	}

	for _, domain := range rule.Domains {
		if s.cfg.ResolvesLocally(domain) {
			return fmt.Errorf("%w: %s", types.ErrGroupDNSRuleReservedZone, domain)
		}
	}

	model := s.AccessModel()

	for _, id := range rule.GroupIDs {
		group, ok := model.Group(id)
		if !ok {
			return fmt.Errorf("%w: %d", types.ErrGroupNotFound, id)
		}

		if group.IsSelf() {
			return types.ErrGroupSelfMembers
		}
	}

	return nil
}

// applyGroupDNSChange reloads the model and hands every client its DNS
// configuration again. The rules are not policy, so the policy manager
// reports no change; the DNS push is what carries them.
func (s *State) applyGroupDNSChange() (change.Change, error) {
	_, err := s.loadAccessModel()
	if err != nil {
		return change.Change{}, err
	}

	c := change.DNSConfig()
	c.Reason = "group dns rules"

	return c, nil
}

func dnsRuleNames(rules []types.GroupDNSRule) string {
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.Name)
	}

	return strings.Join(names, ", ")
}
