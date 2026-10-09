package state

import (
	"net/netip"
	"strconv"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// Regions and round trips from the production tailnet the feature was
// built for: an office connector in Hanoi (custom region 900), a second
// site's DERP (901), an EU connector homed in Frankfurt (4).
const (
	regionOffice tailcfg.DERPRegionID = 900
	regionDCOld  tailcfg.DERPRegionID = 901
	regionFra    tailcfg.DERPRegionID = 4
	regionAms    tailcfg.DERPRegionID = 14
	regionSyd    tailcfg.DERPRegionID = 5
)

// measured builds a viewer homed in home with IPv4 round trips in
// milliseconds per region.
func measured(id types.NodeID, home tailcfg.DERPRegionID, ms map[tailcfg.DERPRegionID]float64) viewerSite {
	latency := make(map[string]float64, len(ms))
	for region, v := range ms {
		latency[strconv.Itoa(int(region))+"-v4"] = v / 1000
	}

	return viewerSiteFrom(id, &tailcfg.NetInfo{PreferredDERP: home, DERPLatency: latency})
}

// The three viewers of the incident: a laptop in Amsterdam whose
// incremental netcheck reported its three nearest regions only, a
// machine in the Hanoi office, and one in Sydney.
func amsterdam(id types.NodeID) viewerSite {
	return measured(id, regionAms, map[tailcfg.DERPRegionID]float64{regionAms: 6.4, regionFra: 10.8, 18: 20.4})
}

func hanoi(id types.NodeID) viewerSite {
	return measured(id, regionOffice, map[tailcfg.DERPRegionID]float64{
		regionOffice: 2, regionDCOld: 8, 902: 27.7, regionFra: 203, regionAms: 225.7,
	})
}

func sydney(id types.NodeID) viewerSite {
	return measured(id, regionSyd, map[tailcfg.DERPRegionID]float64{
		regionSyd: 52.3, regionOffice: 265.5, regionDCOld: 277.5, regionFra: 342.7,
	})
}

func TestViewerLatency(t *testing.T) {
	t.Parallel()

	v := viewerSiteFrom(1, &tailcfg.NetInfo{DERPLatency: map[string]float64{
		"4-v4": 0.020, "4-v6": 0.012, "5-v4": 0, "6-v6": 0.050,
	}})

	rtt, ok := v.latency(4)
	require.True(t, ok)
	assert.Equal(t, int64(12), rtt.Milliseconds(), "the faster family counts")

	_, ok = v.latency(5)
	assert.False(t, ok, "a zero round trip is no measurement")

	rtt, ok = v.latency(6)
	require.True(t, ok)
	assert.Equal(t, int64(50), rtt.Milliseconds(), "IPv6 alone counts")

	_, ok = v.latency(7)
	assert.False(t, ok)

	_, ok = viewerSite{id: 1}.latency(4)
	assert.False(t, ok, "a viewer without NetInfo measured nothing")
}

func TestNearest(t *testing.T) {
	t.Parallel()

	office := site{id: 27, region: regionOffice}
	dcOld := site{id: 24, region: regionDCOld}
	dcNew := site{id: 25, region: 902}
	eu := site{id: 48, region: regionFra}
	all := []site{dcOld, dcNew, office, eu}

	assert.Equal(t, []site{eu}, nearest(all, amsterdam(1)),
		"a region the viewer did not measure counts as far")
	assert.Equal(t, []site{dcOld, office}, nearest(all, hanoi(1)),
		"within 20ms of the nearest is as near; 27.7ms against 2ms is not")
	assert.Equal(t, []site{dcOld, office}, nearest(all, sydney(1)),
		"past 200ms the slack is 10%: 277.5ms is within 10% of 265.5ms, 342.7ms is not")
	assert.Nil(t, nearest(all, viewerSite{id: 1}), "no measurement, no steering")
	assert.Nil(t, nearest(all, measured(1, 3, map[tailcfg.DERPRegionID]float64{3: 5})),
		"measuring only other regions is no measurement of these")
}

func TestRendezvous(t *testing.T) {
	t.Parallel()

	a, b, c := site{id: 1}, site{id: 2}, site{id: 3}

	picked := map[types.NodeID]int{}

	for viewer := range types.NodeID(200) {
		pick := rendezvous([]site{a, b, c}, viewer)
		picked[pick]++

		assert.Equal(t, pick, rendezvous([]site{c, a, b}, viewer), "order does not matter")

		// Dropping a router the viewer did not pick leaves it in place.
		for _, gone := range []site{a, b, c} {
			if gone.id == pick {
				continue
			}

			rest := []site{}

			for _, s := range []site{a, b, c} {
				if s != gone {
					rest = append(rest, s)
				}
			}

			assert.Equal(t, pick, rendezvous(rest, viewer))
		}
	}

	for _, s := range []site{a, b, c} {
		assert.Greater(t, picked[s.id], 40, "viewers spread over every router: %v", picked)
	}
}

func TestSteer(t *testing.T) {
	t.Parallel()

	public := netip.MustParsePrefix("104.20.39.105/32")
	private := netip.MustParsePrefix("192.168.100.0/24")

	office := site{id: 27, region: regionOffice}
	dcOld := site{id: 24, region: regionDCOld}
	eu := site{id: 48, region: regionFra}
	sites := []site{dcOld, office, eu}

	pick := func(prefix netip.Prefix, viewer viewerSite, fallback types.NodeID) types.NodeID {
		return steer(prefix, sites, nearest(sites, viewer), viewer, fallback)
	}

	assert.Equal(t, eu.id, pick(public, amsterdam(1), office.id), "Amsterdam leaves through Frankfurt")
	assert.Equal(t, eu.id, pick(private, amsterdam(1), office.id), "for a private prefix too")
	assert.Equal(t, office.id, pick(public, viewerSite{id: 1}, office.id), "no measurement keeps the primary")
	assert.Equal(t, office.id, pick(public, amsterdam(office.id), office.id),
		"a router viewing its own prefix keeps the primary")

	// Hanoi is as near to the office as to dc-old: a public prefix is
	// spread, a private one stays on the primary, or on the first near
	// router when the primary is far.
	spread := map[types.NodeID]bool{}
	for id := range types.NodeID(50) {
		spread[pick(public, hanoi(id+100), eu.id)] = true
	}

	assert.Equal(t, map[types.NodeID]bool{office.id: true, dcOld.id: true}, spread)
	assert.Equal(t, office.id, pick(private, hanoi(1), office.id))
	assert.Equal(t, dcOld.id, pick(private, hanoi(1), dcOld.id))
	assert.Equal(t, dcOld.id, pick(private, hanoi(1), eu.id), "the primary is far: first near router by ID")
}

func TestExitPriorities(t *testing.T) {
	t.Parallel()

	office := exitSite{id: 27, region: regionOffice, priority: 30}
	dcOld := exitSite{id: 24, region: regionDCOld, priority: 20}
	eu := exitSite{id: 48, region: regionFra, priority: 5}
	exits := []exitSite{dcOld, office, eu}

	assert.Equal(t, map[types.NodeID]int{eu.id: 35}, exitPriorities(exits, amsterdam(1)),
		"Frankfurt is lifted above the operator's first choice")
	assert.Equal(t, map[types.NodeID]int{office.id: 60, dcOld.id: 50}, exitPriorities(exits, hanoi(1)),
		"the near ones keep the operator's order among themselves")
	assert.Nil(t, exitPriorities(exits, viewerSite{id: 1}), "no measurement, operator order")
	assert.Nil(t, exitPriorities([]exitSite{office, dcOld}, hanoi(1)), "every exit node near changes nothing")
	assert.Nil(t, exitPriorities([]exitSite{eu}, amsterdam(1)), "one exit node changes nothing")
}

// TestPrimaryRoutesSteered is the incident: a Cloudflare address two
// connectors learned, the office's (the tailnet-wide primary, lower ID)
// and the EU one. A viewer in Amsterdam, a region neither connector is
// homed in, goes through Frankfurt; a viewer in Hanoi through the office;
// one without measurements through the primary.
func TestPrimaryRoutesSteered(t *testing.T) {
	t.Parallel()

	cloudflare := netip.MustParsePrefix("104.20.39.105/32")
	online := true

	router := func(id types.NodeID, region tailcfg.DERPRegionID) *types.Node {
		return &types.Node{
			ID: id, IsOnline: &online, ApprovedRoutes: []netip.Prefix{cloudflare},
			Hostinfo: &tailcfg.Hostinfo{
				RoutableIPs: []netip.Prefix{cloudflare},
				NetInfo:     &tailcfg.NetInfo{PreferredDERP: region},
			},
		}
	}

	store := NewNodeStore(
		types.Nodes{router(27, regionOffice), router(48, regionFra)},
		allowAllPeersFunc, TestBatchSize, TestBatchTimeout,
	)

	assert.Equal(t, []netip.Prefix{cloudflare}, store.PrimaryRoutesForNode(27), "office is the tailnet-wide primary")

	assert.Equal(t, []netip.Prefix{cloudflare}, store.PrimaryRoutesForNodeFrom(48, amsterdam(58)))
	assert.Empty(t, store.PrimaryRoutesForNodeFrom(27, amsterdam(58)))
	assert.Equal(t, []netip.Prefix{cloudflare}, store.PrimaryRoutesForNodeFrom(27, hanoi(3)))
	assert.Empty(t, store.PrimaryRoutesForNodeFrom(48, hanoi(3)))
	assert.Equal(t, []netip.Prefix{cloudflare}, store.PrimaryRoutesForNodeFrom(27, sydney(54)))
	assert.Equal(t, []netip.Prefix{cloudflare}, store.PrimaryRoutesForNodeFrom(27, viewerSite{id: 7}))

	assert.True(t, store.SteeringDiffers(hanoi(58), amsterdam(58)), "flying to Amsterdam re-steers")
	assert.False(t, store.SteeringDiffers(hanoi(58), sydney(58)), "both pick the office")
}
