// Package conntrack turns the gateway's connection tracking table into
// per-connection byte and packet deltas. Tailscale masquerades what it
// forwards, but the tracking entry's original tuple keeps the node's
// tailnet address and the real destination.
//
// The kernel reports a connection's counters only when the connection
// ends, so long-lived connections are read from periodic dumps; events
// report connections that begin and end between two dumps.
package conntrack

import (
	"github.com/aislopware/slopscale/flowd/names"
	"github.com/aislopware/slopscale/flowd/rollup"
)

// missedDumpsBeforeForget is how many dumps may miss a connection before
// it is forgotten. Its end event may still be queued behind the dump, and
// forgetting it first would count its bytes twice.
const missedDumpsBeforeForget = 2

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

// Tracker remembers each connection's last counters. It is not safe for
// concurrent use; the collector drives it from one goroutine.
type Tracker struct {
	keep    func(names.Conn) bool
	entries map[entryKey]*entry
	primed  bool
}

// NewTracker returns a tracker that follows the connections keep accepts.
func NewTracker(keep func(names.Conn) bool) *Tracker {
	return &Tracker{keep: keep, entries: make(map[entryKey]*entry)}
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
		// Before the first dump every connection predates the agent and
		// its counters hold traffic from before; after it, an unknown
		// connection is one whose start event was lost.
		if !t.primed {
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
	for _, e := range t.entries {
		e.missed++
	}

	var deltas []Delta

	for _, f := range flows {
		if !t.keep(f.Conn) {
			continue
		}

		key := entryKey{id: f.ID, conn: f.Conn}

		e, ok := t.entries[key]
		if !ok {
			e = &entry{pendingConn: t.primed}
			if !t.primed {
				e.last = f
			}

			t.entries[key] = e
		}

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
			delete(t.entries, key)
		}
	}

	t.primed = true

	return deltas
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
