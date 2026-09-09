package servertest_test

import (
	"context"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
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
