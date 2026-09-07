package db

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

// Rows of the access tables; see schema.sql.
type (
	groupRow struct {
		ID          uint64 `sql:"primary_key"`
		Name        string
		Description string
		Builtin     string
		Requestable bool
		CreatedAt   *time.Time
		UpdatedAt   *time.Time
	}
	groupNodeRow struct {
		ID        uint64 `sql:"primary_key"`
		GroupID   uint64
		NodeID    uint64
		CreatedAt *time.Time
		ExpiresAt *time.Time
	}
	groupUserRow struct {
		ID        uint64 `sql:"primary_key"`
		GroupID   uint64
		UserID    uint64
		CreatedAt *time.Time
		ExpiresAt *time.Time
	}
	accessRuleRow struct {
		ID            uint64 `sql:"primary_key"`
		Name          string
		Description   string
		Enabled       bool
		Protocol      string
		Ports         string
		Bidirectional bool
		ExpiresAt     *time.Time
		CreatedAt     *time.Time
		UpdatedAt     *time.Time
	}
	accessRuleGroupRow struct {
		ID      uint64 `sql:"primary_key"`
		RuleID  uint64
		GroupID uint64
		Side    string
	}

	groupRecord struct {
		Group groupRow `alias:"groups"`
	}
	groupNodeRecord struct {
		GroupNode groupNodeRow `alias:"group_nodes"`
	}
	groupUserRecord struct {
		GroupUser groupUserRow `alias:"group_users"`
	}
	accessRuleRecord struct {
		AccessRule accessRuleRow `alias:"access_rules"`
	}
	accessRuleGroupRecord struct {
		AccessRuleGroup accessRuleGroupRow `alias:"access_rule_groups"`
	}
)

const (
	ruleSideSource      = "src"
	ruleSideDestination = "dst"
)

// accessGroup adds the membership rows to a group under construction.
type accessGroup types.AccessGroup

func (g *accessGroup) addNode(r groupNodeRow) {
	id := types.NodeID(r.NodeID)
	g.NodeIDs = append(g.NodeIDs, id)

	if r.ExpiresAt != nil {
		if g.NodeExpiries == nil {
			g.NodeExpiries = map[types.NodeID]time.Time{}
		}

		g.NodeExpiries[id] = *r.ExpiresAt
	}
}

func (g *accessGroup) addUser(r groupUserRow) {
	id := types.UserID(r.UserID)
	g.UserIDs = append(g.UserIDs, id)

	if r.ExpiresAt != nil {
		if g.UserExpiries == nil {
			g.UserExpiries = map[types.UserID]time.Time{}
		}

		g.UserExpiries[id] = *r.ExpiresAt
	}
}

func (r groupRow) group() types.AccessGroup {
	g := types.AccessGroup{
		ID:          types.GroupID(r.ID),
		Name:        r.Name,
		Description: r.Description,
		Builtin:     r.Builtin,
		Requestable: r.Requestable,
	}

	if r.CreatedAt != nil {
		g.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		g.UpdatedAt = *r.UpdatedAt
	}

	return g
}

func (r accessRuleRow) rule() types.AccessRule {
	rule := types.AccessRule{
		ID:            types.AccessRuleID(r.ID),
		Name:          r.Name,
		Description:   r.Description,
		Enabled:       r.Enabled,
		Protocol:      types.AccessProtocol(r.Protocol),
		Ports:         r.Ports,
		Bidirectional: r.Bidirectional,
	}

	if r.ExpiresAt != nil {
		at := *r.ExpiresAt
		rule.ExpiresAt = &at
	}

	if r.CreatedAt != nil {
		rule.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		rule.UpdatedAt = *r.UpdatedAt
	}

	return rule
}

// LoadAccessModel reads every group with its members and every rule with
// its sides.
func (hsdb *HSDatabase) LoadAccessModel() (types.AccessModel, error) {
	return Read(hsdb, func(rx *Tx) (types.AccessModel, error) {
		return LoadAccessModel(rx)
	})
}

// LoadAccessModel is the query behind [HSDatabase.LoadAccessModel].
func LoadAccessModel(q Querier) (types.AccessModel, error) {
	var (
		groups     []groupRecord
		nodes      []groupNodeRecord
		users      []groupUserRecord
		rules      []accessRuleRecord
		ruleGroups []accessRuleGroupRecord
	)

	ex := q.executor()

	err := ex.query(jet.SELECT(table.Groups.AllColumns).FROM(table.Groups).ORDER_BY(table.Groups.ID.ASC()), &groups)
	if err != nil {
		return types.AccessModel{}, fmt.Errorf("loading groups: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.GroupNodes.AllColumns).FROM(table.GroupNodes).ORDER_BY(table.GroupNodes.NodeID.ASC()),
		&nodes,
	)
	if err != nil {
		return types.AccessModel{}, fmt.Errorf("loading group nodes: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.GroupUsers.AllColumns).FROM(table.GroupUsers).ORDER_BY(table.GroupUsers.UserID.ASC()),
		&users,
	)
	if err != nil {
		return types.AccessModel{}, fmt.Errorf("loading group users: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.AccessRules.AllColumns).FROM(table.AccessRules).ORDER_BY(table.AccessRules.ID.ASC()),
		&rules,
	)
	if err != nil {
		return types.AccessModel{}, fmt.Errorf("loading access rules: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.AccessRuleGroups.AllColumns).
			FROM(table.AccessRuleGroups).
			ORDER_BY(table.AccessRuleGroups.GroupID.ASC()),
		&ruleGroups,
	)
	if err != nil {
		return types.AccessModel{}, fmt.Errorf("loading access rule groups: %w", err)
	}

	networks, err := loadNetworks(q)
	if err != nil {
		return types.AccessModel{}, err
	}

	postures, err := loadPostures(q)
	if err != nil {
		return types.AccessModel{}, err
	}

	rulePostures, err := loadRulePostures(q)
	if err != nil {
		return types.AccessModel{}, err
	}

	model := types.AccessModel{
		Groups:   make([]types.AccessGroup, 0, len(groups)),
		Rules:    make([]types.AccessRule, 0, len(rules)),
		Networks: networks,
		Postures: postures,
	}

	groupIdx := make(map[uint64]int, len(groups))

	for i, r := range groups {
		model.Groups = append(model.Groups, r.Group.group())
		groupIdx[r.Group.ID] = i
	}

	for _, r := range nodes {
		if i, ok := groupIdx[r.GroupNode.GroupID]; ok {
			(*accessGroup)(&model.Groups[i]).addNode(r.GroupNode)
		}
	}

	for _, r := range users {
		if i, ok := groupIdx[r.GroupUser.GroupID]; ok {
			(*accessGroup)(&model.Groups[i]).addUser(r.GroupUser)
		}
	}

	ruleIdx := make(map[uint64]int, len(rules))

	for i, r := range rules {
		rule := r.AccessRule.rule()
		rule.PostureIDs = rulePostures[rule.ID]
		model.Rules = append(model.Rules, rule)
		ruleIdx[r.AccessRule.ID] = i
	}

	for _, r := range ruleGroups {
		i, ok := ruleIdx[r.AccessRuleGroup.RuleID]
		if !ok {
			continue
		}

		gid := types.GroupID(r.AccessRuleGroup.GroupID)

		switch r.AccessRuleGroup.Side {
		case ruleSideSource:
			model.Rules[i].SourceGroupIDs = append(model.Rules[i].SourceGroupIDs, gid)
		case ruleSideDestination:
			model.Rules[i].DestinationGroupIDs = append(model.Rules[i].DestinationGroupIDs, gid)
		}
	}

	return model, nil
}

// EnsureAllGroup creates the builtin group that holds every node when it
// is missing and returns it.
func (hsdb *HSDatabase) EnsureAllGroup() (types.AccessGroup, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessGroup, error) {
		var existing []groupRecord

		err := tx.executor().query(
			jet.SELECT(table.Groups.AllColumns).FROM(table.Groups).
				WHERE(table.Groups.Builtin.EQ(jet.String(types.GroupBuiltinAll))),
			&existing,
		)
		if err != nil {
			return types.AccessGroup{}, fmt.Errorf("looking up the builtin all group: %w", err)
		}

		if len(existing) > 0 {
			return existing[0].Group.group(), nil
		}

		return insertGroup(tx, types.AccessGroup{
			Name:        types.GroupAllName,
			Description: "Every machine in the tailnet.",
			Builtin:     types.GroupBuiltinAll,
		})
	})
}

func insertGroup(q Querier, group types.AccessGroup) (types.AccessGroup, error) {
	now := time.Now().UTC()
	row := groupRow{
		Name:        group.Name,
		Description: group.Description,
		Builtin:     group.Builtin,
		Requestable: group.Requestable,
		CreatedAt:   &now,
		UpdatedAt:   &now,
	}

	var inserted idRow

	err := q.executor().query(
		table.Groups.INSERT(table.Groups.MutableColumns).MODEL(&row).RETURNING(table.Groups.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return types.AccessGroup{}, types.ErrGroupNameTaken
		}

		return types.AccessGroup{}, fmt.Errorf("creating group: %w", err)
	}

	row.ID = inserted.ID

	return row.group(), nil
}

// CreateGroup adds an operator-made group.
func (hsdb *HSDatabase) CreateGroup(name, description string, requestable bool) (types.AccessGroup, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessGroup, error) {
		return insertGroup(tx, types.AccessGroup{Name: name, Description: description, Requestable: requestable})
	})
}

// GetGroup reads one group with its members.
func (hsdb *HSDatabase) GetGroup(id types.GroupID) (types.AccessGroup, error) {
	return Read(hsdb, func(rx *Tx) (types.AccessGroup, error) {
		return getGroup(rx, id)
	})
}

func getGroup(q Querier, id types.GroupID) (types.AccessGroup, error) {
	var record groupRecord

	err := q.executor().query(
		jet.SELECT(table.Groups.AllColumns).FROM(table.Groups).WHERE(table.Groups.ID.EQ(jet.Uint64(uint64(id)))),
		&record,
	)
	if errors.Is(err, ErrNotFound) {
		return types.AccessGroup{}, types.ErrGroupNotFound
	}

	if err != nil {
		return types.AccessGroup{}, fmt.Errorf("loading group %d: %w", id, err)
	}

	group := record.Group.group()

	var (
		nodes []groupNodeRecord
		users []groupUserRecord
	)

	err = q.executor().query(
		jet.SELECT(table.GroupNodes.AllColumns).FROM(table.GroupNodes).
			WHERE(table.GroupNodes.GroupID.EQ(jet.Uint64(uint64(id)))).
			ORDER_BY(table.GroupNodes.NodeID.ASC()),
		&nodes,
	)
	if err != nil {
		return types.AccessGroup{}, fmt.Errorf("loading nodes of group %d: %w", id, err)
	}

	err = q.executor().query(
		jet.SELECT(table.GroupUsers.AllColumns).FROM(table.GroupUsers).
			WHERE(table.GroupUsers.GroupID.EQ(jet.Uint64(uint64(id)))).
			ORDER_BY(table.GroupUsers.UserID.ASC()),
		&users,
	)
	if err != nil {
		return types.AccessGroup{}, fmt.Errorf("loading users of group %d: %w", id, err)
	}

	for _, r := range nodes {
		(*accessGroup)(&group).addNode(r.GroupNode)
	}

	for _, r := range users {
		(*accessGroup)(&group).addUser(r.GroupUser)
	}

	return group, nil
}

// UpdateGroup renames or re-describes a group.
func (hsdb *HSDatabase) UpdateGroup(
	id types.GroupID, name, description string, requestable bool,
) (types.AccessGroup, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessGroup, error) {
		now := time.Now().UTC()

		affected, err := tx.executor().exec(
			table.Groups.UPDATE(
				table.Groups.Name, table.Groups.Description, table.Groups.Requestable, table.Groups.UpdatedAt,
			).
				SET(name, description, requestable, now).
				WHERE(table.Groups.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return types.AccessGroup{}, types.ErrGroupNameTaken
			}

			return types.AccessGroup{}, fmt.Errorf("updating group %d: %w", id, err)
		}

		if affected == 0 {
			return types.AccessGroup{}, types.ErrGroupNotFound
		}

		return getGroup(tx, id)
	})
}

// DeleteGroup removes a group; memberships and rule sides cascade. The
// caller refuses builtin and in-use groups before getting here.
func (hsdb *HSDatabase) DeleteGroup(id types.GroupID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.Groups.DELETE().WHERE(table.Groups.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting group %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrGroupNotFound
		}

		return nil
	})
}

// SetGroupMembers replaces the group's direct nodes and users.
func (hsdb *HSDatabase) SetGroupMembers(
	id types.GroupID,
	nodeIDs []types.NodeID,
	userIDs []types.UserID,
) (types.AccessGroup, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessGroup, error) {
		ex := tx.executor()

		// A member that stays keeps its expiry, so replacing the list
		// does not quietly make a temporary membership permanent.
		before, err := getGroup(tx, id)
		if err != nil {
			return types.AccessGroup{}, err
		}

		_, err = ex.exec(table.GroupNodes.DELETE().WHERE(table.GroupNodes.GroupID.EQ(jet.Uint64(uint64(id)))))
		if err != nil {
			return types.AccessGroup{}, fmt.Errorf("clearing nodes of group %d: %w", id, err)
		}

		_, err = ex.exec(table.GroupUsers.DELETE().WHERE(table.GroupUsers.GroupID.EQ(jet.Uint64(uint64(id)))))
		if err != nil {
			return types.AccessGroup{}, fmt.Errorf("clearing users of group %d: %w", id, err)
		}

		for _, nid := range dedupe(nodeIDs) {
			err = AddGroupNode(tx, id, nid, expiryPtr(before.NodeExpiry(nid)))
			if err != nil {
				return types.AccessGroup{}, err
			}
		}

		for _, uid := range dedupe(userIDs) {
			err = AddGroupUser(tx, id, uid, expiryPtr(before.UserExpiry(uid)))
			if err != nil {
				return types.AccessGroup{}, err
			}
		}

		err = touchGroup(tx, id)
		if err != nil {
			return types.AccessGroup{}, err
		}

		return getGroup(tx, id)
	})
}

func dedupe[T cmp.Ordered](ids []T) []T {
	out := slices.Clone(ids)
	slices.Sort(out)

	return slices.Compact(out)
}

func touchGroup(q Querier, id types.GroupID) error {
	_, err := q.executor().exec(
		table.Groups.UPDATE(table.Groups.UpdatedAt).SET(time.Now().UTC()).
			WHERE(table.Groups.ID.EQ(jet.Uint64(uint64(id)))),
	)
	if err != nil {
		return fmt.Errorf("touching group %d: %w", id, err)
	}

	return nil
}

// AddGroupNode makes the node a direct member of the group, until the
// expiry when one is given.
func (hsdb *HSDatabase) AddGroupNode(id types.GroupID, nodeID types.NodeID, expiresAt *time.Time) error {
	return hsdb.Write(func(tx *Tx) error {
		err := AddGroupNode(tx, id, nodeID, expiresAt)
		if err != nil {
			return err
		}

		return touchGroup(tx, id)
	})
}

// AddGroupNode is the query behind [HSDatabase.AddGroupNode].
func AddGroupNode(q Querier, id types.GroupID, nodeID types.NodeID, expiresAt *time.Time) error {
	now := time.Now().UTC()
	row := groupNodeRow{GroupID: uint64(id), NodeID: nodeID.Uint64(), CreatedAt: &now, ExpiresAt: utcPtr(expiresAt)}

	_, err := q.executor().exec(table.GroupNodes.INSERT(table.GroupNodes.MutableColumns).MODEL(&row))
	if err != nil {
		if isUniqueViolation(err) {
			return types.ErrGroupMemberExists
		}

		return fmt.Errorf("adding node %d to group %d: %w", nodeID, id, err)
	}

	return nil
}

// GrantGroupNode adds a temporary membership or extends one: a member
// whose membership ends earlier gets the new expiry, a permanent member
// stays permanent.
func GrantGroupNode(q Querier, id types.GroupID, nodeID types.NodeID, expiresAt time.Time) error {
	err := AddGroupNode(q, id, nodeID, &expiresAt)
	if !errors.Is(err, types.ErrGroupMemberExists) {
		return err
	}

	_, err = q.executor().exec(
		table.GroupNodes.UPDATE(table.GroupNodes.ExpiresAt).SET(expiresAt.UTC()).WHERE(
			table.GroupNodes.GroupID.EQ(jet.Uint64(uint64(id))).
				AND(table.GroupNodes.NodeID.EQ(jet.Uint64(nodeID.Uint64()))).
				AND(table.GroupNodes.ExpiresAt.IS_NOT_NULL()).
				AND(table.GroupNodes.ExpiresAt.LT(jet.TimestampExp(timeArg(expiresAt.UTC())))),
		),
	)
	if err != nil {
		return fmt.Errorf("extending node %d in group %d: %w", nodeID, id, err)
	}

	return nil
}

// GrantGroupUser is [GrantGroupNode] for a user membership.
func GrantGroupUser(q Querier, id types.GroupID, userID types.UserID, expiresAt time.Time) error {
	err := AddGroupUser(q, id, userID, &expiresAt)
	if !errors.Is(err, types.ErrGroupMemberExists) {
		return err
	}

	_, err = q.executor().exec(
		table.GroupUsers.UPDATE(table.GroupUsers.ExpiresAt).SET(expiresAt.UTC()).WHERE(
			table.GroupUsers.GroupID.EQ(jet.Uint64(uint64(id))).
				AND(table.GroupUsers.UserID.EQ(jet.Uint64(uint64(userID)))).
				AND(table.GroupUsers.ExpiresAt.IS_NOT_NULL()).
				AND(table.GroupUsers.ExpiresAt.LT(jet.TimestampExp(timeArg(expiresAt.UTC())))),
		),
	)
	if err != nil {
		return fmt.Errorf("extending user %d in group %d: %w", userID, id, err)
	}

	return nil
}

// DeleteExpiredMemberships drops the temporary memberships whose expiry
// passed and reports how many went.
func (hsdb *HSDatabase) DeleteExpiredMemberships(now time.Time) (int64, error) {
	return Write(hsdb, func(tx *Tx) (int64, error) {
		at := jet.TimestampExp(timeArg(now.UTC()))

		nodes, err := tx.executor().exec(
			table.GroupNodes.DELETE().WHERE(
				table.GroupNodes.ExpiresAt.IS_NOT_NULL().AND(table.GroupNodes.ExpiresAt.LT_EQ(at)),
			),
		)
		if err != nil {
			return 0, fmt.Errorf("deleting expired node memberships: %w", err)
		}

		users, err := tx.executor().exec(
			table.GroupUsers.DELETE().WHERE(
				table.GroupUsers.ExpiresAt.IS_NOT_NULL().AND(table.GroupUsers.ExpiresAt.LT_EQ(at)),
			),
		)
		if err != nil {
			return 0, fmt.Errorf("deleting expired user memberships: %w", err)
		}

		return nodes + users, nil
	})
}

// expiryPtr turns a zero-or-not instant into the optional form.
func expiryPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return &t
}

// utcPtr copies an optional instant in UTC for storage.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	at := t.UTC()

	return &at
}

// RemoveGroupNode drops the node's direct membership.
func (hsdb *HSDatabase) RemoveGroupNode(id types.GroupID, nodeID types.NodeID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.GroupNodes.DELETE().WHERE(
				table.GroupNodes.GroupID.EQ(jet.Uint64(uint64(id))).
					AND(table.GroupNodes.NodeID.EQ(jet.Uint64(nodeID.Uint64()))),
			),
		)
		if err != nil {
			return fmt.Errorf("removing node %d from group %d: %w", nodeID, id, err)
		}

		if affected == 0 {
			return types.ErrGroupMemberMissing
		}

		return touchGroup(tx, id)
	})
}

// AddGroupUser makes the user's devices members of the group, until the
// expiry when one is given.
func (hsdb *HSDatabase) AddGroupUser(id types.GroupID, userID types.UserID, expiresAt *time.Time) error {
	return hsdb.Write(func(tx *Tx) error {
		err := AddGroupUser(tx, id, userID, expiresAt)
		if err != nil {
			return err
		}

		return touchGroup(tx, id)
	})
}

// AddGroupUser is the query behind [HSDatabase.AddGroupUser].
func AddGroupUser(q Querier, id types.GroupID, userID types.UserID, expiresAt *time.Time) error {
	now := time.Now().UTC()
	row := groupUserRow{GroupID: uint64(id), UserID: uint64(userID), CreatedAt: &now, ExpiresAt: utcPtr(expiresAt)}

	_, err := q.executor().exec(table.GroupUsers.INSERT(table.GroupUsers.MutableColumns).MODEL(&row))
	if err != nil {
		if isUniqueViolation(err) {
			return types.ErrGroupMemberExists
		}

		return fmt.Errorf("adding user %d to group %d: %w", userID, id, err)
	}

	return nil
}

// RemoveGroupUser drops the user's membership.
func (hsdb *HSDatabase) RemoveGroupUser(id types.GroupID, userID types.UserID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.GroupUsers.DELETE().WHERE(
				table.GroupUsers.GroupID.EQ(jet.Uint64(uint64(id))).
					AND(table.GroupUsers.UserID.EQ(jet.Uint64(uint64(userID)))),
			),
		)
		if err != nil {
			return fmt.Errorf("removing user %d from group %d: %w", userID, id, err)
		}

		if affected == 0 {
			return types.ErrGroupMemberMissing
		}

		return touchGroup(tx, id)
	})
}

// AddNodeToGroups joins the node to every group listed, skipping groups
// that no longer exist and memberships that already exist. It runs at
// registration for the key's groups.
func AddNodeToGroups(q Querier, nodeID types.NodeID, groupIDs []types.GroupID) error {
	for _, gid := range dedupe(groupIDs) {
		_, err := getGroup(q, gid)
		if errors.Is(err, types.ErrGroupNotFound) {
			continue
		}

		if err != nil {
			return err
		}

		err = AddGroupNode(q, gid, nodeID, nil)
		if err != nil && !errors.Is(err, types.ErrGroupMemberExists) {
			return err
		}
	}

	return nil
}

// CreateAccessRule stores a rule with its sides. The caller validated it.
func (hsdb *HSDatabase) CreateAccessRule(rule types.AccessRule) (types.AccessRule, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessRule, error) {
		now := time.Now().UTC()
		row := accessRuleRow{
			Name:          rule.Name,
			Description:   rule.Description,
			Enabled:       rule.Enabled,
			Protocol:      string(rule.Protocol),
			Ports:         rule.Ports,
			Bidirectional: rule.Bidirectional,
			ExpiresAt:     utcPtr(rule.ExpiresAt),
			CreatedAt:     &now,
			UpdatedAt:     &now,
		}

		var inserted idRow

		err := tx.executor().query(
			table.AccessRules.INSERT(table.AccessRules.MutableColumns).MODEL(&row).
				RETURNING(table.AccessRules.ID.AS("id_row.id")),
			&inserted,
		)
		if err != nil {
			return types.AccessRule{}, fmt.Errorf("creating access rule: %w", err)
		}

		id := types.AccessRuleID(inserted.ID)

		err = setRuleSides(tx, id, rule.SourceGroupIDs, rule.DestinationGroupIDs)
		if err != nil {
			return types.AccessRule{}, err
		}

		err = setRulePostures(tx, id, rule.PostureIDs)
		if err != nil {
			return types.AccessRule{}, err
		}

		return getAccessRule(tx, id)
	})
}

// UpdateAccessRule replaces every field of the rule.
func (hsdb *HSDatabase) UpdateAccessRule(rule types.AccessRule) (types.AccessRule, error) {
	return Write(hsdb, func(tx *Tx) (types.AccessRule, error) {
		now := time.Now().UTC()

		affected, err := tx.executor().exec(
			table.AccessRules.UPDATE(
				table.AccessRules.Name, table.AccessRules.Description, table.AccessRules.Enabled,
				table.AccessRules.Protocol, table.AccessRules.Ports, table.AccessRules.Bidirectional,
				table.AccessRules.ExpiresAt, table.AccessRules.UpdatedAt,
			).SET(
				rule.Name, rule.Description, rule.Enabled,
				string(rule.Protocol), rule.Ports, rule.Bidirectional,
				utcPtr(rule.ExpiresAt), now,
			).WHERE(table.AccessRules.ID.EQ(jet.Uint64(uint64(rule.ID)))),
		)
		if err != nil {
			return types.AccessRule{}, fmt.Errorf("updating access rule %d: %w", rule.ID, err)
		}

		if affected == 0 {
			return types.AccessRule{}, types.ErrRuleNotFound
		}

		_, err = tx.executor().exec(
			table.AccessRuleGroups.DELETE().WHERE(table.AccessRuleGroups.RuleID.EQ(jet.Uint64(uint64(rule.ID)))),
		)
		if err != nil {
			return types.AccessRule{}, fmt.Errorf("clearing sides of access rule %d: %w", rule.ID, err)
		}

		err = setRuleSides(tx, rule.ID, rule.SourceGroupIDs, rule.DestinationGroupIDs)
		if err != nil {
			return types.AccessRule{}, err
		}

		err = setRulePostures(tx, rule.ID, rule.PostureIDs)
		if err != nil {
			return types.AccessRule{}, err
		}

		return getAccessRule(tx, rule.ID)
	})
}

func setRuleSides(q Querier, id types.AccessRuleID, sources, destinations []types.GroupID) error {
	rows := make([]accessRuleGroupRow, 0, len(sources)+len(destinations))

	for _, gid := range dedupe(sources) {
		rows = append(rows, accessRuleGroupRow{RuleID: uint64(id), GroupID: uint64(gid), Side: ruleSideSource})
	}

	for _, gid := range dedupe(destinations) {
		rows = append(rows, accessRuleGroupRow{RuleID: uint64(id), GroupID: uint64(gid), Side: ruleSideDestination})
	}

	if len(rows) == 0 {
		return nil
	}

	_, err := q.executor().exec(table.AccessRuleGroups.INSERT(table.AccessRuleGroups.MutableColumns).MODELS(rows))
	if err != nil {
		return fmt.Errorf("storing sides of access rule %d: %w", id, err)
	}

	return nil
}

// GetAccessRule reads one rule with its sides.
func (hsdb *HSDatabase) GetAccessRule(id types.AccessRuleID) (types.AccessRule, error) {
	return Read(hsdb, func(rx *Tx) (types.AccessRule, error) {
		return getAccessRule(rx, id)
	})
}

func getAccessRule(q Querier, id types.AccessRuleID) (types.AccessRule, error) {
	var record accessRuleRecord

	err := q.executor().query(
		jet.SELECT(table.AccessRules.AllColumns).FROM(table.AccessRules).
			WHERE(table.AccessRules.ID.EQ(jet.Uint64(uint64(id)))),
		&record,
	)
	if errors.Is(err, ErrNotFound) {
		return types.AccessRule{}, types.ErrRuleNotFound
	}

	if err != nil {
		return types.AccessRule{}, fmt.Errorf("loading access rule %d: %w", id, err)
	}

	rule := record.AccessRule.rule()

	var sides []accessRuleGroupRecord

	err = q.executor().query(
		jet.SELECT(table.AccessRuleGroups.AllColumns).FROM(table.AccessRuleGroups).
			WHERE(table.AccessRuleGroups.RuleID.EQ(jet.Uint64(uint64(id)))).
			ORDER_BY(table.AccessRuleGroups.GroupID.ASC()),
		&sides,
	)
	if err != nil {
		return types.AccessRule{}, fmt.Errorf("loading sides of access rule %d: %w", id, err)
	}

	for _, r := range sides {
		gid := types.GroupID(r.AccessRuleGroup.GroupID)

		switch r.AccessRuleGroup.Side {
		case ruleSideSource:
			rule.SourceGroupIDs = append(rule.SourceGroupIDs, gid)
		case ruleSideDestination:
			rule.DestinationGroupIDs = append(rule.DestinationGroupIDs, gid)
		}
	}

	var postures []accessRulePostureRecord

	err = q.executor().query(
		jet.SELECT(table.AccessRulePostures.AllColumns).FROM(table.AccessRulePostures).
			WHERE(table.AccessRulePostures.RuleID.EQ(jet.Uint64(uint64(id)))).
			ORDER_BY(table.AccessRulePostures.PostureID.ASC()),
		&postures,
	)
	if err != nil {
		return types.AccessRule{}, fmt.Errorf("loading postures of access rule %d: %w", id, err)
	}

	for _, r := range postures {
		rule.PostureIDs = append(rule.PostureIDs, types.PostureID(r.AccessRulePosture.PostureID))
	}

	return rule, nil
}

// DeleteAccessRule removes a rule and its sides.
func (hsdb *HSDatabase) DeleteAccessRule(id types.AccessRuleID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.AccessRules.DELETE().WHERE(table.AccessRules.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting access rule %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrRuleNotFound
		}

		return nil
	})
}

// isUniqueViolation reports whether err is a unique constraint failure
// from either driver. SQLite's driver is only imported by sqliteconfig, so
// its error is matched by message; PostgreSQL's by SQLSTATE 23505.
func isUniqueViolation(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23505"
	}

	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
