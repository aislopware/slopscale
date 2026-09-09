package apiv2

import (
	"math"
	"strconv"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
	"tailscale.com/types/opt"
)

// The nested objects Tailscale returns only for fields=all.
type (
	// DevicePostureIdentity is what the client reported about the machine
	// it runs on, or that it was told not to report anything.
	DevicePostureIdentity struct {
		SerialNumbers []string `json:"serialNumbers" nullable:"false"`
		Disabled      bool     `json:"disabled"`
	}

	// Distro names the Linux distribution the client runs on, empty
	// elsewhere.
	Distro struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		CodeName string `json:"codeName"`
	}

	// ClientSupports is what the client's own network report found. It is
	// the client's measurement, not the server's; hairPinning is always
	// false because current clients no longer report it.
	ClientSupports struct {
		HairPinning bool `json:"hairPinning"`
		IPv6        bool `json:"ipv6"`
		PCP         bool `json:"pcp"`
		PMP         bool `json:"pmp"`
		UDP         bool `json:"udp"`
		UPnP        bool `json:"upnp"`
	}

	// DERPRegionLatency is the client's round trip to one relay region and
	// whether it homes on it.
	DERPRegionLatency struct {
		Preferred bool    `json:"preferred,omitempty"`
		LatencyMs float64 `json:"latencyMs"`
	}

	// ClientConnectivity is the client's last network report: where it can
	// be reached, what its NAT looks like and how far each relay region is.
	// Latency is keyed by region name, as Tailscale keys it.
	ClientConnectivity struct {
		Endpoints             []string                     `json:"endpoints"             nullable:"false"`
		MappingVariesByDestIP bool                         `json:"mappingVariesByDestIP"`
		Latency               map[string]DERPRegionLatency `json:"latency"               nullable:"false"`
		ClientSupports        ClientSupports               `json:"clientSupports"`
	}
)

// addDeviceDetail fills the fields=all half of the response.
func addDeviceDetail(b Backend, view types.NodeView, d *Device) {
	d.PostureIdentity = postureIdentityOf(view)
	d.ClientConnectivity = clientConnectivityOf(b, view)

	hi := view.Hostinfo()
	if !hi.Valid() {
		return
	}

	d.SSHEnabled = hi.SSH_HostKeys().Len() > 0

	if hi.Distro() != "" {
		d.Distro = &Distro{
			Name:     hi.Distro(),
			Version:  hi.DistroVersion(),
			CodeName: hi.DistroCodeName(),
		}
	}
}

// postureIdentityOf reports the serial numbers the client collected, or
// that it was told to collect none. A node that has never answered has no
// posture identity at all.
func postureIdentityOf(view types.NodeView) *DevicePostureIdentity {
	posture := view.Posture()
	if !posture.Valid() {
		return nil
	}

	return &DevicePostureIdentity{
		SerialNumbers: emptyIfNil(posture.SerialNumbers().AsSlice()),
		Disabled:      posture.Disabled(),
	}
}

// tailnetLockKey is the device's lock key, empty until the client has one.
func tailnetLockKey(view types.NodeView) string {
	if view.NLKey().IsZero() {
		return ""
	}

	// CLIString is the nlpub:… form the Tailscale CLI and API print.
	return view.NLKey().CLIString()
}

// tailnetLockError says why a device is not trusted under tailnet lock. It
// is empty while the lock is off, since nothing is signed then.
func tailnetLockError(b Backend, view types.NodeView) string {
	if b.State == nil || !b.State.TailnetLockEnabled() {
		return ""
	}

	if view.KeySignature().Len() > 0 {
		return ""
	}

	return "node key is not signed by the tailnet lock authority"
}

// clientConnectivityOf renders the client's last network report. It reads
// the same Hostinfo.NetInfo the v1 relay latency report does, keyed by
// region name rather than region id because that is what Tailscale's shape
// carries.
func clientConnectivityOf(b Backend, view types.NodeView) *ClientConnectivity {
	hi := view.Hostinfo()
	if !hi.Valid() || !hi.NetInfo().Valid() {
		return nil
	}

	ni := hi.NetInfo()

	endpoints := make([]string, 0, view.Endpoints().Len())
	for _, ep := range view.Endpoints().All() {
		endpoints = append(endpoints, ep.String())
	}

	return &ClientConnectivity{
		Endpoints:             endpoints,
		MappingVariesByDestIP: optBoolValue(ni.MappingVariesByDestIP()),
		Latency:               derpLatency(b, ni),
		ClientSupports: ClientSupports{
			IPv6: optBoolValue(ni.WorkingIPv6()),
			PCP:  optBoolValue(ni.PCP()),
			PMP:  optBoolValue(ni.PMP()),
			UDP:  optBoolValue(ni.WorkingUDP()),
			UPnP: optBoolValue(ni.UPnP()),
		},
	}
}

// derpLatency turns the client's per-family measurements into one entry
// per region, keyed by the region's name in the relay map. A region the
// map no longer has is skipped, since there is no name to key it by.
func derpLatency(b Backend, ni tailcfg.NetInfoView) map[string]DERPRegionLatency {
	out := map[string]DERPRegionLatency{}

	if b.State == nil {
		return out
	}

	dm := b.State.DERPMap()
	if !dm.Valid() {
		return out
	}

	for key, seconds := range ni.DERPLatency().All() {
		region, family, found := strings.Cut(key, "-")
		if !found || (family != "v4" && family != "v6") {
			continue
		}

		id, err := strconv.Atoi(region)
		if err != nil {
			continue
		}

		r, ok := dm.Regions().GetOk(tailcfg.DERPRegionID(id))
		if !ok || !r.Valid() || r.RegionName() == "" {
			continue
		}

		ms := math.Round(seconds*msPerSecond*msDecimals) / msDecimals

		entry, seen := out[r.RegionName()]
		if !seen || ms < entry.LatencyMs {
			entry.LatencyMs = ms
		}

		entry.Preferred = id == int(ni.PreferredDERP())
		out[r.RegionName()] = entry
	}

	return out
}

// Latency arithmetic: the client reports seconds, the API shows tenths of
// a millisecond.
const (
	msPerSecond = 1000
	msDecimals  = 10
)

// optBoolValue reads a tri-state client measurement as false when the
// client did not report it.
func optBoolValue(v opt.Bool) bool {
	b, ok := v.Get()

	return ok && b
}
