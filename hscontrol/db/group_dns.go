package db

import (
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

// Rows of the group DNS rule tables; see schema.sql.
type (
	groupDNSRuleRow struct {
		ID          uint64 `sql:"primary_key"`
		Name        string
		Description string
		Enabled     bool
		Domains     string
		Nameservers string
		CreatedAt   *time.Time
		UpdatedAt   *time.Time
	}
	groupDNSRuleGroupRow struct {
		ID      uint64 `sql:"primary_key"`
		RuleID  uint64
		GroupID uint64
	}

	groupDNSRuleRecord struct {
		GroupDNSRule groupDNSRuleRow `alias:"group_dns_rules"`
	}
	groupDNSRuleGroupRecord struct {
		GroupDNSRuleGroup groupDNSRuleGroupRow `alias:"group_dns_rule_groups"`
	}
)

func (r groupDNSRuleRow) rule() (types.GroupDNSRule, error) {
	rule := types.GroupDNSRule{
		ID:          types.GroupDNSRuleID(r.ID),
		Name:        r.Name,
		Description: r.Description,
		Enabled:     r.Enabled,
	}

	err := unmarshalJSONColumn(r.Domains, &rule.Domains)
	if err != nil {
		return types.GroupDNSRule{}, fmt.Errorf("dns rule %d domains: %w", r.ID, err)
	}

	err = unmarshalJSONColumn(r.Nameservers, &rule.Nameservers)
	if err != nil {
		return types.GroupDNSRule{}, fmt.Errorf("dns rule %d nameservers: %w", r.ID, err)
	}

	if r.CreatedAt != nil {
		rule.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		rule.UpdatedAt = *r.UpdatedAt
	}

	return rule, nil
}

func groupDNSRuleRowFrom(rule types.GroupDNSRule, now time.Time) (groupDNSRuleRow, error) {
	domains, err := marshalJSONColumn(rule.Domains)
	if err != nil {
		return groupDNSRuleRow{}, err
	}

	nameservers, err := marshalJSONColumn(rule.Nameservers)
	if err != nil {
		return groupDNSRuleRow{}, err
	}

	return groupDNSRuleRow{
		Name:        rule.Name,
		Description: rule.Description,
		Enabled:     rule.Enabled,
		Domains:     domains,
		Nameservers: nameservers,
		UpdatedAt:   &now,
	}, nil
}

// loadGroupDNSRules reads every rule with its groups, in ID order.
func loadGroupDNSRules(q Querier) ([]types.GroupDNSRule, error) {
	var (
		rules  []groupDNSRuleRecord
		groups []groupDNSRuleGroupRecord
	)

	ex := q.executor()

	err := ex.query(
		jet.SELECT(table.GroupDNSRules.AllColumns).FROM(table.GroupDNSRules).ORDER_BY(table.GroupDNSRules.ID.ASC()),
		&rules,
	)
	if err != nil {
		return nil, fmt.Errorf("loading dns rules: %w", err)
	}

	err = ex.query(
		jet.SELECT(table.GroupDNSRuleGroups.AllColumns).
			FROM(table.GroupDNSRuleGroups).
			ORDER_BY(table.GroupDNSRuleGroups.GroupID.ASC()),
		&groups,
	)
	if err != nil {
		return nil, fmt.Errorf("loading dns rule groups: %w", err)
	}

	out := make([]types.GroupDNSRule, 0, len(rules))
	idx := make(map[uint64]int, len(rules))

	for i, r := range rules {
		rule, err := r.GroupDNSRule.rule()
		if err != nil {
			return nil, err
		}

		out = append(out, rule)
		idx[r.GroupDNSRule.ID] = i
	}

	for _, r := range groups {
		if i, ok := idx[r.GroupDNSRuleGroup.RuleID]; ok {
			out[i].GroupIDs = append(out[i].GroupIDs, types.GroupID(r.GroupDNSRuleGroup.GroupID))
		}
	}

	return out, nil
}

// CreateGroupDNSRule stores a rule with its groups.
func (hsdb *HSDatabase) CreateGroupDNSRule(rule types.GroupDNSRule) (types.GroupDNSRule, error) {
	return Write(hsdb, func(tx *Tx) (types.GroupDNSRule, error) {
		now := time.Now().UTC()

		row, err := groupDNSRuleRowFrom(rule, now)
		if err != nil {
			return types.GroupDNSRule{}, err
		}

		row.CreatedAt = &now

		var inserted idRow

		err = tx.executor().query(
			table.GroupDNSRules.INSERT(table.GroupDNSRules.MutableColumns).MODEL(&row).
				RETURNING(table.GroupDNSRules.ID.AS("id_row.id")),
			&inserted,
		)
		if err != nil {
			if isUniqueViolation(err) {
				return types.GroupDNSRule{}, types.ErrGroupDNSRuleNameTaken
			}

			return types.GroupDNSRule{}, fmt.Errorf("creating dns rule: %w", err)
		}

		id := types.GroupDNSRuleID(inserted.ID)

		err = setGroupDNSRuleGroups(tx, id, rule.GroupIDs)
		if err != nil {
			return types.GroupDNSRule{}, err
		}

		return getGroupDNSRule(tx, id)
	})
}

// UpdateGroupDNSRule replaces every field of the rule.
func (hsdb *HSDatabase) UpdateGroupDNSRule(rule types.GroupDNSRule) (types.GroupDNSRule, error) {
	return Write(hsdb, func(tx *Tx) (types.GroupDNSRule, error) {
		now := time.Now().UTC()

		row, err := groupDNSRuleRowFrom(rule, now)
		if err != nil {
			return types.GroupDNSRule{}, err
		}

		id := jet.Uint64(uint64(rule.ID))

		affected, err := tx.executor().exec(
			table.GroupDNSRules.UPDATE(
				table.GroupDNSRules.Name, table.GroupDNSRules.Description, table.GroupDNSRules.Enabled,
				table.GroupDNSRules.Domains, table.GroupDNSRules.Nameservers, table.GroupDNSRules.UpdatedAt,
			).SET(
				row.Name, row.Description, row.Enabled, row.Domains, row.Nameservers, now,
			).WHERE(table.GroupDNSRules.ID.EQ(id)),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return types.GroupDNSRule{}, types.ErrGroupDNSRuleNameTaken
			}

			return types.GroupDNSRule{}, fmt.Errorf("updating dns rule %d: %w", rule.ID, err)
		}

		if affected == 0 {
			return types.GroupDNSRule{}, types.ErrGroupDNSRuleNotFound
		}

		_, err = tx.executor().exec(
			table.GroupDNSRuleGroups.DELETE().WHERE(table.GroupDNSRuleGroups.RuleID.EQ(id)),
		)
		if err != nil {
			return types.GroupDNSRule{}, fmt.Errorf("clearing groups of dns rule %d: %w", rule.ID, err)
		}

		err = setGroupDNSRuleGroups(tx, rule.ID, rule.GroupIDs)
		if err != nil {
			return types.GroupDNSRule{}, err
		}

		return getGroupDNSRule(tx, rule.ID)
	})
}

func setGroupDNSRuleGroups(q Querier, id types.GroupDNSRuleID, groupIDs []types.GroupID) error {
	groups := dedupe(groupIDs)
	if len(groups) == 0 {
		return nil
	}

	rows := make([]groupDNSRuleGroupRow, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, groupDNSRuleGroupRow{RuleID: uint64(id), GroupID: uint64(g)})
	}

	_, err := q.executor().exec(
		table.GroupDNSRuleGroups.INSERT(table.GroupDNSRuleGroups.MutableColumns).MODELS(rows),
	)
	if err != nil {
		return fmt.Errorf("storing groups of dns rule %d: %w", id, err)
	}

	return nil
}

// GetGroupDNSRule reads one rule with its groups.
func (hsdb *HSDatabase) GetGroupDNSRule(id types.GroupDNSRuleID) (types.GroupDNSRule, error) {
	return Read(hsdb, func(rx *Tx) (types.GroupDNSRule, error) {
		return getGroupDNSRule(rx, id)
	})
}

func getGroupDNSRule(q Querier, id types.GroupDNSRuleID) (types.GroupDNSRule, error) {
	rules, err := loadGroupDNSRules(q)
	if err != nil {
		return types.GroupDNSRule{}, err
	}

	for _, r := range rules {
		if r.ID == id {
			return r, nil
		}
	}

	return types.GroupDNSRule{}, types.ErrGroupDNSRuleNotFound
}

// DeleteGroupDNSRule removes the rule; its group rows cascade.
func (hsdb *HSDatabase) DeleteGroupDNSRule(id types.GroupDNSRuleID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.GroupDNSRules.DELETE().WHERE(table.GroupDNSRules.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting dns rule %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrGroupDNSRuleNotFound
		}

		return nil
	})
}
