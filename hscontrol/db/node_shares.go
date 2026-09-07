package db

import (
	"errors"
	"fmt"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

var (
	ErrNodeAlreadyShared = errors.New("node is already shared with the user")
	ErrNodeNotShared     = errors.New("node is not shared with the user")
)

// nodeShareRow mirrors the node_shares table; see schema.sql.
type nodeShareRow struct {
	ID        uint64 `sql:"primary_key"`
	NodeID    uint64
	UserID    uint64
	CreatedBy *uint64
	CreatedAt time.Time
}

type nodeShareRecord struct {
	NodeShare nodeShareRow `alias:"node_shares"`
}

// Share statements on the registration and map request paths, rendered
// once; see [fixedSQL]. Every node read attaches its shares, so these
// run alongside [nodeByID], [nodeByNodeKey] and [allNodes].
var (
	sharesOfNode = newFixedSQL(func() statement {
		return selectNodeShares().WHERE(table.NodeShares.NodeID.EQ(jet.Uint64(0)))
	})
	allShares = newFixedSQL(func() statement { return selectNodeShares() })
)

func selectNodeShares() jet.SelectStatement {
	return jet.SELECT(table.NodeShares.AllColumns).
		FROM(table.NodeShares).
		ORDER_BY(table.NodeShares.NodeID.ASC(), table.NodeShares.UserID.ASC())
}

// attachShares fills [types.Node.SharedWith] on every node from one
// query over node_shares.
func attachShares(q Querier, nodes types.Nodes) error {
	if len(nodes) == 0 {
		return nil
	}

	var records []nodeShareRecord

	err := q.executor().queryFixed(allShares, &records)
	if err != nil {
		return fmt.Errorf("loading node shares: %w", err)
	}

	byNode := make(map[types.NodeID][]types.UserID, len(records))
	for _, r := range records {
		nid := types.NodeID(r.NodeShare.NodeID)
		byNode[nid] = append(byNode[nid], types.UserID(r.NodeShare.UserID))
	}

	for _, node := range nodes {
		node.SharedWith = byNode[node.ID]
	}

	return nil
}

// attachSharesToNode fills [types.Node.SharedWith] on one node.
func attachSharesToNode(q Querier, node *types.Node) error {
	var records []nodeShareRecord

	err := q.executor().queryFixed(sharesOfNode, &records, node.ID.Uint64())
	if err != nil {
		return fmt.Errorf("loading shares of node %d: %w", node.ID, err)
	}

	node.SharedWith = nil
	for _, r := range records {
		node.SharedWith = append(node.SharedWith, types.UserID(r.NodeShare.UserID))
	}

	return nil
}

// ShareNode records that the node is shared with the user. createdBy is
// the user who shared it, nil when an administrator credential without
// a user did.
func (hsdb *HSDatabase) ShareNode(nodeID types.NodeID, userID types.UserID, createdBy *types.UserID) error {
	return hsdb.Write(func(tx *Tx) error {
		return ShareNode(tx, nodeID, userID, createdBy)
	})
}

// ShareNode is the query behind [HSDatabase.ShareNode].
func ShareNode(q Querier, nodeID types.NodeID, userID types.UserID, createdBy *types.UserID) error {
	var existing []nodeShareRecord

	err := q.executor().query(
		selectNodeShares().WHERE(
			table.NodeShares.NodeID.EQ(jet.Uint64(nodeID.Uint64())).
				AND(table.NodeShares.UserID.EQ(jet.Uint64(uint64(userID)))),
		),
		&existing,
	)
	if err != nil {
		return fmt.Errorf("checking share of node %d: %w", nodeID, err)
	}

	if len(existing) > 0 {
		return ErrNodeAlreadyShared
	}

	row := nodeShareRow{
		NodeID:    nodeID.Uint64(),
		UserID:    uint64(userID),
		CreatedAt: time.Now(),
	}

	if createdBy != nil {
		by := uint64(*createdBy)
		row.CreatedBy = &by
	}

	_, err = q.executor().exec(table.NodeShares.INSERT(table.NodeShares.MutableColumns).MODEL(row))
	if err != nil {
		return fmt.Errorf("sharing node %d with user %d: %w", nodeID, userID, err)
	}

	return nil
}

// UnshareNode removes the share of the node with the user.
func (hsdb *HSDatabase) UnshareNode(nodeID types.NodeID, userID types.UserID) error {
	return hsdb.Write(func(tx *Tx) error {
		return UnshareNode(tx, nodeID, userID)
	})
}

// UnshareNode is the query behind [HSDatabase.UnshareNode].
func UnshareNode(q Querier, nodeID types.NodeID, userID types.UserID) error {
	affected, err := q.executor().exec(
		table.NodeShares.DELETE().WHERE(
			table.NodeShares.NodeID.EQ(jet.Uint64(nodeID.Uint64())).
				AND(table.NodeShares.UserID.EQ(jet.Uint64(uint64(userID)))),
		),
	)
	if err != nil {
		return fmt.Errorf("unsharing node %d from user %d: %w", nodeID, userID, err)
	}

	if affected == 0 {
		return ErrNodeNotShared
	}

	return nil
}
