package db

import (
	"errors"
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/posture"
	"github.com/juanfont/headscale/hscontrol/types"
)

type (
	postureRow struct {
		ID          uint64 `sql:"primary_key"`
		Name        string
		Description string
		Expressions string
		Schedule    *string
		CreatedAt   *time.Time
		UpdatedAt   *time.Time
	}
	accessRulePostureRow struct {
		ID        uint64 `sql:"primary_key"`
		RuleID    uint64
		PostureID uint64
	}
	postureRecord struct {
		Posture postureRow `alias:"postures"`
	}
	accessRulePostureRecord struct {
		AccessRulePosture accessRulePostureRow `alias:"access_rule_postures"`
	}
)

func (r postureRow) posture() (types.Posture, error) {
	p := types.Posture{
		ID:          types.PostureID(r.ID),
		Name:        r.Name,
		Description: r.Description,
		Expressions: []string{},
	}

	err := unmarshalJSONColumn(r.Expressions, &p.Expressions)
	if err != nil {
		return types.Posture{}, fmt.Errorf("posture %d expressions: %w", r.ID, err)
	}

	if r.Schedule != nil && hasJSONValue(*r.Schedule) {
		var s posture.Schedule

		err = unmarshalJSONColumn(*r.Schedule, &s)
		if err != nil {
			return types.Posture{}, fmt.Errorf("posture %d schedule: %w", r.ID, err)
		}

		p.Schedule = &s
	}

	if r.CreatedAt != nil {
		p.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		p.UpdatedAt = *r.UpdatedAt
	}

	return p, nil
}

func postureRowFrom(p types.Posture, now time.Time) (postureRow, error) {
	exprs, err := marshalJSONColumn(p.Expressions)
	if err != nil {
		return postureRow{}, err
	}

	row := postureRow{
		ID:          uint64(p.ID),
		Name:        p.Name,
		Description: p.Description,
		Expressions: exprs,
		CreatedAt:   &now,
		UpdatedAt:   &now,
	}

	if p.Schedule != nil {
		schedule, err := marshalJSONColumn(p.Schedule)
		if err != nil {
			return postureRow{}, err
		}

		row.Schedule = &schedule
	}

	return row, nil
}

// loadPostures reads every posture in id order.
func loadPostures(q Querier) ([]types.Posture, error) {
	var records []postureRecord

	err := q.executor().query(
		jet.SELECT(table.Postures.AllColumns).FROM(table.Postures).ORDER_BY(table.Postures.ID.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading postures: %w", err)
	}

	postures := make([]types.Posture, 0, len(records))

	for _, r := range records {
		p, err := r.Posture.posture()
		if err != nil {
			return nil, err
		}

		postures = append(postures, p)
	}

	return postures, nil
}

// loadRulePostures returns the posture IDs of every rule.
func loadRulePostures(q Querier) (map[types.AccessRuleID][]types.PostureID, error) {
	var records []accessRulePostureRecord

	err := q.executor().query(
		jet.SELECT(table.AccessRulePostures.AllColumns).
			FROM(table.AccessRulePostures).
			ORDER_BY(table.AccessRulePostures.PostureID.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading access rule postures: %w", err)
	}

	out := make(map[types.AccessRuleID][]types.PostureID)

	for _, r := range records {
		id := types.AccessRuleID(r.AccessRulePosture.RuleID)
		out[id] = append(out[id], types.PostureID(r.AccessRulePosture.PostureID))
	}

	return out, nil
}

// setRulePostures replaces the postures of a rule.
func setRulePostures(q Querier, id types.AccessRuleID, postures []types.PostureID) error {
	_, err := q.executor().exec(
		table.AccessRulePostures.DELETE().WHERE(table.AccessRulePostures.RuleID.EQ(jet.Uint64(uint64(id)))),
	)
	if err != nil {
		return fmt.Errorf("clearing postures of access rule %d: %w", id, err)
	}

	ids := dedupe(postures)
	if len(ids) == 0 {
		return nil
	}

	rows := make([]accessRulePostureRow, 0, len(ids))
	for _, pid := range ids {
		rows = append(rows, accessRulePostureRow{RuleID: uint64(id), PostureID: uint64(pid)})
	}

	_, err = q.executor().exec(table.AccessRulePostures.INSERT(table.AccessRulePostures.MutableColumns).MODELS(rows))
	if err != nil {
		return fmt.Errorf("storing postures of access rule %d: %w", id, err)
	}

	return nil
}

// CreatePosture stores a posture. The caller validated it.
func (hsdb *HSDatabase) CreatePosture(p types.Posture) (types.Posture, error) {
	return Write(hsdb, func(tx *Tx) (types.Posture, error) {
		row, err := postureRowFrom(p, time.Now().UTC())
		if err != nil {
			return types.Posture{}, err
		}

		var inserted idRow

		err = tx.executor().query(
			table.Postures.INSERT(table.Postures.MutableColumns).MODEL(&row).
				RETURNING(table.Postures.ID.AS("id_row.id")),
			&inserted,
		)
		if isUniqueViolation(err) {
			return types.Posture{}, types.ErrPostureNameTaken
		}

		if err != nil {
			return types.Posture{}, fmt.Errorf("creating posture: %w", err)
		}

		return getPosture(tx, types.PostureID(inserted.ID))
	})
}

// UpdatePosture replaces every field of the posture.
func (hsdb *HSDatabase) UpdatePosture(p types.Posture) (types.Posture, error) {
	return Write(hsdb, func(tx *Tx) (types.Posture, error) {
		row, err := postureRowFrom(p, time.Now().UTC())
		if err != nil {
			return types.Posture{}, err
		}

		schedule := jet.NULL
		if row.Schedule != nil {
			schedule = jet.String(*row.Schedule)
		}

		affected, err := tx.executor().exec(
			table.Postures.UPDATE(
				table.Postures.Name, table.Postures.Description, table.Postures.Expressions,
				table.Postures.Schedule, table.Postures.UpdatedAt,
			).SET(
				row.Name, row.Description, row.Expressions, schedule, *row.UpdatedAt,
			).WHERE(table.Postures.ID.EQ(jet.Uint64(uint64(p.ID)))),
		)
		if isUniqueViolation(err) {
			return types.Posture{}, types.ErrPostureNameTaken
		}

		if err != nil {
			return types.Posture{}, fmt.Errorf("updating posture %d: %w", p.ID, err)
		}

		if affected == 0 {
			return types.Posture{}, types.ErrPostureNotFound
		}

		return getPosture(tx, p.ID)
	})
}

func getPosture(q Querier, id types.PostureID) (types.Posture, error) {
	var record postureRecord

	err := q.executor().query(
		jet.SELECT(table.Postures.AllColumns).FROM(table.Postures).
			WHERE(table.Postures.ID.EQ(jet.Uint64(uint64(id)))),
		&record,
	)
	if errors.Is(err, ErrNotFound) {
		return types.Posture{}, types.ErrPostureNotFound
	}

	if err != nil {
		return types.Posture{}, fmt.Errorf("loading posture %d: %w", id, err)
	}

	return record.Posture.posture()
}

// DeletePosture removes a posture; the rules that named it lose it.
func (hsdb *HSDatabase) DeletePosture(id types.PostureID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.Postures.DELETE().WHERE(table.Postures.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting posture %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrPostureNotFound
		}

		return nil
	})
}
