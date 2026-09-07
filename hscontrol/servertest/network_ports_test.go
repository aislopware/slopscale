package servertest_test

import (
	"maps"
	"net/http"
	"net/netip"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/tsaddr"
	"tailscale.com/types/netmap"
	"tailscale.com/wgengine/filter/filtertype"
)

// TestNetworkPorts proves a network can narrow what its groups reach
// behind the routers to a protocol and ports: the router's packet
// filter admits the group on those ports alone, widening the network
// reopens every port, and the API refuses ports on a protocol without
// them. The subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestNetworkPorts(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "netports-owner")
	eng := srv.CreateUser(t, "netports-eng")
	ownerKey := srv.CreateAPIKey(t, owner)

	router := servertest.NewClient(t, srv, routerName, servertest.WithUser(owner))
	engNode := servertest.NewClient(t, srv, "eng-laptop", servertest.WithUser(eng))

	for _, c := range []*servertest.TestClient{router, engNode} {
		c.WaitForPeers(t, 1, networkWait)
	}

	office := netip.MustParsePrefix("10.10.0.0/24")
	routerID := findNodeID(t, srv, routerName)

	advertise(t, router, routerName, office)

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{
		"name": "Engineering", "userIds": []string{userID(eng)},
	})
	require.Equal(t, http.StatusOK, status, body)

	engGroupID, ok := field(t, body, "group", "id").(string)
	require.True(t, ok)

	// A rule makes the tailnet enforce; without one a network alone
	// leaves the filter wide open.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-rule", map[string]any{
		"name":     "Engineers reach the router",
		"protocol": "tcp",
		"ports":    "443",
		"sourceGroupIds": []string{
			engGroupID,
		},
		"destinationGroupIds": []string{groupNamed(t, client, ownerKey, v1, "All")},
	})
	require.Equal(t, http.StatusOK, status, body)

	var networkID string

	t.Run("ports need a protocol that carries them", func(t *testing.T) {
		for _, bad := range []map[string]any{
			{"protocol": "icmp", "ports": "22"},
			{"protocol": "gre"},
			{"protocol": "tcp", "ports": "22-"},
		} {
			body := map[string]any{
				"name": "Office", "prefixes": []string{office.String()},
				"routerNodeIds": []string{routerID.String()}, "groupIds": []string{engGroupID},
			}
			maps.Copy(body, bad)

			status, resp := apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", body)
			assert.Equal(t, http.StatusBadRequest, status, resp)
		}
	})

	t.Run("a network narrowed to tcp 22 admits the group on that port alone", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name": "Office", "prefixes": []string{office.String()}, "protocol": "TCP", "ports": "22, 8000-8100",
			"routerNodeIds": []string{routerID.String()}, "groupIds": []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "tcp", field(t, body, "network", "protocol"))
		assert.Equal(t, "22, 8000-8100", field(t, body, "network", "ports"))

		id, ok := field(t, body, "network", "id").(string)
		require.True(t, ok)

		networkID = id

		router.WaitForCondition(t, "filter narrowed to the ports", networkWait, func(nm *netmap.NetworkMap) bool {
			ports := officePorts(nm, office)

			return len(ports) == 2 && ports[0] == (filtertype.PortRange{First: 22, Last: 22}) &&
				ports[1] == (filtertype.PortRange{First: 8000, Last: 8100})
		})

		engNode.WaitForCondition(t, "route reaches the engineer", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, office)
		})
	})

	t.Run("widening the network reopens every port", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1+"/network/"+networkID, map[string]any{
			"name": "Office", "prefixes": []string{office.String()},
			"routerNodeIds": []string{routerID.String()}, "groupIds": []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "all", field(t, body, "network", "protocol"))
		assert.Empty(t, field(t, body, "network", "ports"))

		router.WaitForCondition(t, "filter open on every port", networkWait, func(nm *netmap.NetworkMap) bool {
			ports := officePorts(nm, office)

			return len(ports) == 1 && ports[0] == filtertype.AllPorts
		})
	})
}

// officePorts collects the port ranges the netmap's filter allows toward
// the prefix, skipping the tailnet's own addresses.
func officePorts(nm *netmap.NetworkMap, prefix netip.Prefix) []filtertype.PortRange {
	var ports []filtertype.PortRange

	for _, rule := range nm.PacketFilter {
		for _, dst := range rule.Dsts {
			if dst.Net == prefix && !tsaddr.IsTailscaleIP(dst.Net.Addr()) {
				ports = append(ports, dst.Ports)
			}
		}
	}

	return ports
}
