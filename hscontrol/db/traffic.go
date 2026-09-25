package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// trafficReporterRow is a row of the traffic_reporters table; see
// schema.sql.
type trafficReporterRow struct {
	NodeID       uint64 `sql:"primary_key"`
	Instance     string
	LastSeq      int64
	Version      *string
	Status       *string
	DNSListen    *string
	FirstSeenAt  *time.Time
	LastReportAt *time.Time
	Unattributed int64
	Dropped      int64
}

type trafficReporterRecord struct {
	Reporter trafficReporterRow `alias:"traffic_reporters"`
}

func (r trafficReporterRow) reporter() (types.TrafficReporter, error) {
	rep := types.TrafficReporter{
		NodeID:       types.NodeID(r.NodeID),
		Instance:     r.Instance,
		LastSeq:      fromSQLInt(r.LastSeq),
		Unattributed: fromSQLInt(r.Unattributed),
		Dropped:      fromSQLInt(r.Dropped),
	}

	if r.Version != nil {
		rep.Version = *r.Version
	}

	if r.FirstSeenAt != nil {
		rep.FirstSeenAt = *r.FirstSeenAt
	}

	if r.LastReportAt != nil {
		rep.LastReportAt = *r.LastReportAt
	}

	if r.Status != nil && *r.Status != "" {
		err := json.Unmarshal([]byte(*r.Status), &rep.Status)
		if err != nil {
			return types.TrafficReporter{}, fmt.Errorf("decoding the status of traffic reporter %d: %w", r.NodeID, err)
		}
	}

	if r.DNSListen != nil && *r.DNSListen != "" {
		err := json.Unmarshal([]byte(*r.DNSListen), &rep.DNSListen)
		if err != nil {
			return types.TrafficReporter{}, fmt.Errorf(
				"decoding the resolvers of traffic reporter %d: %w",
				r.NodeID,
				err,
			)
		}
	}

	return rep, nil
}

// toSQLInt stores a counter in a signed 64-bit column; nothing the agents
// count gets near the top bit, but a hostile report must not wrap.
func toSQLInt(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(v)
}

func fromSQLInt(v int64) uint64 {
	return uint64(max(v, 0))
}

// The upserts a report writes, one row per statement, adding the counts
// to the row the bucket already has. They run inside the report's
// transaction for every row, so they are rendered once from the builders
// below; fixed_test.go pins each builder to the argument order its
// *Args function supplies.
var (
	upsertTrafficTotal = newFixedSQL(func() statement {
		return trafficTotalUpsert(types.TrafficTotal{})
	})
	upsertTrafficDestination = newFixedSQL(func() statement {
		return trafficDestinationUpsert(types.TrafficDestination{})
	})
	upsertTrafficDNS = newFixedSQL(func() statement {
		return trafficDNSUpsert(types.TrafficDNS{})
	})
)

func trafficTotalUpsert(r types.TrafficTotal) jet.InsertStatement {
	t := table.TrafficTotals

	return t.INSERT(
		t.Resolution, t.Bucket, t.NodeID, t.ReporterID,
		t.TxBytes, t.RxBytes, t.TxPackets, t.RxPackets, t.Conns,
	).VALUES(
		jet.Int64(r.Resolution), jet.Int64(r.Bucket), jet.Uint64(r.NodeID.Uint64()), jet.Uint64(r.ReporterID.Uint64()),
		jet.Int64(toSQLInt(r.TxBytes)), jet.Int64(toSQLInt(r.RxBytes)),
		jet.Int64(toSQLInt(r.TxPackets)), jet.Int64(toSQLInt(r.RxPackets)), jet.Int64(toSQLInt(r.Conns)),
	).ON_CONFLICT(t.Resolution, t.Bucket, t.NodeID, t.ReporterID).DO_UPDATE(jet.SET(
		t.TxBytes.SET(t.TxBytes.ADD(t.EXCLUDED.TxBytes)),
		t.RxBytes.SET(t.RxBytes.ADD(t.EXCLUDED.RxBytes)),
		t.TxPackets.SET(t.TxPackets.ADD(t.EXCLUDED.TxPackets)),
		t.RxPackets.SET(t.RxPackets.ADD(t.EXCLUDED.RxPackets)),
		t.Conns.SET(t.Conns.ADD(t.EXCLUDED.Conns)),
	))
}

func trafficDestinationUpsert(r types.TrafficDestination) jet.InsertStatement {
	t := table.TrafficDestinations

	return t.INSERT(
		t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Dst, t.Port, t.Proto, t.Host,
		t.HostSource, t.Asn, t.Country,
		t.TxBytes, t.RxBytes, t.TxPackets, t.RxPackets, t.Conns,
	).VALUES(
		jet.Int64(r.Resolution), jet.Int64(r.Bucket), jet.Uint64(r.NodeID.Uint64()), jet.Uint64(r.ReporterID.Uint64()),
		jet.String(r.Dst), jet.Int64(int64(r.Port)), jet.Int64(int64(r.Proto)), jet.String(r.Host),
		jet.String(r.HostSource), jet.Int64(int64(r.ASN)), jet.String(r.Country),
		jet.Int64(toSQLInt(r.TxBytes)), jet.Int64(toSQLInt(r.RxBytes)),
		jet.Int64(toSQLInt(r.TxPackets)), jet.Int64(toSQLInt(r.RxPackets)), jet.Int64(toSQLInt(r.Conns)),
	).ON_CONFLICT(
		t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Dst, t.Port, t.Proto, t.Host,
	).DO_UPDATE(jet.SET(
		t.HostSource.SET(t.EXCLUDED.HostSource),
		t.Asn.SET(t.EXCLUDED.Asn),
		t.Country.SET(t.EXCLUDED.Country),
		t.TxBytes.SET(t.TxBytes.ADD(t.EXCLUDED.TxBytes)),
		t.RxBytes.SET(t.RxBytes.ADD(t.EXCLUDED.RxBytes)),
		t.TxPackets.SET(t.TxPackets.ADD(t.EXCLUDED.TxPackets)),
		t.RxPackets.SET(t.RxPackets.ADD(t.EXCLUDED.RxPackets)),
		t.Conns.SET(t.Conns.ADD(t.EXCLUDED.Conns)),
	))
}

func trafficDNSUpsert(r types.TrafficDNS) jet.InsertStatement {
	t := table.TrafficDNS

	return t.INSERT(
		t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Name, t.Queries, t.Failed,
	).VALUES(
		jet.Int64(r.Resolution), jet.Int64(r.Bucket), jet.Uint64(r.NodeID.Uint64()), jet.Uint64(r.ReporterID.Uint64()),
		jet.String(r.Name), jet.Int64(toSQLInt(r.Queries)), jet.Int64(toSQLInt(r.Failed)),
	).ON_CONFLICT(t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Name).DO_UPDATE(jet.SET(
		t.Queries.SET(t.Queries.ADD(t.EXCLUDED.Queries)),
		t.Failed.SET(t.Failed.ADD(t.EXCLUDED.Failed)),
	))
}

func totalArgs(r types.TrafficTotal) []any {
	return []any{
		r.Resolution, r.Bucket, r.NodeID.Uint64(), r.ReporterID.Uint64(),
		toSQLInt(r.TxBytes), toSQLInt(r.RxBytes), toSQLInt(r.TxPackets), toSQLInt(r.RxPackets), toSQLInt(r.Conns),
	}
}

func destinationArgs(r types.TrafficDestination) []any {
	return []any{
		r.Resolution, r.Bucket, r.NodeID.Uint64(), r.ReporterID.Uint64(),
		r.Dst, int64(r.Port), int64(r.Proto), r.Host,
		r.HostSource, int64(r.ASN), r.Country,
		toSQLInt(r.TxBytes), toSQLInt(r.RxBytes), toSQLInt(r.TxPackets), toSQLInt(r.RxPackets), toSQLInt(r.Conns),
	}
}

func dnsArgs(r types.TrafficDNS) []any {
	return []any{
		r.Resolution, r.Bucket, r.NodeID.Uint64(), r.ReporterID.Uint64(), r.Name,
		toSQLInt(r.Queries), toSQLInt(r.Failed),
	}
}

// ApplyTrafficBatch writes a report in one transaction: the reporter's
// row always, since a resent report still says the agent is alive, and
// the rollups only when the report is new for the agent's instance. It
// returns the reporter as stored and whether the rollups were applied.
func (hsdb *HSDatabase) ApplyTrafficBatch(batch types.TrafficBatch) (types.TrafficReporter, bool, error) {
	type result struct {
		reporter types.TrafficReporter
		applied  bool
	}

	res, err := Write(hsdb, func(tx *Tx) (result, error) {
		reporter, applied, err := saveTrafficReporter(tx, batch.Reporter)
		if err != nil {
			return result{}, err
		}

		if !applied {
			return result{reporter: reporter}, nil
		}

		for _, r := range batch.Totals {
			err = tx.executor().execFixed(upsertTrafficTotal, totalArgs(r)...)
			if err != nil {
				return result{}, fmt.Errorf("writing traffic totals: %w", err)
			}
		}

		for _, r := range batch.Destinations {
			err = tx.executor().execFixed(upsertTrafficDestination, destinationArgs(r)...)
			if err != nil {
				return result{}, fmt.Errorf("writing traffic destinations: %w", err)
			}
		}

		for _, r := range batch.DNS {
			err = tx.executor().execFixed(upsertTrafficDNS, dnsArgs(r)...)
			if err != nil {
				return result{}, fmt.Errorf("writing traffic dns: %w", err)
			}
		}

		return result{reporter: reporter, applied: true}, nil
	})

	return res.reporter, res.applied, err
}

// saveTrafficReporter merges the report's reporter into the stored row.
// A report whose sequence the instance has already passed is a resend:
// the row's liveness fields move on, its counters and sequence do not.
func saveTrafficReporter(tx *Tx, next types.TrafficReporter) (types.TrafficReporter, bool, error) {
	current, found, err := getTrafficReporter(tx, next.NodeID)
	if err != nil {
		return types.TrafficReporter{}, false, err
	}

	applied := !found || current.Instance != next.Instance || next.LastSeq > current.LastSeq

	merged := next
	merged.FirstSeenAt = next.LastReportAt

	if found {
		merged.FirstSeenAt = current.FirstSeenAt
		merged.Unattributed += current.Unattributed
		merged.Dropped += current.Dropped
	}

	if !applied {
		merged.LastSeq = current.LastSeq
		merged.Unattributed = current.Unattributed
		merged.Dropped = current.Dropped
	}

	status, err := json.Marshal(merged.Status)
	if err != nil {
		return types.TrafficReporter{}, false, fmt.Errorf("encoding traffic reporter status: %w", err)
	}

	listen := merged.DNSListen
	if listen == nil {
		listen = []netip.AddrPort{}
	}

	dnsListen, err := json.Marshal(listen)
	if err != nil {
		return types.TrafficReporter{}, false, fmt.Errorf("encoding traffic reporter resolvers: %w", err)
	}

	t := table.TrafficReporters

	_, err = tx.executor().exec(
		t.INSERT(
			t.NodeID, t.Instance, t.LastSeq, t.Version, t.Status, t.DNSListen,
			t.FirstSeenAt, t.LastReportAt, t.Unattributed, t.Dropped,
		).VALUES(
			merged.NodeID.Uint64(), merged.Instance, toSQLInt(merged.LastSeq), merged.Version,
			string(status), string(dnsListen), merged.FirstSeenAt.UTC(), merged.LastReportAt.UTC(),
			toSQLInt(merged.Unattributed), toSQLInt(merged.Dropped),
		).ON_CONFLICT(t.NodeID).DO_UPDATE(jet.SET(
			t.Instance.SET(t.EXCLUDED.Instance),
			t.LastSeq.SET(t.EXCLUDED.LastSeq),
			t.Version.SET(t.EXCLUDED.Version),
			t.Status.SET(t.EXCLUDED.Status),
			t.DNSListen.SET(t.EXCLUDED.DNSListen),
			t.LastReportAt.SET(t.EXCLUDED.LastReportAt),
			t.Unattributed.SET(t.EXCLUDED.Unattributed),
			t.Dropped.SET(t.EXCLUDED.Dropped),
		)),
	)
	if err != nil {
		return types.TrafficReporter{}, false, fmt.Errorf("saving traffic reporter %d: %w", merged.NodeID, err)
	}

	return merged, applied, nil
}

func getTrafficReporter(q Querier, id types.NodeID) (types.TrafficReporter, bool, error) {
	var records []trafficReporterRecord

	err := q.executor().query(
		jet.SELECT(table.TrafficReporters.AllColumns).
			FROM(table.TrafficReporters).
			WHERE(table.TrafficReporters.NodeID.EQ(jet.Uint64(id.Uint64()))),
		&records,
	)
	if err != nil {
		return types.TrafficReporter{}, false, fmt.Errorf("reading traffic reporter %d: %w", id, err)
	}

	if len(records) == 0 {
		return types.TrafficReporter{}, false, nil
	}

	rep, err := records[0].Reporter.reporter()

	return rep, err == nil, err
}

// ListTrafficReporters reads every gateway that has reported, in node ID
// order.
func (hsdb *HSDatabase) ListTrafficReporters() ([]types.TrafficReporter, error) {
	var records []trafficReporterRecord

	err := hsdb.ex.query(
		jet.SELECT(table.TrafficReporters.AllColumns).
			FROM(table.TrafficReporters).
			ORDER_BY(table.TrafficReporters.NodeID.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("listing traffic reporters: %w", err)
	}

	out := make([]types.TrafficReporter, 0, len(records))

	for _, r := range records {
		rep, err := r.Reporter.reporter()
		if err != nil {
			return nil, err
		}

		out = append(out, rep)
	}

	return out, nil
}

// ErrTrafficReporterNotFound is returned for a node that never reported.
var ErrTrafficReporterNotFound = errors.New("traffic reporter not found")

// DeleteTrafficReporter forgets a gateway; the traffic it reported stays
// until the retention removes it.
func (hsdb *HSDatabase) DeleteTrafficReporter(id types.NodeID) error {
	affected, err := hsdb.ex.exec(
		table.TrafficReporters.DELETE().
			WHERE(table.TrafficReporters.NodeID.EQ(jet.Uint64(id.Uint64()))),
	)
	if err != nil {
		return fmt.Errorf("deleting traffic reporter %d: %w", id, err)
	}

	if affected == 0 {
		return ErrTrafficReporterNotFound
	}

	return nil
}

// trafficColumns are the key columns every traffic table shares.
type trafficColumns struct {
	resolution, bucket, nodeID, reporterID jet.ColumnInteger
}

var (
	totalsColumns = trafficColumns{
		resolution: table.TrafficTotals.Resolution,
		bucket:     table.TrafficTotals.Bucket,
		nodeID:     table.TrafficTotals.NodeID,
		reporterID: table.TrafficTotals.ReporterID,
	}
	destinationColumns = trafficColumns{
		resolution: table.TrafficDestinations.Resolution,
		bucket:     table.TrafficDestinations.Bucket,
		nodeID:     table.TrafficDestinations.NodeID,
		reporterID: table.TrafficDestinations.ReporterID,
	}
	dnsColumns = trafficColumns{
		resolution: table.TrafficDNS.Resolution,
		bucket:     table.TrafficDNS.Bucket,
		nodeID:     table.TrafficDNS.NodeID,
		reporterID: table.TrafficDNS.ReporterID,
	}
)

// where is the part of a filter every table shares: resolution, range,
// node and gateway.
func (c trafficColumns) where(f types.TrafficFilter) jet.BoolExpression {
	cond := c.resolution.EQ(jet.Int64(f.Resolution)).
		AND(c.bucket.GT_EQ(jet.Int64(f.Start.Unix()))).
		AND(c.bucket.LT(jet.Int64(f.End.Unix())))

	if f.NodeID != 0 {
		cond = cond.AND(c.nodeID.EQ(jet.Uint64(f.NodeID.Uint64())))
	}

	if f.ReporterID != 0 {
		cond = cond.AND(c.reporterID.EQ(jet.Uint64(f.ReporterID.Uint64())))
	}

	return cond
}

// sum is SUM(col) as a 64-bit integer, zero over no rows: PostgreSQL
// widens a bigint sum to numeric, which the row mapping cannot read into
// an integer. The zero is a literal rather than a bound argument so the
// same expression in SELECT and ORDER BY stays textually identical.
func sum(col jet.Column) jet.IntegerExpression {
	return jet.IntExp(jet.CAST(jet.COALESCE(jet.SUM(col), jet.RawInt("0"))).AS("BIGINT"))
}

// countsRow receives summed counters.
type countsRow struct {
	TxBytes   int64
	RxBytes   int64
	TxPackets int64
	RxPackets int64
	Conns     int64
}

func (r countsRow) counts() types.TrafficCounts {
	return countsOf(r.TxBytes, r.RxBytes, r.TxPackets, r.RxPackets, r.Conns)
}

// countsOf reads counters from a row. The row types spell the counter
// fields out rather than embed countsRow, because the result mapping
// only fills an embedded struct under its own type's alias.
func countsOf(tx, rx, txp, rxp, conns int64) types.TrafficCounts {
	return types.TrafficCounts{
		TxBytes:   fromSQLInt(tx),
		RxBytes:   fromSQLInt(rx),
		TxPackets: fromSQLInt(txp),
		RxPackets: fromSQLInt(rxp),
		Conns:     fromSQLInt(conns),
	}
}

// countProjections sums the counters of a traffic table into alias.
func countProjections(alias string, tx, rx, txp, rxp, conns jet.Column) []jet.Projection {
	return []jet.Projection{
		sum(tx).AS(alias + ".tx_bytes"),
		sum(rx).AS(alias + ".rx_bytes"),
		sum(txp).AS(alias + ".tx_packets"),
		sum(rxp).AS(alias + ".rx_packets"),
		sum(conns).AS(alias + ".conns"),
	}
}

func totalsCounts(alias string) []jet.Projection {
	t := table.TrafficTotals

	return countProjections(alias, t.TxBytes, t.RxBytes, t.TxPackets, t.RxPackets, t.Conns)
}

func destinationCounts(alias string) []jet.Projection {
	t := table.TrafficDestinations

	return countProjections(alias, t.TxBytes, t.RxBytes, t.TxPackets, t.RxPackets, t.Conns)
}

// byVolume orders groups largest first.
func byVolume(tx, rx jet.Column) jet.OrderByClause {
	return sum(tx).ADD(sum(rx)).DESC()
}

// limitOf is the LIMIT a query applies: the filter's, or everything.
func limitOf(f types.TrafficFilter) int64 {
	if f.Limit > 0 {
		return int64(f.Limit)
	}

	return math.MaxInt32
}

type trafficPointRow struct {
	Bucket    int64
	TxBytes   int64
	RxBytes   int64
	TxPackets int64
	RxPackets int64
	Conns     int64
}

// TrafficSeries sums the totals per bucket, oldest first.
func (hsdb *HSDatabase) TrafficSeries(f types.TrafficFilter) ([]types.TrafficPoint, error) {
	t := table.TrafficTotals

	projections := append(
		[]jet.Projection{t.Bucket.AS("traffic_point_row.bucket")},
		totalsCounts("traffic_point_row")...,
	)

	var rows []trafficPointRow

	err := hsdb.ex.query(
		jet.SELECT(projections[0], projections[1:]...).
			FROM(t).
			WHERE(totalsColumns.where(f)).
			GROUP_BY(t.Bucket).
			ORDER_BY(t.Bucket.ASC()),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("reading the traffic series: %w", err)
	}

	out := make([]types.TrafficPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.TrafficPoint{
			Bucket:        r.Bucket,
			TrafficCounts: countsOf(r.TxBytes, r.RxBytes, r.TxPackets, r.RxPackets, r.Conns),
		})
	}

	return out, nil
}

type trafficNodeRow struct {
	NodeID    int64
	TxBytes   int64
	RxBytes   int64
	TxPackets int64
	RxPackets int64
	Conns     int64
}

// TrafficTopNodes sums the totals per node, or per gateway when
// byReporter, largest first.
func (hsdb *HSDatabase) TrafficTopNodes(f types.TrafficFilter, byReporter bool) ([]types.TrafficNodeSum, error) {
	t := table.TrafficTotals

	key := t.NodeID
	if byReporter {
		key = t.ReporterID
	}

	projections := append(
		[]jet.Projection{key.AS("traffic_node_row.node_id")},
		totalsCounts("traffic_node_row")...,
	)

	var rows []trafficNodeRow

	err := hsdb.ex.query(
		jet.SELECT(projections[0], projections[1:]...).
			FROM(t).
			WHERE(totalsColumns.where(f)).
			GROUP_BY(key).
			ORDER_BY(byVolume(t.TxBytes, t.RxBytes), key.ASC()).
			LIMIT(limitOf(f)),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("reading the traffic per node: %w", err)
	}

	out := make([]types.TrafficNodeSum, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.TrafficNodeSum{
			NodeID:        types.NodeID(fromSQLInt(r.NodeID)),
			TrafficCounts: countsOf(r.TxBytes, r.RxBytes, r.TxPackets, r.RxPackets, r.Conns),
		})
	}

	return out, nil
}

// TrafficSum sums every total in the filter.
func (hsdb *HSDatabase) TrafficSum(f types.TrafficFilter) (types.TrafficCounts, error) {
	projections := totalsCounts("counts_row")

	var row countsRow

	err := hsdb.ex.query(
		jet.SELECT(projections[0], projections[1:]...).
			FROM(table.TrafficTotals).
			WHERE(totalsColumns.where(f)),
		&row,
	)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return types.TrafficCounts{}, fmt.Errorf("summing traffic: %w", err)
	}

	return row.counts(), nil
}

// searchPattern is a LIKE pattern matching s anywhere, lower case to
// match the stored names; a % in s is dropped rather than escaped, since
// no host name holds one.
func searchPattern(s string) string {
	return "%" + strings.ReplaceAll(strings.ToLower(s), "%", "") + "%"
}

func destinationWhere(f types.TrafficFilter) jet.BoolExpression {
	t := table.TrafficDestinations
	cond := destinationColumns.where(f)

	if f.Search != "" {
		pattern := jet.String(searchPattern(f.Search))
		cond = cond.AND(t.Host.LIKE(pattern).OR(t.Dst.LIKE(pattern)))
	}

	if f.ASN != 0 {
		cond = cond.AND(t.Asn.EQ(jet.Int64(int64(f.ASN))))
	}

	if f.Country != "" {
		cond = cond.AND(t.Country.EQ(jet.String(strings.ToUpper(f.Country))))
	}

	if f.Proto != 0 {
		cond = cond.AND(t.Proto.EQ(jet.Int64(int64(f.Proto))))

		if f.Port != 0 {
			cond = cond.AND(t.Port.EQ(jet.Int64(int64(f.Port))))
		}
	}

	return cond
}

type trafficDestinationRow struct {
	Dst       string
	Port      int64
	Proto     int64
	Host      string
	Asn       int64
	Country   *string
	NodeID    int64
	Nodes     int64
	TxBytes   int64
	RxBytes   int64
	TxPackets int64
	RxPackets int64
	Conns     int64
}

func (r trafficDestinationRow) sum() types.TrafficDestinationSum {
	out := types.TrafficDestinationSum{
		Dst:           r.Dst,
		Port:          uint16(min(max(r.Port, 0), math.MaxUint16)),
		Proto:         uint8(min(max(r.Proto, 0), math.MaxUint8)),
		Host:          r.Host,
		ASN:           uint32(min(max(r.Asn, 0), math.MaxUint32)),
		NodeID:        types.NodeID(fromSQLInt(r.NodeID)),
		TrafficCounts: countsOf(r.TxBytes, r.RxBytes, r.TxPackets, r.RxPackets, r.Conns),
		Nodes:         fromSQLInt(r.Nodes),
	}

	if r.Country != nil {
		out.Country = *r.Country
	}

	return out
}

// destinationGrouping returns the columns a grouping selects and groups
// by.
func destinationGrouping(group types.TrafficGroup) ([]jet.Projection, []jet.GroupByClause, error) {
	t := table.TrafficDestinations

	const alias = "traffic_destination_row."

	switch group {
	case types.TrafficByDestination, "":
		return []jet.Projection{
				t.Dst.AS(alias + "dst"), t.Port.AS(alias + "port"), t.Proto.AS(alias + "proto"),
				t.Host.AS(alias + "host"), t.Asn.AS(alias + "asn"), t.Country.AS(alias + "country"),
			},
			[]jet.GroupByClause{t.Dst, t.Port, t.Proto, t.Host, t.Asn, t.Country},
			nil
	case types.TrafficByHost:
		// A destination without a name groups by its address instead,
		// so unnamed traffic does not collapse into one row.
		// The empty string is a literal: PostgreSQL only accepts the
		// grouped expression in SELECT when both are the same text.
		key := jet.COALESCE(jet.NULLIF(t.Host, jet.StringExp(jet.Raw("''"))), t.Dst)

		return []jet.Projection{key.AS(alias + "host")}, []jet.GroupByClause{key}, nil
	case types.TrafficByASN:
		return []jet.Projection{t.Asn.AS(alias + "asn")}, []jet.GroupByClause{t.Asn}, nil
	case types.TrafficByCountry:
		return []jet.Projection{t.Country.AS(alias + "country")}, []jet.GroupByClause{t.Country}, nil
	case types.TrafficByPort:
		return []jet.Projection{t.Proto.AS(alias + "proto"), t.Port.AS(alias + "port")},
			[]jet.GroupByClause{t.Proto, t.Port},
			nil
	case types.TrafficByNode:
		return []jet.Projection{t.NodeID.AS(alias + "node_id")}, []jet.GroupByClause{t.NodeID}, nil
	case types.TrafficByReporter:
		return []jet.Projection{t.ReporterID.AS(alias + "node_id")}, []jet.GroupByClause{t.ReporterID}, nil
	case types.TrafficByName:
	}

	return nil, nil, fmt.Errorf("%w: %q", types.ErrTrafficGroupUnknown, group)
}

// TrafficDestinations sums the destinations in the filter by group,
// largest first.
func (hsdb *HSDatabase) TrafficDestinations(
	f types.TrafficFilter,
	group types.TrafficGroup,
) ([]types.TrafficDestinationSum, error) {
	t := table.TrafficDestinations

	keys, groupBy, err := destinationGrouping(group)
	if err != nil {
		return nil, err
	}

	projections := slices.Concat(
		keys,
		destinationCounts("traffic_destination_row"),
		[]jet.Projection{jet.COUNT(jet.DISTINCT(t.NodeID)).AS("traffic_destination_row.nodes")},
	)

	var rows []trafficDestinationRow

	err = hsdb.ex.query(
		jet.SELECT(projections[0], projections[1:]...).
			FROM(t).
			WHERE(destinationWhere(f)).
			GROUP_BY(groupBy...).
			ORDER_BY(byVolume(t.TxBytes, t.RxBytes)).
			LIMIT(limitOf(f)),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("reading traffic destinations: %w", err)
	}

	out := make([]types.TrafficDestinationSum, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.sum())
	}

	return out, nil
}

// TrafficDestinationRows reads the destination rows in the filter as they
// are stored, oldest bucket first, for the flow log export.
func (hsdb *HSDatabase) TrafficDestinationRows(f types.TrafficFilter) ([]types.TrafficDestination, error) {
	var records []trafficDestinationRecord

	err := hsdb.ex.query(
		jet.SELECT(table.TrafficDestinations.AllColumns).
			FROM(table.TrafficDestinations).
			WHERE(destinationWhere(f)).
			ORDER_BY(
				table.TrafficDestinations.Bucket.ASC(),
				table.TrafficDestinations.ReporterID.ASC(),
				table.TrafficDestinations.NodeID.ASC(),
			).
			LIMIT(limitOf(f)),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("reading traffic destination rows: %w", err)
	}

	out := make([]types.TrafficDestination, 0, len(records))
	for _, r := range records {
		out = append(out, r.Row.destination())
	}

	return out, nil
}

// trafficDestinationStored is a whole traffic_destinations row.
type trafficDestinationStored struct {
	Resolution int64
	Bucket     int64
	NodeID     int64
	ReporterID int64
	Dst        string
	Port       int64
	Proto      int64
	Host       string
	HostSource *string
	Asn        int64
	Country    *string
	TxBytes    int64
	RxBytes    int64
	TxPackets  int64
	RxPackets  int64
	Conns      int64
}

type trafficDestinationRecord struct {
	Row trafficDestinationStored `alias:"traffic_destinations"`
}

func (r trafficDestinationStored) destination() types.TrafficDestination {
	d := types.TrafficDestination{
		Resolution:    r.Resolution,
		Bucket:        r.Bucket,
		NodeID:        types.NodeID(fromSQLInt(r.NodeID)),
		ReporterID:    types.NodeID(fromSQLInt(r.ReporterID)),
		Dst:           r.Dst,
		Port:          uint16(min(max(r.Port, 0), math.MaxUint16)),
		Proto:         uint8(min(max(r.Proto, 0), math.MaxUint8)),
		Host:          r.Host,
		ASN:           uint32(min(max(r.Asn, 0), math.MaxUint32)),
		TrafficCounts: countsOf(r.TxBytes, r.RxBytes, r.TxPackets, r.RxPackets, r.Conns),
	}

	if r.HostSource != nil {
		d.HostSource = *r.HostSource
	}

	if r.Country != nil {
		d.Country = *r.Country
	}

	return d
}

type trafficNameRow struct {
	Name    string
	NodeID  int64
	Queries int64
	Failed  int64
	Nodes   int64
}

// TrafficNames sums the DNS questions in the filter by name or by node,
// most asked first.
func (hsdb *HSDatabase) TrafficNames(f types.TrafficFilter, group types.TrafficGroup) ([]types.TrafficNameSum, error) {
	t := table.TrafficDNS

	var (
		key     jet.Projection
		groupBy jet.GroupByClause
	)

	switch group {
	case types.TrafficByName, "":
		key, groupBy = t.Name.AS("traffic_name_row.name"), t.Name
	case types.TrafficByNode:
		key, groupBy = t.NodeID.AS("traffic_name_row.node_id"), t.NodeID
	case types.TrafficByDestination, types.TrafficByHost, types.TrafficByASN, types.TrafficByCountry,
		types.TrafficByPort, types.TrafficByReporter:
		return nil, fmt.Errorf("%w for names: %q", types.ErrTrafficGroupUnknown, group)
	default:
		return nil, fmt.Errorf("%w: %q", types.ErrTrafficGroupUnknown, group)
	}

	cond := dnsColumns.where(f)
	if f.Search != "" {
		cond = cond.AND(t.Name.LIKE(jet.String(searchPattern(f.Search))))
	}

	var rows []trafficNameRow

	err := hsdb.ex.query(
		jet.SELECT(
			key,
			sum(t.Queries).AS("traffic_name_row.queries"),
			sum(t.Failed).AS("traffic_name_row.failed"),
			jet.COUNT(jet.DISTINCT(t.NodeID)).AS("traffic_name_row.nodes"),
		).
			FROM(t).
			WHERE(cond).
			GROUP_BY(groupBy).
			ORDER_BY(sum(t.Queries).DESC()).
			LIMIT(limitOf(f)),
		&rows,
	)
	if err != nil {
		return nil, fmt.Errorf("reading traffic names: %w", err)
	}

	out := make([]types.TrafficNameSum, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.TrafficNameSum{
			Name:    r.Name,
			NodeID:  types.NodeID(fromSQLInt(r.NodeID)),
			Queries: fromSQLInt(r.Queries),
			Failed:  fromSQLInt(r.Failed),
			Nodes:   fromSQLInt(r.Nodes),
		})
	}

	return out, nil
}

// PruneTraffic deletes the rows of each resolution older than its
// retention and returns how many went.
func (hsdb *HSDatabase) PruneTraffic(now time.Time, retention types.TrafficRetention) (int64, error) {
	return Write(hsdb, func(tx *Tx) (int64, error) {
		var deleted int64

		for _, target := range []struct {
			del         func() jet.DeleteStatement
			cols        trafficColumns
			resolutions []int64
		}{
			{
				table.TrafficTotals.DELETE, totalsColumns,
				[]int64{types.TrafficMinute, types.TrafficHour, types.TrafficDay},
			},
			{table.TrafficDestinations.DELETE, destinationColumns, []int64{types.TrafficHour, types.TrafficDay}},
			{table.TrafficDNS.DELETE, dnsColumns, []int64{types.TrafficHour, types.TrafficDay}},
		} {
			for _, res := range target.resolutions {
				cutoff := now.Add(-retention.Of(res)).Unix()

				n, err := tx.executor().exec(
					target.del().WHERE(
						target.cols.resolution.EQ(jet.Int64(res)).AND(target.cols.bucket.LT(jet.Int64(cutoff))),
					),
				)
				if err != nil {
					return 0, fmt.Errorf("pruning traffic: %w", err)
				}

				deleted += n
			}
		}

		return deleted, nil
	})
}
