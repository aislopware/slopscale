package db

import (
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// nodeAttributeRow mirrors the node_attributes table; see schema.sql.
type nodeAttributeRow struct {
	ID        uint64 `sql:"primary_key"`
	NodeID    uint64
	Key       string
	Value     string
	ExpiresAt *time.Time
	Comment   *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type nodeAttributeRecord struct {
	NodeAttribute nodeAttributeRow `alias:"node_attributes"`
}

func (r nodeAttributeRow) attribute() (types.NodeAttribute, error) {
	a := types.NodeAttribute{Key: r.Key}

	err := unmarshalJSONColumn(r.Value, &a.Value)
	if err != nil {
		return types.NodeAttribute{}, fmt.Errorf("attribute %s of node %d: %w", r.Key, r.NodeID, err)
	}

	if r.ExpiresAt != nil {
		a.ExpiresAt = *r.ExpiresAt
	}

	if r.Comment != nil {
		a.Comment = *r.Comment
	}

	return a, nil
}

// Attribute statements on the registration and map request paths,
// rendered once; see [fixedSQL]. Every node read attaches its
// attributes, next to its shares.
var (
	attributesOfNode = newFixedSQL(func() statement {
		return selectNodeAttributes().WHERE(table.NodeAttributes.NodeID.EQ(jet.Uint64(0)))
	})
	allAttributes = newFixedSQL(func() statement { return selectNodeAttributes() })
)

func selectNodeAttributes() jet.SelectStatement {
	return jet.SELECT(table.NodeAttributes.AllColumns).
		FROM(table.NodeAttributes).
		ORDER_BY(table.NodeAttributes.NodeID.ASC(), table.NodeAttributes.Key.ASC())
}

// attachAttributes fills [types.Node.Attributes] on every node from one
// query over node_attributes.
func attachAttributes(q Querier, nodes types.Nodes) error {
	if len(nodes) == 0 {
		return nil
	}

	var records []nodeAttributeRecord

	err := q.executor().queryFixed(allAttributes, &records)
	if err != nil {
		return fmt.Errorf("loading node attributes: %w", err)
	}

	byNode := make(map[types.NodeID][]types.NodeAttribute, len(records))

	for _, r := range records {
		a, err := r.NodeAttribute.attribute()
		if err != nil {
			return err
		}

		nid := types.NodeID(r.NodeAttribute.NodeID)
		byNode[nid] = append(byNode[nid], a)
	}

	for _, node := range nodes {
		node.Attributes = byNode[node.ID]
	}

	return nil
}

// attachAttributesToNode fills [types.Node.Attributes] on one node.
func attachAttributesToNode(q Querier, node *types.Node) error {
	var records []nodeAttributeRecord

	err := q.executor().queryFixed(attributesOfNode, &records, node.ID.Uint64())
	if err != nil {
		return fmt.Errorf("loading attributes of node %d: %w", node.ID, err)
	}

	node.Attributes = nil

	for _, r := range records {
		a, err := r.NodeAttribute.attribute()
		if err != nil {
			return err
		}

		node.Attributes = append(node.Attributes, a)
	}

	return nil
}

// SetNodeAttribute stores the attribute, replacing one with the same key.
func (hsdb *HSDatabase) SetNodeAttribute(nodeID types.NodeID, a types.NodeAttribute) error {
	return hsdb.Write(func(tx *Tx) error {
		return SetNodeAttribute(tx, nodeID, a)
	})
}

// SetNodeAttribute is the query behind [HSDatabase.SetNodeAttribute].
func SetNodeAttribute(q Querier, nodeID types.NodeID, a types.NodeAttribute) error {
	value, err := marshalJSONColumn(a.Value)
	if err != nil {
		return err
	}

	now := time.Now()
	row := nodeAttributeRow{
		NodeID:    nodeID.Uint64(),
		Key:       a.Key,
		Value:     value,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if !a.ExpiresAt.IsZero() {
		at := a.ExpiresAt
		row.ExpiresAt = &at
	}

	if a.Comment != "" {
		comment := a.Comment
		row.Comment = &comment
	}

	affected, err := q.executor().exec(
		table.NodeAttributes.UPDATE(
			table.NodeAttributes.Value, table.NodeAttributes.ExpiresAt,
			table.NodeAttributes.Comment, table.NodeAttributes.UpdatedAt,
		).
			SET(row.Value, row.ExpiresAt, row.Comment, row.UpdatedAt).
			WHERE(table.NodeAttributes.NodeID.EQ(jet.Uint64(row.NodeID)).
				AND(table.NodeAttributes.Key.EQ(jet.String(row.Key)))),
	)
	if err != nil {
		return fmt.Errorf("updating attribute %s of node %d: %w", a.Key, nodeID, err)
	}

	if affected > 0 {
		return nil
	}

	_, err = q.executor().exec(table.NodeAttributes.INSERT(table.NodeAttributes.MutableColumns).MODEL(row))
	if err != nil {
		return fmt.Errorf("storing attribute %s of node %d: %w", a.Key, nodeID, err)
	}

	return nil
}

// DeleteNodeAttribute removes the attribute; [types.ErrAttributeNotFound]
// when the node has none with the key.
func (hsdb *HSDatabase) DeleteNodeAttribute(nodeID types.NodeID, key string) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.NodeAttributes.DELETE().
				WHERE(table.NodeAttributes.NodeID.EQ(jet.Uint64(nodeID.Uint64())).
					AND(table.NodeAttributes.Key.EQ(jet.String(key)))),
		)
		if err != nil {
			return fmt.Errorf("deleting attribute %s of node %d: %w", key, nodeID, err)
		}

		if affected == 0 {
			return types.ErrAttributeNotFound
		}

		return nil
	})
}

// DeleteExpiredNodeAttributes drops every attribute whose expiry has
// passed and returns the nodes that lost one.
func (hsdb *HSDatabase) DeleteExpiredNodeAttributes(now time.Time) ([]types.NodeID, error) {
	return Write(hsdb, func(tx *Tx) ([]types.NodeID, error) {
		var rows []idRow

		expired := table.NodeAttributes.ExpiresAt.IS_NOT_NULL().
			AND(table.NodeAttributes.ExpiresAt.LT_EQ(jet.TimestampExp(timeArg(now.UTC()))))

		err := tx.executor().query(
			jet.SELECT(table.NodeAttributes.NodeID.AS("id_row.id")).
				FROM(table.NodeAttributes).WHERE(expired).GROUP_BY(table.NodeAttributes.NodeID),
			&rows,
		)
		if err != nil {
			return nil, fmt.Errorf("listing expired attributes: %w", err)
		}

		if len(rows) == 0 {
			return nil, nil
		}

		_, err = tx.executor().exec(table.NodeAttributes.DELETE().WHERE(expired))
		if err != nil {
			return nil, fmt.Errorf("deleting expired attributes: %w", err)
		}

		ids := make([]types.NodeID, len(rows))
		for i, r := range rows {
			ids[i] = types.NodeID(r.ID)
		}

		return ids, nil
	})
}
