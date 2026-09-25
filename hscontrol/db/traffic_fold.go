package db

import (
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

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
func (hsdb *HSDatabase) FoldTraffic(resolution int64, from, to time.Time, keep int) (int64, error) {
	return Write(hsdb, func(tx *Tx) (int64, error) {
		destinations, err := foldDestinations(tx, resolution, from, to, keep)
		if err != nil {
			return 0, err
		}

		names, err := foldNames(tx, resolution, from, to, keep)
		if err != nil {
			return 0, err
		}

		return destinations + names, nil
	})
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
	tx *Tx,
	cols trafficColumns,
	from jet.ReadableTable,
	notRemainder jet.BoolExpression,
	r foldRange,
) ([]trafficGroupRow, error) {
	var groups []trafficGroupRow

	err := tx.executor().query(
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

func foldDestinations(tx *Tx, resolution int64, from, to time.Time, keep int) (int64, error) {
	t := table.TrafficDestinations
	notRemainder := t.Dst.NOT_EQ(jet.String(""))

	groups, err := crowdedGroups(tx, destinationColumns, t, notRemainder, foldRange{resolution, from, to, keep})
	if err != nil {
		return 0, err
	}

	var folded int64

	for _, g := range groups {
		key := g.key(resolution)

		var records []trafficDestinationRecord

		err = tx.executor().query(
			jet.SELECT(t.AllColumns).
				FROM(t).
				WHERE(destinationColumns.keyWhere(key).AND(notRemainder)).
				ORDER_BY(t.TxBytes.ADD(t.RxBytes).DESC(), t.Dst.ASC()),
			&records,
		)
		if err != nil {
			return 0, fmt.Errorf("reading traffic destinations to fold: %w", err)
		}

		remainder := types.TrafficDestination{TrafficKey: key}

		for _, r := range records[min(keep, len(records)):] {
			d := r.Row.destination()
			remainder.Add(d.TrafficCounts)

			_, err = tx.executor().exec(t.DELETE().WHERE(
				destinationColumns.keyWhere(key).
					AND(t.Dst.EQ(jet.String(d.Dst))).
					AND(t.Port.EQ(jet.Int64(int64(d.Port)))).
					AND(t.Proto.EQ(jet.Int64(int64(d.Proto)))).
					AND(t.Host.EQ(jet.String(d.Host))),
			))
			if err != nil {
				return 0, fmt.Errorf("folding a traffic destination: %w", err)
			}

			folded++
		}

		err = tx.executor().execFixed(upsertTrafficDestination, destinationArgs(remainder)...)
		if err != nil {
			return 0, fmt.Errorf("writing the folded traffic destinations: %w", err)
		}
	}

	return folded, nil
}

// trafficDNSStored is the part of a traffic_dns row a fold reads.
type trafficDNSStored struct {
	Name    string
	Queries int64
	Failed  int64
}

type trafficDNSRecord struct {
	Row trafficDNSStored `alias:"traffic_dns"`
}

func foldNames(tx *Tx, resolution int64, from, to time.Time, keep int) (int64, error) {
	t := table.TrafficDNS
	notRemainder := t.Name.NOT_EQ(jet.String(""))

	groups, err := crowdedGroups(tx, dnsColumns, t, notRemainder, foldRange{resolution, from, to, keep})
	if err != nil {
		return 0, err
	}

	var folded int64

	for _, g := range groups {
		key := g.key(resolution)

		var records []trafficDNSRecord

		err = tx.executor().query(
			jet.SELECT(t.Name, t.Queries, t.Failed).
				FROM(t).
				WHERE(dnsColumns.keyWhere(key).AND(notRemainder)).
				ORDER_BY(t.Queries.DESC(), t.Name.ASC()),
			&records,
		)
		if err != nil {
			return 0, fmt.Errorf("reading traffic names to fold: %w", err)
		}

		remainder := types.TrafficDNS{TrafficKey: key}

		for _, r := range records[min(keep, len(records)):] {
			remainder.Queries += fromSQLInt(r.Row.Queries)
			remainder.Failed += fromSQLInt(r.Row.Failed)

			_, err = tx.executor().exec(
				t.DELETE().WHERE(dnsColumns.keyWhere(key).AND(t.Name.EQ(jet.String(r.Row.Name)))),
			)
			if err != nil {
				return 0, fmt.Errorf("folding a traffic name: %w", err)
			}

			folded++
		}

		err = tx.executor().execFixed(upsertTrafficDNS, dnsArgs(remainder)...)
		if err != nil {
			return 0, fmt.Errorf("writing the folded traffic names: %w", err)
		}
	}

	return folded, nil
}
