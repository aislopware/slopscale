// Package traffic is the wire contract between slopscale-flowd, the agent
// that runs on an exit node, subnet router or app connector, and the
// server. The agent reads the gateway's connection tracking table, so it
// sees every connection a tailnet node opens through the gateway with the
// node's tailnet address before masquerading, and reports per-minute
// rollups; the server attributes them to nodes and keeps them.
//
// The package holds only types and constants, with no dependency beyond
// the standard library, so the agent binary stays small.
package traffic

import (
	"net/netip"
	"time"
)

const (
	// ReportPath is where the agent POSTs a [Report] on the server URL.
	ReportPath = "/traffic/v1/report"

	// Audience is the identity token audience the agent asks its local
	// tailscaled for (`tailscale id-token`). The token, signed by the
	// server, is the agent's only credential: it names the gateway node,
	// so there is no secret to install or rotate.
	Audience = "slopscale-flowd"

	// BucketSeconds is the width of the rollup buckets a report carries.
	BucketSeconds = 60

	// MaxReportBytes bounds a report. The body may be sent
	// zstd-compressed (Content-Encoding: zstd); the bound applies to the
	// body after decompression, and the server refuses a larger one with
	// 413.
	MaxReportBytes = 4 << 20

	// MaxFlowsPerReport and MaxQueriesPerReport bound the entries in one
	// report; an agent with more splits them over several reports.
	MaxFlowsPerReport   = 20000
	MaxQueriesPerReport = 20000
)

// HostSource says how the agent learnt the hostname of a flow.
type HostSource string

const (
	// HostSNI is the server name of the TLS ClientHello (TCP) or the
	// QUIC Initial the node sent on the flow itself.
	HostSNI HostSource = "sni"
	// HostECH is the outer, public server name of a ClientHello that
	// uses Encrypted Client Hello; the real name is hidden, so a DNS
	// answer, when one is known, is preferred over it.
	HostECH HostSource = "ech"
	// HostDNS is a name the same node resolved through the agent's
	// resolver to the flow's destination shortly before.
	HostDNS HostSource = "dns"
	// HostDNSShared is a name another node resolved to the destination;
	// likely right for single-tenant addresses, a guess for CDNs.
	HostDNSShared HostSource = "dns-shared"
	// HostAppConnector is a domain of the gateway's app connector that
	// resolved to the destination.
	HostAppConnector HostSource = "appc"
)

// Report is one POST from an agent.
type Report struct {
	// Version is the agent's version.
	Version string `json:"version"`
	// Instance is random per agent state directory. With Seq it makes a
	// resent report idempotent: the server acknowledges a Seq it has
	// already applied for the Instance without applying it again.
	Instance string `json:"instance"`
	// Seq increases by one per report of an Instance.
	Seq uint64 `json:"seq"`
	// SentAt is the agent's clock when the report was built.
	SentAt time.Time `json:"sentAt"`

	// Status is the state of each collector on the gateway.
	Status Status `json:"status"`
	// DNSListen are the addresses the agent's resolver answers on; empty
	// while the resolver is off or failed to start.
	DNSListen []netip.AddrPort `json:"dnsListen,omitempty"`

	// Flows are per-minute rollups of traffic tailnet nodes sent through
	// the gateway to destinations outside the tailnet.
	Flows []Flow `json:"flows,omitempty"`
	// Queries are per-minute rollups of the DNS questions tailnet nodes
	// asked the agent's resolver.
	Queries []Query `json:"queries,omitempty"`
	// Dropped counts rollup entries the agent discarded since the last
	// report it delivered, because its spool was full.
	Dropped uint64 `json:"dropped,omitempty"`
}

// Status is the health of the agent's collectors.
type Status struct {
	Conntrack Collector `json:"conntrack"`
	SNI       Collector `json:"sni"`
	DNS       Collector `json:"dns"`
	// AppConnector is whether the gateway's app connector domain map
	// could be read; disabled when the node is no app connector.
	AppConnector Collector `json:"appConnector"`
}

// Collector is the state of one collector.
type Collector struct {
	// Enabled is whether the collector is meant to run.
	Enabled bool `json:"enabled"`
	// Error is why an enabled collector is not working; empty when it is.
	Error string `json:"error,omitempty"`
}

// Flow is the traffic between one tailnet node and one destination in one
// bucket, seen by the gateway.
type Flow struct {
	// Bucket is the Unix time of the bucket start, a multiple of
	// [BucketSeconds].
	Bucket int64 `json:"t"`
	// Src is the node's tailnet address.
	Src netip.Addr `json:"src"`
	// Dst is the destination address, outside the tailnet ranges.
	Dst netip.Addr `json:"dst"`
	// Proto is the IP protocol number (6 TCP, 17 UDP, 1 ICMP, 58 ICMPv6).
	Proto uint8 `json:"proto"`
	// Port is the destination port for TCP, UDP and SCTP, else zero.
	Port uint16 `json:"port,omitempty"`
	// Host is the destination's hostname when the agent knows it.
	Host string `json:"host,omitempty"`
	// HostSource says how Host was learnt; empty when Host is.
	HostSource HostSource `json:"hostSource,omitempty"`
	// TxBytes and TxPackets were sent by the node (upload); RxBytes and
	// RxPackets were sent to it (download). Counts are of IP packets as
	// the gateway forwarded them.
	TxBytes   uint64 `json:"txBytes"`
	RxBytes   uint64 `json:"rxBytes"`
	TxPackets uint64 `json:"txPkts"`
	RxPackets uint64 `json:"rxPkts"`
	// Conns counts connections the node opened in the bucket.
	Conns uint32 `json:"conns,omitempty"`
}

// Query is the DNS questions one node asked about one name in one bucket.
type Query struct {
	// Bucket is the Unix time of the bucket start.
	Bucket int64 `json:"t"`
	// Src is the node's tailnet address.
	Src netip.Addr `json:"src"`
	// Name is the question name, lower case, without the trailing dot.
	Name string `json:"name"`
	// Count is the number of questions, of any type.
	Count uint32 `json:"count"`
	// Failed counts the questions answered with an error (NXDOMAIN,
	// SERVFAIL, REFUSED) or not answered at all.
	Failed uint32 `json:"failed,omitempty"`
}

// Response is the server's answer to a report it accepted.
type Response struct {
	// Seq is the report sequence the server has applied up to for the
	// Instance; the agent drops spooled reports up to it.
	Seq uint64 `json:"seq"`
	// Config is how the server wants the agent to run from now on.
	Config Config `json:"config"`
}

// Config is the agent configuration the server hands out with every
// response, so the operator changes it in one place.
type Config struct {
	// SNI turns hostname capture from TLS and QUIC handshakes on.
	SNI bool `json:"sni"`
	// DNS turns the agent's resolver on. The server points a node at it
	// only while the node uses this gateway as its exit node, and only
	// once the agent reports it answering.
	DNS bool `json:"dns"`
	// Upstreams are the resolvers the agent forwards to, as IP, IP:port
	// or https:// (DoH) addresses; empty means the gateway's own
	// resolver configuration, which is what the gateway would use for
	// its exit node users without the monitor.
	Upstreams []string `json:"upstreams,omitempty"`
	// LogSources are the tailnet addresses of the nodes using this
	// gateway as their exit node right now. The agent records the DNS
	// questions of these nodes only, and names flows from their answers
	// only; it answers any other asker without recording anything.
	LogSources []netip.Addr `json:"logSources,omitempty"`
	// ReportInterval is how often the agent reports, in seconds.
	ReportInterval int `json:"reportInterval"`
}
