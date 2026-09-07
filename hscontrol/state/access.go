package state

import (
	"fmt"
	"strings"
	"time"

	hsdb "github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/rs/zerolog/log"
)

// AccessModel returns the cached groups and rules. It is refreshed
// after every mutation and at start.
func (s *State) AccessModel() types.AccessModel {
	model := s.access.Load()
	if model == nil {
		return types.AccessModel{}
	}

	return *model
}

// loadAccessModel creates the builtin group when missing, reads the
// model from the database and hands it to the policy manager. It runs
// at start and after every mutation.
func (s *State) loadAccessModel() (change.Change, error) {
	_, err := s.db.EnsureAllGroup()
	if err != nil {
		return change.Change{}, fmt.Errorf("ensuring the builtin all group: %w", err)
	}

	model, err := s.db.LoadAccessModel()
	if err != nil {
		return change.Change{}, fmt.Errorf("loading access model: %w", err)
	}

	s.access.Store(&model)

	changed, err := s.polMan.SetAccessModel(model)
	if err != nil {
		return change.Change{}, fmt.Errorf("updating policy manager access model: %w", err)
	}

	if !changed {
		return change.Change{}, nil
	}

	s.nodeStore.RebuildPeerMaps()

	// Groups, rules and networks are policy to the tailnet, so a change to
	// them is a policy update to a webhook subscriber.
	s.emitPolicyUpdate()

	c := change.PolicyChange()
	c.Reason = "access model"

	return c, nil
}

// GetGroup returns one group with its members.
func (s *State) GetGroup(id types.GroupID) (types.AccessGroup, error) {
	group, ok := s.AccessModel().Group(id)
	if !ok {
		return types.AccessGroup{}, types.ErrGroupNotFound
	}

	return group, nil
}

// CreateGroup adds a group with no members.
func (s *State) CreateGroup(name, description string, requestable bool) (types.AccessGroup, change.Change, error) {
	name = strings.TrimSpace(name)

	err := types.ValidateGroupName(name)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	group, err := s.db.CreateGroup(name, strings.TrimSpace(description), requestable)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	log.Info().Uint64("group.id", uint64(group.ID)).Str("group.name", group.Name).Msg("Group created")

	return group, c, nil
}

// UpdateGroup renames or re-describes a group and sets whether members
// may request to join it. Builtin groups are fixed.
func (s *State) UpdateGroup(
	id types.GroupID, name, description string, requestable bool,
) (types.AccessGroup, change.Change, error) {
	existing, err := s.GetGroup(id)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	if existing.IsBuiltin() {
		return types.AccessGroup{}, change.Change{}, types.ErrGroupBuiltin
	}

	name = strings.TrimSpace(name)

	err = types.ValidateGroupName(name)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	group, err := s.db.UpdateGroup(id, name, strings.TrimSpace(description), requestable)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	return group, c, nil
}

// DeleteGroup removes a group nobody uses. A group named by a rule must
// leave the rule first; the builtin group stays.
func (s *State) DeleteGroup(id types.GroupID) (change.Change, error) {
	model := s.AccessModel()

	group, ok := model.Group(id)
	if !ok {
		return change.Change{}, types.ErrGroupNotFound
	}

	if group.IsBuiltin() {
		return change.Change{}, types.ErrGroupBuiltin
	}

	if rules := model.RulesUsingGroup(id); len(rules) > 0 {
		names := make([]string, 0, len(rules))
		for _, r := range rules {
			names = append(names, r.Name)
		}

		return change.Change{}, fmt.Errorf("%w: %s", types.ErrGroupInUse, strings.Join(names, ", "))
	}

	if networks := model.NetworksUsingGroup(id); len(networks) > 0 {
		return change.Change{}, fmt.Errorf("%w: network %s", types.ErrGroupInUse, networkNames(networks))
	}

	if rules := model.DNSRulesUsingGroup(id); len(rules) > 0 {
		return change.Change{}, fmt.Errorf("%w: dns rule %s", types.ErrGroupInUse, dnsRuleNames(rules))
	}

	err := s.db.DeleteGroup(id)
	if err != nil {
		return change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Uint64("group.id", uint64(id)).Str("group.name", group.Name).Msg("Group deleted")

	return c, nil
}

// SetGroupMembers replaces the group's direct nodes and users.
func (s *State) SetGroupMembers(
	id types.GroupID,
	nodeIDs []types.NodeID,
	userIDs []types.UserID,
) (types.AccessGroup, change.Change, error) {
	err := s.requireEditableGroup(id)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	for _, nid := range nodeIDs {
		if _, ok := s.nodeStore.GetNode(nid); !ok {
			return types.AccessGroup{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nid)
		}
	}

	for _, uid := range userIDs {
		_, err = s.db.GetUserByID(uid)
		if err != nil {
			return types.AccessGroup{}, change.Change{}, fmt.Errorf("looking up user %d: %w", uid, err)
		}
	}

	group, err := s.db.SetGroupMembers(id, nodeIDs, userIDs)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	return group, c, nil
}

// AddGroupNode makes the node a direct member of the group, until the
// expiry when one is given.
func (s *State) AddGroupNode(
	id types.GroupID, nodeID types.NodeID, expiresAt *time.Time,
) (types.AccessGroup, change.Change, error) {
	err := s.requireEditableGroup(id)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	if _, ok := s.nodeStore.GetNode(nodeID); !ok {
		return types.AccessGroup{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	err = validateMemberExpiry(expiresAt)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	err = s.db.AddGroupNode(id, nodeID, expiresAt)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	return s.groupAfterChange(id)
}

// RemoveGroupNode drops the node's direct membership.
func (s *State) RemoveGroupNode(id types.GroupID, nodeID types.NodeID) (types.AccessGroup, change.Change, error) {
	err := s.requireEditableGroup(id)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	err = s.db.RemoveGroupNode(id, nodeID)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	return s.groupAfterChange(id)
}

// AddGroupUser makes every device the user owns a member of the group,
// until the expiry when one is given.
func (s *State) AddGroupUser(
	id types.GroupID, userID types.UserID, expiresAt *time.Time,
) (types.AccessGroup, change.Change, error) {
	err := s.requireEditableGroup(id)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	_, err = s.db.GetUserByID(userID)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, fmt.Errorf("looking up user %d: %w", userID, err)
	}

	err = validateMemberExpiry(expiresAt)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	err = s.db.AddGroupUser(id, userID, expiresAt)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	return s.groupAfterChange(id)
}

// RemoveGroupUser drops the user's membership.
func (s *State) RemoveGroupUser(id types.GroupID, userID types.UserID) (types.AccessGroup, change.Change, error) {
	err := s.requireEditableGroup(id)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	err = s.db.RemoveGroupUser(id, userID)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	return s.groupAfterChange(id)
}

// validateMemberExpiry refuses a temporary membership that would already
// be over.
func validateMemberExpiry(expiresAt *time.Time) error {
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return types.ErrMemberExpiryPast
	}

	return nil
}

func (s *State) requireEditableGroup(id types.GroupID) error {
	group, err := s.GetGroup(id)
	if err != nil {
		return err
	}

	if group.IsBuiltin() {
		return types.ErrGroupBuiltin
	}

	return nil
}

func (s *State) groupAfterChange(id types.GroupID) (types.AccessGroup, change.Change, error) {
	c, err := s.loadAccessModel()
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	group, err := s.GetGroup(id)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
	}

	return group, c, nil
}

// enrolNodeInKeyGroups joins a node registered with a pre-auth key to
// the key's groups. Missing groups are skipped, so a deleted group
// never blocks a registration.
func (s *State) enrolNodeInKeyGroups(nodeID types.NodeID, groupIDs []types.GroupID) error {
	if len(groupIDs) == 0 {
		return nil
	}

	err := s.db.Write(func(tx *hsdb.Tx) error {
		return hsdb.AddNodeToGroups(tx, nodeID, groupIDs)
	})
	if err != nil {
		return fmt.Errorf("enrolling node %d in the key's groups: %w", nodeID, err)
	}

	_, err = s.loadAccessModel()

	return err
}

// ListAccessRules returns every rule.
func (s *State) ListAccessRules() []types.AccessRule {
	return s.AccessModel().Rules
}

// PolicyFileEnforces reports whether the policy file restricts traffic on
// its own, which decides whether the access rules are all that stands
// between the tailnet and allow-all.
func (s *State) PolicyFileEnforces() bool {
	return s.polMan.FileEnforces()
}

// SetAccessRuleEnabled flips one rule's switch, reading the rest of the
// rule from the store so a stale client copy cannot overwrite it.
func (s *State) SetAccessRuleEnabled(
	id types.AccessRuleID,
	enabled bool,
) (types.AccessRule, change.Change, error) {
	rule, err := s.GetAccessRule(id)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	rule.Enabled = enabled

	return s.UpdateAccessRule(rule)
}

// GetAccessRule returns one rule.
func (s *State) GetAccessRule(id types.AccessRuleID) (types.AccessRule, error) {
	for _, r := range s.AccessModel().Rules {
		if r.ID == id {
			return r, nil
		}
	}

	return types.AccessRule{}, types.ErrRuleNotFound
}

// CreateAccessRule validates and stores a rule.
func (s *State) CreateAccessRule(rule types.AccessRule) (types.AccessRule, change.Change, error) {
	rule, err := s.normalizeAccessRule(rule)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	if rule.Expired(time.Now()) {
		return types.AccessRule{}, change.Change{}, types.ErrRuleExpiryPast
	}

	created, err := s.db.CreateAccessRule(rule)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	log.Info().Uint64("rule.id", uint64(created.ID)).Str("rule.name", created.Name).Msg("Access rule created")

	return created, c, nil
}

// UpdateAccessRule replaces every field of a rule. An expiry that has
// passed is refused when it is new; an expired rule may be edited as
// long as its expiry is kept, extended or cleared.
func (s *State) UpdateAccessRule(rule types.AccessRule) (types.AccessRule, change.Change, error) {
	existing, err := s.GetAccessRule(rule.ID)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	rule, err = s.normalizeAccessRule(rule)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	if !sameExpiry(existing.ExpiresAt, rule.ExpiresAt) && rule.Expired(time.Now()) {
		return types.AccessRule{}, change.Change{}, types.ErrRuleExpiryPast
	}

	updated, err := s.db.UpdateAccessRule(rule)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	return updated, c, nil
}

// DeleteAccessRule removes a rule.
func (s *State) DeleteAccessRule(id types.AccessRuleID) (change.Change, error) {
	rule, err := s.GetAccessRule(id)
	if err != nil {
		return change.Change{}, err
	}

	err = s.db.DeleteAccessRule(id)
	if err != nil {
		return change.Change{}, err
	}

	c, err := s.loadAccessModel()
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Uint64("rule.id", uint64(id)).Str("rule.name", rule.Name).Msg("Access rule deleted")

	return c, nil
}

// sameExpiry reports whether two optional instants agree.
func sameExpiry(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return a.Equal(*b)
}

// ExpireAccess ends what ran out between the two instants: temporary
// memberships past their expiry are deleted and the policy is rebuilt
// without them and without the rules that expired. It runs from the
// minute ticker, so a grant ends within a minute of its time.
func (s *State) ExpireAccess(since, now time.Time) (change.Change, error) {
	next := s.AccessModel().NextExpiry(since)
	if next.IsZero() || next.After(now) {
		return change.Change{}, nil
	}

	gone, err := s.db.DeleteExpiredMemberships(now)
	if err != nil {
		return change.Change{}, err
	}

	log.Info().Int64("memberships", gone).Msg("Temporary access ran out")

	return s.loadAccessModel()
}

// normalizeAccessRule trims, validates and checks that every group the
// rule names exists.
func (s *State) normalizeAccessRule(rule types.AccessRule) (types.AccessRule, error) {
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Description = strings.TrimSpace(rule.Description)
	rule.Ports = strings.TrimSpace(rule.Ports)

	err := types.ValidateAccessRule(rule)
	if err != nil {
		return types.AccessRule{}, err
	}

	ranges, _ := types.ParsePortList(rule.Ports)
	rule.Ports = types.NormalizePortList(ranges)

	model := s.AccessModel()

	for _, id := range append(append([]types.GroupID{}, rule.SourceGroupIDs...), rule.DestinationGroupIDs...) {
		if _, ok := model.Group(id); !ok {
			return types.AccessRule{}, fmt.Errorf("%w: %d", types.ErrGroupNotFound, id)
		}
	}

	for _, id := range rule.PostureIDs {
		if _, ok := model.Posture(id); !ok {
			return types.AccessRule{}, fmt.Errorf("%w: %d", types.ErrPostureNotFound, id)
		}
	}

	return rule, nil
}
