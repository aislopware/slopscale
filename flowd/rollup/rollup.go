// Package rollup sums traffic and DNS questions into the per-minute
// buckets a report carries.
package rollup

import (
	"cmp"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic"
)

// MaxEntries bounds the open flow and query entries each, so a burst of
// distinct destinations cannot exhaust the gateway's memory; beyond it new
// keys are dropped and counted.
const MaxEntries = 500_000

// Bucket returns the start of the bucket t falls in, as Unix seconds.
func Bucket(t time.Time) int64 {
	s := t.Unix()

	return s - s%traffic.BucketSeconds
}

// FlowKey identifies a flow entry.
type FlowKey struct {
	Bucket     int64
	Src, Dst   netip.Addr
	Proto      uint8
	Port       uint16
	Host       string
	HostSource traffic.HostSource
}

// Counters are what a flow entry sums.
type Counters struct {
	TxBytes, RxBytes     uint64
	TxPackets, RxPackets uint64
	Conns                uint32
}

// Empty reports whether the counters hold nothing.
func (c Counters) Empty() bool {
	return c == Counters{}
}

func (c Counters) plus(o Counters) Counters {
	return Counters{
		TxBytes:   c.TxBytes + o.TxBytes,
		RxBytes:   c.RxBytes + o.RxBytes,
		TxPackets: c.TxPackets + o.TxPackets,
		RxPackets: c.RxPackets + o.RxPackets,
		Conns:     c.Conns + o.Conns,
	}
}

type queryKey struct {
	bucket int64
	src    netip.Addr
	name   string
}

type queryCounts struct {
	count, failed uint32
}

// Table holds the open entries. It is safe for concurrent use.
type Table struct {
	mu      sync.Mutex
	flows   map[FlowKey]*Counters
	queries map[queryKey]*queryCounts
	dropped uint64
}

// NewTable returns an empty table.
func NewTable() *Table {
	return &Table{
		flows:   make(map[FlowKey]*Counters),
		queries: make(map[queryKey]*queryCounts),
	}
}

// AddFlow adds counters to a flow entry.
func (t *Table) AddFlow(key FlowKey, c Counters) {
	if c.Empty() {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if cur, ok := t.flows[key]; ok {
		*cur = cur.plus(c)

		return
	}

	if len(t.flows) >= MaxEntries {
		t.dropped++

		return
	}

	t.flows[key] = &c
}

// AddQuery counts one DNS question.
func (t *Table) AddQuery(bucket int64, src netip.Addr, name string, failed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := queryKey{bucket: bucket, src: src, name: name}

	cur, ok := t.queries[key]
	if !ok {
		if len(t.queries) >= MaxEntries {
			t.dropped++

			return
		}

		cur = &queryCounts{}
		t.queries[key] = cur
	}

	cur.count++

	if failed {
		cur.failed++
	}
}

// Drain removes and returns the entries of buckets before before, oldest
// first, with the number of entries dropped since the last drain.
func (t *Table) Drain(before int64) ([]traffic.Flow, []traffic.Query, uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var flows []traffic.Flow

	for key, c := range t.flows {
		if key.Bucket >= before {
			continue
		}

		flows = append(flows, traffic.Flow{
			Bucket:     key.Bucket,
			Src:        key.Src,
			Dst:        key.Dst,
			Proto:      key.Proto,
			Port:       key.Port,
			Host:       key.Host,
			HostSource: key.HostSource,
			TxBytes:    c.TxBytes,
			RxBytes:    c.RxBytes,
			TxPackets:  c.TxPackets,
			RxPackets:  c.RxPackets,
			Conns:      c.Conns,
		})

		delete(t.flows, key)
	}

	var queries []traffic.Query

	for key, c := range t.queries {
		if key.bucket >= before {
			continue
		}

		queries = append(queries, traffic.Query{
			Bucket: key.bucket,
			Src:    key.src,
			Name:   key.name,
			Count:  c.count,
			Failed: c.failed,
		})

		delete(t.queries, key)
	}

	slices.SortFunc(flows, func(a, b traffic.Flow) int { return cmp.Compare(a.Bucket, b.Bucket) })
	slices.SortFunc(queries, func(a, b traffic.Query) int { return cmp.Compare(a.Bucket, b.Bucket) })

	dropped := t.dropped
	t.dropped = 0

	return flows, queries, dropped
}
