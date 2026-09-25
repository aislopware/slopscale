package db

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forEachDialect runs fn against a fresh SQLite and a fresh PostgreSQL
// database, so every traffic query is proven on both.
func forEachDialect(t *testing.T, fn func(t *testing.T, db *HSDatabase)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()

		db, err := newSQLiteTestDB()
		require.NoError(t, err)

		fn(t, db)
	})

	t.Run("postgres", func(t *testing.T) {
		t.Parallel()

		fn(t, newPostgresTestDB(t))
	})
}

type trafficFixture struct {
	laptop, phone, gateway types.NodeID
	hour                   int64
}

func newTrafficFixture(t *testing.T, db *HSDatabase) trafficFixture {
	t.Helper()

	user := db.CreateUserForTest("traffic")

	return trafficFixture{
		laptop:  db.CreateNodeForTest(user, "laptop").ID,
		phone:   db.CreateNodeForTest(user, "phone").ID,
		gateway: db.CreateNodeForTest(user, "gateway").ID,
		hour:    time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC).Unix(),
	}
}

func (f trafficFixture) key(res, bucket int64, node types.NodeID) types.TrafficKey {
	return types.TrafficKey{Resolution: res, Bucket: bucket, NodeID: node, ReporterID: f.gateway}
}

func (f trafficFixture) reporter(instance string, seq uint64) types.TrafficReporter {
	return types.TrafficReporter{
		NodeID:       f.gateway,
		Instance:     instance,
		LastSeq:      seq,
		Version:      "1.0.0",
		Status:       traffic.Status{Conntrack: traffic.Collector{Enabled: true}},
		DNSListen:    []netip.AddrPort{netip.MustParseAddrPort("100.64.0.9:53")},
		LastReportAt: time.Unix(f.hour, 0).UTC(),
		Unattributed: 2,
		Dropped:      1,
	}
}

// batch is one report: the laptop sends 100/1000 bytes to a named host
// and 10/10 to an address, the phone 5/50 to the named host, all in one
// minute, rolled up to the minute, the hour and the day.
func (f trafficFixture) batch(instance string, seq uint64) types.TrafficBatch {
	named := types.TrafficCounts{TxBytes: 100, RxBytes: 1000, TxPackets: 1, RxPackets: 2, Conns: 1}
	bare := types.TrafficCounts{TxBytes: 10, RxBytes: 10}
	phone := types.TrafficCounts{TxBytes: 5, RxBytes: 50}
	laptop := named
	laptop.Add(bare)

	b := types.TrafficBatch{Reporter: f.reporter(instance, seq)}

	for _, res := range []int64{types.TrafficMinute, types.TrafficHour, types.TrafficDay} {
		bucket := f.hour - f.hour%res
		b.Totals = append(b.Totals,
			types.TrafficTotal{TrafficKey: f.key(res, bucket, f.laptop), TrafficCounts: laptop},
			types.TrafficTotal{TrafficKey: f.key(res, bucket, f.phone), TrafficCounts: phone},
		)

		if res == types.TrafficMinute {
			continue
		}

		b.Destinations = append(b.Destinations,
			types.TrafficDestination{
				TrafficKey: f.key(res, bucket, f.laptop), Dst: "142.250.1.1", Port: 443, Proto: 6,
				Host: "www.google.com", HostSource: "sni", ASN: 15169, Country: "US", TrafficCounts: named,
			},
			types.TrafficDestination{
				TrafficKey: f.key(res, bucket, f.laptop), Dst: "203.0.113.7", Port: 22, Proto: 6,
				ASN: 64500, Country: "VN", TrafficCounts: bare,
			},
			types.TrafficDestination{
				TrafficKey: f.key(res, bucket, f.phone), Dst: "142.250.1.1", Port: 443, Proto: 6,
				Host: "www.google.com", HostSource: "dns", ASN: 15169, Country: "US", TrafficCounts: phone,
			},
		)
		b.DNS = append(b.DNS,
			types.TrafficDNS{TrafficKey: f.key(res, bucket, f.laptop), Name: "www.google.com", Queries: 3},
			types.TrafficDNS{TrafficKey: f.key(res, bucket, f.phone), Name: "www.google.com", Queries: 1, Failed: 1},
			types.TrafficDNS{TrafficKey: f.key(res, bucket, f.phone), Name: "tracker.example", Queries: 7},
		)
	}

	return b
}

func (f trafficFixture) filter(res int64) types.TrafficFilter {
	return types.TrafficFilter{
		Resolution: res,
		Start:      time.Unix(f.hour, 0).Add(-24 * time.Hour),
		End:        time.Unix(f.hour, 0).Add(24 * time.Hour),
	}
}

func TestTrafficBatchAndQueries(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)

		res, err := db.ApplyTrafficBatch(f.batch("a", 1))
		require.NoError(t, err)
		assert.True(t, res.Applied)
		assert.Equal(t, uint64(1), res.Seq)
		assert.Equal(t, uint64(1), res.Reporter.LastSeq)

		reporters, err := db.ListTrafficReporters()
		require.NoError(t, err)
		require.Len(t, reporters, 1)
		assert.Equal(t, f.gateway, reporters[0].NodeID)
		assert.Equal(t, "1.0.0", reporters[0].Version)
		assert.True(t, reporters[0].Status.Conntrack.Enabled)
		assert.Equal(t, []netip.AddrPort{netip.MustParseAddrPort("100.64.0.9:53")}, reporters[0].DNSListen)
		assert.Equal(t, uint64(2), reporters[0].Unattributed)
		assert.Equal(t, uint64(1), reporters[0].Dropped)

		sum, err := db.TrafficSum(f.filter(types.TrafficHour))
		require.NoError(t, err)
		assert.Equal(t, uint64(115), sum.TxBytes)
		assert.Equal(t, uint64(1060), sum.RxBytes)

		series, err := db.TrafficSeries(f.filter(types.TrafficMinute))
		require.NoError(t, err)
		require.Len(t, series, 1)
		assert.Equal(t, f.hour, series[0].Bucket)
		assert.Equal(t, uint64(1175), series[0].Bytes())

		top, err := db.TrafficTopNodes(f.filter(types.TrafficDay), false)
		require.NoError(t, err)
		require.Len(t, top, 2)
		assert.Equal(t, f.laptop, top[0].NodeID, "the laptop moved more")
		assert.Equal(t, uint64(1120), top[0].Bytes())

		byGateway, err := db.TrafficTopNodes(f.filter(types.TrafficDay), true)
		require.NoError(t, err)
		require.Len(t, byGateway, 1)
		assert.Equal(t, f.gateway, byGateway[0].NodeID)

		onlyPhone := f.filter(types.TrafficHour)
		onlyPhone.NodeID = f.phone
		phone, err := db.TrafficSum(onlyPhone)
		require.NoError(t, err)
		assert.Equal(t, uint64(55), phone.Bytes())

		empty := f.filter(types.TrafficHour)
		empty.Start, empty.End = time.Unix(0, 0), time.Unix(60, 0)
		none, err := db.TrafficSum(empty)
		require.NoError(t, err)
		assert.Zero(t, none.Bytes(), "an empty range sums to zero")
	})
}

func TestTrafficDestinationGroupings(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)

		_, err := db.ApplyTrafficBatch(f.batch("a", 1))
		require.NoError(t, err)

		filter := f.filter(types.TrafficHour)

		rows, err := db.TrafficDestinations(filter, types.TrafficByDestination)
		require.NoError(t, err)
		require.Len(t, rows, 2, "two nodes to the same destination are one group")
		assert.Equal(t, uint64(2), rows[0].Nodes)
		assert.Equal(t, "US", rows[0].Country)
		assert.Equal(t, uint32(15169), rows[0].ASN)

		hosts, err := db.TrafficDestinations(filter, types.TrafficByHost)
		require.NoError(t, err)
		require.Len(t, hosts, 2)
		assert.Equal(t, "www.google.com", hosts[0].Host)
		assert.Equal(t, uint64(1155), hosts[0].Bytes())
		assert.Equal(t, uint64(2), hosts[0].Nodes)
		assert.Equal(t, "203.0.113.7", hosts[1].Host, "an unnamed destination groups by its address")

		asns, err := db.TrafficDestinations(filter, types.TrafficByASN)
		require.NoError(t, err)
		require.Len(t, asns, 2)
		assert.Equal(t, uint32(15169), asns[0].ASN)

		countries, err := db.TrafficDestinations(filter, types.TrafficByCountry)
		require.NoError(t, err)
		require.Len(t, countries, 2)
		assert.Equal(t, "US", countries[0].Country)

		ports, err := db.TrafficDestinations(filter, types.TrafficByPort)
		require.NoError(t, err)
		require.Len(t, ports, 2)
		assert.Equal(t, uint16(443), ports[0].Port)
		assert.Equal(t, uint8(6), ports[0].Proto)

		nodes, err := db.TrafficDestinations(filter, types.TrafficByNode)
		require.NoError(t, err)
		require.Len(t, nodes, 2)
		assert.Equal(t, f.laptop, nodes[0].NodeID)

		gateways, err := db.TrafficDestinations(filter, types.TrafficByReporter)
		require.NoError(t, err)
		require.Len(t, gateways, 1)
		assert.Equal(t, f.gateway, gateways[0].NodeID)

		search := filter
		search.Search = "GOOGLE"
		found, err := db.TrafficDestinations(search, types.TrafficByHost)
		require.NoError(t, err)
		require.Len(t, found, 1, "search is case-insensitive")

		// LIKE wildcards in a search are literal characters.
		for _, q := range []string{"www_google", "%", "_", "!"} {
			search.Search = q
			found, err = db.TrafficDestinations(search, types.TrafficByHost)
			require.NoError(t, err)
			assert.Empty(t, found, "%q matches only itself", q)
		}

		search.Search = "www.google"
		found, err = db.TrafficDestinations(search, types.TrafficByHost)
		require.NoError(t, err)
		require.Len(t, found, 1)

		ssh := filter
		ssh.Proto, ssh.Port = 6, 22
		sshRows, err := db.TrafficDestinations(ssh, types.TrafficByDestination)
		require.NoError(t, err)
		require.Len(t, sshRows, 1)
		assert.Equal(t, "VN", sshRows[0].Country)

		limited := filter
		limited.Limit = 1
		one, err := db.TrafficDestinations(limited, types.TrafficByHost)
		require.NoError(t, err)
		require.Len(t, one, 1)

		_, err = db.TrafficDestinations(filter, "bogus")
		require.ErrorIs(t, err, types.ErrTrafficGroupUnknown)

		names, err := db.TrafficNames(filter, types.TrafficByName)
		require.NoError(t, err)
		require.Len(t, names, 2)
		assert.Equal(t, "tracker.example", names[0].Name)
		assert.Equal(t, uint64(7), names[0].Queries)
		assert.Equal(t, "www.google.com", names[1].Name)
		assert.Equal(t, uint64(4), names[1].Queries)
		assert.Equal(t, uint64(1), names[1].Failed)
		assert.Equal(t, uint64(2), names[1].Nodes)

		askers, err := db.TrafficNames(filter, types.TrafficByNode)
		require.NoError(t, err)
		require.Len(t, askers, 2)
		assert.Equal(t, f.phone, askers[0].NodeID)

		exported, err := db.TrafficDestinationRows(filter)
		require.NoError(t, err)
		require.Len(t, exported, 3)
		assert.Equal(t, "sni", exported[0].HostSource)
	})
}

func TestTrafficBatchIdempotent(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)

		_, err := db.ApplyTrafficBatch(f.batch("a", 5))
		require.NoError(t, err)

		resent := f.batch("a", 5)
		resent.Reporter.LastReportAt = resent.Reporter.LastReportAt.Add(time.Minute)
		resent.Reporter.Version = "1.0.1"

		res, err := db.ApplyTrafficBatch(resent)
		require.NoError(t, err)
		assert.False(t, res.Applied, "a resend of an applied sequence is not applied again")
		assert.Equal(t, uint64(5), res.Seq)
		assert.Equal(t, uint64(5), res.Reporter.LastSeq)

		sum, err := db.TrafficSum(f.filter(types.TrafficHour))
		require.NoError(t, err)
		assert.Equal(t, uint64(1175), sum.Bytes(), "the counts are not doubled")

		reporters, err := db.ListTrafficReporters()
		require.NoError(t, err)
		require.Len(t, reporters, 1)
		assert.Equal(t, "1.0.1", reporters[0].Version, "a resend still refreshes the reporter")
		assert.Equal(t, resent.Reporter.LastReportAt, reporters[0].LastReportAt.UTC())
		assert.Equal(t, uint64(2), reporters[0].Unattributed, "a resend adds no counters")

		res, err = db.ApplyTrafficBatch(f.batch("a", 4))
		require.NoError(t, err)
		assert.False(t, res.Applied, "an older sequence is not applied")

		res, err = db.ApplyTrafficBatch(f.batch("b", 1))
		require.NoError(t, err)
		assert.True(t, res.Applied, "a new instance starts its own sequence")

		sum, err = db.TrafficSum(f.filter(types.TrafficHour))
		require.NoError(t, err)
		assert.Equal(t, uint64(2350), sum.Bytes())

		// The first instance resends after the second wrote: still a
		// resend, not new traffic.
		res, err = db.ApplyTrafficBatch(f.batch("a", 5))
		require.NoError(t, err)
		assert.False(t, res.Applied, "an older instance's resend is recognised")
		assert.Equal(t, uint64(5), res.Seq)

		sum, err = db.TrafficSum(f.filter(types.TrafficHour))
		require.NoError(t, err)
		assert.Equal(t, uint64(2350), sum.Bytes())

		reporters, err = db.ListTrafficReporters()
		require.NoError(t, err)
		assert.Equal(t, uint64(4), reporters[0].Unattributed)
		assert.Equal(t, time.Unix(f.hour, 0).UTC(), reporters[0].FirstSeenAt.UTC(), "first seen stays")

		// Only the newest instances are remembered.
		for i := range trafficInstancesKept + 2 {
			b := f.batch(fmt.Sprintf("n%02d", i), 1)
			b.Reporter.LastReportAt = b.Reporter.LastReportAt.Add(time.Duration(i+2) * time.Minute)
			_, err = db.ApplyTrafficBatch(b)
			require.NoError(t, err)
		}

		var instances []trafficInstanceRecord

		require.NoError(t, db.ex.query(
			jet.SELECT(table.TrafficInstances.AllColumns).FROM(table.TrafficInstances),
			&instances,
		))
		assert.Len(t, instances, trafficInstancesKept)
	})
}

// TestTrafficBatchResumes proves a large report is written in several
// short transactions, and that a report failing part way is completed by
// its resend without counting what was already written twice.
func TestTrafficBatchResumes(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)

		batch := types.TrafficBatch{Reporter: f.reporter("a", 1)}
		key := f.key(types.TrafficHour, f.hour, f.laptop)

		const destinations = 2*trafficChunkRows + 10

		for i := range destinations {
			batch.Destinations = append(batch.Destinations, types.TrafficDestination{
				TrafficKey: key, Dst: fmt.Sprintf("198.18.%d.%d", i/256, i%256), Port: 443, Proto: 6,
				TxBytes: 1,
			})
		}

		chunks := trafficChunks(batch)
		require.Len(t, chunks, 3, "at most trafficChunkRows rows per transaction")

		// The first chunk lands, then the write fails: the instance says
		// how far it got.
		require.NoError(t, db.Write(func(tx *Tx) error {
			for _, stmt := range chunks[0] {
				_, err := tx.executor().exec(stmt)
				if err != nil {
					return err
				}
			}

			return saveTrafficInstance(tx, batch.Reporter, 0, 1, 1)
		}))

		res, err := db.ApplyTrafficBatch(batch)
		require.NoError(t, err)
		assert.True(t, res.Applied)
		assert.Len(t, res.Holds, 3, "the two missing chunks and the reporter")

		rows, err := db.TrafficDestinationRows(f.filter(types.TrafficHour))
		require.NoError(t, err)
		require.Len(t, rows, destinations)

		for _, r := range rows {
			require.Equal(t, uint64(1), r.TxBytes, "%s written once", r.Dst)
		}

		res, err = db.ApplyTrafficBatch(batch)
		require.NoError(t, err)
		assert.False(t, res.Applied)
	})
}

// TestTrafficDestinationUpsertKeepsWhatItKnows proves a later report that
// does not know a destination's network, or learnt its name a weaker way,
// does not erase what an earlier one stored.
// TestTrafficPrivateGroups checks that a group of destinations says it is
// private when every destination in it is, and that grouped by network or
// country the private destinations form their own row instead of joining
// the public ones the ASN table does not know.
func TestTrafficPrivateGroups(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)
		key := f.key(types.TrafficHour, f.hour, f.laptop)
		dest := func(dst, host string, private bool, tx uint64) types.TrafficDestination {
			return types.TrafficDestination{
				TrafficKey: key, Dst: dst, Port: 443, Proto: 6, Host: host, Private: private, TxBytes: tx,
			}
		}

		_, err := db.ApplyTrafficBatch(types.TrafficBatch{
			Reporter: f.reporter("a", 1),
			Destinations: []types.TrafficDestination{
				dest("192.168.1.5", "nas.office.lan", true, 400),
				dest("192.168.1.6", "nas.office.lan", true, 300),
				dest("10.0.0.9", "mixed.example", true, 20),
				dest("198.51.100.9", "mixed.example", false, 10),
				dest("203.0.113.7", "", false, 5),
			},
		})
		require.NoError(t, err)

		sums := func(group types.TrafficGroup) []types.TrafficDestinationSum {
			t.Helper()

			out, readErr := db.TrafficDestinations(f.filter(types.TrafficHour), group)
			require.NoError(t, readErr)

			return out
		}

		byHost := sums(types.TrafficByHost)
		require.Len(t, byHost, 3)
		assert.Equal(t, "nas.office.lan", byHost[0].Host)
		assert.True(t, byHost[0].Private, "every address of the name is private")
		assert.Equal(t, "mixed.example", byHost[1].Host)
		assert.False(t, byHost[1].Private, "one address of the name is public")
		assert.False(t, byHost[2].Private)

		byASN := sums(types.TrafficByASN)
		require.Len(t, byASN, 2, "the private destinations apart from the unknown public ones")
		assert.True(t, byASN[0].Private)
		assert.Equal(t, uint64(720), byASN[0].TxBytes)
		assert.False(t, byASN[1].Private)
		assert.Equal(t, uint64(15), byASN[1].TxBytes)

		assert.Len(t, sums(types.TrafficByCountry), 2)

		rows, err := db.TrafficDestinationRows(f.filter(types.TrafficHour))
		require.NoError(t, err)

		private := 0

		for _, r := range rows {
			if r.Private {
				private++
			}
		}

		assert.Equal(t, 3, private, "the flag is stored with the row")
	})
}

func TestTrafficDestinationUpsertKeepsWhatItKnows(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)
		key := f.key(types.TrafficHour, f.hour, f.laptop)

		write := func(seq uint64, d types.TrafficDestination) {
			t.Helper()

			d.TrafficKey, d.Dst, d.Port, d.Proto, d.Host = key, "142.250.1.1", 443, 6, "www.google.com"
			d.TxBytes = 1

			_, err := db.ApplyTrafficBatch(types.TrafficBatch{
				Reporter:     f.reporter("a", seq),
				Destinations: []types.TrafficDestination{d},
			})
			require.NoError(t, err)
		}

		read := func() types.TrafficDestination {
			t.Helper()

			rows, err := db.TrafficDestinationRows(f.filter(types.TrafficHour))
			require.NoError(t, err)
			require.Len(t, rows, 1)

			return rows[0]
		}

		write(1, types.TrafficDestination{HostSource: "dns", ASN: 15169, Country: "US"})
		write(2, types.TrafficDestination{HostSource: "dns-shared"})

		row := read()
		assert.Equal(t, "dns", row.HostSource, "a weaker source does not replace a stronger one")
		assert.Equal(t, uint32(15169), row.ASN, "an unknown network keeps the known one")
		assert.Equal(t, "US", row.Country)

		write(3, types.TrafficDestination{HostSource: "sni", ASN: 396982, Country: "NL"})

		row = read()
		assert.Equal(t, "sni", row.HostSource, "a stronger source replaces a weaker one")
		assert.Equal(t, uint32(396982), row.ASN, "a known network replaces the stored one")
		assert.Equal(t, "NL", row.Country)
		assert.Equal(t, uint64(3), row.TxBytes)
	})
}

func TestTrafficFoldAndPrune(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)

		key := f.key(types.TrafficHour, f.hour, f.laptop)
		batch := types.TrafficBatch{Reporter: f.reporter("a", 1)}

		for i, dst := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3", "198.51.100.4", "198.51.100.5"} {
			batch.Destinations = append(batch.Destinations, types.TrafficDestination{
				TrafficKey: key, Dst: dst, Port: 443, Proto: 6,
				TxBytes: uint64(100 * (i + 1)),
			})
			batch.DNS = append(batch.DNS, types.TrafficDNS{
				TrafficKey: key, Name: dst + ".example", Queries: uint64(i + 1),
			})
		}

		batch.Totals = append(batch.Totals,
			types.TrafficTotal{TrafficKey: f.key(types.TrafficMinute, f.hour, f.laptop)},
			types.TrafficTotal{TrafficKey: f.key(types.TrafficMinute, f.hour-3*3600, f.laptop)},
		)

		_, err := db.ApplyTrafficBatch(batch)
		require.NoError(t, err)

		start := time.Unix(f.hour, 0)

		folded, err := db.FoldTraffic(types.TrafficHour, start.Add(-time.Hour), start, 2)
		require.NoError(t, err)
		assert.Zero(t, folded, "a bucket outside the range is left alone")

		folded, err = db.FoldTraffic(types.TrafficHour, start, start.Add(time.Hour), 2)
		require.NoError(t, err)
		assert.Equal(t, int64(6), folded, "three destinations and three names beyond the two largest")

		rows, err := db.TrafficDestinations(f.filter(types.TrafficHour), types.TrafficByDestination)
		require.NoError(t, err)
		require.Len(t, rows, 3)
		assert.Empty(t, rows[0].Dst, "the rest is one remainder row, the largest: 100+200+300")
		assert.Equal(t, uint64(600), rows[0].TxBytes)
		assert.Equal(t, "198.51.100.5", rows[1].Dst)
		assert.Equal(t, "198.51.100.4", rows[2].Dst)

		names, err := db.TrafficNames(f.filter(types.TrafficHour), types.TrafficByName)
		require.NoError(t, err)
		require.Len(t, names, 3)
		assert.Empty(t, names[0].Name, "the remainder asked most: 1+2+3")
		assert.Equal(t, uint64(6), names[0].Queries)

		folded, err = db.FoldTraffic(types.TrafficHour, start, start.Add(time.Hour), 2)
		require.NoError(t, err)
		assert.Zero(t, folded, "folding twice changes nothing")

		retention := types.TrafficRetention{MinuteHours: 2, HourDays: 1, DayDays: 1}

		deleted, err := db.PruneTraffic(start.Add(time.Hour), retention)
		require.NoError(t, err)
		assert.Equal(t, int64(1), deleted, "only the minute three hours back is past two hours")

		deleted, err = db.PruneTraffic(start.Add(48*time.Hour), retention)
		require.NoError(t, err)
		assert.Equal(t, int64(7), deleted, "a minute, three destinations and three names")
	})
}

func TestTrafficRowsFollowTheNode(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)

		_, err := db.ApplyTrafficBatch(f.batch("a", 1))
		require.NoError(t, err)

		node, err := GetNodeByID(db, f.laptop)
		require.NoError(t, err)
		require.NoError(t, DeleteNode(db, node))

		sum, err := db.TrafficSum(f.filter(types.TrafficHour))
		require.NoError(t, err)
		assert.Equal(t, uint64(55), sum.Bytes(), "deleting a node deletes its traffic")

		require.NoError(t, db.DeleteTrafficReporter(f.gateway))
		require.ErrorIs(t, db.DeleteTrafficReporter(f.gateway), ErrTrafficReporterNotFound)

		sum, err = db.TrafficSum(f.filter(types.TrafficHour))
		require.NoError(t, err)
		assert.Equal(t, uint64(55), sum.Bytes(), "forgetting a gateway keeps what it reported")

		gateway, err := GetNodeByID(db, f.gateway)
		require.NoError(t, err)
		require.NoError(t, DeleteNode(db, gateway))

		sum, err = db.TrafficSum(f.filter(types.TrafficHour))
		require.NoError(t, err)
		assert.Zero(t, sum.Bytes(), "deleting the gateway deletes what it reported")

		names, err := db.TrafficNames(f.filter(types.TrafficHour), types.TrafficByName)
		require.NoError(t, err)
		assert.Empty(t, names)
	})
}

func TestTrafficResolverApproval(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)

		require.ErrorIs(t, db.SetTrafficResolverApproval(f.gateway, nil), ErrTrafficReporterNotFound)

		_, err := db.ApplyTrafficBatch(f.batch("a", 1))
		require.NoError(t, err)

		at := time.Unix(f.hour, 0).UTC()
		require.NoError(t, db.SetTrafficResolverApproval(f.gateway, &at))

		_, err = db.ApplyTrafficBatch(f.batch("a", 2))
		require.NoError(t, err)

		reporters, err := db.ListTrafficReporters()
		require.NoError(t, err)
		require.Len(t, reporters, 1)
		assert.Equal(t, at, reporters[0].ResolverApprovedAt.UTC(), "a report keeps the approval")

		require.NoError(t, db.SetTrafficResolverApproval(f.gateway, nil))

		reporters, err = db.ListTrafficReporters()
		require.NoError(t, err)
		assert.True(t, reporters[0].ResolverApprovedAt.IsZero())
	})
}

func TestTrafficSettings(t *testing.T) {
	t.Parallel()

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		settings, err := db.LoadTrafficSettings()
		require.NoError(t, err)
		assert.Equal(t, types.DefaultTrafficSettings(), settings)

		settings.DNSLogging = true
		settings.Retention.HourDays = 7
		require.NoError(t, db.SaveTrafficSettings(settings))

		loaded, err := db.LoadTrafficSettings()
		require.NoError(t, err)
		assert.Equal(t, settings, loaded)

		marks, err := db.LoadTrafficFoldMarks()
		require.NoError(t, err)
		assert.Zero(t, marks)

		require.NoError(t, db.SaveTrafficFoldMarks(types.TrafficFoldMarks{Hour: 3600, Day: 86400}))

		marks, err = db.LoadTrafficFoldMarks()
		require.NoError(t, err)
		assert.Equal(t, types.TrafficFoldMarks{Hour: 3600, Day: 86400}, marks)

		switches, err := db.LoadSettings()
		require.NoError(t, err, "the JSON rows are not read as switches")
		assert.False(t, switches.DevicesApprovalOn)
	})
}

// TestTrafficIngestLoad applies the largest report the contract allows,
// every flow and question to a distinct destination and name, rolled up
// to the hour and the day: about 80000 rows. No transaction may hold the
// database for long, since SQLite has one writer and the map request path
// waits behind it. It measures wall time, so it is skipped with -short,
// where every package's tests share the machine.
func TestTrafficIngestLoad(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("measures transaction hold times; run it on an idle machine without -short")
	}

	const maxHold = 500 * time.Millisecond

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		f := newTrafficFixture(t, db)
		counts := types.TrafficCounts{TxBytes: 1000, RxBytes: 1000, TxPackets: 1, RxPackets: 1, Conns: 1}

		batch := func(seq uint64) types.TrafficBatch {
			b := types.TrafficBatch{Reporter: f.reporter("load", seq)}

			for _, res := range []int64{types.TrafficMinute, types.TrafficHour, types.TrafficDay} {
				bucket := f.hour - f.hour%res
				b.Totals = append(b.Totals, types.TrafficTotal{TrafficKey: f.key(res, bucket, f.laptop)})

				if res == types.TrafficMinute {
					continue
				}

				for i := range traffic.MaxFlowsPerReport {
					b.Destinations = append(b.Destinations, types.TrafficDestination{
						TrafficKey: f.key(res, bucket, f.laptop),
						Dst:        fmt.Sprintf("198.18.%d.%d", i>>8, i&0xff),
						Port:       443, Proto: 6, Host: fmt.Sprintf("host-%d.example.com", i), HostSource: "sni",
						ASN: 64500, Country: "US", TrafficCounts: counts,
					})
				}

				for i := range traffic.MaxQueriesPerReport {
					b.DNS = append(b.DNS, types.TrafficDNS{
						TrafficKey: f.key(
							res,
							bucket,
							f.laptop,
						),
						Name:    fmt.Sprintf("name-%d.example.com", i),
						Queries: 1,
					})
				}
			}

			return b
		}

		for _, run := range []struct {
			name string
			seq  uint64
		}{{"insert", 1}, {"update", 2}} {
			start := time.Now()

			res, err := db.ApplyTrafficBatch(batch(run.seq))
			require.NoError(t, err)
			require.True(t, res.Applied)

			slowest := slices.Max(res.Holds)
			t.Logf("%s: %d transactions in %s, slowest %s",
				run.name, len(res.Holds), time.Since(start).Round(time.Millisecond), slowest.Round(time.Millisecond))
			assert.Less(t, slowest, maxHold, "%s: a transaction held the database too long", run.name)
		}

		rows, err := db.TrafficDestinationRows(f.filter(types.TrafficDay))
		require.NoError(t, err)
		require.Len(t, rows, traffic.MaxFlowsPerReport)
		assert.Equal(t, 2*counts.TxBytes, rows[0].TxBytes, "the second report added to the first")

		start := time.Now()
		later := time.Unix(f.hour, 0).AddDate(2, 0, 0)

		deleted, err := db.PruneTraffic(later, types.DefaultTrafficSettings().Retention)
		require.NoError(t, err)
		t.Logf("prune: %d rows in %s", deleted, time.Since(start).Round(time.Millisecond))
		assert.Equal(t, int64(3+4*traffic.MaxFlowsPerReport), deleted)
	})
}
