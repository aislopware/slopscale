package servertest_test

import (
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/netmap"
)

const servicesWait = 15 * time.Second

// serviceHostMapping reads the service-host cap of a netmap's self node.
func serviceHostMapping(nm *netmap.NetworkMap) tailcfg.ServiceIPMappings {
	if nm == nil || !nm.SelfNode.Valid() {
		return nil
	}

	mappings, err := tailcfg.UnmarshalNodeCapViewJSON[tailcfg.ServiceIPMappings](
		nm.SelfNode.CapMap(),
		nodecap.ServiceHost,
	)
	if err != nil || len(mappings) == 0 {
		return nil
	}

	return mappings[0]
}

// peerAllowedIPs returns the AllowedIPs of the named peer.
func peerAllowedIPs(nm *netmap.NetworkMap, hostname string) []netip.Prefix {
	if nm == nil {
		return nil
	}

	for _, p := range nm.Peers {
		if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == hostname {
			return p.AllowedIPs().AsSlice()
		}
	}

	return nil
}

func hasRecord(nm *netmap.NetworkMap, name, value string) bool {
	if nm == nil || nm.DNS.ExtraRecords == nil {
		return false
	}

	for _, r := range nm.DNS.ExtraRecords {
		if r.Name == name && r.Value == value {
			return true
		}
	}

	return false
}

// TestVIPServices proves the service round trip: an operator creates a
// service and it gets addresses; a tagged node reports the service in
// its serve configuration and the server fetches the report over c2n;
// once approved, the host gets the service-host mapping, its peers route
// the service's addresses to it, MagicDNS answers the service name and
// every node that can reach it lists it; a policy rule opens it by
// name and an auto-approver approves a second host; deleting the
// service takes all of it back. The subtests build on one another.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestVIPServices(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "svc.test"}))
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "alice")
	ownerKey := srv.CreateAPIKey(t, owner)

	webPorts := []tailcfg.ProtoPortRange{{Proto: 6, Ports: tailcfg.PortRange{First: 443, Last: 443}}}
	reported := tailcfg.VIPService{Name: "svc:web", Ports: webPorts, Active: true}

	host := servertest.NewClient(t, srv, "web1", servertest.WithTags("tag:web"), servertest.WithVIPServices(reported))
	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	host.WaitForPeerCount(t, 1, servicesWait)
	laptop.WaitForPeerCount(t, 1, servicesWait)

	var addrs []netip.Prefix

	t.Run("create gives the service addresses", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/services",
			map[string]any{"name": "web", "displayName": "Web", "ports": []string{"tcp:443"}})
		require.Equal(t, http.StatusCreated, status, body)
		assert.Equal(t, "svc:web", field(t, body, "service", "name"))
		assert.Equal(t, "web.svc.test", field(t, body, "service", "dnsName"))

		for _, raw := range field(t, body, "service", "addresses").([]any) { //nolint:forcetypeassert // JSON list
			addr := netip.MustParseAddr(raw.(string)) //nolint:forcetypeassert // JSON string
			addrs = append(addrs, netip.PrefixFrom(addr, addr.BitLen()))
		}

		require.Len(t, addrs, 2)

		status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/services", map[string]any{"name": "web"})
		assert.Equal(t, http.StatusConflict, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/services", map[string]any{"name": "not a label"})
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("the server fetches what the host reports", func(t *testing.T) {
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/service/svc:web", nil)
			if !assert.Equal(c, http.StatusOK, status) {
				return
			}

			hosts, _ := field(t, body, "service", "hosts").([]any)
			if !assert.Len(c, hosts, 1) {
				return
			}

			assert.Equal(c, host.NodeIDString(), field(t, body, "service", "hosts", "0", "nodeId"))
			assert.Equal(c, true, field(t, body, "service", "hosts", "0", "announced"))
			assert.Equal(c, true, field(t, body, "service", "hosts", "0", "active"))
			assert.Equal(c, false, field(t, body, "service", "hosts", "0", "approved"))
			assert.Equal(c, []any{"tcp:443"}, field(t, body, "service", "hosts", "0", "ports"))
		}, servicesWait, 100*time.Millisecond)

		// Nothing reaches the peers before approval.
		assert.Nil(t, serviceHostMapping(host.Netmap()))
		assert.NotContains(t, peerAllowedIPs(laptop.Netmap(), "web1"), addrs[0])
	})

	t.Run("a user-owned node cannot host", func(t *testing.T) {
		status, _ := apiCall(
			t,
			client,
			ownerKey,
			http.MethodPost,
			v1+"/node/"+laptop.NodeIDString()+"/approve_services",
			map[string]any{"services": []string{"svc:web"}},
		)
		assert.Equal(t, http.StatusBadRequest, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/node/"+host.NodeIDString()+"/approve_services",
			map[string]any{"services": []string{"svc:nope"}})
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("approval puts the service on the host", func(t *testing.T) {
		status, body := apiCall(
			t,
			client,
			ownerKey,
			http.MethodPost,
			v1+"/node/"+host.NodeIDString()+"/approve_services",
			map[string]any{"services": []string{"web"}},
		)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"svc:web"}, field(t, body, "node", "approvedServices"))
		assert.Equal(t, "svc:web", field(t, body, "node", "announcedServices", "0", "name"))

		host.WaitForCondition(t, "service-host mapping", servicesWait, func(nm *netmap.NetworkMap) bool {
			return len(serviceHostMapping(nm)["svc:web"]) == 2
		})
		assert.ElementsMatch(
			t,
			[]netip.Addr{addrs[0].Addr(), addrs[1].Addr()},
			serviceHostMapping(host.Netmap())["svc:web"],
		)

		host.WaitForCondition(t, "self AllowedIPs carry the service", servicesWait, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid() &&
				nm.SelfNode.AllowedIPs().ContainsFunc(func(p netip.Prefix) bool { return p == addrs[0] })
		})
		assert.Empty(t, host.Netmap().SelfNode.PrimaryRoutes().AsSlice(), "a service address is not a subnet route")

		laptop.WaitForCondition(t, "peer routes the service", servicesWait, func(nm *netmap.NetworkMap) bool {
			ips := peerAllowedIPs(nm, "web1")

			return len(ips) > 0 && hasRecord(nm, "web.svc.test", addrs[0].Addr().String()) &&
				nm.Services()["svc:web"].Name == "svc:web"
		})
		assert.Subset(t, peerAllowedIPs(laptop.Netmap(), "web1"), addrs)
		assert.Equal(t, "Web", laptop.Netmap().Services()["svc:web"].DisplayName)
		assert.Equal(t, webPorts, laptop.Netmap().Services()["svc:web"].Ports)
		assert.True(t, laptop.Netmap().SelfNode.CapMap().Contains(nodecap.ServicesInDesktopClients))

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/service/svc:web", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "service", "hosts", "0", "approved"))
		assert.Equal(t, true, field(t, body, "service", "hosts", "0", "primary"))
	})

	t.Run("the policy opens a service by name and approves hosts", func(t *testing.T) {
		changed, err := srv.State().SetPolicy([]byte(`{
			"tagOwners": {"tag:web": ["alice@"]},
			"autoApprovers": {"services": {"svc:web": ["tag:web"]}},
			"grants": [
				{"src": ["alice@"], "dst": ["svc:web"], "ip": ["tcp:443"]},
				{"src": ["tag:web"], "dst": ["*"], "ip": ["*"]}
			]
		}`))
		require.NoError(t, err)
		require.True(t, changed)

		changes, err := srv.State().ReloadPolicy()
		require.NoError(t, err)
		srv.App.Change(changes...)

		laptop.WaitForCondition(t, "filter names the service", servicesWait, func(nm *netmap.NetworkMap) bool {
			for _, rule := range nm.PacketFilter {
				for _, dst := range rule.Dsts {
					if dst.Net.Contains(addrs[0].Addr()) {
						return true
					}
				}
			}

			return false
		})

		web2 := servertest.NewClient(
			t,
			srv,
			"web2",
			servertest.WithTags("tag:web"),
			servertest.WithVIPServices(reported),
		)
		web2.WaitForPeerCount(t, 2, servicesWait)

		web2.WaitForCondition(t, "auto-approved host gets the mapping", servicesWait, func(nm *netmap.NetworkMap) bool {
			return len(serviceHostMapping(nm)["svc:web"]) == 2
		})

		// One host at a time carries the addresses, the first approved
		// one while it is online.
		laptop.WaitForCondition(t, "both hosts visible", servicesWait, func(nm *netmap.NetworkMap) bool {
			return len(peerAllowedIPs(nm, "web2")) > 0
		})
		assert.Subset(t, peerAllowedIPs(laptop.Netmap(), "web1"), addrs)
		assert.NotContains(t, peerAllowedIPs(laptop.Netmap(), "web2"), addrs[0])

		host.Disconnect(t)

		laptop.WaitForCondition(t, "the other host takes over", servicesWait, func(nm *netmap.NetworkMap) bool {
			ips := peerAllowedIPs(nm, "web2")

			return len(ips) > 0 && ips[len(ips)-1] == addrs[1] || (len(ips) > 2 && ips[0] == addrs[0])
		})
		assert.Subset(t, peerAllowedIPs(laptop.Netmap(), "web2"), addrs)

		host.Reconnect(t)
	})

	t.Run("delete takes everything back", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/service/svc:web", nil)
		require.Equal(t, http.StatusNoContent, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/service/svc:web", nil)
		assert.Equal(t, http.StatusNotFound, status)

		laptop.WaitForCondition(t, "peers drop the service", servicesWait, func(nm *netmap.NetworkMap) bool {
			for _, p := range nm.Peers {
				if p.AllowedIPs().ContainsFunc(func(pfx netip.Prefix) bool { return pfx == addrs[0] }) {
					return false
				}
			}

			return len(nm.Services()) == 0 && !hasRecord(nm, "web.svc.test", addrs[0].Addr().String())
		})

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+host.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{}, field(t, body, "node", "approvedServices"))

		// The name is free again.
		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/services", map[string]any{"name": "web"})
		require.Equal(t, http.StatusCreated, status, body)
		assert.Len(t, field(t, body, "service", "addresses"), 2)
	})
}
