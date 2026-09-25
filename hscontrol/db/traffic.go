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
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

const (
	// trafficChunkRows bounds the rows one transaction of a report writes.
	// SQLite has one connection, shared with the map request path, so a
	// large report is written in several short transactions rather than
	// one long one.
	trafficChunkRows = 2000

	// trafficStatementRows is how many rows one upsert statement carries.
	trafficStatementRows = 250

	// trafficInstancesKept is how many of a gateway's agent instances the
	// idempotency record remembers; an instance older than that which
	// resends a report is counted again.
	trafficInstancesKept = 8
)

// trafficReporterRow is a row of the traffic_reporters table; see
// schema.sql.
type trafficReporterRow struct {
	NodeID             uint64 `sql:"primary_key"`
	Instance           string
	LastSeq            int64
	Version            *string
	Status             *string
	DNSListen          *string
	FirstSeenAt        *time.Time
	LastReportAt       *time.Time
	Unattributed       int64
	Dropped            int64
	ResolverApprovedAt *time.Time
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

	if r.ResolverApprovedAt != nil {
		rep.ResolverApprovedAt = *r.ResolverApprovedAt
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

// trafficInstanceRow is a row of the traffic_instances table.
type trafficInstanceRow struct {
	NodeID        uint64
	Instance      string
	LastSeq       int64
	PendingSeq    int64
	PendingChunks int64
}

type trafficInstanceRecord struct {
	Row trafficInstanceRow `alias:"traffic_instances"`
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

// hostSourceRank ranks how a host name was learnt, in SQL: the name the
// node itself sent beats the one it looked up, which beats the app
// connector's, the outer ECH name and another node's lookup. The ranks
// are literals: PostgreSQL types a bound CASE result as text.
func hostSourceRank(col jet.StringExpression) jet.IntegerExpression {
	return jet.IntExp(jet.CASE(col).
		WHEN(jet.String(string(traffic.HostSNI))).THEN(jet.RawInt("5")).
		WHEN(jet.String(string(traffic.HostDNS))).THEN(jet.RawInt("4")).
		WHEN(jet.String(string(traffic.HostAppConnector))).THEN(jet.RawInt("3")).
		WHEN(jet.String(string(traffic.HostECH))).THEN(jet.RawInt("2")).
		WHEN(jet.String(string(traffic.HostDNSShared))).THEN(jet.RawInt("1")).
		ELSE(jet.RawInt("0")))
}

// trafficTotalsUpsert adds rows to the totals their buckets already have.
func trafficTotalsUpsert(rows []types.TrafficTotal) jet.InsertStatement {
	t := table.TrafficTotals

	stmt := t.INSERT(
		t.Resolution, t.Bucket, t.NodeID, t.ReporterID,
		t.TxBytes, t.RxBytes, t.TxPackets, t.RxPackets, t.Conns,
	)

	for _, r := range rows {
		stmt = stmt.VALUES(
			r.Resolution, r.Bucket, r.NodeID.Uint64(), r.ReporterID.Uint64(),
			toSQLInt(r.TxBytes), toSQLInt(r.RxBytes),
			toSQLInt(r.TxPackets), toSQLInt(r.RxPackets), toSQLInt(r.Conns),
		)
	}

	return stmt.ON_CONFLICT(t.Resolution, t.Bucket, t.NodeID, t.ReporterID).DO_UPDATE(jet.SET(
		t.TxBytes.SET(t.TxBytes.ADD(t.EXCLUDED.TxBytes)),
		t.RxBytes.SET(t.RxBytes.ADD(t.EXCLUDED.RxBytes)),
		t.TxPackets.SET(t.TxPackets.ADD(t.EXCLUDED.TxPackets)),
		t.RxPackets.SET(t.RxPackets.ADD(t.EXCLUDED.RxPackets)),
		t.Conns.SET(t.Conns.ADD(t.EXCLUDED.Conns)),
	))
}

// trafficDestinationsUpsert adds rows to the destinations their buckets
// already have. A row keeps its network when the new one does not know
// it (the ASN table was not loaded), and its host source unless the new
// one ranks higher.
func trafficDestinationsUpsert(rows []types.TrafficDestination) jet.InsertStatement {
	t := table.TrafficDestinations

	stmt := t.INSERT(
		t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Dst, t.Port, t.Proto, t.Host,
		t.HostSource, t.Asn, t.Country, t.Private,
		t.TxBytes, t.RxBytes, t.TxPackets, t.RxPackets, t.Conns,
	)

	for _, r := range rows {
		stmt = stmt.VALUES(
			r.Resolution, r.Bucket, r.NodeID.Uint64(), r.ReporterID.Uint64(),
			r.Dst, int64(r.Port), int64(r.Proto), r.Host,
			r.HostSource, int64(r.ASN), r.Country, sqlFlag(r.Private),
			toSQLInt(r.TxBytes), toSQLInt(r.RxBytes),
			toSQLInt(r.TxPackets), toSQLInt(r.RxPackets), toSQLInt(r.Conns),
		)
	}

	return stmt.ON_CONFLICT(
		t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Dst, t.Port, t.Proto, t.Host,
	).DO_UPDATE(jet.SET(
		t.HostSource.SET(jet.StringExp(jet.CASE().
			WHEN(hostSourceRank(t.EXCLUDED.HostSource).GT(hostSourceRank(t.HostSource))).
			THEN(t.EXCLUDED.HostSource).
			ELSE(t.HostSource))),
		t.Asn.SET(jet.IntExp(jet.CASE().
			WHEN(t.EXCLUDED.Asn.NOT_EQ(jet.Int(0))).THEN(t.EXCLUDED.Asn).
			ELSE(t.Asn))),
		t.Country.SET(jet.StringExp(jet.CASE().
			WHEN(t.EXCLUDED.Country.NOT_EQ(jet.String(""))).THEN(t.EXCLUDED.Country).
			ELSE(t.Country))),
		t.TxBytes.SET(t.TxBytes.ADD(t.EXCLUDED.TxBytes)),
		t.RxBytes.SET(t.RxBytes.ADD(t.EXCLUDED.RxBytes)),
		t.TxPackets.SET(t.TxPackets.ADD(t.EXCLUDED.TxPackets)),
		t.RxPackets.SET(t.RxPackets.ADD(t.EXCLUDED.RxPackets)),
		t.Conns.SET(t.Conns.ADD(t.EXCLUDED.Conns)),
	))
}

// trafficDNSUpsert adds rows to the names their buckets already have.
func trafficDNSUpsert(rows []types.TrafficDNS) jet.InsertStatement {
	t := table.TrafficDNS

	stmt := t.INSERT(t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Name, t.Queries, t.Failed)

	for _, r := range rows {
		stmt = stmt.VALUES(
			r.Resolution, r.Bucket, r.NodeID.Uint64(), r.ReporterID.Uint64(),
			r.Name, toSQLInt(r.Queries), toSQLInt(r.Failed),
		)
	}

	return stmt.ON_CONFLICT(t.Resolution, t.Bucket, t.NodeID, t.ReporterID, t.Name).DO_UPDATE(jet.SET(
		t.Queries.SET(t.Queries.ADD(t.EXCLUDED.Queries)),
		t.Failed.SET(t.Failed.ADD(t.EXCLUDED.Failed)),
	))
}

// trafficChunks splits a batch into the statements of each transaction:
// at most trafficChunkRows rows per transaction and trafficStatementRows
// per statement. The split depends only on the batch, so a resend of the
// same report splits the same way and can skip what was written.
func trafficChunks(batch types.TrafficBatch) [][]jet.Statement {
	var (
		chunks  [][]jet.Statement
		current []jet.Statement
		rows    int
	)

	add := func(stmt jet.Statement, n int) {
		if rows+n > trafficChunkRows && len(current) > 0 {
			chunks = append(chunks, current)
			current, rows = nil, 0
		}

		current = append(current, stmt)
		rows += n
	}

	for part := range slices.Chunk(batch.Totals, trafficStatementRows) {
		add(trafficTotalsUpsert(part), len(part))
	}

	for part := range slices.Chunk(batch.Destinations, trafficStatementRows) {
		add(trafficDestinationsUpsert(part), len(part))
	}

	for part := range slices.Chunk(batch.DNS, trafficStatementRows) {
		add(trafficDNSUpsert(part), len(part))
	}

	if len(current) > 0 {
		chunks = append(chunks, current)
	}

	return chunks
}

// TrafficApplied is what [HSDatabase.ApplyTrafficBatch] did.
type TrafficApplied struct {
	// Reporter is the gateway as stored.
	Reporter types.TrafficReporter
	// Applied is whether the rollups were written; false for a resend.
	Applied bool
	// Seq is how far the report's instance is applied, what the agent
	// may drop from its spool.
	Seq uint64
	// Holds is how long each transaction held the database.
	Holds []time.Duration
}

// ApplyTrafficBatch writes a report: the rollups in transactions of at
// most trafficChunkRows rows, then the reporter. The reporter's liveness
// always moves on, since a resent report still says the agent is alive;
// the rollups are written only when the report's sequence is new for its
// instance. Each transaction records how far the report got, so a report
// sent again after a failure part way writes only what is missing. The
// caller must not apply two reports of one gateway at once.
func (hsdb *HSDatabase) ApplyTrafficBatch(batch types.TrafficBatch) (TrafficApplied, error) {
	next := batch.Reporter
	seq := next.LastSeq

	inst, found, err := getTrafficInstance(hsdb, next.NodeID, next.Instance)
	if err != nil {
		return TrafficApplied{}, err
	}

	if found && seq <= fromSQLInt(inst.LastSeq) {
		out := TrafficApplied{Seq: fromSQLInt(inst.LastSeq)}

		hold, writeErr := timedWrite(hsdb, func(tx *Tx) error {
			var saveErr error

			out.Reporter, saveErr = saveTrafficReporter(tx, next, false)

			return saveErr
		})
		out.Holds = append(out.Holds, hold)

		return out, writeErr
	}

	skip := 0
	if found && fromSQLInt(inst.PendingSeq) == seq {
		skip = int(min(max(inst.PendingChunks, 0), math.MaxInt32))
	}

	out := TrafficApplied{Applied: true, Seq: seq}
	chunks := trafficChunks(batch)

	for i := skip; i < len(chunks); i++ {
		hold, writeErr := timedWrite(hsdb, func(tx *Tx) error {
			for _, stmt := range chunks[i] {
				_, execErr := tx.executor().exec(stmt)
				if execErr != nil {
					return fmt.Errorf("writing traffic rollups: %w", execErr)
				}
			}

			return saveTrafficInstance(tx, next, 0, seq, i+1)
		})
		out.Holds = append(out.Holds, hold)

		if writeErr != nil {
			return TrafficApplied{}, writeErr
		}
	}

	hold, err := timedWrite(hsdb, func(tx *Tx) error {
		txErr := saveTrafficInstance(tx, next, seq, 0, 0)
		if txErr != nil {
			return txErr
		}

		txErr = trimTrafficInstances(tx, next.NodeID)
		if txErr != nil {
			return txErr
		}

		out.Reporter, txErr = saveTrafficReporter(tx, next, true)

		return txErr
	})
	out.Holds = append(out.Holds, hold)

	if err != nil {
		return TrafficApplied{}, err
	}

	return out, nil
}

// sqlFlag is a flag as the integer column that stores it.
func sqlFlag(on bool) int64 {
	if on {
		return 1
	}

	return 0
}

// timedWrite runs fn in a write transaction and returns how long the
// transaction took.
func timedWrite(hsdb *HSDatabase, fn func(tx *Tx) error) (time.Duration, error) {
	start := time.Now()
	err := hsdb.Write(fn)

	return time.Since(start), err
}

func getTrafficInstance(q Querier, node types.NodeID, instance string) (trafficInstanceRow, bool, error) {
	t := table.TrafficInstances

	var records []trafficInstanceRecord

	err := q.executor().query(
		jet.SELECT(t.NodeID, t.Instance, t.LastSeq, t.PendingSeq, t.PendingChunks).
			FROM(t).
			WHERE(t.NodeID.EQ(jet.Uint64(node.Uint64())).AND(t.Instance.EQ(jet.String(instance)))),
		&records,
	)
	if err != nil {
		return trafficInstanceRow{}, false, fmt.Errorf("reading traffic instance %d/%s: %w", node, instance, err)
	}

	if len(records) == 0 {
		return trafficInstanceRow{}, false, nil
	}

	return records[0].Row, true, nil
}

// saveTrafficInstance records how far a report of the instance got: the
// last sequence applied in full, or the chunks of pendingSeq written. A
// zero lastSeq keeps the stored one.
func saveTrafficInstance(tx *Tx, r types.TrafficReporter, lastSeq, pendingSeq uint64, pendingChunks int) error {
	t := table.TrafficInstances

	update := []jet.ColumnAssigment{
		t.PendingSeq.SET(t.EXCLUDED.PendingSeq),
		t.PendingChunks.SET(t.EXCLUDED.PendingChunks),
		t.SeenAt.SET(t.EXCLUDED.SeenAt),
	}
	if lastSeq > 0 {
		update = append(update, t.LastSeq.SET(t.EXCLUDED.LastSeq))
	}

	_, err := tx.executor().exec(
		t.INSERT(t.NodeID, t.Instance, t.LastSeq, t.PendingSeq, t.PendingChunks, t.SeenAt).
			VALUES(r.NodeID.Uint64(), r.Instance, toSQLInt(lastSeq), toSQLInt(pendingSeq),
				int64(pendingChunks), r.LastReportAt.UTC()).
			ON_CONFLICT(t.NodeID, t.Instance).DO_UPDATE(jet.SET(update...)),
	)
	if err != nil {
		return fmt.Errorf("saving traffic instance %d/%s: %w", r.NodeID, r.Instance, err)
	}

	return nil
}

// trimTrafficInstances forgets all but the gateway's newest instances.
func trimTrafficInstances(tx *Tx, node types.NodeID) error {
	t := table.TrafficInstances
	byNode := t.NodeID.EQ(jet.Uint64(node.Uint64()))

	_, err := tx.executor().exec(t.DELETE().WHERE(byNode.AND(t.Instance.NOT_IN(
		jet.SELECT(t.Instance).FROM(t).WHERE(byNode).
			ORDER_BY(t.SeenAt.DESC(), t.Instance.ASC()).
			LIMIT(trafficInstancesKept),
	))))
	if err != nil {
		return fmt.Errorf("trimming traffic instances of %d: %w", node, err)
	}

	return nil
}

// saveTrafficReporter merges the report's reporter into the stored row:
// the liveness fields always, the counters only when the report was
// applied. The resolver approval is the operator's and never changes here.
func saveTrafficReporter(tx *Tx, next types.TrafficReporter, applied bool) (types.TrafficReporter, error) {
	current, found, err := getTrafficReporter(tx, next.NodeID)
	if err != nil {
		return types.TrafficReporter{}, err
	}

	merged := next
	merged.FirstSeenAt = next.LastReportAt

	if found {
		merged.FirstSeenAt = current.FirstSeenAt
		merged.ResolverApprovedAt = current.ResolverApprovedAt
		merged.Unattributed += current.Unattributed
		merged.Dropped += current.Dropped

		if !applied {
			merged.LastSeq = current.LastSeq
			merged.Unattributed = current.Unattributed
			merged.Dropped = current.Dropped
		}
	}

	status, err := json.Marshal(merged.Status)
	if err != nil {
		return types.TrafficReporter{}, fmt.Errorf("encoding traffic reporter status: %w", err)
	}

	listen := merged.DNSListen
	if listen == nil {
		listen = []netip.AddrPort{}
	}

	dnsListen, err := json.Marshal(listen)
	if err != nil {
		return types.TrafficReporter{}, fmt.Errorf("encoding traffic reporter resolvers: %w", err)
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
		return types.TrafficReporter{}, fmt.Errorf("saving traffic reporter %d: %w", merged.NodeID, err)
	}

	return merged, nil
}

// SetTrafficResolverApproval records whether the tailnet's clients may use
// the gateway's resolver: approved at the time given, or not when nil.
func (hsdb *HSDatabase) SetTrafficResolverApproval(id types.NodeID, at *time.Time) error {
	t := table.TrafficReporters

	var value any = jet.NULL
	if at != nil {
		value = at.UTC()
	}

	affected, err := hsdb.ex.exec(
		t.UPDATE(t.ResolverApprovedAt).
			SET(value).
			WHERE(t.NodeID.EQ(jet.Uint64(id.Uint64()))),
	)
	if err != nil {
		return fmt.Errorf("setting the resolver approval of traffic reporter %d: %w", id, err)
	}

	if affected == 0 {
		return ErrTrafficReporterNotFound
	}

	return nil
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

// searchEscape escapes what LIKE would read as a wildcard, with ! as the
// escape character, so a search for "a_b" finds only "a_b".
var searchEscape = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// contains matches col holding s anywhere, lower case to match the stored
// names. jet's LIKE takes no ESCAPE clause, so it is spelled out; both
// dialects accept it.
func contains(col jet.StringExpression, s string) jet.BoolExpression {
	pattern := jet.String("%" + searchEscape.Replace(strings.ToLower(s)) + "%")

	return jet.BoolExp(jet.CustomExpression(col, jet.Token("LIKE"), pattern, jet.Token("ESCAPE '!'")))
}

func destinationWhere(f types.TrafficFilter) jet.BoolExpression {
	t := table.TrafficDestinations
	cond := destinationColumns.where(f)

	if f.Search != "" {
		cond = cond.AND(contains(t.Host, f.Search).OR(contains(t.Dst, f.Search)))
	}

	if f.Host != "" {
		host := jet.String(strings.ToLower(f.Host))
		cond = cond.AND(t.Host.EQ(host).OR(t.Host.EQ(jet.String("")).AND(t.Dst.EQ(host))))
	}

	if f.Dst != "" {
		cond = cond.AND(t.Dst.EQ(jet.String(f.Dst)))
	}

	if f.Private {
		cond = cond.AND(t.Private.EQ(jet.Int64(sqlFlag(true))))
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
	Private   int64
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
		Private:       r.Private != 0,
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

	// A group is private when every destination in it is. Private
	// destinations have no network or country, so grouped by either they
	// get their own row rather than joining the unknown public ones.
	allPrivate := jet.MIN(t.Private).AS(alias + "private")

	switch group {
	case types.TrafficByDestination, "":
		return []jet.Projection{
				t.Dst.AS(alias + "dst"), t.Port.AS(alias + "port"), t.Proto.AS(alias + "proto"),
				t.Host.AS(alias + "host"), t.Asn.AS(alias + "asn"), t.Country.AS(alias + "country"), allPrivate,
			},
			[]jet.GroupByClause{t.Dst, t.Port, t.Proto, t.Host, t.Asn, t.Country},
			nil
	case types.TrafficByHost:
		// A destination without a name groups by its address instead,
		// so unnamed traffic does not collapse into one row.
		// The empty string is a literal: PostgreSQL only accepts the
		// grouped expression in SELECT when both are the same text.
		key := jet.COALESCE(jet.NULLIF(t.Host, jet.StringExp(jet.Raw("''"))), t.Dst)

		return []jet.Projection{key.AS(alias + "host"), allPrivate}, []jet.GroupByClause{key}, nil
	case types.TrafficByASN:
		return []jet.Projection{t.Asn.AS(alias + "asn"), allPrivate},
			[]jet.GroupByClause{t.Asn, t.Private},
			nil
	case types.TrafficByCountry:
		return []jet.Projection{t.Country.AS(alias + "country"), allPrivate},
			[]jet.GroupByClause{t.Country, t.Private},
			nil
	case types.TrafficByPort:
		return []jet.Projection{t.Proto.AS(alias + "proto"), t.Port.AS(alias + "port"), allPrivate},
			[]jet.GroupByClause{t.Proto, t.Port},
			nil
	case types.TrafficByNode:
		return []jet.Projection{t.NodeID.AS(alias + "node_id"), allPrivate}, []jet.GroupByClause{t.NodeID}, nil
	case types.TrafficByReporter:
		return []jet.Projection{t.ReporterID.AS(alias + "node_id"), allPrivate},
			[]jet.GroupByClause{t.ReporterID},
			nil
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
	Private    int64
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
		Private:       r.Private != 0,
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
		cond = cond.AND(contains(t.Name, f.Search))
	}

	if f.Name != "" {
		cond = cond.AND(t.Name.EQ(jet.String(strings.ToLower(strings.TrimSuffix(f.Name, ".")))))
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
