package db

import (
	"fmt"
	"net/netip"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

// networkRouteApprovalRow is a row of network_route_approvals: one route
// approval a network made on a node; see schema.sql.
type networkRouteApprovalRow struct {
	ID     uint64 `sql:"primary_key"`
	NodeID uint64
	Prefix string
}

type networkRouteApprovalRecord struct {
	Approval networkRouteApprovalRow `alias:"network_route_approvals"`
}

// NetworkRouteApprovals returns, per node, the prefixes whose approval a
// network made rather than an operator.
func (hsdb *HSDatabase) NetworkRouteApprovals() (map[types.NodeID][]netip.Prefix, error) {
	var rows []networkRouteApprovalRecord

	err := hsdb.executor().query(
		jet.SELECT(table.NetworkRouteApprovals.AllColumns).
			FROM(table.NetworkRouteApprovals).
			ORDER_BY(table.NetworkRouteApprovals.ID.ASC()),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("loading network route approvals: %w", err)
	}

	out := make(map[types.NodeID][]netip.Prefix)

	for _, r := range rows {
		prefix, err := netip.ParsePrefix(r.Approval.Prefix)
		if err != nil {
			return nil, fmt.Errorf(
				"network route approval %d holds prefix %q: %w", r.Approval.ID, r.Approval.Prefix, err,
			)
		}

		id := types.NodeID(r.Approval.NodeID)
		out[id] = append(out[id], prefix)
	}

	return out, nil
}

// SetNetworkRouteApprovals records the approvals a network made on the
// node (added) and forgets the ones it withdrew (removed).
func (hsdb *HSDatabase) SetNetworkRouteApprovals(nodeID types.NodeID, added, removed []netip.Prefix) error {
	if len(added) == 0 && len(removed) == 0 {
		return nil
	}

	return hsdb.Write(func(tx *Tx) error {
		ex := tx.executor()

		for _, p := range removed {
			_, err := ex.exec(
				table.NetworkRouteApprovals.DELETE().WHERE(
					table.NetworkRouteApprovals.NodeID.EQ(jet.Uint64(uint64(nodeID))).
						AND(table.NetworkRouteApprovals.Prefix.EQ(jet.String(p.String()))),
				),
			)
			if err != nil {
				return fmt.Errorf("forgetting network route approval %s of node %d: %w", p, nodeID, err)
			}
		}

		for _, p := range added {
			// An approval already on record stays as it is.
			_, err := ex.exec(
				table.NetworkRouteApprovals.DELETE().WHERE(
					table.NetworkRouteApprovals.NodeID.EQ(jet.Uint64(uint64(nodeID))).
						AND(table.NetworkRouteApprovals.Prefix.EQ(jet.String(p.String()))),
				),
			)
			if err != nil {
				return fmt.Errorf("replacing network route approval %s of node %d: %w", p, nodeID, err)
			}

			row := networkRouteApprovalRow{NodeID: uint64(nodeID), Prefix: p.String()}

			_, err = ex.exec(table.NetworkRouteApprovals.INSERT(table.NetworkRouteApprovals.MutableColumns).MODEL(&row))
			if err != nil {
				return fmt.Errorf("recording network route approval %s of node %d: %w", p, nodeID, err)
			}
		}

		return nil
	})
}
