package servertest_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestPostureIntegrations covers the device management integration round trip:
// an httptest server fakes Kolide's device search and check endpoints,
// an integration checks credentials (502 on upstream auth failure), duplicate enabled
// integrations are rejected with 409, sync populates integration attributes and node posture,
// an access rule using the posture admits the device, and deleting the integration
// cleans up the attributes.
func TestPostureIntegrations(t *testing.T) {
	t.Parallel()

	var checkFails atomic.Bool

	fakeKolide := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkFails.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "unauthorized"}`))

			return
		}

		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.RawQuery, "per_page=1") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": []}`))

			return
		}

		if strings.Contains(r.URL.Query().Get("query"), "serial:X") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"serial": "X", "auth_state": "good"}]}`))

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": []}`))
	}))
	defer fakeKolide.Close()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "posture-int-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	mac := servertest.NewClient(
		t,
		srv,
		"mac",
		servertest.WithUser(owner),
		servertest.WithSerialNumbers("X"),
	)
	targetServer := servertest.NewClient(t, srv, "server", servertest.WithUser(owner))

	mac.WaitForPeerCount(t, 1, 10*time.Second)
	targetServer.WaitForPeerCount(t, 1, 10*time.Second)

	// Enable posture identity collection and collect serial from mac.
	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings",
		map[string]bool{"postureIdentityOn": true})
	require.Equal(t, http.StatusOK, status, body)

	status, body = apiCall(t, client, ownerKey, http.MethodPost,
		v1+"/node/"+mac.NodeIDString()+"/posture/collect", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []any{"X"}, field(t, body, "identity", "serialNumbers"))

	// POST /api/v1/posture-integrations/check succeeds when credentials are good,
	// and returns 502 when the upstream answers 401.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture-integrations/check", map[string]any{
		"provider": "kolide",
		"name":     "kolide-check",
		"baseUrl":  fakeKolide.URL,
		"apiToken": "good-token",
	})
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, true, body["ok"])

	checkFails.Store(true)

	status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture-integrations/check", map[string]any{
		"provider": "kolide",
		"name":     "kolide-check",
		"baseUrl":  fakeKolide.URL,
		"apiToken": "bad-token",
	})
	assert.Equal(t, http.StatusBadGateway, status)
	checkFails.Store(false)

	// Create the integration; sync runs at once on creation.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture-integrations", map[string]any{
		"provider": "kolide",
		"name":     "kolide-int",
		"baseUrl":  fakeKolide.URL,
		"apiToken": "kolide-token",
	})
	require.Equal(t, http.StatusCreated, status, body)
	integrationID, ok := field(t, body, "integration", "id").(string)
	require.True(t, ok)

	// A second enabled integration for the same provider is refused with 409.
	status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture-integrations", map[string]any{
		"provider": "kolide",
		"name":     "kolide-dup",
		"baseUrl":  fakeKolide.URL,
		"apiToken": "kolide-token",
	})
	assert.Equal(t, http.StatusConflict, status)

	// Node posture carries kolide:authState in integration and attributes.
	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+mac.NodeIDString()+"/posture", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "Good", field(t, body, "attributes", "kolide:authState"))

	integAttrs, ok := field(t, body, "integration").([]any)
	require.True(t, ok)

	found := false

	for i := range integAttrs {
		if field(t, integAttrs, strconv.Itoa(i), "key") == "kolide:authState" {
			assert.Equal(t, "Good", field(t, integAttrs, strconv.Itoa(i), "value"))

			found = true

			break
		}
	}

	assert.True(t, found, "kolide:authState should be present in integration attributes")

	// Posture rule with kolide:authState == 'Good' admits the node.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture", map[string]any{
		"name":        "Kolide Good",
		"expressions": []string{"kolide:authState == 'Good'"},
	})
	require.Equal(t, http.StatusOK, status, body)
	postureID, ok := field(t, body, "posture", "id").(string)
	require.True(t, ok)

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{"name": "Servers"})
	require.Equal(t, http.StatusOK, status, body)
	serversID, ok := field(t, body, "group", "id").(string)
	require.True(t, ok)

	status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+serversID+"/member",
		map[string]any{"nodeId": targetServer.NodeIDString()})
	require.Equal(t, http.StatusOK, status)

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/group", nil)
	require.Equal(t, http.StatusOK, status, body)
	groups, ok := field(t, body, "groups").([]any)
	require.True(t, ok)

	var allID string

	for i := range groups {
		if field(t, groups, strconv.Itoa(i), "name") == "All" {
			allID, _ = field(t, groups, strconv.Itoa(i), "id").(string)

			break
		}
	}

	require.NotEmpty(t, allID)

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-rule", map[string]any{
		"name":                "SSH to servers",
		"protocol":            "tcp",
		"ports":               "22",
		"sourceGroupIds":      []string{allID},
		"destinationGroupIds": []string{serversID},
		"postureIds":          []string{postureID},
	})
	require.Equal(t, http.StatusOK, status, body)

	mac.WaitForCondition(t, "mac can reach server", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return len(nm.Peers) == 1 && nm.Peers[0].ID() == targetServer.Netmap().SelfNode.ID()
	})

	// Deleting the integration removes the attributes.
	status, _ = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/posture-integration/"+integrationID, nil)
	require.Equal(t, http.StatusNoContent, status)

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+mac.NodeIDString()+"/posture", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Nil(t, field(t, body, "attributes", "kolide:authState"))

	integAttrs, _ = field(t, body, "integration").([]any)
	for i := range integAttrs {
		assert.NotEqual(t, "kolide:authState", field(t, integAttrs, strconv.Itoa(i), "key"))
	}
}
