// Package conntrack turns the gateway's connection tracking table into
// per-connection byte and packet deltas. Tailscale masquerades what it
// forwards, but the tracking entry's original tuple keeps the node's
// tailnet address and the real destination.
//
// The kernel reports a connection's counters only when the connection
// ends, so long-lived connections are read from periodic dumps; events
// report connections that begin and end between two dumps.
//
// A dump carries the whole table, including the gateway's own NAT
// traffic: the kernel filters a dump by family, mark, status or zone, none
// of which marks tailnet traffic, and not by address prefix. The table is
// read one family at a time and only tailnet connections are kept, so the
// cost is one pass over the table per dump interval (30 s).
package conntrack

import (
	"time"

	"github.com/aislopware/slopscale/flowd/names"
	"github.com/aislopware/slopscale/flowd/rollup"
)

const (
	// missedDumpsBeforeForget is how many dumps may miss a connection
	// before it is forgotten. Its end event may still be queued behind the
	// dump, and forgetting it first would count its bytes twice.
	missedDumpsBeforeForget = 2
	// forgottenTTL is how long a forgotten connection's counters are kept,
	// in case it shows up again (a dump cut short) or its end event
	// arrives late, so it is not counted from zero a second time.
	forgottenTTL = 10 * time.Minute
	// maxForgotten bounds the forgotten connections kept.
	maxForgotten = 1 << 16
)

// Flow is one tracking entry as read from the kernel.
type Flow struct {
	// ID is the kernel's id for the entry; ids are reused, so an entry is
	// identified by the id and the tuple together.
	ID   uint32
	Conn names.Conn
	// Tx counters are the original direction (the node sending), Rx the
	// reply direction.
	TxBytes, TxPackets uint64
	RxBytes, RxPackets uint64
	// Start is when the kernel created the entry, zero when it has no
	// timestamp (created before timestamps were turned on).
	Start time.Time
}

// Delta is traffic a connection carried since it was last read.
type Delta struct {
	Conn     names.Conn
	Counters rollup.Counters
}

type entryKey struct {
	id   uint32
	conn names.Conn
}

type entry struct {
	last        Flow
	pendingConn bool
	missed      int
}

type forgotten struct {
	entry   *entry
	expires time.Time
}

// Tracker remembers each connection's last counters. It is not safe for
// concurrent use; the collector drives it from one goroutine.
type Tracker struct {
	keep      func(names.Conn) bool
	entries   map[entryKey]*entry
	forgotten map[entryKey]forgotten
	primed    bool

	// since is when the tracker started following, and stamped whether
	// the kernel stamps entries with their creation time. With stamps, a
	// connection seen for the first time is new only if it began after
	// since; one that began before carries traffic an earlier agent (or
	// nobody) counted, and only its growth from here on is counted.
	since   time.Time
	stamped bool
	now     func() time.Time
}

// NewTracker returns a tracker that follows the connections keep accepts.
func NewTracker(keep func(names.Conn) bool) *Tracker {
	return &Tracker{
		keep:      keep,
		entries:   make(map[entryKey]*entry),
		forgotten: make(map[entryKey]forgotten),
		now:       time.Now,
	}
}

// UseTimestamps tells the tracker the kernel stamps entries with their
// creation time and that it follows connections from since.
func (t *Tracker) UseTimestamps(since time.Time) {
	t.since, t.stamped = since, true
}

// Len is the number of connections followed.
func (t *Tracker) Len() int {
	return len(t.entries)
}

// New records a connection that just began. Its connection count is held
// back until it carries traffic, so the count lands on the same hostname
// as the bytes once the handshake has named it.
func (t *Tracker) New(f Flow) {
	if !t.keep(f.Conn) {
		return
	}

	key := entryKey{id: f.ID, conn: f.Conn}
	if _, ok := t.entries[key]; !ok {
		t.entries[key] = &entry{pendingConn: true}
	}
}

// Destroy returns the traffic of a connection that ended since it was last
// read.
func (t *Tracker) Destroy(f Flow) (Delta, bool) {
	if !t.keep(f.Conn) {
		return Delta{}, false
	}

	key := entryKey{id: f.ID, conn: f.Conn}

	e, ok := t.entries[key]
	if !ok {
		e, ok = t.recall(key)
	}

	if !ok {
		// A connection that began before the tracker holds traffic from
		// before, counted by an earlier agent up to its last dump or by
		// nobody; one that began since is one whose start event was lost.
		if t.predates(f) {
			return Delta{}, false
		}

		e = &entry{pendingConn: true}
	}

	delete(t.entries, key)

	d := e.delta(f)
	d.Counters.Conns = boolCount(e.pendingConn)

	return d, !d.Counters.Empty()
}

// Dump reads a full table dump and returns the traffic every connection
// carried since it was last read. The first dump only learns the counters
// of connections that predate the agent.
func (t *Tracker) Dump(flows []Flow) []Delta {
	now := t.now()

	for _, e := range t.entries {
		e.missed++
	}

	for key, f := range t.forgotten {
		if now.After(f.expires) {
			delete(t.forgotten, key)
		}
	}

	var deltas []Delta

	for _, f := range flows {
		if !t.keep(f.Conn) {
			continue
		}

		key := entryKey{id: f.ID, conn: f.Conn}

		e, ok := t.entries[key]
		if !ok {
			e, ok = t.recall(key)
		}

		if !ok {
			// A connection that predates the tracker only counts from
			// here; one that began since, from its first byte.
			e = &entry{pendingConn: true}
			if t.predates(f) {
				e = &entry{last: f}
			}
		}

		t.entries[key] = e

		e.missed = 0

		d := e.delta(f)
		e.last = f

		if e.pendingConn && d.Counters.TxBytes > 0 {
			d.Counters.Conns = 1
			e.pendingConn = false
		}

		if !d.Counters.Empty() {
			deltas = append(deltas, d)
		}
	}

	for key, e := range t.entries {
		if e.missed >= missedDumpsBeforeForget {
			t.forget(key, e, now)
		}
	}

	t.primed = true

	return deltas
}

// predates reports whether a connection seen for the first time began
// before the tracker followed the table, so its counters hold traffic
// that is not this tracker's to count. Without timestamps only the first
// dump can tell.
func (t *Tracker) predates(f Flow) bool {
	if t.stamped {
		return f.Start.IsZero() || f.Start.Before(t.since)
	}

	return !t.primed
}

// recall takes back a forgotten connection's entry.
func (t *Tracker) recall(key entryKey) (*entry, bool) {
	f, ok := t.forgotten[key]
	if !ok {
		return nil, false
	}

	delete(t.forgotten, key)

	return f.entry, true
}

// forget moves a connection the dumps no longer show aside.
func (t *Tracker) forget(key entryKey, e *entry, now time.Time) {
	delete(t.entries, key)

	if len(t.forgotten) >= maxForgotten {
		return
	}

	e.missed = 0
	t.forgotten[key] = forgotten{entry: e, expires: now.Add(forgottenTTL)}
}

func (e *entry) delta(f Flow) Delta {
	return Delta{
		Conn: f.Conn,
		Counters: rollup.Counters{
			TxBytes:   since(f.TxBytes, e.last.TxBytes),
			TxPackets: since(f.TxPackets, e.last.TxPackets),
			RxBytes:   since(f.RxBytes, e.last.RxBytes),
			RxPackets: since(f.RxPackets, e.last.RxPackets),
		},
	}
}

// since is the growth of a counter; a counter that went down was zeroed
// by another reader, so all of it is new.
func since(cur, last uint64) uint64 {
	if cur < last {
		return cur
	}

	return cur - last
}

func boolCount(b bool) uint32 {
	if b {
		return 1
	}

	return 0
}
