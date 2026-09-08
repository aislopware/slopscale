package servertest_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

// setDERPRegion tells the server the client is homed in region, as a
// client does after its first netcheck.
func setDERPRegion(t *testing.T, c *servertest.TestClient, region tailcfg.DERPRegionID) {
	t.Helper()

	c.Direct().SetNetInfo(&tailcfg.NetInfo{PreferredDERP: region})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = c.Direct().SendUpdate(ctx)
}

// TestHARegionalRouting covers regional routing: with a router for the
// same prefix in two DERP regions, a viewer is steered to the router in
// its own region, a viewer without a region gets the tailnet-wide
// primary, a region whose router is unhealthy falls back to the
// tailnet-wide primary, and a viewer that moves region is re-steered.
func TestHARegionalRouting(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	user := srv.CreateUser(t, "ha-regional")

	route := netip.MustParsePrefix("10.200.0.0/24")

	r1 := servertest.NewClient(t, srv, "regional-r1", servertest.WithUser(user))
	r2 := servertest.NewClient(t, srv, "regional-r2", servertest.WithUser(user))
	east := servertest.NewClient(t, srv, "regional-east", servertest.WithUser(user))
	west := servertest.NewClient(t, srv, "regional-west", servertest.WithUser(user))
	nowhere := servertest.NewClient(t, srv, "regional-nowhere", servertest.WithUser(user))

	for _, c := range []*servertest.TestClient{r1, r2, east, west, nowhere} {
		c.WaitForPeers(t, 4, 10*time.Second)
	}

	setDERPRegion(t, r1, 1)
	setDERPRegion(t, r2, 2)
	setDERPRegion(t, east, 1)
	setDERPRegion(t, west, 2)

	id1 := advertiseAndApproveRoute(t, srv, r1, route)
	id2 := advertiseAndApproveRoute(t, srv, r2, route)

	require.Contains(t, srv.State().GetNodePrimaryRoutes(id1), route, "the lower ID is the tailnet-wide primary")
	require.NotContains(t, srv.State().GetNodePrimaryRoutes(id2), route)

	via := func(c *servertest.TestClient, router string) {
		t.Helper()

		other := "regional-r2"
		if router == other {
			other = "regional-r1"
		}

		c.WaitForCondition(t, c.Name+" reaches the prefix through "+router, 10*time.Second,
			func(nm *netmap.NetworkMap) bool {
				return hasPeerPrimaryRoute(nm, router, route) && !hasPeerPrimaryRoute(nm, other, route)
			})
	}

	via(east, "regional-r1")
	via(west, "regional-r2")
	via(nowhere, "regional-r1")

	// The west router fails: west falls back to the tailnet-wide primary.
	require.True(t, srv.State().SetNodeHealth(id2, false))
	srv.App.Change(change.PolicyChange())
	via(west, "regional-r1")

	// It recovers: west is steered home again.
	require.True(t, srv.State().SetNodeHealth(id2, true))
	srv.App.Change(change.PolicyChange())
	via(west, "regional-r2")

	// A viewer that moves region follows its new region's router.
	setDERPRegion(t, east, 2)
	via(east, "regional-r2")
}
