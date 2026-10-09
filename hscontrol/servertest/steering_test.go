package servertest_test

import (
	"context"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

const (
	steerOffice tailcfg.DERPRegionID = 900
	steerFra    tailcfg.DERPRegionID = 4
	steerAms    tailcfg.DERPRegionID = 14
)

// setNetInfo tells the server the client is homed in home and measured
// the given IPv4 round trips in milliseconds, as a client does after a
// netcheck.
func setNetInfo(
	t *testing.T,
	c *servertest.TestClient,
	home tailcfg.DERPRegionID,
	ms map[tailcfg.DERPRegionID]float64,
) {
	t.Helper()

	latency := make(map[string]float64, len(ms))
	for region, v := range ms {
		latency[strconv.Itoa(int(region))+"-v4"] = v / 1000
	}

	c.Direct().SetNetInfo(&tailcfg.NetInfo{PreferredDERP: home, DERPLatency: latency})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = c.Direct().SendUpdate(ctx)
}

// The round trips of the incident's machines: a laptop in Amsterdam whose
// netcheck reported its three nearest regions, one in the Hanoi office.
var (
	inAmsterdam = map[tailcfg.DERPRegionID]float64{steerAms: 6.4, steerFra: 10.8, 18: 20.4}
	inHanoi     = map[tailcfg.DERPRegionID]float64{steerOffice: 2, steerFra: 203, steerAms: 225.7}
)

// TestRouterSteering covers the incident: an internet address that an
// office connector in Hanoi and an EU connector in Frankfurt both route.
// The office connector has the lower ID, so it is the tailnet-wide
// primary, and before steering a laptop in Amsterdam (a region neither
// connector is homed in) reached the address through Hanoi. Now it goes
// through Frankfurt, the Hanoi laptop through the office, a laptop that
// measured nothing through the primary, and a laptop that flies home is
// re-steered. With two connectors equally near, Amsterdam laptops are
// spread over both for an internet address, but a private prefix keeps
// one primary.
func TestRouterSteering(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	user := srv.CreateUser(t, "steering")

	cloudflare := netip.MustParsePrefix("104.20.39.105/32")

	office := servertest.NewClient(t, srv, "steer-office", servertest.WithUser(user))
	eu := servertest.NewClient(t, srv, "steer-eu", servertest.WithUser(user))
	ams := servertest.NewClient(t, srv, "steer-ams", servertest.WithUser(user))
	hanoi := servertest.NewClient(t, srv, "steer-hanoi", servertest.WithUser(user))
	unmeasured := servertest.NewClient(t, srv, "steer-unmeasured", servertest.WithUser(user))

	for _, c := range []*servertest.TestClient{office, eu, ams, hanoi, unmeasured} {
		c.WaitForPeers(t, 4, 10*time.Second)
	}

	officeID := advertiseAndApproveRoute(t, srv, office, cloudflare)
	advertiseAndApproveRoute(t, srv, eu, cloudflare)

	setNetInfo(t, office, steerOffice, map[tailcfg.DERPRegionID]float64{steerOffice: 0.5})
	setNetInfo(t, eu, steerFra, map[tailcfg.DERPRegionID]float64{steerFra: 1.3})
	setNetInfo(t, ams, steerAms, inAmsterdam)
	setNetInfo(t, hanoi, steerOffice, inHanoi)

	require.Contains(t, srv.State().GetNodePrimaryRoutes(officeID), cloudflare, "office is the tailnet-wide primary")

	via := func(c *servertest.TestClient, router, other string, route netip.Prefix) {
		t.Helper()

		c.WaitForCondition(t, c.Name+" reaches "+route.String()+" through "+router, 10*time.Second,
			func(nm *netmap.NetworkMap) bool {
				return hasPeerPrimaryRoute(nm, router, route) && !hasPeerPrimaryRoute(nm, other, route)
			})
	}

	via(ams, "steer-eu", "steer-office", cloudflare)
	via(hanoi, "steer-office", "steer-eu", cloudflare)
	via(unmeasured, "steer-office", "steer-eu", cloudflare)

	// The Amsterdam laptop flies to Hanoi.
	setNetInfo(t, ams, steerOffice, inHanoi)
	via(ams, "steer-office", "steer-eu", cloudflare)

	// The office connector moves to Frankfurt: both are now equally near
	// Amsterdam. The Amsterdam laptops spread over them for the internet
	// address and all keep the primary for a private prefix.
	lan := netip.MustParsePrefix("10.77.0.0/24")

	setNetInfo(t, office, steerFra, map[tailcfg.DERPRegionID]float64{steerFra: 0.9})

	for _, c := range []*servertest.TestClient{office, eu} {
		c.Direct().SetHostinfo(&tailcfg.Hostinfo{
			BackendLogID: "servertest-" + c.Name,
			Hostname:     c.Name,
			RoutableIPs:  []netip.Prefix{cloudflare, lan},
		})

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_ = c.Direct().SendUpdate(ctx)

		cancel()

		_, rc, err := srv.State().SetApprovedRoutes(findNodeID(t, srv, c.Name), []netip.Prefix{cloudflare, lan})
		require.NoError(t, err)
		srv.App.Change(rc)
	}

	laptops := make([]*servertest.TestClient, 0, 8)
	laptops = append(laptops, ams)

	for i := range 7 {
		c := servertest.NewClient(t, srv, "steer-ams-"+strconv.Itoa(i), servertest.WithUser(user))
		laptops = append(laptops, c)
	}

	for _, c := range laptops {
		c.WaitForPeers(t, 3+len(laptops), 10*time.Second)
		setNetInfo(t, c, steerAms, inAmsterdam)
	}

	for _, c := range laptops {
		via(c, "steer-office", "steer-eu", lan)
	}

	used := map[string]int{}

	for _, c := range laptops {
		c.WaitForCondition(t, c.Name+" routes the address through one connector", 10*time.Second,
			func(nm *netmap.NetworkMap) bool {
				return hasPeerPrimaryRoute(nm, "steer-office", cloudflare) !=
					hasPeerPrimaryRoute(nm, "steer-eu", cloudflare)
			})

		if hasPeerPrimaryRoute(c.Netmap(), "steer-eu", cloudflare) {
			used["steer-eu"]++
		} else {
			used["steer-office"]++
		}
	}

	assert.Len(t, used, 2, "the laptops are spread over both connectors: %v", used)
}

// TestExitNodeSteering covers global exit nodes with a priority: the
// operator put the office first (30) and the EU node last (5). A laptop
// in Amsterdam sees the EU node above the office, so its client's traffic
// steering picks Frankfurt; a laptop in Hanoi keeps the operator's order.
func TestExitNodeSteering(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	user := srv.CreateUser(t, "exit-steering")

	office := servertest.NewClient(t, srv, "exit-office", servertest.WithUser(user))
	eu := servertest.NewClient(t, srv, "exit-eu", servertest.WithUser(user))
	ams := servertest.NewClient(t, srv, "exit-ams", servertest.WithUser(user))
	hanoi := servertest.NewClient(t, srv, "exit-hanoi", servertest.WithUser(user))

	for _, c := range []*servertest.TestClient{office, eu, ams, hanoi} {
		c.WaitForPeers(t, 3, 10*time.Second)
	}

	mark := func(c *servertest.TestClient, region tailcfg.DERPRegionID, priority int) {
		c.Direct().SetHostinfo(&tailcfg.Hostinfo{
			BackendLogID: "servertest-" + c.Name,
			Hostname:     c.Name,
			RoutableIPs:  tsaddr.ExitRoutes(),
		})
		setNetInfo(t, c, region, map[tailcfg.DERPRegionID]float64{region: 1})

		_, rc, err := srv.State().SetGlobalExitNode(findNodeID(t, srv, c.Name), true, &priority)
		require.NoError(t, err)
		srv.App.Change(rc)
	}

	mark(office, steerOffice, 30)
	mark(eu, steerFra, 5)

	setNetInfo(t, ams, steerAms, inAmsterdam)
	setNetInfo(t, hanoi, steerOffice, inHanoi)

	ams.WaitForCondition(t, "the EU exit node ranks first in Amsterdam", 10*time.Second,
		func(nm *netmap.NetworkMap) bool {
			return peerPriority(nm, "exit-eu") > peerPriority(nm, "exit-office") &&
				peerPriority(nm, "exit-office") == 30
		})
	hanoi.WaitForCondition(t, "the office exit node ranks first in Hanoi", 10*time.Second,
		func(nm *netmap.NetworkMap) bool {
			return peerPriority(nm, "exit-office") > peerPriority(nm, "exit-eu") &&
				peerPriority(nm, "exit-eu") == 5
		})

	// The Amsterdam laptop flies to Hanoi and follows the operator's order.
	setNetInfo(t, ams, steerOffice, inHanoi)
	ams.WaitForCondition(t, "the office exit node ranks first after the move", 10*time.Second,
		func(nm *netmap.NetworkMap) bool {
			return peerPriority(nm, "exit-office") > peerPriority(nm, "exit-eu")
		})

	// The operator's own view of the nodes is untouched.
	node, ok := srv.State().GetNodeByID(findNodeID(t, srv, "exit-eu"))
	require.True(t, ok)
	assert.Equal(t, 5, node.ExitNodePriority())
}
