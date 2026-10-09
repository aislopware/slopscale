package state

import (
	"net/netip"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"tailscale.com/tailcfg"
)

// TestElectRegionalRoutes pins the regional election: a prefix with
// healthy routers in two regions gets a primary per region, the previous
// regional choice survives while healthy, an unhealthy router drops its
// region, and a prefix whose routers all share one region takes no part.
func TestElectRegionalRoutes(t *testing.T) {
	t.Parallel()

	shared := netip.MustParsePrefix("10.1.0.0/24")
	single := netip.MustParsePrefix("10.2.0.0/24")

	router := func(id types.NodeID, region tailcfg.DERPRegionID, unhealthy bool, routes ...netip.Prefix) types.Node {
		online := true

		return types.Node{
			ID:             id,
			IsOnline:       &online,
			Unhealthy:      unhealthy,
			ApprovedRoutes: routes,
			Hostinfo: &tailcfg.Hostinfo{
				RoutableIPs: routes,
				NetInfo:     &tailcfg.NetInfo{PreferredDERP: region},
			},
		}
	}

	nodes := map[types.NodeID]types.Node{
		1: router(1, 1, false, shared, single),
		2: router(2, 1, false, shared, single),
		3: router(3, 2, false, shared),
		4: router(4, 2, false, shared),
	}

	got := electRegionalRoutes(nodes, onlineAdvertisers(nodes), nil)
	assert.Equal(t, map[tailcfg.DERPRegionID]map[netip.Prefix]types.NodeID{
		1: {shared: 1},
		2: {shared: 3},
	}, got, "one primary per region for the shared prefix only")

	// Region 2 keeps its choice while it is healthy, even when a lower
	// ID is available.
	prev := map[tailcfg.DERPRegionID]map[netip.Prefix]types.NodeID{2: {shared: 4}}
	got = electRegionalRoutes(nodes, onlineAdvertisers(nodes), prev)
	assert.Equal(t, types.NodeID(4), got[2][shared])

	// An unhealthy router leaves its region; the region's other router
	// takes over rather than the previous choice.
	nodes[4] = router(4, 2, true, shared)
	got = electRegionalRoutes(nodes, onlineAdvertisers(nodes), prev)
	assert.Equal(t, types.NodeID(3), got[2][shared])

	// With region 2 entirely unhealthy the prefix has one region left and
	// no regional entry at all: viewers fall back to the tailnet-wide
	// primary.
	nodes[3] = router(3, 2, true, shared)
	assert.Nil(t, electRegionalRoutes(nodes, onlineAdvertisers(nodes), prev))
}

// TestPrimaryRoutesForNodeFrom checks the viewer-side lookup without
// round trips: a region with its own primary overrides the tailnet-wide
// one, and other regions and region 0 see the tailnet-wide assignment.
func TestPrimaryRoutesForNodeFrom(t *testing.T) {
	t.Parallel()

	shared := netip.MustParsePrefix("10.1.0.0/24")

	online := true
	nodes := types.Nodes{
		{ID: 1, IsOnline: &online, ApprovedRoutes: []netip.Prefix{shared}, Hostinfo: &tailcfg.Hostinfo{
			RoutableIPs: []netip.Prefix{shared}, NetInfo: &tailcfg.NetInfo{PreferredDERP: 1},
		}},
		{ID: 2, IsOnline: &online, ApprovedRoutes: []netip.Prefix{shared}, Hostinfo: &tailcfg.Hostinfo{
			RoutableIPs: []netip.Prefix{shared}, NetInfo: &tailcfg.NetInfo{PreferredDERP: 2},
		}},
	}

	store := NewNodeStore(nodes, allowAllPeersFunc, TestBatchSize, TestBatchTimeout)

	in := func(region tailcfg.DERPRegionID) viewerSite {
		return viewerSiteFrom(99, &tailcfg.NetInfo{PreferredDERP: region})
	}

	assert.Equal(t, []netip.Prefix{shared}, store.PrimaryRoutesForNodeFrom(1, in(0)))
	assert.Empty(t, store.PrimaryRoutesForNodeFrom(2, in(0)))
	assert.Equal(t, []netip.Prefix{shared}, store.PrimaryRoutesForNodeFrom(1, in(1)))
	assert.Empty(t, store.PrimaryRoutesForNodeFrom(2, in(1)))
	assert.Equal(t, []netip.Prefix{shared}, store.PrimaryRoutesForNodeFrom(2, in(2)))
	assert.Empty(t, store.PrimaryRoutesForNodeFrom(1, in(2)))
	assert.Equal(
		t,
		[]netip.Prefix{shared},
		store.PrimaryRoutesForNodeFrom(1, in(3)),
		"an unknown region sees the tailnet-wide primary",
	)

	assert.True(t, store.SteeringDiffers(in(1), in(2)))
	assert.True(t, store.SteeringDiffers(in(0), in(2)))
	assert.False(t, store.SteeringDiffers(in(0), in(1)), "region 1 agrees with the tailnet-wide primary")
	assert.False(t, store.SteeringDiffers(in(2), in(2)))
}
