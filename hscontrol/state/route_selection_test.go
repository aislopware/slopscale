package state

import (
	"net/netip"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestRouteSelectionModes runs one tailnet through every route selection:
// an internet address served by an office router in Hanoi (27, the
// tailnet-wide primary) and an EU router in Frankfurt (48), both also
// global exit nodes with the office first. The viewers are a laptop in
// Amsterdam, where no router is homed, one in Frankfurt and one in Hanoi.
func TestRouteSelectionModes(t *testing.T) {
	t.Parallel()

	cloudflare := netip.MustParsePrefix("104.20.39.105/32")
	online := true

	router := func(id types.NodeID, region tailcfg.DERPRegionID, priority int) *types.Node {
		routes := []netip.Prefix{cloudflare, netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}

		return &types.Node{
			ID: id, IsOnline: &online, ApprovedRoutes: routes,
			GlobalExitNode: true, ExitNodePriority: priority,
			Hostinfo: &tailcfg.Hostinfo{
				RoutableIPs: routes,
				NetInfo:     &tailcfg.NetInfo{PreferredDERP: region},
			},
		}
	}

	store := NewNodeStore(
		types.Nodes{router(27, regionOffice, 30), router(48, regionFra, 5)},
		allowAllPeersFunc, TestBatchSize, TestBatchTimeout,
	)
	store.Start()
	t.Cleanup(store.Stop)

	frankfurt := measured(60, regionFra, map[tailcfg.DERPRegionID]float64{regionFra: 1, regionOffice: 200})

	type want struct {
		ams, fra, hanoi types.NodeID
		// lifted is whether the Amsterdam laptop sees the EU exit node
		// lifted above the office.
		lifted bool
	}

	cases := []struct {
		selection types.RouteSelection
		want      want
	}{
		{types.RouteSelectionRegional, want{ams: 48, fra: 48, hanoi: 27, lifted: true}},
		{types.RouteSelectionActivePassive, want{ams: 27, fra: 27, hanoi: 27}},
		{types.RouteSelectionRegionalFailover, want{ams: 27, fra: 48, hanoi: 27}},
		{types.RouteSelectionMagicRoute, want{ams: 48, fra: 48, hanoi: 27, lifted: true}},
		{"", want{ams: 48, fra: 48, hanoi: 27, lifted: true}},
	}

	via := func(viewer viewerSite) types.NodeID {
		t.Helper()

		var holders []types.NodeID

		for _, id := range []types.NodeID{27, 48} {
			if len(store.PrimaryRoutesForNodeFrom(id, viewer)) > 0 {
				holders = append(holders, id)
			}
		}

		require.Len(t, holders, 1, "one router per viewer")

		return holders[0]
	}

	for _, tc := range cases {
		before := store.PrimaryRoutes()
		store.SetRouteSelection(tc.selection)

		if tc.selection == types.RouteSelectionActivePassive {
			assert.False(t, before.Equal(store.PrimaryRoutes()), "dropping the steering moves the ledger")
		}

		assert.Equal(t, tc.want.ams, via(amsterdam(58)), "%q: Amsterdam", tc.selection)
		assert.Equal(t, tc.want.fra, via(frankfurt), "%q: Frankfurt", tc.selection)
		assert.Equal(t, tc.want.hanoi, via(hanoi(3)), "%q: Hanoi", tc.selection)
		assert.Equal(t, []netip.Prefix{cloudflare}, store.PrimaryRoutesForNode(27),
			"%q: the tailnet-wide primary never moves", tc.selection)

		lifted := store.ExitNodePrioritiesFor(amsterdam(58))[48] > 30
		assert.Equal(t, tc.want.lifted, lifted, "%q: exit node lifting", tc.selection)

		assert.Equal(t, tc.want.ams != tc.want.hanoi, store.SteeringDiffers(hanoi(58), amsterdam(58)),
			"%q: flying from Hanoi to Amsterdam re-steers only where the viewers differ", tc.selection)
	}

	assert.False(t, store.SetRouteSelection(types.RouteSelectionRegional), "the empty selection is regional routing")
}

func TestRouteSelectionValues(t *testing.T) {
	t.Parallel()

	for _, r := range types.RouteSelections {
		got, err := types.ParseRouteSelection(string(r))
		require.NoError(t, err)
		assert.Equal(t, r, got)
	}

	_, err := types.ParseRouteSelection("nearest")
	require.ErrorIs(t, err, types.ErrRouteSelectionUnknown)

	_, err = types.ParseRouteSelection("")
	require.ErrorIs(t, err, types.ErrRouteSelectionUnknown, "the default is stored by name")

	assert.Equal(t, types.RouteSelectionRegional, types.RouteSelection("").OrDefault())
	assert.True(t, types.RouteSelection("from-a-newer-server").Nearest(),
		"an unknown stored value steers as the default")
}
