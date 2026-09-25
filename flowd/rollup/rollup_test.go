package rollup

import (
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucket(t *testing.T) {
	assert.Equal(t, int64(1_800_000_000), Bucket(time.Unix(1_800_000_020, 0)))
	assert.Equal(t, int64(1_800_000_060), Bucket(time.Unix(1_800_000_099, 999)))
}

func TestDrainClosedBuckets(t *testing.T) {
	table := NewTable()
	src := netip.MustParseAddr("100.64.0.3")
	dst := netip.MustParseAddr("203.0.113.9")

	key := FlowKey{Bucket: 60, Src: src, Dst: dst, Proto: 6, Port: 443, Host: "a.example", HostSource: traffic.HostSNI}
	table.AddFlow(key, Counters{TxBytes: 100, RxBytes: 1000, TxPackets: 2, RxPackets: 3, Conns: 1})
	table.AddFlow(key, Counters{TxBytes: 50, RxBytes: 500, TxPackets: 1, RxPackets: 1})
	table.AddFlow(key, Counters{}) // nothing to add

	open := key
	open.Bucket = 120
	table.AddFlow(open, Counters{TxBytes: 7})

	table.AddQuery(60, src, "a.example", false)
	table.AddQuery(60, src, "a.example", true)
	table.AddQuery(120, src, "b.example", false)

	flows, queries, dropped := table.Drain(120)
	require.Len(t, flows, 1)
	assert.Equal(t, traffic.Flow{
		Bucket: 60, Src: src, Dst: dst, Proto: 6, Port: 443, Host: "a.example", HostSource: traffic.HostSNI,
		TxBytes: 150, RxBytes: 1500, TxPackets: 3, RxPackets: 4, Conns: 1,
	}, flows[0])
	assert.Equal(t, []traffic.Query{{Bucket: 60, Src: src, Name: "a.example", Count: 2, Failed: 1}}, queries)
	assert.Zero(t, dropped)

	flows, queries, _ = table.Drain(180)
	require.Len(t, flows, 1)
	assert.Equal(t, uint64(7), flows[0].TxBytes)
	assert.Len(t, queries, 1)

	flows, queries, _ = table.Drain(1 << 62)
	assert.Empty(t, flows)
	assert.Empty(t, queries)
}

func TestTableBound(t *testing.T) {
	table := NewTable()
	src := netip.MustParseAddr("100.64.0.3")

	for i := range MaxEntries + 10 {
		table.AddFlow(
			FlowKey{Bucket: 0, Src: src, Port: uint16(i % 65536), Proto: uint8(i / 65536)},
			Counters{TxBytes: 1},
		)
	}

	flows, _, dropped := table.Drain(60)
	assert.Len(t, flows, MaxEntries)
	assert.Equal(t, uint64(10), dropped)
}
