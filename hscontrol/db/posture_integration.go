package db

import (
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// postureIntegrationRow is a row of the posture_integrations table; see
// schema.sql.
type postureIntegrationRow struct {
	ID          uint64 `sql:"primary_key"`
	Provider    string
	Name        string
	Config      string
	Enabled     bool
	LastSyncAt  *time.Time
	LastError   *string
	LastMatched *int64
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
}

type postureIntegrationRecord struct {
	PostureIntegration postureIntegrationRow `alias:"posture_integrations"`
}

func (r postureIntegrationRow) integration() (types.PostureIntegration, error) {
	i := types.PostureIntegration{
		ID:       types.PostureIntegrationID(r.ID),
		Provider: types.PostureProvider(r.Provider),
		Name:     r.Name,
		Enabled:  r.Enabled,
	}

	err := unmarshalJSONColumn(r.Config, &i.Config)
	if err != nil {
		return types.PostureIntegration{}, fmt.Errorf("posture integration %d config: %w", r.ID, err)
	}

	if r.LastSyncAt != nil {
		i.LastSyncAt = *r.LastSyncAt
	}

	if r.LastError != nil {
		i.LastError = *r.LastError
	}

	if r.LastMatched != nil {
		i.LastMatched = int(*r.LastMatched)
	}

	if r.CreatedAt != nil {
		i.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		i.UpdatedAt = *r.UpdatedAt
	}

	return i, nil
}

func postureIntegrationRowFrom(i types.PostureIntegration, now time.Time) (postureIntegrationRow, error) {
	config, err := marshalJSONColumn(i.Config)
	if err != nil {
		return postureIntegrationRow{}, err
	}

	return postureIntegrationRow{
		ID:        i.ID.Uint64(),
		Provider:  string(i.Provider),
		Name:      i.Name,
		Config:    config,
		Enabled:   i.Enabled,
		UpdatedAt: &now,
	}, nil
}

// ListPostureIntegrations reads every integration in name order.
func (hsdb *HSDatabase) ListPostureIntegrations() ([]types.PostureIntegration, error) {
	var records []postureIntegrationRecord

	err := hsdb.executor().query(
		jet.SELECT(table.PostureIntegrations.AllColumns).FROM(table.PostureIntegrations).
			ORDER_BY(table.PostureIntegrations.Name.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("loading posture integrations: %w", err)
	}

	out := make([]types.PostureIntegration, 0, len(records))

	for _, r := range records {
		i, err := r.PostureIntegration.integration()
		if err != nil {
			return nil, err
		}

		out = append(out, i)
	}

	return out, nil
}

func getPostureIntegration(q Querier, id types.PostureIntegrationID) (types.PostureIntegration, error) {
	var records []postureIntegrationRecord

	err := q.executor().query(
		jet.SELECT(table.PostureIntegrations.AllColumns).FROM(table.PostureIntegrations).
			WHERE(table.PostureIntegrations.ID.EQ(jet.Uint64(id.Uint64()))).LIMIT(1),
		&records,
	)
	if err != nil {
		return types.PostureIntegration{}, fmt.Errorf("loading posture integration %d: %w", id, err)
	}

	if len(records) == 0 {
		return types.PostureIntegration{}, types.ErrPostureIntegrationNotFound
	}

	return records[0].PostureIntegration.integration()
}

// CreatePostureIntegration stores an integration.
func (hsdb *HSDatabase) CreatePostureIntegration(i types.PostureIntegration) (types.PostureIntegration, error) {
	return Write(hsdb, func(tx *Tx) (types.PostureIntegration, error) {
		now := time.Now().UTC()

		row, err := postureIntegrationRowFrom(i, now)
		if err != nil {
			return types.PostureIntegration{}, err
		}

		row.CreatedAt = &now

		id, err := insertReturningID(tx,
			table.PostureIntegrations.INSERT(table.PostureIntegrations.MutableColumns).MODEL(&row).
				RETURNING(table.PostureIntegrations.ID.AS("id_row.id")),
			"posture integration", types.ErrPostureIntegrationNameTaken)
		if err != nil {
			return types.PostureIntegration{}, err
		}

		return getPostureIntegration(tx, types.PostureIntegrationID(id))
	})
}

// UpdatePostureIntegration replaces the provider, name, config and
// switch; the sync record stays.
func (hsdb *HSDatabase) UpdatePostureIntegration(i types.PostureIntegration) (types.PostureIntegration, error) {
	return Write(hsdb, func(tx *Tx) (types.PostureIntegration, error) {
		now := time.Now().UTC()

		row, err := postureIntegrationRowFrom(i, now)
		if err != nil {
			return types.PostureIntegration{}, err
		}

		affected, err := tx.executor().exec(
			table.PostureIntegrations.UPDATE(
				table.PostureIntegrations.Provider, table.PostureIntegrations.Name, table.PostureIntegrations.Config,
				table.PostureIntegrations.Enabled, table.PostureIntegrations.UpdatedAt,
			).
				SET(row.Provider, row.Name, row.Config, row.Enabled, row.UpdatedAt).
				WHERE(table.PostureIntegrations.ID.EQ(jet.Uint64(i.ID.Uint64()))),
		)
		if err != nil {
			if isUniqueViolation(err) {
				return types.PostureIntegration{}, types.ErrPostureIntegrationNameTaken
			}

			return types.PostureIntegration{}, fmt.Errorf("updating posture integration %d: %w", i.ID, err)
		}

		if affected == 0 {
			return types.PostureIntegration{}, types.ErrPostureIntegrationNotFound
		}

		return getPostureIntegration(tx, i.ID)
	})
}

// RecordPostureIntegrationSync stores the outcome of a sync.
func (hsdb *HSDatabase) RecordPostureIntegrationSync(
	id types.PostureIntegrationID, at time.Time, syncErr string, matched int,
) error {
	return hsdb.Write(func(tx *Tx) error {
		at = at.UTC()
		count := int64(matched)

		_, err := tx.executor().exec(
			table.PostureIntegrations.UPDATE(
				table.PostureIntegrations.LastSyncAt, table.PostureIntegrations.LastError,
				table.PostureIntegrations.LastMatched,
			).
				SET(&at, &syncErr, &count).
				WHERE(table.PostureIntegrations.ID.EQ(jet.Uint64(id.Uint64()))),
		)
		if err != nil {
			return fmt.Errorf("recording posture integration sync %d: %w", id, err)
		}

		return nil
	})
}

// DeletePostureIntegration removes an integration.
func (hsdb *HSDatabase) DeletePostureIntegration(id types.PostureIntegrationID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.PostureIntegrations.DELETE().WHERE(table.PostureIntegrations.ID.EQ(jet.Uint64(id.Uint64()))),
		)
		if err != nil {
			return fmt.Errorf("deleting posture integration %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrPostureIntegrationNotFound
		}

		return nil
	})
}

// ReplacePrefixedNodeAttributes makes each listed node's attributes under
// the prefix exactly the given ones: the others under it are deleted,
// these are upserted, and attributes with other prefixes are untouched.
// A node listed with no attributes loses every attribute under the
// prefix. One transaction, so a reader never sees half a sync.
func (hsdb *HSDatabase) ReplacePrefixedNodeAttributes(
	prefix string, byNode map[types.NodeID][]types.NodeAttribute,
) error {
	return hsdb.Write(func(tx *Tx) error {
		for nodeID, attrs := range byNode {
			err := replacePrefixedNodeAttributes(tx, nodeID, prefix, attrs)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// DeletePrefixedNodeAttributes removes every node's attributes under the
// prefix and returns the nodes that had some.
func (hsdb *HSDatabase) DeletePrefixedNodeAttributes(prefix string) ([]types.NodeID, error) {
	return Write(hsdb, func(tx *Tx) ([]types.NodeID, error) {
		var rows []struct {
			NodeID uint64 `alias:"node_attributes.node_id"`
		}

		err := tx.executor().query(
			jet.SELECT(table.NodeAttributes.NodeID).DISTINCT().FROM(table.NodeAttributes).
				WHERE(table.NodeAttributes.Key.LIKE(jet.String(prefix+"%"))),
			&rows,
		)
		if err != nil {
			return nil, fmt.Errorf("finding %s attributes: %w", prefix, err)
		}

		if len(rows) == 0 {
			return nil, nil
		}

		_, err = tx.executor().exec(
			table.NodeAttributes.DELETE().WHERE(table.NodeAttributes.Key.LIKE(jet.String(prefix + "%"))),
		)
		if err != nil {
			return nil, fmt.Errorf("deleting %s attributes: %w", prefix, err)
		}

		ids := make([]types.NodeID, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, types.NodeID(r.NodeID))
		}

		return ids, nil
	})
}

// replacePrefixedNodeAttributes is one node of
// [HSDatabase.ReplacePrefixedNodeAttributes]. The prefix is a provider's
// (falcon:, intune:), letters and a colon, so the LIKE pattern needs no
// escaping.
func replacePrefixedNodeAttributes(q Querier, nodeID types.NodeID, prefix string, attrs []types.NodeAttribute) error {
	keep := make([]jet.Expression, 0, len(attrs))
	for _, a := range attrs {
		keep = append(keep, jet.String(a.Key))
	}

	where := table.NodeAttributes.NodeID.EQ(jet.Uint64(nodeID.Uint64())).
		AND(table.NodeAttributes.Key.LIKE(jet.String(prefix + "%")))
	if len(keep) > 0 {
		where = where.AND(table.NodeAttributes.Key.NOT_IN(keep...))
	}

	_, err := q.executor().exec(table.NodeAttributes.DELETE().WHERE(where))
	if err != nil {
		return fmt.Errorf("clearing %s attributes of node %d: %w", prefix, nodeID, err)
	}

	for _, a := range attrs {
		err = SetNodeAttribute(q, nodeID, a)
		if err != nil {
			return err
		}
	}

	return nil
}
