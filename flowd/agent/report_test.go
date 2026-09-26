package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/flowd/rollup"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bigRollup is the reviewer's worst case: 50000 flows to IPv6 destinations
// with hostnames and 50000 questions, 18 MB of JSON in one report before.
func bigRollup() ([]traffic.Flow, []traffic.Query) {
	flows := make([]traffic.Flow, 0, 50000)
	queries := make([]traffic.Query, 0, 50000)

	for i := range 50000 {
		dst := netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 15: byte(i), 14: byte(i >> 8), 13: byte(i >> 16)})
		flows = append(flows, traffic.Flow{
			Bucket: 1_800_000_000, Src: netip.MustParseAddr("fd7a:115c:a1e0::1234:5678"), Dst: dst,
			Proto: 6, Port: 443, Host: fmt.Sprintf("host-%05d.cdn.example-provider.net", i),
			HostSource: traffic.HostSNI, TxBytes: math.MaxUint32, RxBytes: math.MaxUint32,
			TxPackets: math.MaxUint32, RxPackets: math.MaxUint32, Conns: 1,
		})
		queries = append(queries, traffic.Query{
			Bucket: 1_800_000_000, Src: netip.MustParseAddr("fd7a:115c:a1e0::1234:5678"),
			Name: fmt.Sprintf("name-%05d.tracking.example-provider.net", i), Count: 3, Failed: 1,
		})
	}

	return flows, queries
}

// TestSplitKeepsEveryReportUnderTheLimit: no report goes over the server's
// size or entry limits, stamped at its widest, and no entry goes missing.
func TestSplitKeepsEveryReportUnderTheLimit(t *testing.T) {
	flows, queries := bigRollup()

	base := traffic.Report{
		Version: "test", SentAt: time.Now().UTC(),
		Status: traffic.Status{Conntrack: traffic.Collector{Enabled: true, Error: strings.Repeat("e", 2000)}},
	}

	reports := splitReport(base, flows, queries)
	require.Greater(t, len(reports), 4)

	var gotFlows, gotQueries int

	for _, r := range reports {
		r.Instance, r.Seq, r.Dropped = strings.Repeat("f", 32), math.MaxUint64, math.MaxUint64

		raw, err := json.Marshal(r)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(raw), traffic.MaxReportBytes)
		assert.LessOrEqual(t, len(r.Flows), traffic.MaxFlowsPerReport)
		assert.LessOrEqual(t, len(r.Queries), traffic.MaxQueriesPerReport)

		gotFlows += len(r.Flows)
		gotQueries += len(r.Queries)
	}

	assert.Equal(t, len(flows), gotFlows)
	assert.Equal(t, len(queries), gotQueries)

	assert.Len(t, splitReport(base, nil, nil), 1, "a heartbeat is one empty report")
}

// TestLargeRollupReachesTheServer delivers the worst case through the
// spool to a server that enforces the limits; nothing is refused.
func TestLargeRollupReachesTheServer(t *testing.T) {
	srv := &reportServer{config: traffic.Config{SNI: true, ReportInterval: 60}}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	a, err := newAgent(t.Context(), Options{
		Local:      &fakeLocal{},
		Server:     ts.URL,
		StateDir:   t.TempDir(),
		SpoolBytes: 256 << 20,
	})
	require.NoError(t, err)

	flows, queries := bigRollup()
	for _, f := range flows {
		a.table.AddFlow(rollup.FlowKey{
			Bucket:     f.Bucket,
			Src:        f.Src,
			Dst:        f.Dst,
			Proto:      f.Proto,
			Port:       f.Port,
			Host:       f.Host,
			HostSource: f.HostSource,
		}, rollup.Counters{TxBytes: f.TxBytes, RxBytes: f.RxBytes, TxPackets: f.TxPackets, RxPackets: f.RxPackets})
	}

	for _, q := range queries {
		a.table.AddQuery(q.Bucket, q.Src, q.Name, false)
	}

	a.report(math.MaxInt64)

	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)

	var gotFlows, gotQueries int

	for _, r := range srv.reports {
		gotFlows += len(r.Flows)
		gotQueries += len(r.Queries)
	}

	assert.Equal(t, len(flows), gotFlows)
	assert.Equal(t, len(queries), gotQueries)
	assert.Zero(t, a.spool.Len())
}

// TestStatusCarriesGatewayNotes: a userspace tailscaled, a clock far off
// the server's and a refused report all reach the server in the next
// report's status, and buckets follow the server's clock.
func TestStatusCarriesGatewayNotes(t *testing.T) {
	var (
		refuse atomic.Bool
		ahead  atomic.Int64
	)

	srv := &reportServer{config: traffic.Config{SNI: true, ReportInterval: 60}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().Add(time.Duration(ahead.Load())).UTC().Format(http.TimeFormat))

		if refuse.CompareAndSwap(true, false) {
			http.Error(w, "bucket 1800000600 is in the future", http.StatusBadRequest)

			return
		}

		srv.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	a, err := newAgent(t.Context(), Options{
		Local:      &fakeLocal{},
		Server:     ts.URL,
		StateDir:   t.TempDir(),
		SpoolBytes: 1 << 20,
		Logger:     slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)

	// A healthy gateway with a good clock reports no error.
	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)
	assert.Empty(t, srv.last().Status.Conntrack.Error)

	// The server's clock is ten minutes ahead, it refuses a report, and
	// tailscaled turns out to run in userspace.
	ahead.Store(int64(10 * time.Minute))
	refuse.Store(true)
	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)

	a.mu.Lock()
	a.userspace = true
	a.mu.Unlock()

	assert.Equal(t, rollup.Bucket(time.Now().Add(10*time.Minute)), a.bucket(), "buckets follow the server's clock")

	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)

	got := srv.last().Status.Conntrack.Error
	assert.Contains(t, got, "userspace-networking")
	assert.Contains(t, got, "clock is 10m0s off")
	assert.Contains(t, got, "refused report 2 (400 Bad Request): bucket 1800000600 is in the future")

	// Delivered once, the refusal is not repeated.
	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)
	assert.NotContains(t, srv.last().Status.Conntrack.Error, "refused")
}
