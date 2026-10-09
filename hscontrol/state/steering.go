package state

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/tailcfg"
)

// Steering picks, per viewer, among the routers that serve one prefix and
// among the global exit nodes with a priority, from the round trips the
// viewer measured to the DERP regions the routers are homed in
// (NetInfo.DERPLatency). A router whose region the viewer did not measure
// counts as far: an incremental netcheck only reports the nearest few
// regions. A viewer with no measurement for any of them keeps the
// regional or tailnet-wide primary.
//
// The routers within nearSlack of the nearest one are equally near. For a
// public prefix, which is egress to the internet and so returns through the
// router that sent it, each viewer takes one of them by rendezvous hash,
// which spreads viewers across the routers and moves only a router's share
// when one joins or leaves. A private prefix keeps one primary for every
// viewer the routers are equally near to, because a LAN without SNAT
// returns its traffic through one router. Among equally near exit nodes
// the operator's priority still decides; equal priorities are spread by
// the client.
//
// A client sends its latency only with a NetInfo change worth reporting
// (a new home region, a lost UDP path), so the steering moves when the
// machine does, not with every netcheck.

// nearSlack is how much further than the nearest router another may be and
// still count as equally near: nearSlackMin, or a nearSlackDivisor-th of
// the nearest round trip (10%) when that is more.
const (
	nearSlackMin     = 20 * time.Millisecond
	nearSlackDivisor = 10
)

// site is a router or exit node and the DERP region it is homed in.
type site struct {
	id     types.NodeID
	region tailcfg.DERPRegionID
}

// exitSite is a global exit node with the operator's priority.
type exitSite struct {
	site

	priority int
}

// viewerSite is what steering knows about a viewer: its ID, which the
// rendezvous hash keys on, and its NetInfo, which holds its home region
// and its round trips.
type viewerSite struct {
	id types.NodeID
	ni tailcfg.NetInfoView
}

// viewerSiteOf returns the viewer's site.
func viewerSiteOf(n types.NodeView) viewerSite {
	v := viewerSite{id: n.ID()}
	if n.Valid() && n.Hostinfo().Valid() {
		v.ni = n.Hostinfo().NetInfo()
	}

	return v
}

// viewerSiteFrom is [viewerSiteOf] for a NetInfo the node no longer holds.
func viewerSiteFrom(id types.NodeID, ni *tailcfg.NetInfo) viewerSite {
	return viewerSite{id: id, ni: ni.View()}
}

// region is the viewer's home DERP region, 0 when unknown.
func (v viewerSite) region() tailcfg.DERPRegionID {
	if !v.ni.Valid() {
		return 0
	}

	return v.ni.PreferredDERP()
}

// latency returns the viewer's round trip to a DERP region, the faster
// of IPv4 and IPv6, and whether it measured one.
func (v viewerSite) latency(region tailcfg.DERPRegionID) (time.Duration, bool) {
	if !v.ni.Valid() || region == 0 {
		return 0, false
	}

	measured := v.ni.DERPLatency()
	prefix := strconv.Itoa(int(region))

	var (
		best  time.Duration
		found bool
	)

	for _, family := range [...]string{"-v4", "-v6"} {
		seconds, ok := measured.GetOk(prefix + family)
		if !ok || seconds <= 0 {
			continue
		}

		d := time.Duration(seconds * float64(time.Second))
		if !found || d < best {
			best, found = d, true
		}
	}

	return best, found
}

// nearest returns the sites within the slack of the nearest one the
// viewer measured, in their given order, or nil when it measured none.
func nearest(sites []site, viewer viewerSite) []site {
	rtts := make([]time.Duration, len(sites))
	best := time.Duration(math.MaxInt64)

	for i, s := range sites {
		rtt, ok := viewer.latency(s.region)
		if !ok {
			rtts[i] = -1

			continue
		}

		rtts[i] = rtt
		best = min(best, rtt)
	}

	if best == math.MaxInt64 {
		return nil
	}

	limit := best + max(nearSlackMin, best/nearSlackDivisor)

	var out []site

	for i, s := range sites {
		if rtts[i] >= 0 && rtts[i] <= limit {
			out = append(out, s)
		}
	}

	return out
}

// rendezvous returns the site the viewer's hash ranks highest. The rank
// of a pair does not depend on the other sites, so a router joining or
// leaving only moves the viewers it wins or held.
func rendezvous(sites []site, viewer types.NodeID) types.NodeID {
	var (
		pick types.NodeID
		top  uint64
	)

	var buf [16]byte

	binary.BigEndian.PutUint64(buf[:8], viewer.Uint64())

	for i, s := range sites {
		binary.BigEndian.PutUint64(buf[8:], s.id.Uint64())

		h := fnv.New64a()
		_, _ = h.Write(buf[:])

		if score := h.Sum64(); i == 0 || score > top {
			pick, top = s.id, score
		}
	}

	return pick
}

// steer picks the router a viewer uses for a prefix among the sites that
// serve it, given the primary it would otherwise use. near is the result
// of [nearest] for these sites, which the caller computes once per set.
func steer(prefix netip.Prefix, sites, near []site, viewer viewerSite, fallback types.NodeID) types.NodeID {
	// A router viewing its own prefix keeps the primary, so HA
	// secondaries still learn which peer holds it.
	if len(near) == 0 || slices.ContainsFunc(sites, func(s site) bool { return s.id == viewer.id }) {
		return fallback
	}

	if isPublicAddr(prefix.Addr()) {
		return rendezvous(near, viewer.id)
	}

	if slices.ContainsFunc(near, func(s site) bool { return s.id == fallback }) {
		return fallback
	}

	return near[0].id
}

// exitPriorities returns the priority each global exit node carries in the
// viewer's map where steering moves it: the exit nodes nearest the viewer
// are lifted above every other, keeping their order among themselves. Nil
// when steering changes nothing.
func exitPriorities(exits []exitSite, viewer viewerSite) map[types.NodeID]int {
	if len(exits) < 2 {
		return nil
	}

	sites := make([]site, len(exits))
	top := 0

	for i, e := range exits {
		sites[i] = e.site
		top = max(top, e.priority)
	}

	near := nearest(sites, viewer)
	if len(near) == 0 || len(near) == len(exits) || top >= math.MaxInt/2 {
		return nil
	}

	out := make(map[types.NodeID]int, len(near))

	for _, e := range exits {
		if slices.ContainsFunc(near, func(s site) bool { return s.id == e.id }) {
			out[e.id] = e.priority + top
		}
	}

	return out
}
