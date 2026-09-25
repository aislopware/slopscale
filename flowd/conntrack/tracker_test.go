package conntrack

import (
	"net/netip"
	"testing"

	"github.com/aislopware/slopscale/flowd/names"
	"github.com/aislopware/slopscale/flowd/rollup"
	"github.com/aislopware/slopscale/flowd/tailnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func keepEgress(c names.Conn) bool { return tailnet.Egress(c.Src.Addr(), c.Dst.Addr()) }

func conn(src, dst string) names.Conn {
	return names.Conn{Src: netip.MustParseAddrPort(src), Dst: netip.MustParseAddrPort(dst), Proto: 6}
}

func flow(id uint32, c names.Conn, tx, rx uint64) Flow {
	return Flow{ID: id, Conn: c, TxBytes: tx, TxPackets: tx / 100, RxBytes: rx, RxPackets: rx / 100}
}

func sum(deltas []Delta) rollup.Counters {
	var total rollup.Counters

	for _, d := range deltas {
		total.TxBytes += d.Counters.TxBytes
		total.RxBytes += d.Counters.RxBytes
		total.TxPackets += d.Counters.TxPackets
		total.RxPackets += d.Counters.RxPackets
		total.Conns += d.Counters.Conns
	}

	return total
}

// TestBaselineThenDeltas: connections open before the agent contribute
// only what they carry afterwards, and a long-lived one is counted from
// successive dumps.
func TestBaselineThenDeltas(t *testing.T) {
	tr := NewTracker(keepEgress)
	old := conn("100.64.0.3:40000", "203.0.113.5:443")

	assert.Empty(t, tr.Dump([]Flow{flow(1, old, 5000, 90000)}))

	deltas := tr.Dump([]Flow{flow(1, old, 6000, 100000)})
	require.Len(t, deltas, 1)
	assert.Equal(t, rollup.Counters{TxBytes: 1000, TxPackets: 10, RxBytes: 10000, RxPackets: 100}, deltas[0].Counters,
		"a connection older than the agent is never counted as opened")

	d, ok := tr.Destroy(flow(1, old, 6500, 100000))
	require.True(t, ok)
	assert.Equal(t, uint64(500), d.Counters.TxBytes)
	assert.Zero(t, tr.Len())

	// Its end event before any dump would have been ignored entirely.
	fresh := NewTracker(keepEgress)
	_, ok = fresh.Destroy(flow(9, old, 1, 1))
	assert.False(t, ok)
}

// TestShortConnectionBetweenDumps counts a connection that starts and ends
// between two dumps once, with its full counters.
func TestShortConnectionBetweenDumps(t *testing.T) {
	tr := NewTracker(keepEgress)
	tr.Dump(nil)

	c := conn("100.64.0.3:40001", "198.51.100.1:443")
	tr.New(flow(2, c, 0, 0))

	d, ok := tr.Destroy(flow(2, c, 1200, 64000))
	require.True(t, ok)
	assert.Equal(t, rollup.Counters{TxBytes: 1200, TxPackets: 12, RxBytes: 64000, RxPackets: 640, Conns: 1}, d.Counters)

	// A lost start event changes nothing once the agent is primed.
	lost := conn("100.64.0.3:40002", "198.51.100.1:443")
	d, ok = tr.Destroy(flow(3, lost, 300, 400))
	require.True(t, ok)
	assert.Equal(t, uint32(1), d.Counters.Conns)
	assert.Equal(t, uint64(300), d.Counters.TxBytes)
}

// TestConnCountWaitsForTraffic holds the connection count until bytes flow,
// and counts it exactly once whether the dump or the end event sees it.
func TestConnCountWaitsForTraffic(t *testing.T) {
	tr := NewTracker(keepEgress)
	tr.Dump(nil)

	c := conn("100.64.0.3:40003", "198.51.100.1:443")
	tr.New(flow(4, c, 0, 0))

	// Only the handshake's SYN so far: no counted bytes on the dump.
	deltas := tr.Dump([]Flow{flow(4, c, 0, 0)})
	assert.Empty(t, deltas)

	deltas = tr.Dump([]Flow{flow(4, c, 700, 3000)})
	require.Len(t, deltas, 1)
	assert.Equal(t, uint32(1), deltas[0].Counters.Conns)

	d, ok := tr.Destroy(flow(4, c, 800, 3000))
	require.True(t, ok)
	assert.Zero(t, d.Counters.Conns)
	assert.Equal(t, uint64(100), d.Counters.TxBytes)
}

// TestNewSeenFirstByDump: a connection that shows up in a dump before its
// start event is processed is counted from zero and opened once.
func TestNewSeenFirstByDump(t *testing.T) {
	tr := NewTracker(keepEgress)
	tr.Dump(nil)

	c := conn("100.64.0.3:40004", "198.51.100.1:443")

	deltas := tr.Dump([]Flow{flow(5, c, 100, 200)})
	require.Len(t, deltas, 1)
	assert.Equal(
		t,
		rollup.Counters{TxBytes: 100, TxPackets: 1, RxBytes: 200, RxPackets: 2, Conns: 1},
		deltas[0].Counters,
	)

	tr.New(flow(5, c, 0, 0)) // late start event
	d, ok := tr.Destroy(flow(5, c, 150, 200))
	require.True(t, ok)
	assert.Equal(t, rollup.Counters{TxBytes: 50}, d.Counters)
}

// TestLateDestroyAfterMissedDump: a connection missing from one dump whose
// end event arrives after it is not counted twice.
func TestLateDestroyAfterMissedDump(t *testing.T) {
	tr := NewTracker(keepEgress)
	tr.Dump(nil)

	c := conn("100.64.0.3:40005", "198.51.100.1:443")
	tr.New(flow(6, c, 0, 0))
	sum1 := sum(tr.Dump([]Flow{flow(6, c, 1000, 1000)}))

	assert.Empty(t, tr.Dump(nil), "gone from the table, end event still queued")

	d, ok := tr.Destroy(flow(6, c, 1500, 1200))
	require.True(t, ok)
	assert.Equal(t, uint64(1500), sum1.TxBytes+d.Counters.TxBytes)
	assert.Equal(t, uint64(1200), sum1.RxBytes+d.Counters.RxBytes)

	// A connection whose end event never comes is forgotten after two
	// dumps.
	gone := conn("100.64.0.3:40006", "198.51.100.1:443")
	tr.Dump([]Flow{flow(7, gone, 10, 10)})
	tr.Dump(nil)
	assert.Equal(t, 1, tr.Len())
	tr.Dump(nil)
	assert.Zero(t, tr.Len())
}

func TestFiltersAndIDReuse(t *testing.T) {
	tr := NewTracker(keepEgress)
	tr.Dump(nil)

	intra := conn("100.64.0.3:40007", "100.64.0.4:22")
	fromLAN := conn("192.168.1.5:40008", "203.0.113.5:443")

	tr.New(flow(8, intra, 0, 0))
	tr.New(flow(9, fromLAN, 0, 0))
	assert.Zero(t, tr.Len())

	_, ok := tr.Destroy(flow(8, intra, 10, 10))
	assert.False(t, ok)

	// The kernel reuses id 10 for another connection: separate entries.
	a := conn("100.64.0.3:40009", "203.0.113.5:443")
	b := conn("100.64.0.5:40010", "203.0.113.6:443")

	deltas := tr.Dump([]Flow{flow(10, a, 100, 100), flow(10, b, 200, 200)})
	assert.Equal(t, uint64(300), sum(deltas).TxBytes)

	// Counters zeroed by another reader restart from zero.
	deltas = tr.Dump([]Flow{flow(10, a, 40, 40), flow(10, b, 250, 250)})
	assert.Equal(t, uint64(40+50), sum(deltas).TxBytes)
}
