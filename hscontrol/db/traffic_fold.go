package db

import (
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// trafficPruneRows is about how many rows one pruning transaction
// deletes, so shortening the retention never holds the database long.
const trafficPruneRows = 5000

// trafficGroupRow is one bucket of one node through one gateway that
// holds more rows than a fold keeps.
type trafficGroupRow struct {
	Bucket     int64
	NodeID     int64
	ReporterID int64
}

func (g trafficGroupRow) key(resolution int64) types.TrafficKey {
	return types.TrafficKey{
		Resolution: resolution,
		Bucket:     g.Bucket,
		NodeID:     types.NodeID(fromSQLInt(g.NodeID)),
		ReporterID: types.NodeID(fromSQLInt(g.ReporterID)),
	}
}

// keyWhere matches the rows of one bucket of one node through one
// gateway.
func (c trafficColumns) keyWhere(k types.TrafficKey) jet.BoolExpression {
	return c.resolution.EQ(jet.Int64(k.Resolution)).
		AND(c.bucket.EQ(jet.Int64(k.Bucket))).
		AND(c.nodeID.EQ(jet.Uint64(k.NodeID.Uint64()))).
		AND(c.reporterID.EQ(jet.Uint64(k.ReporterID.Uint64())))
}

// FoldTraffic keeps, in every bucket of resolution starting in [from, to),
// each node's largest destinations and most asked names up to keep per
// gateway, and adds the rest into the bucket's remainder row (an empty
// destination or name). It returns how many rows it folded. Buckets still
// filling must not be folded, or a destination that grows later would
// start a second row next to the remainder it was folded into.
//
// Each bucket is folded in its own transaction by one DELETE that returns
// what it removed, so a report adding to a row at the same time is either
// folded with it or lands after it; its bytes are never lost.
func (hsdb *HSDatabase) FoldTraffic(resolution int64, from, to time.Time, keep int) (int64, error) {
	r := foldRange{resolution, from, to, keep}

	destinations, err := foldDestinations(hsdb, r)
	if err != nil {
		return 0, err
	}

	names, err := foldNames(hsdb, r)
	if err != nil {
		return destinations, err
	}

	return destinations + names, nil
}

// foldRange is the buckets a fold looks at.
type foldRange struct {
	resolution int64
	from, to   time.Time
	keep       int
}

// crowdedGroups finds the buckets in the range with more than keep rows
// besides the remainder.
func crowdedGroups(
	q Querier,
	cols trafficColumns,
	from jet.ReadableTable,
	notRemainder jet.BoolExpression,
	r foldRange,
) ([]trafficGroupRow, error) {
	var groups []trafficGroupRow

	err := q.executor().query(
		jet.SELECT(
			cols.bucket.AS("traffic_group_row.bucket"),
			cols.nodeID.AS("traffic_group_row.node_id"),
			cols.reporterID.AS("traffic_group_row.reporter_id"),
		).
			FROM(from).
			WHERE(
				cols.where(types.TrafficFilter{Resolution: r.resolution, Start: r.from, End: r.to}).
					AND(notRemainder),
			).
			GROUP_BY(cols.bucket, cols.nodeID, cols.reporterID).
			HAVING(jet.COUNT(jet.STAR).GT(jet.Int64(int64(r.keep)))),
		&groups,
	)
	if err != nil {
		return nil, fmt.Errorf("finding crowded traffic buckets: %w", err)
	}

	return groups, nil
}

// foldedCounts receives the counters a fold deleted.
type foldedCounts struct {
	TxBytes   int64
	RxBytes   int64
	TxPackets int64
	RxPackets int64
	Conns     int64
}

type foldedDestination struct {
	Row foldedCounts `alias:"traffic_destinations"`
}

func foldDestinations(hsdb *HSDatabase, r foldRange) (int64, error) {
	t := table.TrafficDestinations
	notRemainder := t.Dst.NOT_EQ(jet.String(""))

	groups, err := crowdedGroups(hsdb, destinationColumns, t, notRemainder, r)
	if err != nil {
		return 0, err
	}

	var folded int64

	for _, g := range groups {
		key := g.key(r.resolution)
		inGroup := destinationColumns.keyWhere(key).AND(notRemainder)

		err = hsdb.Write(func(tx *Tx) error {
			var gone []foldedDestination

			txErr := tx.executor().query(
				t.DELETE().
					WHERE(inGroup.AND(jet.ROW(t.Dst, t.Port, t.Proto, t.Host).NOT_IN(
						jet.SELECT(t.Dst, t.Port, t.Proto, t.Host).
							FROM(t).
							WHERE(inGroup).
							ORDER_BY(t.TxBytes.ADD(t.RxBytes).DESC(), t.Dst.ASC(), t.Port.ASC(),
								t.Proto.ASC(), t.Host.ASC()).
							LIMIT(int64(r.keep)),
					))).
					RETURNING(t.TxBytes, t.RxBytes, t.TxPackets, t.RxPackets, t.Conns),
				&gone,
			)
			if txErr != nil {
				return fmt.Errorf("folding traffic destinations: %w", txErr)
			}

			if len(gone) == 0 {
				return nil
			}

			remainder := types.TrafficDestination{TrafficKey: key}
			for _, d := range gone {
				remainder.Add(countsOf(d.Row.TxBytes, d.Row.RxBytes, d.Row.TxPackets, d.Row.RxPackets, d.Row.Conns))
			}

			folded += int64(len(gone))

			_, txErr = tx.executor().exec(trafficDestinationsUpsert([]types.TrafficDestination{remainder}))
			if txErr != nil {
				return fmt.Errorf("writing the folded traffic destinations: %w", txErr)
			}

			return nil
		})
		if err != nil {
			return folded, err
		}
	}

	return folded, nil
}

// foldedQuestions receives the counters a fold of names deleted.
type foldedQuestions struct {
	Queries int64
	Failed  int64
}

type foldedName struct {
	Row foldedQuestions `alias:"traffic_dns"`
}

func foldNames(hsdb *HSDatabase, r foldRange) (int64, error) {
	t := table.TrafficDNS
	notRemainder := t.Name.NOT_EQ(jet.String(""))

	groups, err := crowdedGroups(hsdb, dnsColumns, t, notRemainder, r)
	if err != nil {
		return 0, err
	}

	var folded int64

	for _, g := range groups {
		key := g.key(r.resolution)
		inGroup := dnsColumns.keyWhere(key).AND(notRemainder)

		err = hsdb.Write(func(tx *Tx) error {
			var gone []foldedName

			txErr := tx.executor().query(
				t.DELETE().
					WHERE(inGroup.AND(t.Name.NOT_IN(
						jet.SELECT(t.Name).
							FROM(t).
							WHERE(inGroup).
							ORDER_BY(t.Queries.DESC(), t.Name.ASC()).
							LIMIT(int64(r.keep)),
					))).
					RETURNING(t.Queries, t.Failed),
				&gone,
			)
			if txErr != nil {
				return fmt.Errorf("folding traffic names: %w", txErr)
			}

			if len(gone) == 0 {
				return nil
			}

			remainder := types.TrafficDNS{TrafficKey: key}
			for _, n := range gone {
				remainder.Queries += fromSQLInt(n.Row.Queries)
				remainder.Failed += fromSQLInt(n.Row.Failed)
			}

			folded += int64(len(gone))

			_, txErr = tx.executor().exec(trafficDNSUpsert([]types.TrafficDNS{remainder}))
			if txErr != nil {
				return fmt.Errorf("writing the folded traffic names: %w", txErr)
			}

			return nil
		})
		if err != nil {
			return folded, err
		}
	}

	return folded, nil
}

// pruneGroupRow is the rows of one bucket of one node, the unit a prune
// deletes.
type pruneGroupRow struct {
	Bucket int64
	NodeID int64
	Rows   int64
}

// pruneTarget is one table and the resolutions it keeps.
type pruneTarget struct {
	from        jet.Table
	del         func() jet.DeleteStatement
	cols        trafficColumns
	resolutions []int64
}

// PruneTraffic deletes the rows of each resolution older than its
// retention and returns how many went. It deletes about
// trafficPruneRows rows per transaction, whole buckets of one node at a
// time.
func (hsdb *HSDatabase) PruneTraffic(now time.Time, retention types.TrafficRetention) (int64, error) {
	var deleted int64

	for _, target := range []pruneTarget{
		{
			table.TrafficTotals, table.TrafficTotals.DELETE, totalsColumns,
			[]int64{types.TrafficMinute, types.TrafficHour, types.TrafficDay},
		},
		{
			table.TrafficDestinations, table.TrafficDestinations.DELETE, destinationColumns,
			[]int64{types.TrafficHour, types.TrafficDay},
		},
		{table.TrafficDNS, table.TrafficDNS.DELETE, dnsColumns, []int64{types.TrafficHour, types.TrafficDay}},
	} {
		for _, res := range target.resolutions {
			n, err := hsdb.pruneResolution(target, res, now.Add(-retention.Of(res)).Unix())
			deleted += n

			if err != nil {
				return deleted, err
			}
		}
	}

	return deleted, nil
}

// pruneResolution deletes the rows of res starting before cutoff.
func (hsdb *HSDatabase) pruneResolution(target pruneTarget, res, cutoff int64) (int64, error) {
	var deleted int64

	expired := target.cols.resolution.EQ(jet.Int64(res)).AND(target.cols.bucket.LT(jet.Int64(cutoff)))

	for {
		var groups []pruneGroupRow

		err := hsdb.ex.query(
			jet.SELECT(
				target.cols.bucket.AS("prune_group_row.bucket"),
				target.cols.nodeID.AS("prune_group_row.node_id"),
				jet.COUNT(jet.STAR).AS("prune_group_row.rows"),
			).
				FROM(target.from).
				WHERE(expired).
				GROUP_BY(target.cols.bucket, target.cols.nodeID).
				ORDER_BY(target.cols.bucket.ASC(), target.cols.nodeID.ASC()).
				LIMIT(trafficPruneRows),
			&groups,
		)
		if err != nil {
			return deleted, fmt.Errorf("finding expired traffic: %w", err)
		}

		if len(groups) == 0 {
			return deleted, nil
		}

		for len(groups) > 0 {
			var (
				keys []jet.Expression
				rows int64
			)

			for len(groups) > 0 && (len(keys) == 0 || rows+groups[0].Rows <= trafficPruneRows) {
				g := groups[0]
				groups = groups[1:]
				rows += g.Rows
				keys = append(keys, jet.ROW(jet.Int64(g.Bucket), jet.Int64(g.NodeID)))
			}

			n, err := Write(hsdb, func(tx *Tx) (int64, error) {
				return tx.executor().exec(target.del().WHERE(
					expired.AND(jet.ROW(target.cols.bucket, target.cols.nodeID).IN(keys...)),
				))
			})
			if err != nil {
				return deleted, fmt.Errorf("pruning traffic: %w", err)
			}

			deleted += n
		}
	}
}
