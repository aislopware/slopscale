package apiv2

import (
	"context"
	"net/http"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerNetworkLogs)
}

const (
	// maxNetworkLogRange bounds one read of the network flow log: a week
	// of hourly rows is already the largest answer worth building in
	// memory.
	maxNetworkLogRange = 7 * 24 * time.Hour

	// maxNetworkLogFlows bounds the flows in one answer. Tailscale's
	// endpoint has no pagination, so a range holding more is refused and
	// the caller asks for shorter ones.
	maxNetworkLogFlows = 20000
)

// NetworkFlowLog is Tailscale's network flow log record. Slopscale builds
// one per gateway and hour from what the gateway's traffic agent
// reported, so NodeID is the gateway and Src the node that sent the
// traffic through it.
type NetworkFlowLog struct {
	Logged time.Time `json:"logged"`
	NodeID string    `json:"nodeId"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	// SubnetTraffic is traffic to private addresses behind the gateway,
	// ExitTraffic traffic to the internet through it.
	SubnetTraffic []TrafficStats `json:"subnetTraffic,omitempty"`
	ExitTraffic   []TrafficStats `json:"exitTraffic,omitempty"`
}

// TrafficStats is one flow's volume in a record. Src carries port 0: the
// gateways report the node, not its source ports.
type TrafficStats struct {
	Proto   int    `json:"proto,omitempty"`
	Src     string `json:"src,omitempty"`
	Dst     string `json:"dst,omitempty"`
	TxPkts  uint64 `json:"txPkts,omitempty"`
	TxBytes uint64 `json:"txBytes,omitempty"`
	RxPkts  uint64 `json:"rxPkts,omitempty"`
	RxBytes uint64 `json:"rxBytes,omitempty"`
}

type (
	networkLogsInput struct {
		Tailnet string `path:"tailnet"`
		Start   string `doc:"RFC 3339." format:"date-time" query:"start" required:"true"`
		End     string `doc:"RFC 3339." format:"date-time" query:"end"   required:"true"`
	}
	networkLogsOutput struct {
		Body struct {
			Logs []NetworkFlowLog `json:"logs" nullable:"false"`
		}
	}
)

func registerNetworkLogs(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "listNetworkFlowLogs",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/logging/network",
		Summary:     "List network flow logs",
		Description: "The traffic the gateways' agents reported, one record per gateway and hour, " +
			"covering whole hours from start to end (at most a week). Destinations folded into " +
			"a gateway's remainder are left out, and nothing is recorded between two nodes. A " +
			"range holding more than 20000 flows is refused with 400; ask for shorter ranges.",
		Tags:     []string{"Logging", tagTailscaleCompat},
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.LogsNetworkRead), func(_ context.Context, in *networkLogsInput) (*networkLogsOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		f, err := networkLogFilter(in)
		if err != nil {
			return nil, err
		}

		f.Limit = maxNetworkLogFlows + 1

		rows, err := b.State.TrafficDestinationRows(f)
		if err != nil {
			return nil, internalError("reading network flow logs", err)
		}

		if len(rows) > maxNetworkLogFlows {
			return nil, huma.Error400BadRequest(
				"the range holds more than 20000 flows; narrow it and ask for the rest separately")
		}

		out := &networkLogsOutput{}
		out.Body.Logs = networkFlowLogs(b, rows, time.Now())

		return out, nil
	})
}

func networkLogFilter(in *networkLogsInput) (types.TrafficFilter, error) {
	start, err := time.Parse(time.RFC3339, in.Start)
	if err != nil {
		return types.TrafficFilter{}, huma.Error400BadRequest("invalid start timestamp", err)
	}

	end, err := time.Parse(time.RFC3339, in.End)
	if err != nil {
		return types.TrafficFilter{}, huma.Error400BadRequest("invalid end timestamp", err)
	}

	switch {
	case !start.Before(end):
		return types.TrafficFilter{}, huma.Error400BadRequest("start must be before end")
	case end.Sub(start) > maxNetworkLogRange:
		return types.TrafficFilter{}, huma.Error400BadRequest("the range may cover at most 7 days")
	}

	return types.TrafficFilter{
		Resolution: types.TrafficHour,
		Start:      start.Truncate(time.Hour),
		End:        end.Add(time.Hour - time.Nanosecond).Truncate(time.Hour),
	}, nil
}

// networkFlowLogs groups the rows, ordered by bucket and gateway, into one
// record per gateway and hour.
func networkFlowLogs(b Backend, rows []types.TrafficDestination, now time.Time) []NetworkFlowLog {
	addrs := make(map[types.NodeID][]netip.Addr)
	for _, n := range b.State.ListNodes().All() {
		addrs[n.ID()] = n.IPs()
	}

	out := []NetworkFlowLog{}

	var current *NetworkFlowLog

	for _, r := range rows {
		dst, err := netip.ParseAddr(r.Dst)
		if err != nil {
			continue
		}

		src, ok := sourceAddr(addrs[r.NodeID], dst)
		if !ok {
			continue
		}

		start := time.Unix(r.Bucket, 0).UTC()
		gateway := r.ReporterID.String()

		if current == nil || current.NodeID != gateway || !current.Start.Equal(start) {
			end := start.Add(time.Duration(r.Resolution) * time.Second)

			// An hour still open was logged up to now.
			logged := end
			if now.Before(end) {
				logged = now.UTC()
			}

			out = append(out, NetworkFlowLog{Logged: logged, NodeID: gateway, Start: start, End: end})
			current = &out[len(out)-1]
		}

		stats := TrafficStats{
			Proto:   int(r.Proto),
			Src:     netip.AddrPortFrom(src, 0).String(),
			Dst:     netip.AddrPortFrom(dst.Unmap(), r.Port).String(),
			TxPkts:  r.TxPackets,
			TxBytes: r.TxBytes,
			RxPkts:  r.RxPackets,
			RxBytes: r.RxBytes,
		}

		if types.IsPrivateTrafficDestination(r.Dst) {
			current.SubnetTraffic = append(current.SubnetTraffic, stats)
		} else {
			current.ExitTraffic = append(current.ExitTraffic, stats)
		}
	}

	return out
}

// sourceAddr is the node's address of the destination's family.
func sourceAddr(addrs []netip.Addr, dst netip.Addr) (netip.Addr, bool) {
	for _, a := range addrs {
		if a.Is4() == dst.Unmap().Is4() {
			return a, true
		}
	}

	return netip.Addr{}, false
}
