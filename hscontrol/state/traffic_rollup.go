package state

import (
	"cmp"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/traffic/asn"
	"github.com/aislopware/slopscale/hscontrol/types"
)

// hostSourceRank orders how trustworthy a host name is when two flows of
// one destination row disagree: the name the node itself sent beats the
// one it looked up, which beats a guess from another node's lookup.
var hostSourceOrder = []traffic.HostSource{
	traffic.HostDNSShared, traffic.HostECH, traffic.HostAppConnector, traffic.HostDNS, traffic.HostSNI,
}

// hostSourceRank is higher for a more trustworthy source, -1 for none.
func hostSourceRank(s string) int {
	return slices.Index(hostSourceOrder, traffic.HostSource(s))
}

type totalKey struct {
	resolution, bucket int64
	node               types.NodeID
}

type destinationKey struct {
	resolution, bucket int64
	node               types.NodeID
	dst                string
	port               uint16
	proto              uint8
	host               string
}

type nameKey struct {
	resolution, bucket int64
	node               types.NodeID
	name               string
}

// trafficRollup accumulates one report's rows per resolution.
type trafficRollup struct {
	reporter     types.NodeID
	retention    types.TrafficRetention
	now          time.Time
	nodes        map[netip.Addr]types.NodeID
	asns         *asn.Table
	totals       map[totalKey]types.TrafficCounts
	destinations map[destinationKey]*types.TrafficDestination
	names        map[nameKey]*types.TrafficDNS
	unattributed uint64
}

// rollUpTraffic attributes a report's flows and questions to the gateway
// and its peers holding their source addresses now and adds them up per
// minute, hour and day, leaving out the buckets the retention would
// delete anyway.
func (s *State) rollUpTraffic(
	reporter types.NodeID,
	report traffic.Report,
	retention types.TrafficRetention,
	now time.Time,
) types.TrafficBatch {
	r := &trafficRollup{
		reporter:     reporter,
		retention:    retention,
		now:          now,
		nodes:        make(map[netip.Addr]types.NodeID),
		asns:         s.asnTable.Load(),
		totals:       make(map[totalKey]types.TrafficCounts),
		destinations: make(map[destinationKey]*types.TrafficDestination),
		names:        make(map[nameKey]*types.TrafficDNS),
	}

	// Only a node that can reach the gateway can send traffic through
	// it; flows from any other address are counted as unattributed, so a
	// gateway cannot pin traffic on a node it never carried.
	for _, node := range s.nodeStore.ListPeers(reporter).All() {
		for _, ip := range node.IPs() {
			r.nodes[ip] = node.ID()
		}
	}

	if gateway, ok := s.nodeStore.GetNode(reporter); ok {
		for _, ip := range gateway.IPs() {
			r.nodes[ip] = reporter
		}
	}

	for _, f := range report.Flows {
		r.addFlow(f)
	}

	for _, q := range report.Queries {
		r.addQuery(q)
	}

	listen := slices.Clone(report.DNSListen)
	status := report.Status

	for _, c := range []*traffic.Collector{&status.Conntrack, &status.SNI, &status.DNS, &status.AppConnector} {
		c.Error = truncate(c.Error, maxTrafficStatusError)
	}

	return types.TrafficBatch{
		Reporter: types.TrafficReporter{
			NodeID:       reporter,
			Instance:     report.Instance,
			LastSeq:      report.Seq,
			Version:      report.Version,
			Status:       status,
			DNSListen:    listen,
			LastReportAt: now.UTC(),
			Unattributed: r.unattributed,
			Dropped:      report.Dropped,
		},
		Totals:       r.totalRows(),
		Destinations: r.destinationRows(),
		DNS:          r.nameRows(),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n]
}

// kept reports whether a bucket of resolution is still inside its
// retention.
func (r *trafficRollup) kept(resolution, bucket int64) bool {
	return bucket+resolution > r.now.Add(-r.retention.Of(resolution)).Unix()
}

// source is the node holding addr, counting the ones no node holds.
func (r *trafficRollup) source(addr netip.Addr) (types.NodeID, bool) {
	node, ok := r.nodes[addr.Unmap()]
	if !ok {
		r.unattributed++
	}

	return node, ok
}

func (r *trafficRollup) addFlow(f traffic.Flow) {
	if !f.Dst.IsValid() || isTailnetAddr(f.Dst) {
		return
	}

	node, ok := r.source(f.Src)
	if !ok {
		return
	}

	counts := types.TrafficCounts{
		TxBytes: f.TxBytes, RxBytes: f.RxBytes,
		TxPackets: f.TxPackets, RxPackets: f.RxPackets,
		Conns: uint64(f.Conns),
	}

	host, source := normalName(f.Host), string(f.HostSource)
	if host == "" {
		source = ""
	}

	dst := f.Dst.Unmap().String()
	private := types.IsPrivateTrafficDestination(dst)

	// A private address belongs to no public network, whatever the table
	// says about the range.
	var info asn.Info
	if !private {
		info, _ = r.asns.Lookup(f.Dst)
	}

	for _, res := range []int64{types.TrafficMinute, types.TrafficHour, types.TrafficDay} {
		bucket := f.Bucket - f.Bucket%res
		if !r.kept(res, bucket) {
			continue
		}

		tk := totalKey{resolution: res, bucket: bucket, node: node}
		total := r.totals[tk]
		total.Add(counts)
		r.totals[tk] = total

		if res == types.TrafficMinute {
			continue
		}

		dk := destinationKey{
			resolution: res, bucket: bucket, node: node,
			dst: dst, port: f.Port, proto: f.Proto, host: host,
		}

		row, ok := r.destinations[dk]
		if !ok {
			row = &types.TrafficDestination{
				Resolution: res, Bucket: bucket, NodeID: node, ReporterID: r.reporter,
				Dst:     dk.dst,
				Port:    f.Port,
				Proto:   f.Proto,
				Host:    host,
				ASN:     info.ASN,
				Country: info.Country,
				Private: private,
			}
			r.destinations[dk] = row
		}

		row.Add(counts)

		if hostSourceRank(source) > hostSourceRank(row.HostSource) {
			row.HostSource = source
		}
	}
}

func (r *trafficRollup) addQuery(q traffic.Query) {
	name := normalName(q.Name)
	if name == "" {
		return
	}

	node, ok := r.source(q.Src)
	if !ok {
		return
	}

	for _, res := range []int64{types.TrafficHour, types.TrafficDay} {
		bucket := q.Bucket - q.Bucket%res
		if !r.kept(res, bucket) {
			continue
		}

		nk := nameKey{resolution: res, bucket: bucket, node: node, name: name}

		row, ok := r.names[nk]
		if !ok {
			row = &types.TrafficDNS{
				Resolution: res, Bucket: bucket, NodeID: node, ReporterID: r.reporter,
				Name: name,
			}
			r.names[nk] = row
		}

		row.Queries += uint64(q.Count)
		row.Failed += uint64(min(q.Failed, q.Count))
	}
}

// normalName lower-cases a host or question name and drops its trailing
// dot; a name longer than DNS allows, or holding a byte no name has, is
// dropped rather than stored.
func normalName(name string) string {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if len(name) > maxTrafficName {
		return ""
	}

	for i := range len(name) {
		if c := name[i]; c <= ' ' || c == 0x7f {
			return ""
		}
	}

	return name
}

func (r *trafficRollup) totalRows() []types.TrafficTotal {
	out := make([]types.TrafficTotal, 0, len(r.totals))

	for k, counts := range r.totals {
		out = append(out, types.TrafficTotal{
			Resolution: k.resolution, Bucket: k.bucket, NodeID: k.node, ReporterID: r.reporter,
			TrafficCounts: counts,
		})
	}

	// A stable order keeps the writes of concurrent reports from
	// deadlocking on PostgreSQL row locks.
	slices.SortFunc(out, func(a, b types.TrafficTotal) int { return compareTrafficKeys(a.TrafficKey, b.TrafficKey) })

	return out
}

func (r *trafficRollup) destinationRows() []types.TrafficDestination {
	out := make([]types.TrafficDestination, 0, len(r.destinations))
	for _, row := range r.destinations {
		out = append(out, *row)
	}

	slices.SortFunc(out, func(a, b types.TrafficDestination) int {
		return cmp.Or(
			compareTrafficKeys(a.TrafficKey, b.TrafficKey),
			cmp.Compare(a.Dst, b.Dst), cmp.Compare(a.Port, b.Port), cmp.Compare(a.Proto, b.Proto),
			cmp.Compare(a.Host, b.Host),
		)
	})

	return out
}

func (r *trafficRollup) nameRows() []types.TrafficDNS {
	out := make([]types.TrafficDNS, 0, len(r.names))
	for _, row := range r.names {
		out = append(out, *row)
	}

	slices.SortFunc(out, func(a, b types.TrafficDNS) int {
		return cmp.Or(compareTrafficKeys(a.TrafficKey, b.TrafficKey), cmp.Compare(a.Name, b.Name))
	})

	return out
}

func compareTrafficKeys(a, b types.TrafficKey) int {
	return cmp.Or(
		cmp.Compare(a.Resolution, b.Resolution),
		cmp.Compare(a.Bucket, b.Bucket),
		cmp.Compare(a.NodeID, b.NodeID),
	)
}
