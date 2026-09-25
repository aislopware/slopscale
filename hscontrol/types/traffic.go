package types

import (
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic"
)

// SettingTraffic is the settings row that holds [TrafficSettings] as JSON;
// without it the tailnet runs with [DefaultTrafficSettings].
const SettingTraffic SettingKey = "traffic"

// SettingTrafficFold is the settings row that holds [TrafficFoldMarks] as
// JSON: how far the maintenance has folded each resolution.
const SettingTrafficFold SettingKey = "traffic_fold"

// TrafficFoldMarks are, per resolution, the start of the first bucket the
// next fold looks at: every closed bucket before it has been folded. A
// report adding to an older bucket moves the mark back.
type TrafficFoldMarks struct {
	Hour int64 `json:"hour"`
	Day  int64 `json:"day"`
}

// Of returns the mark of resolution.
func (m TrafficFoldMarks) Of(resolution int64) int64 {
	if resolution == TrafficDay {
		return m.Day
	}

	return m.Hour
}

// With returns the marks with resolution's set to mark.
func (m TrafficFoldMarks) With(resolution, mark int64) TrafficFoldMarks {
	if resolution == TrafficDay {
		m.Day = mark
	} else {
		m.Hour = mark
	}

	return m
}

// The resolutions traffic is rolled up at, in seconds. Totals are kept at
// all three; destinations and DNS names only per hour and per day, since
// a minute of them is more rows than it is worth.
const (
	TrafficMinute int64 = 60
	TrafficHour   int64 = 3600
	TrafficDay    int64 = 86400
)

// Retention bounds, so a typo cannot keep a year of minutes or throw the
// history away within the hour.
const (
	maxTrafficMinuteHours = 7 * 24
	maxTrafficHourDays    = 90
	maxTrafficDayDays     = 10 * 365

	defaultTrafficMinuteHours = 48
	defaultTrafficHourDays    = 14
	defaultTrafficDayDays     = 180
)

// Traffic setting and query errors.
var (
	ErrTrafficRetentionInvalid = errors.New("traffic retention is out of range")
	ErrTrafficRangeInvalid     = errors.New("traffic range is invalid")
	ErrTrafficGroupUnknown     = errors.New("unknown traffic grouping")
)

// TrafficSettings are the operator's choices for the traffic monitor. The
// agents receive the collector switches with every report response.
type TrafficSettings struct {
	// SNI has the agents read the server name of TLS and QUIC handshakes
	// to name each destination.
	SNI bool `json:"sni"`
	// DNSLogging has the agents run a resolver and points every client at
	// the ones that answer, so the monitor sees the names each node looks
	// up, even for traffic that does not cross a gateway.
	DNSLogging bool `json:"dnsLogging"`
	// Retention is how long each resolution is kept.
	Retention TrafficRetention `json:"retention"`
}

// TrafficRetention is how long each rollup resolution is kept.
type TrafficRetention struct {
	// MinuteHours keeps the per-minute totals, in hours.
	MinuteHours int `json:"minuteHours"`
	// HourDays keeps the hourly totals, destinations and names, in days.
	HourDays int `json:"hourDays"`
	// DayDays keeps the daily ones, in days.
	DayDays int `json:"dayDays"`
}

// DefaultTrafficSettings is what a tailnet runs with before an operator
// changes anything: names from handshakes on, the DNS log off, since it
// changes every client's resolver.
func DefaultTrafficSettings() TrafficSettings {
	return TrafficSettings{
		SNI: true,
		Retention: TrafficRetention{
			MinuteHours: defaultTrafficMinuteHours,
			HourDays:    defaultTrafficHourDays,
			DayDays:     defaultTrafficDayDays,
		},
	}
}

// Validate checks the retention bounds: each resolution must be kept at
// least as long as the finer one before it, or a range would find the
// coarse rows gone while the fine ones remain.
func (s TrafficSettings) Validate() error {
	r := s.Retention

	switch {
	case r.MinuteHours < 1 || r.MinuteHours > maxTrafficMinuteHours:
		return fmt.Errorf("%w: minuteHours must be 1 to %d", ErrTrafficRetentionInvalid, maxTrafficMinuteHours)
	case r.HourDays < 1 || r.HourDays > maxTrafficHourDays:
		return fmt.Errorf("%w: hourDays must be 1 to %d", ErrTrafficRetentionInvalid, maxTrafficHourDays)
	case r.DayDays < 1 || r.DayDays > maxTrafficDayDays:
		return fmt.Errorf("%w: dayDays must be 1 to %d", ErrTrafficRetentionInvalid, maxTrafficDayDays)
	case r.MinuteHours > r.HourDays*24:
		return fmt.Errorf("%w: minutes cannot outlive hours", ErrTrafficRetentionInvalid)
	case r.HourDays > r.DayDays:
		return fmt.Errorf("%w: hours cannot outlive days", ErrTrafficRetentionInvalid)
	}

	return nil
}

// Of returns how long rows of resolution are kept.
func (r TrafficRetention) Of(resolution int64) time.Duration {
	switch resolution {
	case TrafficMinute:
		return time.Duration(r.MinuteHours) * time.Hour
	case TrafficHour:
		return time.Duration(r.HourDays) * 24 * time.Hour
	default:
		return time.Duration(r.DayDays) * 24 * time.Hour
	}
}

// TrafficReporter is a gateway that runs the agent, as its last report
// left it.
type TrafficReporter struct {
	NodeID NodeID
	// Instance and LastSeq make a resent report idempotent; see
	// [traffic.Report].
	Instance string
	LastSeq  uint64
	Version  string
	Status   traffic.Status
	// DNSListen are the addresses the agent's resolver answers on.
	DNSListen    []netip.AddrPort
	FirstSeenAt  time.Time
	LastReportAt time.Time
	// Unattributed counts flows and queries from addresses no node held
	// when they arrived, or held by a node that may not use the gateway;
	// Dropped the entries the agent discarded.
	Unattributed uint64
	Dropped      uint64
	// ResolverApprovedAt is when an operator let the tailnet's clients
	// use the gateway's resolver; zero while it may not.
	ResolverApprovedAt time.Time
}

// TrafficResolver is a gateway resolver the traffic monitor's DNS log
// points clients at: the gateway and the address it answers on.
type TrafficResolver struct {
	Node NodeID
	Addr netip.Addr
}

// IsPrivateTrafficDestination reports whether dst is an address a gateway
// reaches as a subnet router, inside a private network, rather than as an
// exit node on the internet.
func IsPrivateTrafficDestination(dst string) bool {
	addr, err := netip.ParseAddr(dst)
	if err != nil {
		return false
	}

	addr = addr.Unmap()

	return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified()
}

// TrafficCounts are the counters of a rollup row. Tx is what the node
// sent (upload), Rx what it received.
type TrafficCounts struct {
	TxBytes   uint64
	RxBytes   uint64
	TxPackets uint64
	RxPackets uint64
	Conns     uint64
}

// Add adds o to c.
func (c *TrafficCounts) Add(o TrafficCounts) {
	c.TxBytes += o.TxBytes
	c.RxBytes += o.RxBytes
	c.TxPackets += o.TxPackets
	c.RxPackets += o.RxPackets
	c.Conns += o.Conns
}

// Bytes is the volume both ways, what the monitor ranks by.
func (c *TrafficCounts) Bytes() uint64 {
	return c.TxBytes + c.RxBytes
}

// TrafficKey is one bucket of one node's traffic through one gateway.
type TrafficKey struct {
	Resolution int64
	// Bucket is the Unix time the bucket starts at.
	Bucket     int64
	NodeID     NodeID
	ReporterID NodeID
}

// TrafficTotal is a node's volume through a gateway in one bucket.
type TrafficTotal struct {
	TrafficKey
	TrafficCounts
}

// TrafficDestination is a node's volume to one destination in one bucket.
// An empty Dst is the folded remainder of the smaller destinations.
type TrafficDestination struct {
	TrafficKey
	TrafficCounts

	Dst        string
	Port       uint16
	Proto      uint8
	Host       string
	HostSource string
	// ASN and Country are the destination's network and country when the
	// ASN database knew them as the row was written; zero and empty
	// otherwise.
	ASN     uint32
	Country string
	// Private is a destination inside a private network, reached through
	// a subnet route; it has no network or country.
	Private bool
}

// TrafficDNS is the questions a node asked about one name in one bucket.
// An empty Name is the folded remainder.
type TrafficDNS struct {
	TrafficKey

	Name    string
	Queries uint64
	Failed  uint64
}

// TrafficBatch is one report, attributed and rolled up, ready to be
// written in one transaction.
type TrafficBatch struct {
	Reporter     TrafficReporter
	Totals       []TrafficTotal
	Destinations []TrafficDestination
	DNS          []TrafficDNS
}

// TrafficGroup is what a destination or DNS query groups rows by.
type TrafficGroup string

// The groupings.
const (
	// TrafficByDestination keeps address, port, protocol and host apart.
	TrafficByDestination TrafficGroup = "destination"
	// TrafficByHost groups by host name, or by address when none is known.
	TrafficByHost TrafficGroup = "host"
	// TrafficByASN groups by the destination's network.
	TrafficByASN TrafficGroup = "asn"
	// TrafficByCountry groups by the destination's country.
	TrafficByCountry TrafficGroup = "country"
	// TrafficByPort groups by protocol and port.
	TrafficByPort TrafficGroup = "port"
	// TrafficByNode groups by the node that sent the traffic or asked.
	TrafficByNode TrafficGroup = "node"
	// TrafficByReporter groups by the gateway that saw it.
	TrafficByReporter TrafficGroup = "reporter"
	// TrafficByName groups DNS questions by the name asked.
	TrafficByName TrafficGroup = "name"
)

// TrafficFilter selects the rows a traffic query reads.
type TrafficFilter struct {
	// Resolution is the rollup read; Start and End bound the buckets,
	// [Start, End).
	Resolution int64
	Start      time.Time
	End        time.Time
	// NodeID and ReporterID keep one node or one gateway when set.
	NodeID     NodeID
	ReporterID NodeID
	// Search keeps rows whose host or address (destinations) or name
	// (DNS) contains it.
	Search string
	// ASN, Country, Proto and Port keep one destination network,
	// country or service when set (Port only with Proto).
	ASN     uint32
	Country string
	Proto   uint8
	Port    uint16
	// Limit caps the groups returned, largest first.
	Limit int
}

// TrafficPoint is one bucket of a time series.
type TrafficPoint struct {
	TrafficCounts

	Bucket int64
}

// TrafficNodeSum is a node's (or a gateway's) volume over a range.
type TrafficNodeSum struct {
	TrafficCounts

	NodeID NodeID
}

// TrafficDestinationSum is a destination group's volume over a range.
// Only the fields of the grouping are set.
type TrafficDestinationSum struct {
	TrafficCounts

	Dst     string
	Port    uint16
	Proto   uint8
	Host    string
	ASN     uint32
	Country string
	// Private is set when every destination in the group is private.
	Private bool
	NodeID  NodeID
	// Nodes is how many distinct nodes the group covers.
	Nodes uint64
}

// TrafficNameSum is a DNS group's questions over a range.
type TrafficNameSum struct {
	Name    string
	NodeID  NodeID
	Queries uint64
	Failed  uint64
	Nodes   uint64
}
