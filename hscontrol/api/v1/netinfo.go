package apiv1

import (
	"context"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"tailscale.com/types/opt"
)

func init() {
	registrations = append(registrations, registerDERPLatency)
}

// NodeNetInfo is what the client last reported about its network: the
// relay region it homes on, how far each region is and what its NAT
// looks like. It is the client's own measurement, taken when it
// connects and when its network changes.
type NodeNetInfo struct {
	PreferredDERP     int    `doc:"The relay region the client homes on, 0 while unknown." json:"preferredDerp"`
	PreferredDERPName string `doc:"That region's name from the relay map."                 json:"preferredDerpName"`
	// Latency lists the regions the client measured, nearest first.
	Latency []NodeDERPLatency `json:"latency" nullable:"false"`
	// MappingVariesByDestIP is true behind a NAT that gives a different
	// mapping per destination, which stops direct connections.
	MappingVariesByDestIP *bool  `doc:"true behind a hard NAT: a mapping per destination." json:"mappingVariesByDestIp"`
	WorkingIPv6           *bool  `doc:"Whether the client reaches the internet over IPv6." json:"workingIpv6"`
	WorkingUDP            *bool  `doc:"Whether UDP reaches the internet."                  json:"workingUdp"`
	HavePortMap           bool   `doc:"Whether a UPnP, NAT-PMP or PCP mapping is open."    json:"havePortMap"`
	UPnP                  *bool  `doc:"Whether UPnP was seen on the LAN."                  json:"upnp"`
	PMP                   *bool  `doc:"Whether NAT-PMP was seen on the LAN."               json:"pmp"`
	PCP                   *bool  `doc:"Whether PCP was seen on the LAN."                   json:"pcp"`
	LinkType              string `doc:"wired, wifi or mobile, when the client knows."      json:"linkType"`
}

// NodeDERPLatency is the client's round trip to one relay region.
type NodeDERPLatency struct {
	RegionID int     `json:"regionId"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Ms       float64 `doc:"The fastest recent round trip in milliseconds, over IPv4 or IPv6." json:"ms"`
	IPv4Ms   float64 `doc:"Over IPv4, 0 when not measured."                                   json:"ipv4Ms"`
	IPv6Ms   float64 `doc:"Over IPv6, 0 when not measured."                                   json:"ipv6Ms"`
}

// DERPLatencyRegion sums up what the machines measure to one region.
type DERPLatencyRegion struct {
	RegionID int    `json:"regionId"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	// PreferredBy counts the machines homing on the region.
	PreferredBy int `doc:"Machines that home on the region." json:"preferredBy"`
	// Samples counts the machines that measured the region.
	Samples  int     `doc:"Machines that measured the region." json:"samples"`
	MinMs    float64 `doc:"The best round trip among them."    json:"minMs"`
	MedianMs float64 `doc:"The median round trip."             json:"medianMs"`
	P90Ms    float64 `doc:"The 90th percentile round trip."    json:"p90Ms"`
	MaxMs    float64 `doc:"The worst round trip."              json:"maxMs"`
	// InMap reports whether the region is in the relay map now; a
	// machine may still report a region that was removed.
	InMap bool `doc:"Whether the relay map still has the region." json:"inMap"`
}

// DERPLatencyReport is the tailnet's view of the relays: per region, how
// many machines home on it and how far it is for them.
type DERPLatencyReport struct {
	Regions []DERPLatencyRegion `json:"regions" nullable:"false"`
	// Reporting counts the machines with a measurement; Silent the
	// approved machines without one, which never connected or run a
	// client that sends none.
	Reporting int `doc:"Machines that reported a measurement." json:"reporting"`
	Silent    int `doc:"Machines that reported none."          json:"silent"`
	// HardNAT counts the machines behind a NAT whose mapping varies by
	// destination, which relay most of their traffic.
	HardNAT int `doc:"Machines behind a hard NAT, which relay most of their traffic." json:"hardNat"`
	// Machines lists every reporting machine with its home region and
	// its round trip to it, worst first, so the outliers are on top.
	Machines []DERPLatencyMachine `json:"machines" nullable:"false"`
}

// DERPLatencyMachine is one machine's home region and round trip.
type DERPLatencyMachine struct {
	NodeID        string  `format:"uint64"                                              json:"nodeId"`
	Name          string  `json:"name"`
	Online        bool    `json:"online"`
	PreferredDERP int     `json:"preferredDerp"`
	HomeMs        float64 `doc:"The round trip to the home region, 0 when unmeasured."  json:"homeMs"`
	HardNAT       bool    `doc:"true behind a NAT whose mapping varies by destination." json:"hardNat"`
	LinkType      string  `json:"linkType"`
}

type derpLatencyOutput struct {
	Body DERPLatencyReport
}

// derpRegion is a region's code and name from the relay map.
type derpRegion struct {
	code, name string
}

// derpRegions indexes the relay map by region id.
func (b Backend) derpRegions() map[int]derpRegion {
	out := map[int]derpRegion{}

	dm := b.State.DERPMap()
	if !dm.Valid() {
		return out
	}

	for id, region := range dm.Regions().All() {
		if !region.Valid() {
			continue
		}

		out[int(id)] = derpRegion{code: region.RegionCode(), name: region.RegionName()}
	}

	return out
}

// optBool renders an opt.Bool as a nullable JSON boolean.
func optBool(v opt.Bool) *bool {
	b, ok := v.Get()
	if !ok {
		return nil
	}

	return &b
}

// netInfoFrom renders the client's NetInfo, nil when it reported none.
func netInfoFrom(view types.NodeView, regions map[int]derpRegion) *NodeNetInfo {
	hi := view.Hostinfo()
	if !hi.Valid() || !hi.NetInfo().Valid() {
		return nil
	}

	ni := hi.NetInfo()
	out := &NodeNetInfo{
		PreferredDERP:         int(ni.PreferredDERP()),
		PreferredDERPName:     regions[int(ni.PreferredDERP())].name,
		Latency:               []NodeDERPLatency{},
		MappingVariesByDestIP: optBool(ni.MappingVariesByDestIP()),
		WorkingIPv6:           optBool(ni.WorkingIPv6()),
		WorkingUDP:            optBool(ni.WorkingUDP()),
		HavePortMap:           ni.HavePortMap(),
		UPnP:                  optBool(ni.UPnP()),
		PMP:                   optBool(ni.PMP()),
		PCP:                   optBool(ni.PCP()),
		LinkType:              ni.LinkType(),
	}

	byRegion := map[int]*NodeDERPLatency{}

	for key, seconds := range ni.DERPLatency().All() {
		id, family, ok := parseDERPLatencyKey(key)
		if !ok {
			continue
		}

		entry, exists := byRegion[id]
		if !exists {
			entry = &NodeDERPLatency{RegionID: id, Code: regions[id].code, Name: regions[id].name}
			byRegion[id] = entry
		}

		ms := roundMs(seconds * msPerSecond)

		switch family {
		case "v4":
			entry.IPv4Ms = ms
		case "v6":
			entry.IPv6Ms = ms
		}

		if entry.Ms == 0 || ms < entry.Ms {
			entry.Ms = ms
		}
	}

	for _, entry := range byRegion {
		out.Latency = append(out.Latency, *entry)
	}

	slices.SortFunc(out.Latency, func(a, b NodeDERPLatency) int {
		if a.Ms != b.Ms {
			if a.Ms < b.Ms {
				return -1
			}

			return 1
		}

		return a.RegionID - b.RegionID
	})

	return out
}

// parseDERPLatencyKey splits a NetInfo.DERPLatency key, "<region>-v4" or
// "<region>-v6"; older clients keyed by STUN host:port, which is skipped.
func parseDERPLatencyKey(key string) (int, string, bool) {
	region, family, ok := strings.Cut(key, "-")
	if !ok {
		return 0, "", false
	}

	id, err := strconv.Atoi(region)
	if err != nil || id <= 0 {
		return 0, "", false
	}

	if family != "v4" && family != "v6" {
		return 0, "", false
	}

	return id, family, true
}

// Latency arithmetic: reports are in seconds, the API shows tenths of a
// millisecond, and the summary is the median and 90th percentile.
const (
	msPerSecond = 1000
	msDecimals  = 10
	medianRank  = 0.5
	p90Rank     = 0.9
)

func roundMs(ms float64) float64 {
	return math.Round(ms*msDecimals) / msDecimals
}

// derpLatencyReport builds the tailnet-wide relay report from every
// approved node's NetInfo.
func (b Backend) derpLatencyReport() DERPLatencyReport {
	regions := b.derpRegions()
	report := DERPLatencyReport{Regions: []DERPLatencyRegion{}, Machines: []DERPLatencyMachine{}}
	samples := map[int][]float64{}
	preferred := map[int]int{}

	for _, node := range b.State.ListNodes().All() {
		if !node.IsApproved() {
			continue
		}

		ni := netInfoFrom(node, regions)
		if ni == nil {
			report.Silent++

			continue
		}

		report.Reporting++

		machine := DERPLatencyMachine{
			NodeID:        node.StringID(),
			Name:          node.GivenName(),
			Online:        node.IsOnline().Valid() && node.IsOnline().Get(),
			PreferredDERP: ni.PreferredDERP,
			HardNAT:       ni.MappingVariesByDestIP != nil && *ni.MappingVariesByDestIP,
			LinkType:      ni.LinkType,
		}

		if machine.HardNAT {
			report.HardNAT++
		}

		if ni.PreferredDERP > 0 {
			preferred[ni.PreferredDERP]++
		}

		for _, l := range ni.Latency {
			samples[l.RegionID] = append(samples[l.RegionID], l.Ms)

			if l.RegionID == ni.PreferredDERP {
				machine.HomeMs = l.Ms
			}
		}

		report.Machines = append(report.Machines, machine)
	}

	ids := map[int]struct{}{}
	for id := range samples {
		ids[id] = struct{}{}
	}

	for id := range preferred {
		ids[id] = struct{}{}
	}

	for id := range regions {
		ids[id] = struct{}{}
	}

	for _, id := range slices.Sorted(mapKeys(ids)) {
		region := DERPLatencyRegion{
			RegionID:    id,
			Code:        regions[id].code,
			Name:        regions[id].name,
			PreferredBy: preferred[id],
			Samples:     len(samples[id]),
		}

		_, region.InMap = regions[id]

		if values := samples[id]; len(values) > 0 {
			slices.Sort(values)
			region.MinMs = values[0]
			region.MaxMs = values[len(values)-1]
			region.MedianMs = percentile(values, medianRank)
			region.P90Ms = percentile(values, p90Rank)
		}

		report.Regions = append(report.Regions, region)
	}

	slices.SortFunc(report.Machines, func(a, b DERPLatencyMachine) int {
		switch {
		case a.HomeMs > b.HomeMs:
			return -1
		case a.HomeMs < b.HomeMs:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})

	return report
}

func mapKeys(m map[int]struct{}) func(func(int) bool) {
	return func(yield func(int) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// percentile returns the nearest-rank percentile of sorted values.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	rank := min(max(int(math.Ceil(p*float64(len(sorted))))-1, 0), len(sorted)-1)

	return sorted[rank]
}

func registerDERPLatency(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getDERPLatency",
		Method:      http.MethodGet,
		Path:        "/api/v1/derp/latency",
		Summary:     "Relay latency report",
		Description: "What the machines measure to each relay region: how many home on it and the " +
			"round trips they see, from the network report every client sends when it connects.",
		Tags:     []string{tagDERP},
		Security: bearerAuth,
	}, scope.DevicesCoreRead), func(_ context.Context, _ *struct{}) (*derpLatencyOutput, error) {
		return &derpLatencyOutput{Body: b.derpLatencyReport()}, nil
	})
}
