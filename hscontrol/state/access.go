package state

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	hsdb "github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/juanfont/headscale/hscontrol/util/zlog/zf"
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

// loadAccessModel creates the builtin groups when missing, reads the
// model from the database and hands it to the policy manager. It runs
// at start and after every mutation.
func (s *State) loadAccessModel() (change.Change, error) {
	err := s.db.EnsureBuiltinGroups()
	if err != nil {
		return change.Change{}, fmt.Errorf("ensuring the builtin groups: %w", err)
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
// may request to join it. Builtin groups are fixed, and a synced group
// keeps its name: the sync finds it by the claim's name, so a renamed
// one would be recreated empty at the next sign-in.
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

	if existing.Source == types.GroupSourceOIDC && name != existing.Name {
		return types.AccessGroup{}, change.Change{}, types.ErrGroupSyncedName
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

	err = s.requireEditableGroupUsers(id, userIDs)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, err
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
	err := s.requireEditableGroupUsers(id, nil)
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
	err := s.requireEditableGroupUsers(id, nil)
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

// CheckGroupUsersEditable reports whether userIDs may replace the group's
// users: always for an operator-made group, only when the set is unchanged
// for a synced one. Callers use it to refuse a request before writing.
func (s *State) CheckGroupUsersEditable(id types.GroupID, userIDs []types.UserID) error {
	return s.requireEditableGroupUsers(id, userIDs)
}

// requireEditableGroupUsers is [State.requireEditableGroup] plus the rule
// that a group synced from the identity provider keeps the users its
// claim gives it: a nil userIDs means the caller changes the users, and a
// list is accepted only when it equals the current members.
func (s *State) requireEditableGroupUsers(id types.GroupID, userIDs []types.UserID) error {
	group, err := s.GetGroup(id)
	if err != nil {
		return err
	}

	if group.IsBuiltin() {
		return types.ErrGroupBuiltin
	}

	if group.Source != types.GroupSourceOIDC {
		return nil
	}

	if userIDs == nil || !sameUserSet(group.UserIDs, userIDs) {
		return types.ErrGroupSyncedUsers
	}

	return nil
}

func sameUserSet(a, b []types.UserID) bool {
	set := make(map[types.UserID]bool, len(a))
	for _, id := range a {
		set[id] = true
	}

	other := make(map[types.UserID]bool, len(b))
	for _, id := range b {
		other[id] = true
	}

	return maps.Equal(set, other)
}

// SyncUserGroups mirrors the identity provider's groups claim of one
// user into the groups with [types.GroupSourceOIDC], creating the ones
// that do not exist, and returns the change when a membership moved. A
// claimed name held by an operator-made group is logged and left alone.
func (s *State) SyncUserGroups(userID types.UserID, names []string) (change.Change, error) {
	changed, skipped, err := s.db.SyncUserGroups(userID, names)
	if err != nil {
		return change.Change{}, err
	}

	if len(skipped) > 0 {
		log.Warn().
			Uint64(zf.UserID, uint64(userID)).
			Strs("groups", skipped).
			Msg("identity provider groups share a name with operator-made groups and were not synced")
	}

	if len(changed) == 0 {
		return change.Change{}, nil
	}

	return s.loadAccessModel()
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

// Enforces reports whether the tailnet has a packet filter, from the file
// or from an enabled rule. A network's protocol and ports narrow reach
// only while it does.
func (s *State) Enforces() bool {
	return s.polMan.Enforces()
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
// long as its expiry is kept, extended or cleared. A builtin rule takes
// only its enabled switch; everything else must come back as it is.
func (s *State) UpdateAccessRule(rule types.AccessRule) (types.AccessRule, change.Change, error) {
	existing, err := s.GetAccessRule(rule.ID)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	rule, err = s.normalizeAccessRule(rule)
	if err != nil {
		return types.AccessRule{}, change.Change{}, err
	}

	if existing.IsBuiltin() {
		if !sameRuleButEnabled(existing, rule) {
			return types.AccessRule{}, change.Change{}, types.ErrRuleBuiltin
		}

		rule.Builtin = existing.Builtin
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

// DeleteAccessRule removes a rule; the builtin one stays.
func (s *State) DeleteAccessRule(id types.AccessRuleID) (change.Change, error) {
	rule, err := s.GetAccessRule(id)
	if err != nil {
		return change.Change{}, err
	}

	if rule.IsBuiltin() {
		return change.Change{}, types.ErrRuleBuiltin
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
	// The first sweep after boot ignores since: the caller starts it at
	// the current time, and NextExpiry never looks behind it, so anything
	// that ran out while the server was down would be skipped forever.
	if s.accessSwept.CompareAndSwap(false, true) {
		since = time.Time{}
	}

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

	for _, id := range rule.SourceGroupIDs {
		group, ok := model.Group(id)
		if !ok {
			return types.AccessRule{}, fmt.Errorf("%w: %d", types.ErrGroupNotFound, id)
		}

		if group.IsSelf() {
			return types.AccessRule{}, types.ErrRuleSelfSource
		}
	}

	for _, id := range rule.DestinationGroupIDs {
		group, ok := model.Group(id)
		if !ok {
			return types.AccessRule{}, fmt.Errorf("%w: %d", types.ErrGroupNotFound, id)
		}

		if group.IsSelf() && rule.Bidirectional {
			return types.AccessRule{}, types.ErrRuleSelfBoth
		}
	}

	for _, id := range rule.PostureIDs {
		if _, ok := model.Posture(id); !ok {
			return types.AccessRule{}, fmt.Errorf("%w: %d", types.ErrPostureNotFound, id)
		}
	}

	return rule, nil
}

// sameRuleButEnabled reports whether the update leaves every field of the
// rule as it is except the enabled switch.
func sameRuleButEnabled(existing, update types.AccessRule) bool {
	return existing.Name == update.Name &&
		existing.Description == update.Description &&
		existing.Protocol == update.Protocol &&
		existing.Ports == update.Ports &&
		existing.Bidirectional == update.Bidirectional &&
		slices.Equal(existing.SourceGroupIDs, update.SourceGroupIDs) &&
		slices.Equal(existing.DestinationGroupIDs, update.DestinationGroupIDs) &&
		slices.Equal(existing.PostureIDs, update.PostureIDs) &&
		sameExpiry(existing.ExpiresAt, update.ExpiresAt)
}
