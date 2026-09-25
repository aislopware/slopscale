package apiv1

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerTrafficReads, registerTrafficAdmin)
}

const (
	tagTraffic = "Traffic"

	// trafficDefaultRange is the range a read covers when it names none.
	trafficDefaultRange = 24 * time.Hour
	// trafficMaxPoints bounds a series: a range that would split into
	// more buckets is read at the next coarser resolution.
	trafficMaxPoints = 1500
	// trafficDefaultTopNodes and trafficDefaultRows are the list sizes
	// when a request asks for none.
	trafficDefaultTopNodes = 10
	trafficDefaultRows     = 100
)

// TrafficCounts are a volume: Tx is what the node sent (upload), Rx what
// it received.
type TrafficCounts struct {
	TxBytes   uint64 `json:"txBytes"`
	RxBytes   uint64 `json:"rxBytes"`
	TxPackets uint64 `json:"txPackets"`
	RxPackets uint64 `json:"rxPackets"`
	Conns     uint64 `doc:"Connections that started." json:"conns"`
}

// TrafficWindow is the range a read covered, widened to whole buckets.
type TrafficWindow struct {
	Resolution int64     `doc:"Seconds per bucket: 60, 3600 or 86400." json:"resolution"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
}

// TrafficPoint is one bucket of a series.
type TrafficPoint struct {
	TrafficCounts

	Start time.Time `json:"start"`
}

// TrafficNode is a node's volume, or a gateway's.
type TrafficNode struct {
	TrafficCounts

	NodeID   string `format:"uint64"                             json:"nodeId"`
	NodeName string `doc:"Empty when the node no longer exists." json:"nodeName"`
}

// TrafficDestination is a destination group's volume. Only the fields of
// the grouping are set.
type TrafficDestination struct {
	TrafficCounts

	Dst string `doc:"The address; empty on the folded remainder of the smaller destinations." json:"dst"`

	Port  int    `json:"port"`
	Proto int    `doc:"IP protocol number: 6 TCP, 17 UDP."                            json:"proto"`
	Host  string `doc:"The name the traffic was for, from the handshake or a lookup." json:"host"`

	ASN     int64  `doc:"The network's AS number; 0 when unknown."           json:"asn"`
	ASName  string `doc:"The network's name, from the ASN table."            json:"asName"`
	Country string `doc:"ISO 3166 code of the network's registration."       json:"country"`
	Private bool   `doc:"A private address, reached through a subnet route." json:"private"`

	NodeID string `doc:"Set when grouped by node or reporter." format:"uint64" json:"nodeId"`
	// NodeName is the node's (or gateway's) name when grouped by one.
	NodeName string `json:"nodeName"`
	Nodes    uint64 `doc:"How many nodes the group covers." json:"nodes"`
}

// TrafficName is a DNS group's questions.
type TrafficName struct {
	Name     string `doc:"Empty on the folded remainder, or when grouped by node." json:"name"`
	NodeID   string `doc:"Set when grouped by node."                               format:"uint64" json:"nodeId"`
	NodeName string `json:"nodeName"`
	Queries  uint64 `json:"queries"`
	Failed   uint64 `doc:"Questions answered with an error or not at all."         json:"failed"`
	Nodes    uint64 `doc:"How many nodes asked."                                   json:"nodes"`
}

// TrafficCollector is one of an agent's collectors.
type TrafficCollector struct {
	Enabled bool   `json:"enabled"`
	Error   string `doc:"Why the collector is not working; empty while it works." json:"error"`
}

// TrafficCollectors are an agent's collectors as its last report left
// them.
type TrafficCollectors struct {
	Conntrack    TrafficCollector `json:"conntrack"`
	SNI          TrafficCollector `json:"sni"`
	DNS          TrafficCollector `json:"dns"`
	AppConnector TrafficCollector `json:"appConnector"`
}

// TrafficReporter is a gateway running the traffic agent.
type TrafficReporter struct {
	NodeID       string            `format:"uint64"                           json:"nodeId"`
	NodeName     string            `json:"nodeName"`
	Online       bool              `json:"online"`
	Version      string            `doc:"The agent's version."                json:"version"`
	Instance     string            `doc:"Changes each time the agent starts." json:"instance"`
	FirstSeenAt  time.Time         `json:"firstSeenAt"`
	LastReportAt time.Time         `json:"lastReportAt"`
	Stale        bool              `doc:"No report for three minutes."        json:"stale"`
	Collectors   TrafficCollectors `json:"collectors"`
	DNSListen    []string          `doc:"Where the agent's resolver answers." json:"dnsListen" nullable:"false"`
	// ResolverActive reports whether clients are pointed at this
	// gateway's resolver now.
	ResolverActive bool `json:"resolverActive"`
	// Unattributed counts flows and questions from addresses no node
	// held; Dropped the entries the agent discarded under load.
	Unattributed uint64 `json:"unattributed"`
	Dropped      uint64 `json:"dropped"`
}

// TrafficRetention is how long each resolution is kept.
type TrafficRetention struct {
	MinuteHours int `doc:"Per-minute totals, in hours."                    json:"minuteHours"`
	HourDays    int `doc:"Hourly totals, destinations and names, in days." json:"hourDays"`
	DayDays     int `doc:"Daily ones, in days."                            json:"dayDays"`
}

// TrafficSettings are the traffic monitor's settings.
type TrafficSettings struct {
	SNI bool `doc:"Agents name destinations from TLS and QUIC handshakes." json:"sni"`
	// DNSLogging points every client at the gateways' resolvers, so the
	// monitor sees what each node looks up.
	DNSLogging bool             `json:"dnsLogging"`
	Retention  TrafficRetention `json:"retention"`
}

// TrafficSettingsPatch changes the settings it names.
type TrafficSettingsPatch struct {
	SNI        *bool                  `json:"sni,omitempty"`
	DNSLogging *bool                  `json:"dnsLogging,omitempty"`
	Retention  *TrafficRetentionPatch `json:"retention,omitempty"`
}

// TrafficRetentionPatch changes the retentions it names.
type TrafficRetentionPatch struct {
	MinuteHours *int `json:"minuteHours,omitempty"`
	HourDays    *int `json:"hourDays,omitempty"`
	DayDays     *int `json:"dayDays,omitempty"`
}

// TrafficRange is the range and the node filter every traffic read takes.
// It is exported because huma skips unexported embedded fields.
type TrafficRange struct {
	Start      string `doc:"RFC 3339; defaults to a day before end." format:"date-time" query:"start"`
	End        string `doc:"RFC 3339; defaults to now."              format:"date-time" query:"end"`
	NodeID     string `doc:"Keep the traffic of one node."           format:"uint64"    query:"nodeId"`
	ReporterID string `doc:"Keep the traffic through one gateway."   format:"uint64"    query:"reporterId"`
}

type (
	trafficSummaryInput struct {
		TrafficRange

		Limit int `doc:"How many top nodes, at most 100; default 10." maximum:"100" minimum:"1" query:"limit"`
	}
	trafficSummaryOutput struct {
		Body struct {
			TrafficWindow

			Total TrafficCounts `json:"total"`
			// Series has one point per bucket, zero where nothing was
			// reported.
			Series    []TrafficPoint `json:"series"    nullable:"false"`
			Nodes     []TrafficNode  `json:"nodes"     nullable:"false"`
			Reporters []TrafficNode  `json:"reporters" nullable:"false"`
		}
	}
	trafficDestinationsInput struct {
		TrafficRange

		GroupBy string `default:"destination" enum:"destination,host,asn,country,port,node,reporter" query:"groupBy"`

		Q string `doc:"Keep hosts or addresses containing this." query:"q"`

		ASN int64 `doc:"Keep one network (AS number)." maximum:"4294967295" minimum:"0" query:"asn"`

		Country string `doc:"Keep one country (ISO 3166)." maxLength:"2" query:"country"`

		Proto int `doc:"Keep one IP protocol." maximum:"255" minimum:"0" query:"proto"`

		Port int `doc:"Keep one port; needs proto." maximum:"65535" minimum:"0" query:"port"`

		Limit int `doc:"At most 1000; default 100." maximum:"1000" minimum:"1" query:"limit"`
	}
	trafficDestinationsOutput struct {
		Body struct {
			TrafficWindow

			Destinations []TrafficDestination `json:"destinations" nullable:"false"`
		}
	}
	trafficDNSInput struct {
		TrafficRange

		GroupBy string `default:"name"                    enum:"name,node" query:"groupBy"`
		Q       string `doc:"Keep names containing this." query:"q"`
		Limit   int    `doc:"At most 1000; default 100."  maximum:"1000"   minimum:"1"     query:"limit"`
	}
	trafficDNSOutput struct {
		Body struct {
			TrafficWindow

			Names []TrafficName `json:"names" nullable:"false"`
		}
	}
	trafficReportersOutput struct {
		Body struct {
			Reporters []TrafficReporter `json:"reporters" nullable:"false"`
			// Resolvers are the gateway resolvers every client is
			// pointed at now.
			Resolvers []string `json:"resolvers" nullable:"false"`
			// ASNRanges is how many address ranges the ASN table in use
			// holds; 0 until one is downloaded.
			ASNRanges int `json:"asnRanges"`
		}
	}
	trafficReporterInput struct {
		NodeID string `format:"uint64" path:"nodeId"`
	}
	trafficSettingsOutput struct {
		Body TrafficSettings
	}
	trafficSettingsInput struct {
		Body TrafficSettingsPatch
	}
)

func registerTrafficReads(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getTrafficSummary",
		Method:      http.MethodGet,
		Path:        "/api/v1/traffic/summary",
		Summary:     "Get traffic summary",
		Description: "The volume the gateways saw over a range: the total, a series, the top nodes " +
			"and the volume through each gateway. The resolution is the finest the retention " +
			"still holds for the range, at most 1500 buckets.",
		Tags:     []string{tagTraffic},
		Security: bearerAuth,
	}, scope.LogsNetworkRead), func(_ context.Context, in *trafficSummaryInput) (*trafficSummaryOutput, error) {
		f, err := trafficFilter(b, &in.TrafficRange, types.TrafficMinute)
		if err != nil {
			return nil, err
		}

		return trafficSummary(b, f, cmp0(in.Limit, trafficDefaultTopNodes))
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listTrafficDestinations",
		Method:      http.MethodGet,
		Path:        "/api/v1/traffic/destinations",
		Summary:     "List traffic destinations",
		Description: "Where the traffic went, largest first, read at hourly or daily resolution. " +
			"groupBy host merges the addresses of one name; node and reporter rank the nodes " +
			"and gateways for the filter.",
		Tags:     []string{tagTraffic},
		Security: bearerAuth,
	}, scope.LogsNetworkRead), func(
		_ context.Context, in *trafficDestinationsInput,
	) (*trafficDestinationsOutput, error) {
		f, err := trafficFilter(b, &in.TrafficRange, types.TrafficHour)
		if err != nil {
			return nil, err
		}

		f.Search, f.Country = in.Q, in.Country
		//nolint:gosec // the schema bounds each to its type's range
		f.ASN, f.Proto, f.Port = uint32(in.ASN), uint8(in.Proto), uint16(in.Port)
		f.Limit = cmp0(in.Limit, trafficDefaultRows)

		sums, err := b.State.TrafficDestinations(f, types.TrafficGroup(in.GroupBy))
		if err != nil {
			return nil, mapError("reading traffic destinations", err)
		}

		names := nodeNames(b)
		out := &trafficDestinationsOutput{}
		out.Body.TrafficWindow = trafficWindow(f)
		out.Body.Destinations = make([]TrafficDestination, 0, len(sums))

		for _, s := range sums {
			out.Body.Destinations = append(out.Body.Destinations, trafficDestinationFrom(b, names, s))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listTrafficNames",
		Method:      http.MethodGet,
		Path:        "/api/v1/traffic/dns",
		Summary:     "List looked-up names",
		Description: "The names the nodes asked the gateways' resolvers about, most asked first, " +
			"read at hourly or daily resolution. Empty unless DNS logging is on.",
		Tags:     []string{tagTraffic},
		Security: bearerAuth,
	}, scope.LogsNetworkRead), func(_ context.Context, in *trafficDNSInput) (*trafficDNSOutput, error) {
		f, err := trafficFilter(b, &in.TrafficRange, types.TrafficHour)
		if err != nil {
			return nil, err
		}

		f.Search = in.Q
		f.Limit = cmp0(in.Limit, trafficDefaultRows)

		sums, err := b.State.TrafficNames(f, types.TrafficGroup(in.GroupBy))
		if err != nil {
			return nil, mapError("reading traffic names", err)
		}

		names := nodeNames(b)
		out := &trafficDNSOutput{}
		out.Body.TrafficWindow = trafficWindow(f)
		out.Body.Names = make([]TrafficName, 0, len(sums))

		for _, s := range sums {
			n := TrafficName{Name: s.Name, Queries: s.Queries, Failed: s.Failed, Nodes: s.Nodes}
			if s.NodeID != 0 {
				n.NodeID, n.NodeName = formatID(s.NodeID.Uint64()), names[s.NodeID]
			}

			out.Body.Names = append(out.Body.Names, n)
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listTrafficReporters",
		Method:      http.MethodGet,
		Path:        "/api/v1/traffic/reporters",
		Summary:     "List traffic reporters",
		Description: "The gateways whose agent has reported, with each collector's state and " +
			"the resolvers the clients are pointed at.",
		Tags:     []string{tagTraffic},
		Security: bearerAuth,
	}, scope.LogsNetworkRead), func(_ context.Context, _ *struct{}) (*trafficReportersOutput, error) {
		return trafficReporters(b), nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getTrafficSettings",
		Method:      http.MethodGet,
		Path:        "/api/v1/traffic/settings",
		Summary:     "Get traffic settings",
		Tags:        []string{tagTraffic},
		Security:    bearerAuth,
	}, scope.LogsNetworkRead), func(_ context.Context, _ *struct{}) (*trafficSettingsOutput, error) {
		return &trafficSettingsOutput{Body: trafficSettingsFrom(b.State.TrafficSettings())}, nil
	})
}

func registerTrafficAdmin(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateTrafficSettings",
		Method:      http.MethodPatch,
		Path:        "/api/v1/traffic/settings",
		Summary:     "Update traffic settings",
		Description: "Changes the settings named. The agents take the collector switches with " +
			"their next report. Turning DNS logging on points every client at the gateways' " +
			"resolvers while they report; turning it off points them back.",
		Tags:     []string{tagTraffic},
		Security: bearerAuth,
	}, scope.LogsNetwork), "traffic.settings.update", "", ""), func(
		ctx context.Context, in *trafficSettingsInput,
	) (*trafficSettingsOutput, error) {
		next := patchTrafficSettings(b.State.TrafficSettings(), in.Body)

		saved, c, err := b.State.SetTrafficSettings(next)
		if err != nil {
			return nil, mapError("setting traffic settings", err)
		}

		audit.Detail(ctx, "sni", saved.SNI)
		audit.Detail(ctx, "dnsLogging", saved.DNSLogging)
		audit.Detail(ctx, "retention", trafficSettingsFrom(saved).Retention)

		b.Change(c)

		return &trafficSettingsOutput{Body: trafficSettingsFrom(saved)}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteTrafficReporter",
		Method:      http.MethodDelete,
		Path:        "/api/v1/traffic/reporters/{nodeId}",
		Summary:     "Remove traffic reporter",
		Description: "Forgets a gateway's agent and takes its resolver out of the clients' DNS. " +
			"What it reported stays until the retention removes it; an agent still running " +
			"comes back with its next report.",
		Tags:     []string{tagTraffic},
		Security: bearerAuth,
	}, scope.LogsNetwork), "traffic.reporter.delete", "node", "nodeId"), func(
		_ context.Context, in *trafficReporterInput,
	) (*emptyOutput, error) {
		id, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		c, err := b.State.DeleteTrafficReporter(id)
		if err != nil {
			return nil, mapError("removing traffic reporter", err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	})
}

// cmp0 is v, or def when v is zero.
func cmp0(v, def int) int {
	if v == 0 {
		return def
	}

	return v
}

// trafficFilter turns a range into a filter: the resolution the range and
// the retention allow, no finer than minimum, and the range widened to
// whole buckets of it.
func trafficFilter(b Backend, in *TrafficRange, minimum int64) (types.TrafficFilter, error) {
	now := time.Now()

	end, err := parseTime(in.End, "end")
	if err != nil {
		return types.TrafficFilter{}, err
	}

	if end.IsZero() {
		end = now
	}

	start, err := parseTime(in.Start, "start")
	if err != nil {
		return types.TrafficFilter{}, err
	}

	if start.IsZero() {
		start = end.Add(-trafficDefaultRange)
	}

	if !start.Before(end) {
		return types.TrafficFilter{}, mapError("reading traffic",
			fmt.Errorf("%w: start must be before end", types.ErrTrafficRangeInvalid))
	}

	res := b.State.TrafficResolutionFor(start, end, now, minimum, trafficMaxPoints)
	f := types.TrafficFilter{
		Resolution: res,
		Start:      time.Unix(start.Unix()-start.Unix()%res, 0).UTC(),
		End:        time.Unix(end.Unix()+(res-end.Unix()%res)%res, 0).UTC(),
	}

	if in.NodeID != "" {
		f.NodeID, err = parseNodeID(in.NodeID)
		if err != nil {
			return types.TrafficFilter{}, err
		}
	}

	if in.ReporterID != "" {
		f.ReporterID, err = parseNodeID(in.ReporterID)
		if err != nil {
			return types.TrafficFilter{}, err
		}
	}

	return f, nil
}

func trafficWindow(f types.TrafficFilter) TrafficWindow {
	return TrafficWindow{Resolution: f.Resolution, Start: f.Start, End: f.End}
}

func trafficCountsFrom(c types.TrafficCounts) TrafficCounts {
	return TrafficCounts{
		TxBytes: c.TxBytes, RxBytes: c.RxBytes,
		TxPackets: c.TxPackets, RxPackets: c.RxPackets,
		Conns: c.Conns,
	}
}

// nodeNames maps every node to its name, for the rows that carry only an
// ID.
func nodeNames(b Backend) map[types.NodeID]string {
	nodes := b.State.ListNodes()
	out := make(map[types.NodeID]string, nodes.Len())

	for _, n := range nodes.All() {
		out[n.ID()] = n.GivenName()
	}

	return out
}

func trafficSummary(b Backend, f types.TrafficFilter, top int) (*trafficSummaryOutput, error) {
	total, err := b.State.TrafficSum(f)
	if err != nil {
		return nil, mapError("reading traffic", err)
	}

	points, err := b.State.TrafficSeries(f)
	if err != nil {
		return nil, mapError("reading traffic", err)
	}

	f.Limit = top

	nodes, err := b.State.TrafficTopNodes(f, false)
	if err != nil {
		return nil, mapError("reading traffic", err)
	}

	f.Limit = 0

	reporters, err := b.State.TrafficTopNodes(f, true)
	if err != nil {
		return nil, mapError("reading traffic", err)
	}

	names := nodeNames(b)
	out := &trafficSummaryOutput{}
	out.Body.TrafficWindow = trafficWindow(f)
	out.Body.Total = trafficCountsFrom(total)
	out.Body.Series = trafficSeries(f, points)
	out.Body.Nodes = trafficNodes(names, nodes)
	out.Body.Reporters = trafficNodes(names, reporters)

	return out, nil
}

// trafficSeries fills the buckets nothing was reported for with zeros,
// so a chart can plot the series as it comes.
func trafficSeries(f types.TrafficFilter, points []types.TrafficPoint) []TrafficPoint {
	byBucket := make(map[int64]types.TrafficCounts, len(points))
	for _, p := range points {
		byBucket[p.Bucket] = p.TrafficCounts
	}

	out := make([]TrafficPoint, 0, (f.End.Unix()-f.Start.Unix())/f.Resolution)
	for bucket := f.Start.Unix(); bucket < f.End.Unix(); bucket += f.Resolution {
		out = append(out, TrafficPoint{
			Start:         time.Unix(bucket, 0).UTC(),
			TrafficCounts: trafficCountsFrom(byBucket[bucket]),
		})
	}

	return out
}

func trafficNodes(names map[types.NodeID]string, sums []types.TrafficNodeSum) []TrafficNode {
	out := make([]TrafficNode, 0, len(sums))
	for _, s := range sums {
		out = append(out, TrafficNode{
			NodeID:        formatID(s.NodeID.Uint64()),
			NodeName:      names[s.NodeID],
			TrafficCounts: trafficCountsFrom(s.TrafficCounts),
		})
	}

	return out
}

func trafficDestinationFrom(
	b Backend,
	names map[types.NodeID]string,
	s types.TrafficDestinationSum,
) TrafficDestination {
	d := TrafficDestination{
		Dst:           s.Dst,
		Port:          int(s.Port),
		Proto:         int(s.Proto),
		Host:          s.Host,
		ASN:           int64(s.ASN),
		ASName:        b.State.ASNName(s.ASN),
		Country:       s.Country,
		Private:       types.IsPrivateTrafficDestination(s.Dst),
		Nodes:         s.Nodes,
		TrafficCounts: trafficCountsFrom(s.TrafficCounts),
	}

	if s.NodeID != 0 {
		d.NodeID, d.NodeName = formatID(s.NodeID.Uint64()), names[s.NodeID]
	}

	return d
}

func trafficReporters(b Backend) *trafficReportersOutput {
	reporters, resolvers := b.State.TrafficReporters()
	now := time.Now()

	out := &trafficReportersOutput{}
	out.Body.Reporters = make([]TrafficReporter, 0, len(reporters))
	out.Body.Resolvers = make([]string, 0, len(resolvers))
	out.Body.ASNRanges = b.State.ASNRanges()

	for _, addr := range resolvers {
		out.Body.Resolvers = append(out.Body.Resolvers, addr.String())
	}

	for _, r := range reporters {
		out.Body.Reporters = append(out.Body.Reporters, trafficReporterFrom(b, r, resolvers, now))
	}

	return out
}

func trafficReporterFrom(b Backend, r types.TrafficReporter, resolvers []netip.Addr, now time.Time) TrafficReporter {
	out := TrafficReporter{
		NodeID:       formatID(r.NodeID.Uint64()),
		Version:      r.Version,
		Instance:     r.Instance,
		FirstSeenAt:  r.FirstSeenAt,
		LastReportAt: r.LastReportAt,
		Stale:        state.TrafficReporterStale(r, now),
		Collectors: TrafficCollectors{
			Conntrack:    trafficCollectorFrom(r.Status.Conntrack),
			SNI:          trafficCollectorFrom(r.Status.SNI),
			DNS:          trafficCollectorFrom(r.Status.DNS),
			AppConnector: trafficCollectorFrom(r.Status.AppConnector),
		},
		DNSListen:    make([]string, 0, len(r.DNSListen)),
		Unattributed: r.Unattributed,
		Dropped:      r.Dropped,
	}

	if node, ok := b.State.GetNodeByID(r.NodeID); ok {
		out.NodeName = node.GivenName()
		out.Online = node.IsOnline().Valid() && node.IsOnline().Get()
	}

	for _, ap := range r.DNSListen {
		out.DNSListen = append(out.DNSListen, ap.String())
		out.ResolverActive = out.ResolverActive || slices.Contains(resolvers, ap.Addr())
	}

	return out
}

func trafficCollectorFrom(c traffic.Collector) TrafficCollector {
	return TrafficCollector{Enabled: c.Enabled, Error: c.Error}
}

func trafficSettingsFrom(s types.TrafficSettings) TrafficSettings {
	return TrafficSettings{
		SNI:        s.SNI,
		DNSLogging: s.DNSLogging,
		Retention: TrafficRetention{
			MinuteHours: s.Retention.MinuteHours,
			HourDays:    s.Retention.HourDays,
			DayDays:     s.Retention.DayDays,
		},
	}
}

func patchTrafficSettings(s types.TrafficSettings, p TrafficSettingsPatch) types.TrafficSettings {
	if p.SNI != nil {
		s.SNI = *p.SNI
	}

	if p.DNSLogging != nil {
		s.DNSLogging = *p.DNSLogging
	}

	if r := p.Retention; r != nil {
		if r.MinuteHours != nil {
			s.Retention.MinuteHours = *r.MinuteHours
		}

		if r.HourDays != nil {
			s.Retention.HourDays = *r.HourDays
		}

		if r.DayDays != nil {
			s.Retention.DayDays = *r.DayDays
		}
	}

	return s
}
