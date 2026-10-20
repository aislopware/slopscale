package servertest_test

import (
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

// serviceVia returns the peer whose AllowedIPs carry every one of addrs,
// or "" when none or more than one does.
func serviceVia(nm *netmap.NetworkMap, addrs []netip.Prefix) string {
	if nm == nil {
		return ""
	}

	var via []string

	for _, p := range nm.Peers {
		ips := p.AllowedIPs().AsSlice()
		if !slices.ContainsFunc(addrs, func(a netip.Prefix) bool { return !slices.Contains(ips, a) }) {
			via = append(via, p.Hostinfo().Hostname())
		}
	}

	if len(via) != 1 {
		return ""
	}

	return via[0]
}

// TestServiceHostSteering covers how viewers pick among the hosts of one
// service: per viewer by round trip under regional routing, never an
// unhealthy host, the same host for everyone under active-passive
// failover, and a host that comes back does not take the traffic back.
// The hosts are an office in Hanoi (the lower ID) and a node in
// Frankfurt; the viewers are laptops in Hanoi and Amsterdam and one that
// measured nothing.
func TestServiceHostSteering(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "svc.test"}))
	user := srv.CreateUser(t, "svc-steering")

	svc, c, err := srv.State().CreateVIPService("web", "Web", "", []string{"tcp:443"})
	require.NoError(t, err)
	srv.App.Change(c)

	addrs := svc.Prefixes()
	require.Len(t, addrs, 2)

	reported := tailcfg.VIPService{
		Name:   svc.Name,
		Ports:  []tailcfg.ProtoPortRange{{Proto: 6, Ports: tailcfg.PortRange{First: 443, Last: 443}}},
		Active: true,
	}

	office := servertest.NewClient(t, srv, "svc-office", servertest.WithTags("tag:web"),
		servertest.WithVIPServices(reported))
	eu := servertest.NewClient(t, srv, "svc-eu", servertest.WithTags("tag:web"),
		servertest.WithVIPServices(reported))
	ams := servertest.NewClient(t, srv, "svc-ams", servertest.WithUser(user))
	hanoi := servertest.NewClient(t, srv, "svc-hanoi", servertest.WithUser(user))
	unmeasured := servertest.NewClient(t, srv, "svc-unmeasured", servertest.WithUser(user))

	viewers := []*servertest.TestClient{ams, hanoi, unmeasured}

	for _, cl := range append([]*servertest.TestClient{office, eu}, viewers...) {
		cl.WaitForPeers(t, 4, servicesWait)
	}

	officeID, euID := findNodeID(t, srv, "svc-office"), findNodeID(t, srv, "svc-eu")
	require.Less(t, officeID, euID)

	setNetInfo(t, office, steerOffice, map[tailcfg.DERPRegionID]float64{steerOffice: 0.5})
	setNetInfo(t, eu, steerFra, map[tailcfg.DERPRegionID]float64{steerFra: 1.3})
	setNetInfo(t, ams, steerAms, inAmsterdam)
	setNetInfo(t, hanoi, steerOffice, inHanoi)

	via := func(cl *servertest.TestClient, host string) {
		t.Helper()

		cl.WaitForCondition(t, cl.Name+" reaches the service through "+host, servicesWait,
			func(nm *netmap.NetworkMap) bool {
				return serviceVia(nm, addrs) == host && hasRecord(nm, "web.svc.test", addrs[0].Addr().String())
			})
	}

	hostIs := func(want types.NodeID) {
		t.Helper()

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			assert.Equal(c, want, srv.State().ServiceHosts()[svc.Name])
		}, servicesWait, 20*time.Millisecond)
	}

	approve := func(id types.NodeID) {
		t.Helper()

		_, approved, approveErr := srv.State().SetApprovedServices(id, []string{string(svc.Name)})
		require.NoError(t, approveErr)
		srv.App.Change(approved)
	}

	// The office hosts the service first; the second host does not take
	// it over when it arrives.
	approve(officeID)
	hostIs(officeID)
	approve(euID)

	// Regional routing: each laptop uses the host nearest it; the one
	// without measurements uses the tailnet-wide host, the office.
	hostIs(officeID)
	via(ams, "svc-eu")
	via(hanoi, "svc-office")
	via(unmeasured, "svc-office")

	// The prober finds the office not answering: nobody is sent to it.
	setHealth := func(id types.NodeID, healthy bool) {
		t.Helper()

		if srv.State().BatchSetNodeHealth(map[types.NodeID]bool{id: healthy}) {
			srv.App.Change(change.PolicyChange())
		}
	}

	setHealth(officeID, false)
	hostIs(euID)

	for _, cl := range viewers {
		via(cl, "svc-eu")
	}

	// The office answers again: the Hanoi laptop goes back to its nearest
	// host, but the tailnet-wide host stays in Frankfurt.
	setHealth(officeID, true)
	via(hanoi, "svc-office")
	via(ams, "svc-eu")
	via(unmeasured, "svc-eu")
	hostIs(euID)

	// Active-passive failover: everyone uses the tailnet-wide host.
	c, err = srv.State().SetRouteSelection(types.RouteSelectionActivePassive)
	require.NoError(t, err)
	srv.App.Change(c)

	for _, cl := range viewers {
		via(cl, "svc-eu")
	}

	// Frankfurt goes away and everyone fails over to the office; when it
	// comes back nobody is moved again.
	eu.Disconnect(t)

	for _, cl := range viewers {
		via(cl, "svc-office")
	}

	eu.Reconnect(t)

	for _, cl := range viewers {
		cl.WaitForCondition(t, cl.Name+" sees the Frankfurt host back", servicesWait,
			func(nm *netmap.NetworkMap) bool {
				return slices.ContainsFunc(nm.Peers, func(p tailcfg.NodeView) bool {
					return p.Hostinfo().Hostname() == "svc-eu" && p.Online().Get()
				})
			})
		via(cl, "svc-office")
	}

	hostIs(officeID)
}
