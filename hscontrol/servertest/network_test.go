package servertest_test

import (
	"context"
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
	"tailscale.com/types/netmap"
)

const networkWait = 10 * time.Second

const routerName = "office-router"

// routerHasRoute reports whether the client's netmap shows the router
// with the prefix among its allowed IPs.
func routerHasRoute(nm *netmap.NetworkMap, prefix netip.Prefix) bool {
	for _, p := range nm.Peers {
		if p.Hostinfo().Hostname() == routerName {
			return slices.Contains(p.AllowedIPs().AsSlice(), prefix)
		}
	}

	return false
}

func advertise(t *testing.T, c *servertest.TestClient, name string, routes ...netip.Prefix) {
	t.Helper()

	c.Direct().SetHostinfo(&tailcfg.Hostinfo{
		BackendLogID: "servertest-" + name,
		Hostname:     name,
		RoutableIPs:  routes,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = c.Direct().SendUpdate(ctx)
}

// TestNetworksEndToEnd proves networks: creating one approves its
// prefixes on the router and hands the route to the group's machines
// only, an exit node network does the same for the exit routes, a
// disabled or deleted network withdraws what it approved and leaves
// approvals made by hand alone, and a group a network uses cannot be
// deleted. The subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestNetworksEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "net-owner")
	eng := srv.CreateUser(t, "net-eng")
	guest := srv.CreateUser(t, "net-guest")
	ownerKey := srv.CreateAPIKey(t, owner)

	router := servertest.NewClient(t, srv, "office-router", servertest.WithUser(owner))
	engNode := servertest.NewClient(t, srv, "eng-laptop", servertest.WithUser(eng))
	guestNode := servertest.NewClient(t, srv, "guest-laptop", servertest.WithUser(guest))

	for _, c := range []*servertest.TestClient{router, engNode, guestNode} {
		c.WaitForPeers(t, 2, networkWait)
	}

	office := netip.MustParsePrefix("10.10.0.0/24")
	manual := netip.MustParsePrefix("10.20.0.0/24")
	routerID := findNodeID(t, srv, routerName)

	advertise(
		t,
		router,
		routerName,
		office,
		manual,
		netip.MustParsePrefix("0.0.0.0/0"),
		netip.MustParsePrefix("::/0"),
	)

	_, c, err := srv.State().SetApprovedRoutes(routerID, []netip.Prefix{manual})
	require.NoError(t, err)
	srv.App.Change(c)

	var engGroupID, networkID string

	t.Run("a group for the engineers", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{
			"name":    "Engineering",
			"userIds": []string{userID(eng)},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "group", "id").(string)
		require.True(t, ok)

		engGroupID = id
	})

	t.Run("bad networks are rejected", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name": "Office", "prefixes": []string{"10.10.0.0/24"}, "groupIds": []string{},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name": "Office", "prefixes": []string{"not a prefix"}, "groupIds": []string{engGroupID},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name": "Office", "prefixes": []string{"10.10.0.0/24"}, "groupIds": []string{"99999"},
		})
		assert.Equal(t, http.StatusNotFound, status, body)
	})

	t.Run("creating a network approves the route and hands it to the group", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name":          "Office",
			"description":   "The office LAN",
			"prefixes":      []string{"10.10.0.1/24"},
			"routerNodeIds": []string{routerID.String()},
			"groupIds":      []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "network", "id").(string)
		require.True(t, ok)

		networkID = id

		assert.Equal(t, []any{"10.10.0.0/24"}, field(t, body, "network", "prefixes"), "the prefix is masked")
		assert.Equal(t, false, field(t, body, "network", "exitNode"))

		routers, ok := field(t, body, "network", "routers").([]any)
		require.True(t, ok)
		require.Len(t, routers, 1)
		assert.Equal(t, "office-router", field(t, routers[0], "name"))
		assert.Equal(t, []any{}, field(t, routers[0], "missingPrefixes"))

		node, ok := srv.State().GetNodeByID(routerID)
		require.True(t, ok)
		assert.ElementsMatch(t, []netip.Prefix{manual, office}, node.ApprovedRoutes().AsSlice())

		engNode.WaitForCondition(t, "route reaches the engineer", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, office)
		})

		assert.True(t, routerHasRoute(engNode.Netmap(), manual),
			"a route outside every network still reaches everyone")

		guestNode.WaitForCondition(t, "manual route reaches the guest", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, manual)
		})
		assert.False(t, routerHasRoute(guestNode.Netmap(), office),
			"the network's route is only for its groups")

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/network/"+networkID, nil)
		require.Equal(t, http.StatusOK, status, body)

		routers, ok = field(t, body, "network", "routers").([]any)
		require.True(t, ok)
		assert.Equal(t, []any{"10.10.0.0/24"}, field(t, routers[0], "primaryPrefixes"))
	})

	t.Run("a group a network uses cannot be deleted", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/group/"+engGroupID, nil)
		assert.Equal(t, http.StatusConflict, status, body)
	})

	t.Run("disabling withdraws the approval and the route", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/network/"+networkID,
			map[string]any{"enabled": false})
		require.Equal(t, http.StatusOK, status, body)

		node, ok := srv.State().GetNodeByID(routerID)
		require.True(t, ok)
		assert.Equal(t, []netip.Prefix{manual}, node.ApprovedRoutes().AsSlice(), "the manual approval stays")

		engNode.WaitForCondition(t, "route withdrawn from the engineer", networkWait, func(nm *netmap.NetworkMap) bool {
			return !routerHasRoute(nm, office)
		})

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/network/"+networkID,
			map[string]any{"enabled": true})
		require.Equal(t, http.StatusOK, status, body)

		engNode.WaitForCondition(t, "route back at the engineer", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, office)
		})
	})

	t.Run("an exit node network offers the exit routes to its groups", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name":          "Internet",
			"prefixes":      []string{"0.0.0.0/0"},
			"routerNodeIds": []string{routerID.String()},
			"groupIds":      []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "network", "exitNode"))
		assert.ElementsMatch(t, []any{"0.0.0.0/0", "::/0"}, field(t, body, "network", "prefixes"))

		exit4 := netip.MustParsePrefix("0.0.0.0/0")

		engNode.WaitForCondition(t, "exit route reaches the engineer", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, exit4)
		})

		assert.False(t, routerHasRoute(guestNode.Netmap(), exit4))
	})

	t.Run("replacing the groups moves the route", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/group", nil)
		require.Equal(t, http.StatusOK, status, body)

		var allID string

		groups, ok := body["groups"].([]any)
		require.True(t, ok)

		for _, g := range groups {
			if field(t, g, "builtin") == types.GroupBuiltinAll {
				allID, ok = field(t, g, "id").(string)
				require.True(t, ok)
			}
		}

		status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/network/"+networkID, map[string]any{
			"name":          "Office",
			"prefixes":      []string{"10.10.0.0/24"},
			"routerNodeIds": []string{routerID.String()},
			"groupIds":      []string{allID},
		})
		require.Equal(t, http.StatusOK, status, body)

		guestNode.WaitForCondition(t, "route reaches the guest", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, office)
		})
	})

	t.Run("deleting withdraws the approval", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/network/"+networkID, nil)
		require.Equal(t, http.StatusOK, status, body)

		node, ok := srv.State().GetNodeByID(routerID)
		require.True(t, ok)
		assert.NotContains(t, node.ApprovedRoutes().AsSlice(), office)
		assert.Contains(t, node.ApprovedRoutes().AsSlice(), manual)

		guestNode.WaitForCondition(t, "route gone from the guest", networkWait, func(nm *netmap.NetworkMap) bool {
			return !routerHasRoute(nm, office)
		})

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/network/"+networkID, nil)
		assert.Equal(t, http.StatusNotFound, status, body)
	})

	t.Run("a network over a manual approval leaves it when it goes", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name":          "Lab",
			"prefixes":      []string{manual.String(), office.String()},
			"routerNodeIds": []string{routerID.String()},
			"groupIds":      []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "network", "id").(string)
		require.True(t, ok)

		node, ok := srv.State().GetNodeByID(routerID)
		require.True(t, ok)
		assert.Contains(t, node.ApprovedRoutes().AsSlice(), office, "the network approved the office prefix")

		// The network now narrows the manual route to its group.
		guestNode.WaitForCondition(t, "manual route narrowed", networkWait, func(nm *netmap.NetworkMap) bool {
			return !routerHasRoute(nm, manual)
		})

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/network/"+id, nil)
		require.Equal(t, http.StatusOK, status, body)

		node, ok = srv.State().GetNodeByID(routerID)
		require.True(t, ok)
		assert.Contains(t, node.ApprovedRoutes().AsSlice(), manual, "the operator's approval survives the network")
		assert.NotContains(t, node.ApprovedRoutes().AsSlice(), office, "the network's own approval goes with it")

		guestNode.WaitForCondition(t, "manual route back for everyone", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, manual)
		})
	})

	t.Run("a router outside the network does not get its prefix through co-routing", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/network", map[string]any{
			"name":          "Office again",
			"prefixes":      []string{office.String()},
			"routerNodeIds": []string{routerID.String()},
			"groupIds":      []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)

		engNode.WaitForCondition(t, "route reaches the engineer", networkWait, func(nm *netmap.NetworkMap) bool {
			return routerHasRoute(nm, office)
		})

		// A second router for the same prefix, approved by hand and outside
		// the network. It is an HA secondary, and the co-router exception
		// would normally show it the primary's route.
		outsider := servertest.NewClient(t, srv, "outside-router", servertest.WithUser(guest))
		outsider.WaitForPeers(t, 3, networkWait)
		outsiderID := findNodeID(t, srv, "outside-router")

		advertise(t, outsider, "outside-router", office)

		_, c, err := srv.State().SetApprovedRoutes(outsiderID, []netip.Prefix{office})
		require.NoError(t, err)
		srv.App.Change(c)

		viewer, ok := srv.State().GetNodeByID(outsiderID)
		require.True(t, ok)
		peer, ok := srv.State().GetNodeByID(routerID)
		require.True(t, ok)

		matchers, err := srv.State().MatchersForNode(viewer)
		require.NoError(t, err)
		assert.NotContains(t, srv.State().RoutesForPeer(viewer, peer, matchers), office,
			"the network keeps its prefix from a router outside it")

		outsider.WaitForCondition(t, "the office router's route stays inside the network", networkWait,
			func(nm *netmap.NetworkMap) bool {
				return len(nm.Peers) == 3 && !peerHasRoute(nm, routerName, office)
			})
	})
}

// peerHasRoute reports whether the named peer carries the prefix in the
// netmap.
func peerHasRoute(nm *netmap.NetworkMap, name string, prefix netip.Prefix) bool {
	for _, peer := range nm.Peers {
		if peer.Hostinfo().Valid() && peer.Hostinfo().Hostname() == name {
			return slices.Contains(peer.AllowedIPs().AsSlice(), prefix)
		}
	}

	return false
}
