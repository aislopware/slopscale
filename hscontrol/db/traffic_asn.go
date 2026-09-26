package db

import (
	"fmt"

	"github.com/aislopware/slopscale/gen/jet/table"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// TrafficDestinationNetwork is the network an ASN table names a stored
// destination with.
type TrafficDestinationNetwork struct {
	Dst     string
	ASN     uint32
	Country string
}

// unnamedDestination selects the destination rows stored without a network
// that could have one: public, not the folded remainder. The conditions are
// literals so both databases match them to idx_traffic_destinations_unnamed.
func unnamedDestination() jet.BoolExpression {
	t := table.TrafficDestinations

	return t.Asn.EQ(jet.RawInt("0")).AND(t.Private.EQ(jet.RawInt("0")))
}

type unnamedDestinationRow struct {
	Dst string
}

// TrafficUnnamedDestinations returns up to limit distinct destinations
// stored without a network, in address order after the cursor; an empty
// cursor starts at the first and skips the folded remainder, whose
// address is empty.
func (hsdb *HSDatabase) TrafficUnnamedDestinations(after string, limit int) ([]string, error) {
	t := table.TrafficDestinations

	var rows []unnamedDestinationRow

	err := hsdb.ex.query(
		jet.SELECT(t.Dst.AS("unnamed_destination_row.dst")).DISTINCT().
			FROM(t).
			WHERE(unnamedDestination().AND(t.Dst.GT(jet.String(after)))).
			ORDER_BY(t.Dst.ASC()).
			LIMIT(int64(limit)),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("finding traffic destinations without a network: %w", err)
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Dst)
	}

	return out, nil
}

// NameTrafficDestinations stores the networks of destinations that were
// stored without one, in one transaction; rows that have a network by now
// keep it. It returns how many rows it named.
func (hsdb *HSDatabase) NameTrafficDestinations(networks []TrafficDestinationNetwork) (int64, error) {
	if len(networks) == 0 {
		return 0, nil
	}

	t := table.TrafficDestinations

	named, err := Write(hsdb, func(tx *Tx) (int64, error) {
		var total int64

		for _, n := range networks {
			rows, err := tx.executor().exec(
				t.UPDATE(t.Asn, t.Country).
					SET(int64(n.ASN), n.Country).
					WHERE(unnamedDestination().AND(t.Dst.EQ(jet.String(n.Dst)))),
			)
			if err != nil {
				return total, err
			}

			total += rows
		}

		return total, nil
	})
	if err != nil {
		return named, fmt.Errorf("naming traffic destinations: %w", err)
	}

	return named, nil
}
