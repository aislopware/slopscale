package servertest_test

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/require"
	"go4.org/netipx"
	"tailscale.com/tailcfg"
	"tailscale.com/types/logger"
	"tailscale.com/types/netmap"
	"tailscale.com/types/opt"
	"tailscale.com/wgengine/filter"
)

// connectorAnswersDNS runs the check a connector's client makes before it
// answers a peer's DNS query over PeerAPI
// (offersExitNodeOrAppConnectorAndPeerHasAutogroupInternet in
// ipn/ipnlocal/peerapi.go): its packet filter, with 0.0.0.0 and :: among
// the local addresses as updateFilterLocked adds them for a connector,
// must accept TCP from the peer to 0.0.0.0:53. Since 1.104 nothing else
// lets a tagged connector answer; a query that fails the check gets 403.
func connectorAnswersDNS(nm *netmap.NetworkMap, peer netip.Addr) bool {
	var local netipx.IPSetBuilder

	local.Add(netip.IPv4Unspecified())
	local.Add(netip.IPv6Unspecified())

	localNets, err := local.IPSet()
	if err != nil {
		return false
	}

	f := filter.New(nm.PacketFilter, nil, localNets, &netipx.IPSet{}, nil, logger.Discard)

	return f.CheckTCP(peer, netip.IPv4Unspecified(), 53) == filter.Accept
}

// TestAppDNSOnlyWhereTheConnectorAnswers covers the app's split DNS
// against the connector's own packet filter: a machine gets the route to
// a connector's PeerAPI resolver exactly when that connector would answer
// it. A gateway that is the connector's peer through a node-to-node grant
// alone has no such rule, so the app's names stay with its global
// resolver instead of failing against a resolver that refuses it.
func TestAppDNSOnlyWhereTheConnectorAnswers(t *testing.T) {
	t.Parallel()

	const wait = 10 * time.Second

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{
		MagicDNS:    true,
		BaseDomain:  "ts.example",
		Nameservers: types.Nameservers{Global: []string{"1.1.1.1"}},
	}))
	user := srv.CreateUser(t, "appdns-user")

	// The gateway and the connector are peers the way two gateways are in
	// production: a grant from one node to the other, nothing through it.
	setPolicy := func(gatewayInternet string) {
		t.Helper()

		changed, err := srv.State().SetPolicy(fmt.Appendf(nil, `{
			"tagOwners": {
				"tag:connector": ["appdns-user@"],
				"tag:gateway": ["appdns-user@"]
			},
			"grants": [
				{"src": ["autogroup:member"], "dst": ["autogroup:internet"], "ip": ["*"]},
				{"src": ["tag:gateway"], "dst": ["tag:connector"], "ip": ["tcp:443"]},
				{"src": ["tag:connector"], "dst": ["tag:gateway"], "ip": ["tcp:443"]}
				%s
			]
		}`, gatewayInternet))
		require.NoError(t, err)

		if changed {
			changes, err := srv.State().ReloadPolicy()
			require.NoError(t, err)
			srv.App.Change(changes...)
		}
	}

	setPolicy("")

	connector := servertest.NewClient(t, srv, "appdns-connector", servertest.WithTags("tag:connector"))
	gateway := servertest.NewClient(t, srv, "appdns-gateway", servertest.WithTags("tag:gateway"))
	member := servertest.NewClient(t, srv, "appdns-member", servertest.WithUser(user))

	_, c, err := srv.State().CreateAppConnector(types.AppConnector{
		Name:       "office-apps",
		Domains:    []string{"example.com"},
		Connectors: []string{"tag:connector"},
	})
	require.NoError(t, err)
	srv.App.Change(c)

	learned := netip.MustParsePrefix("198.51.100.1/32")

	connector.Direct().SetHostinfo(&tailcfg.Hostinfo{
		BackendLogID: "servertest-" + connector.Name,
		Hostname:     connector.Name,
		AppConnector: opt.NewBool(true),
		RoutableIPs:  []netip.Prefix{learned},
		Services:     []tailcfg.Service{{Proto: tailcfg.PeerAPI4, Port: 41000}},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = connector.Direct().SendUpdate(ctx)

	v4 := func(c *servertest.TestClient) netip.Addr {
		t.Helper()

		for _, p := range c.Netmap().SelfNode.Addresses().All() {
			if p.Addr().Is4() {
				return p.Addr()
			}
		}

		t.Fatalf("%s has no IPv4 address", c.Name)

		return netip.Addr{}
	}

	// expect waits until the connector's filter and the machine's DNS
	// agree with want: the connector answers the machine, and the
	// machine is sent to the connector, or neither.
	expect := func(c *servertest.TestClient, want bool) {
		t.Helper()

		addr := v4(c)

		connector.WaitForCondition(t, fmt.Sprintf("the connector answers %s: %t", c.Name, want), wait,
			func(nm *netmap.NetworkMap) bool { return connectorAnswersDNS(nm, addr) == want })

		c.WaitForCondition(t, fmt.Sprintf("%s is sent to the connector for the app's names: %t", c.Name, want), wait,
			func(nm *netmap.NetworkMap) bool {
				if len(peerAllowedIPs(nm, connector.Name)) == 0 {
					return false
				}

				return (len(nm.DNS.Routes["example.com"]) > 0) == want
			})
	}

	for _, c := range []*servertest.TestClient{connector, gateway, member} {
		c.WaitForCondition(t, c.Name+" has a netmap", wait, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid()
		})
	}

	// routed waits until the machine's view of the connector carries
	// the route the connector learned for the app, or does not.
	routed := func(c *servertest.TestClient, want bool) {
		t.Helper()

		c.WaitForCondition(t, fmt.Sprintf("%s routes the app's address through the connector: %t", c.Name, want), wait,
			func(nm *netmap.NetworkMap) bool {
				allowed := peerAllowedIPs(nm, connector.Name)

				return len(allowed) > 0 && slices.Contains(allowed, learned) == want
			})
	}

	expect(member, true)
	routed(member, true)
	expect(gateway, false)
	routed(gateway, false)

	// A grant to the internet limited to DNS is enough for the connector
	// to answer, so the gateway is sent there: the route follows the
	// connector's filter, not a guess at what the grant is for.
	setPolicy(`, {"src": ["tag:gateway"], "dst": ["autogroup:internet"], "ip": ["tcp:53", "udp:53"]}`)
	expect(gateway, true)
	expect(member, true)

	// The address is routed whatever the ports, as any subnet route is:
	// the connector's filter then drops what the grant leaves out.
	routed(gateway, true)

	// The client checks TCP, so a UDP grant alone is refused.
	setPolicy(`, {"src": ["tag:gateway"], "dst": ["autogroup:internet"], "ip": ["udp:53"]}`)
	expect(gateway, false)

	// Any TCP reach to the internet through the connector carries the
	// DNS rule the control plane synthesises for it.
	setPolicy(`, {"src": ["tag:gateway"], "dst": ["autogroup:internet"], "ip": ["tcp:443"]}`)
	expect(gateway, true)

	setPolicy("")
	expect(gateway, false)
	routed(gateway, false)
	expect(member, true)
	routed(member, true)
}
