package conntrack

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"time"

	"github.com/aislopware/slopscale/flowd/names"
	"github.com/aislopware/slopscale/flowd/tailnet"
	"github.com/mdlayher/netlink"
	ct "github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
)

const (
	sysctlAccounting = "/proc/sys/net/netfilter/nf_conntrack_acct"
	sysctlTimestamp  = "/proc/sys/net/netfilter/nf_conntrack_timestamp"
	eventBuffer      = 8 << 20
	// eventQueue holds events while a dump is being processed, so a busy
	// gateway's short connections are not lost to an overrun meanwhile.
	eventQueue = 1 << 16
)

// Collector reads the kernel's connection tracking table.
type Collector struct {
	// Interval is how often the whole table is dumped.
	Interval time.Duration
	// Sink receives every delta, from the collector's goroutine.
	Sink func(Delta)
	Log  *slog.Logger
}

// EnableAccounting turns on per-connection counters and creation
// timestamps, and reports whether the kernel has timestamps. Only
// connections created afterwards carry either.
func EnableAccounting() (bool, error) {
	err := os.WriteFile(sysctlAccounting, []byte("1"), 0o600)
	if err != nil {
		return false, fmt.Errorf("enabling connection tracking accounting (is nf_conntrack loaded?): %w", err)
	}

	// Timestamps tell a connection that began before the agent from one
	// whose start event was lost; a kernel without them costs precision
	// after a restart, not the counts.
	err = os.WriteFile(sysctlTimestamp, []byte("1"), 0o600)

	return err == nil, nil
}

// Run follows the table until ctx ends or reading it fails. When ctx ends
// it dumps the table once more, so traffic on connections still open since
// the last dump reaches the sink before Run returns.
func (c *Collector) Run(ctx context.Context) error {
	stamped, err := EnableAccounting()
	if err != nil {
		return err
	}

	dump, err := ct.Dial(nil)
	if err != nil {
		return fmt.Errorf("opening a conntrack socket: %w", err)
	}
	defer dump.Close()

	tracker := NewTracker(keepFunc())

	// Following starts before the event socket opens: every connection
	// created from here on has a start event or a stamp after this.
	if stamped {
		tracker.UseTimestamps(time.Now())
	}

	var events eventSource

	err = events.open()
	if err != nil {
		return err
	}
	defer events.close()

	err = c.dump(dump, tracker)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return c.dump(dump, tracker)
		case ev := <-events.events:
			c.event(tracker, ev)
		case listenErr := <-events.errs:
			// The event socket died (a buffer overrun on an old kernel):
			// listen again and let the dumps reconcile.
			c.Log.Warn("conntrack event socket failed; reopening", "err", listenErr)
			events.close()

			err = events.open()
			if err != nil {
				return err
			}
		case <-ticker.C:
			err = c.dump(dump, tracker)
			if err != nil {
				return err
			}
		}
	}
}

// eventSource is the subscription to connection start and end events.
type eventSource struct {
	conn   *ct.Conn
	events chan ct.Event
	errs   chan error
}

func (e *eventSource) open() error {
	conn, err := ct.Dial(nil)
	if err != nil {
		return fmt.Errorf("opening a conntrack event socket: %w", err)
	}

	// Missing events are reconciled by the next dump; without this the
	// kernel reports an overrun as an error that ends the listener.
	_ = conn.SetOption(netlink.NoENOBUFS, true)
	_ = conn.SetReadBuffer(eventBuffer)

	events := make(chan ct.Event, eventQueue)

	errs, err := conn.Listen(events, 1, []netfilter.NetlinkGroup{netfilter.GroupCTNew, netfilter.GroupCTDestroy})
	if err != nil {
		_ = conn.Close()

		return fmt.Errorf("subscribing to conntrack events: %w", err)
	}

	e.conn, e.events, e.errs = conn, events, errs

	return nil
}

func (e *eventSource) close() {
	if e.conn != nil {
		_ = e.conn.Close()
		e.conn = nil
	}
}

func (c *Collector) event(tracker *Tracker, ev ct.Event) {
	if ev.Flow == nil {
		return
	}

	f, ok := fromKernel(ev.Flow)
	if !ok {
		return
	}

	switch ev.Type {
	case ct.EventNew:
		tracker.New(f)
	case ct.EventDestroy:
		if d, ok := tracker.Destroy(f); ok {
			c.Sink(d)
		}
	case ct.EventUnknown, ct.EventUpdate, ct.EventExpNew, ct.EventExpDestroy:
	}
}

// dump reads the table one address family at a time, keeping only the
// connections the tracker follows: the kernel's entries are large, and on
// a router whose own NAT fills the table most of them are not tailnet
// traffic. The kernel cannot filter by address prefix, so the whole table
// still crosses the socket each dump; see the package documentation.
func (c *Collector) dump(conn *ct.Conn, tracker *Tracker) error {
	var kept []Flow

	for _, family := range []netfilter.ProtoFamily{netfilter.ProtoIPv4, netfilter.ProtoIPv6} {
		flows, err := conn.DumpFilter(ct.NewFilter().Family(family), nil)
		if err != nil {
			// Filtered dumps need Linux 4.20; older kernels read it all.
			flows, err = conn.Dump(nil)
			if err != nil {
				return fmt.Errorf("dumping the conntrack table: %w", err)
			}

			kept = keepFrom(kept, flows, tracker)

			break
		}

		kept = keepFrom(kept, flows, tracker)
	}

	for _, d := range tracker.Dump(kept) {
		c.Sink(d)
	}

	return nil
}

func keepFrom(kept []Flow, flows []ct.Flow, tracker *Tracker) []Flow {
	for i := range flows {
		if f, ok := fromKernel(&flows[i]); ok && tracker.keep(f.Conn) {
			kept = append(kept, f)
		}
	}

	return kept
}

func fromKernel(f *ct.Flow) (Flow, bool) {
	orig := f.TupleOrig
	if !orig.IP.SourceAddress.IsValid() || !orig.IP.DestinationAddress.IsValid() {
		return Flow{}, false
	}

	out := Flow{
		ID: f.ID,
		Conn: names.Conn{
			Src:   netip.AddrPortFrom(orig.IP.SourceAddress.Unmap(), orig.Proto.SourcePort),
			Dst:   netip.AddrPortFrom(orig.IP.DestinationAddress.Unmap(), orig.Proto.DestinationPort),
			Proto: orig.Proto.Protocol,
		},
		TxBytes:   f.CountersOrig.Bytes,
		TxPackets: f.CountersOrig.Packets,
		RxBytes:   f.CountersReply.Bytes,
		RxPackets: f.CountersReply.Packets,
		Start:     f.Timestamp.Start,
	}

	return out, true
}

// keepFunc keeps connections a tailnet node opened to an address outside
// the tailnet that is not the gateway itself.
func keepFunc() func(names.Conn) bool {
	local := localAddrs()
	refreshed := time.Now()

	return func(c names.Conn) bool {
		if !tailnet.Egress(c.Src.Addr(), c.Dst.Addr()) {
			return false
		}

		if time.Since(refreshed) > time.Minute {
			local, refreshed = localAddrs(), time.Now()
		}

		_, isLocal := local[c.Dst.Addr()]

		return !isLocal
	}
}

func localAddrs() map[netip.Addr]struct{} {
	out := make(map[netip.Addr]struct{})

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}

	for _, a := range addrs {
		p, parseErr := netip.ParsePrefix(a.String())
		if parseErr == nil {
			out[p.Addr().Unmap()] = struct{}{}
		}
	}

	return out
}
