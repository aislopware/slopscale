package servertest_test

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/appctype"
	"tailscale.com/types/netmap"
	"tailscale.com/types/opt"
)

// TestApps covers the app connector round trip: a connector node registers with
// the app connector flag, an app is created naming its tag, the node receives
// the tailscale.com/app-connectors capability, learned routes (/32) are auto-approved
// while subnet routes (/24) remain pending, app inspects learned/pending route counts,
// and update/delete/validation behave as expected.
func TestApps(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "apps-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	connector := servertest.NewClient(
		t,
		srv,
		"connector",
		servertest.WithTags("tag:connector"),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.AppConnector = opt.NewBool(true)
		}),
	)

	connector.WaitForCondition(t, "self node valid", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})

	// Validation: bad domain returns 400 Bad Request.
	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apps", map[string]any{
		"name":    "bad-app",
		"domains": []string{"*.foo.*.bar"},
	})
	assert.Equal(t, http.StatusBadRequest, status, body)

	// Create app.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apps", map[string]any{
		"name":       "test-app",
		"domains":    []string{"example.com"},
		"connectors": []string{"tag:connector"},
	})
	require.Equal(t, http.StatusCreated, status, body)
	appID, ok := field(t, body, "app", "id").(string)
	require.True(t, ok)

	// Assert connector's self node caps carry tailscale.com/app-connectors.
	const appCap = "tailscale.com/app-connectors"

	connector.WaitForCondition(t, "connector carries app cap", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid() && nm.SelfNode.CapMap().Contains(appCap)
	})

	attrs, err := tailcfg.UnmarshalNodeCapViewJSON[appctype.AppConnectorAttr](
		connector.Netmap().SelfNode.CapMap(),
		appCap,
	)
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	assert.Equal(t, "test-app", attrs[0].Name)
	assert.Equal(t, []string{"example.com"}, attrs[0].Domains)

	// Connector advertises a /32 route and a /24 route.
	r32 := netip.MustParsePrefix("198.51.100.1/32")
	r24 := netip.MustParsePrefix("10.0.0.0/24")

	connector.Direct().SetHostinfo(&tailcfg.Hostinfo{
		BackendLogID: "servertest-connector",
		Hostname:     "connector",
		AppConnector: opt.NewBool(true),
		RoutableIPs:  []netip.Prefix{r32, r24},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = connector.Direct().SendUpdate(ctx)

	// The /32 route is auto-approved without operator intervention, while /24 stays pending.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		st, nodeBody := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+connector.NodeIDString(), nil)
		if !assert.Equal(c, http.StatusOK, st) {
			return
		}

		approved, hasApproved := field(t, nodeBody, "node", "approvedRoutes").([]any)
		if !assert.True(c, hasApproved) {
			return
		}

		assert.Contains(c, approved, "198.51.100.1/32")
		assert.NotContains(c, approved, "10.0.0.0/24")
	}, 10*time.Second, 100*time.Millisecond)

	// GET /api/v1/app/{id} lists the node with learnedRoutes 1 and pending 1.
	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/app/"+appID, nil)
	require.Equal(t, http.StatusOK, status, body)
	nodes, ok := field(t, body, "app", "nodes").([]any)
	require.True(t, ok)
	require.Len(t, nodes, 1)
	assert.Equal(t, connector.NodeIDString(), field(t, nodes, "0", "nodeId"))
	assert.InDelta(t, 1.0, field(t, nodes, "0", "learnedRoutes"), 0.0)
	assert.InDelta(t, 1.0, field(t, nodes, "0", "pending"), 0.0)

	// Update app works.
	status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/app/"+appID, map[string]any{
		"name":       "test-app-renamed",
		"domains":    []string{"updated.example.com"},
		"connectors": []string{"tag:connector"},
	})
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "test-app-renamed", field(t, body, "app", "name"))
	assert.Equal(t, []any{"updated.example.com"}, field(t, body, "app", "domains"))

	// Delete app works.
	status, _ = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/app/"+appID, nil)
	assert.Equal(t, http.StatusNoContent, status)

	status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/app/"+appID, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

// setEndpoints reports the client's endpoints to the server the way
// magicsock does after a netcheck: the NAT address STUN saw first, then
// the LAN address.
func setEndpoints(t *testing.T, c *servertest.TestClient, stun, lan string) {
	t.Helper()

	c.Direct().SetEndpoints([]tailcfg.Endpoint{
		{Addr: netip.MustParseAddrPort(stun), Type: tailcfg.EndpointSTUN},
		{Addr: netip.MustParseAddrPort(lan), Type: tailcfg.EndpointLocal},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = c.Direct().SendUpdate(ctx)
}

// TestAppConnectorBypassSameEgress covers a machine behind the same public
// address as a connector: it goes straight out, so its view of the
// connector carries no public app routes and its app DNS skips the
// connector, while a machine elsewhere still routes through it. The
// choice follows the machine and the connector when either moves.
func TestAppConnectorBypassSameEgress(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{
		MagicDNS:    true,
		BaseDomain:  "ts.example",
		Nameservers: types.Nameservers{Global: []string{"1.1.1.1"}},
	}))
	user := srv.CreateUser(t, "bypass-user")

	learned := netip.MustParsePrefix("198.51.100.1/32")
	static := netip.MustParsePrefix("203.0.113.0/24")
	private := netip.MustParsePrefix("10.9.0.0/24")

	connector := servertest.NewClient(t, srv, "bypass-connector", servertest.WithTags("tag:connector"))
	office := servertest.NewClient(t, srv, "bypass-office", servertest.WithUser(user))
	home := servertest.NewClient(t, srv, "bypass-home", servertest.WithUser(user))

	for _, c := range []*servertest.TestClient{connector, office, home} {
		c.WaitForPeers(t, 2, 10*time.Second)
	}

	setEndpoints(t, connector, "192.0.2.10:41641", "192.168.1.5:41641")
	setEndpoints(t, office, "192.0.2.10:50123", "192.168.1.20:41641")
	setEndpoints(t, home, "192.0.2.99:41641", "192.168.1.20:41641")

	_, c, err := srv.State().CreateAppConnector(types.AppConnector{
		Name:       "office-apps",
		Domains:    []string{"example.com"},
		Connectors: []string{"tag:connector"},
		Routes:     []netip.Prefix{static, private},
	})
	require.NoError(t, err)
	srv.App.Change(c)

	connector.Direct().SetHostinfo(&tailcfg.Hostinfo{
		BackendLogID: "servertest-" + connector.Name,
		Hostname:     connector.Name,
		AppConnector: opt.NewBool(true),
		RoutableIPs:  []netip.Prefix{learned, static, private},
		Services:     []tailcfg.Service{{Proto: tailcfg.PeerAPI4, Port: 41000}},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = connector.Direct().SendUpdate(ctx)

	all := []netip.Prefix{learned, static, private}

	routesThrough := func(c *servertest.TestClient, want []netip.Prefix, dns bool) {
		t.Helper()

		c.WaitForCondition(t, fmt.Sprintf("%s routes %v through the connector, app DNS %t", c.Name, want, dns),
			10*time.Second, func(nm *netmap.NetworkMap) bool {
				var primary []netip.Prefix

				for _, p := range nm.Peers {
					if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == connector.Name {
						primary = p.PrimaryRoutes().AsSlice()
					}
				}

				allowed := peerAllowedIPs(nm, connector.Name)

				for _, r := range all {
					if slices.Contains(want, r) != slices.Contains(primary, r) ||
						slices.Contains(want, r) != slices.Contains(allowed, r) {
						return false
					}
				}

				return (len(nm.DNS.Routes["example.com"]) > 0) == dns
			})
	}

	// The office machine shares the connector's NAT address: it keeps
	// only the private prefix, which that address says nothing about.
	routesThrough(office, []netip.Prefix{private}, false)
	routesThrough(home, all, true)

	// Taken to a café, it goes through the connector again.
	setEndpoints(t, office, "192.0.2.50:50123", "10.0.0.7:41641")
	routesThrough(office, all, true)

	// Back at the office, it goes straight out again.
	setEndpoints(t, office, "192.0.2.10:50123", "192.168.1.20:41641")
	routesThrough(office, []netip.Prefix{private}, false)

	// With an exit node it goes out from the exit node's address, so it
	// needs the connector even in the office.
	useExitNode := func(id tailcfg.StableNodeID) {
		office.Direct().SetHostinfo(&tailcfg.Hostinfo{
			BackendLogID: "servertest-" + office.Name,
			Hostname:     office.Name,
			ExitNodeID:   id,
		})

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		_ = office.Direct().SendUpdate(ctx)
	}

	useExitNode("exit-elsewhere")
	routesThrough(office, all, true)

	useExitNode("")
	routesThrough(office, []netip.Prefix{private}, false)

	// The connector moves behind the home machine's address: now the
	// home machine skips it and the office machine needs it.
	setEndpoints(t, connector, "192.0.2.99:41641", "192.168.1.5:41641")
	routesThrough(home, []netip.Prefix{private}, false)
	routesThrough(office, all, true)
}
