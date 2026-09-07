package servertest_test

import (
	"net/http"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

// TestPostureRules proves that a posture attached to an access rule
// narrows the rule's sources to the machines that satisfy it, that the
// narrowing follows the inputs (a custom attribute turning the posture
// on and off), and that the API refuses bad expressions and deleting a
// posture a rule names. The subtests build on one another.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestPostureRules(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "posture-rule-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	mac := servertest.NewClient(t, srv, "mac", servertest.WithUser(owner),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.OS = "macOS"
			hi.IPNVersion = "1.86.2-t1234abcd"
		}))
	old := servertest.NewClient(t, srv, "old", servertest.WithUser(owner),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.OS = "linux"
			hi.IPNVersion = "1.30.0-t0000"
		}))
	server := servertest.NewClient(t, srv, "server", servertest.WithUser(owner))

	for _, c := range []*servertest.TestClient{mac, old, server} {
		c.WaitForPeerCount(t, 2, postureWait)
	}

	var currentID, serversID, ruleID string

	t.Run("a posture is validated on the way in", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture", map[string]any{
			"name": "Broken", "expressions": []string{"os == macos"},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture", map[string]any{
			"name": "Nothing",
		})
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture", map[string]any{
			"name": "Somewhere", "expressions": []string{"ip:country == 'VN'"},
		})
		assert.Equal(t, http.StatusBadRequest, status, "ip:country needs a GeoIP database: %s", body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture/check", map[string]any{
			"expressions": []string{"node:os == 'macos'", "node:os =="},
		})
		require.Equal(t, http.StatusOK, status, body)

		errs, ok := field(t, body, "errors").([]any)
		require.True(t, ok)
		require.Len(t, errs, 2)
		assert.Empty(t, errs[0])
		assert.NotEmpty(t, errs[1])

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture", map[string]any{
			"name":        "Current client",
			"expressions": []string{"node:tsVersion >= '1.40'", "custom:blocked NOT SET"},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "posture", "id").(string)
		require.True(t, ok)

		currentID = id

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+mac.NodeIDString()+"/postures", nil)
		require.Equal(t, http.StatusOK, status, body)

		matches, ok := field(t, body, "postures").([]any)
		require.True(t, ok)
		require.Len(t, matches, 1, "the mac satisfies the posture")

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+old.NodeIDString()+"/postures", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Empty(t, field(t, body, "postures"), "the old client does not")
	})

	t.Run("a rule with the posture admits only the current client", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{"name": "Servers"})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "group", "id").(string)
		require.True(t, ok)

		serversID = id

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+serversID+"/member",
			map[string]any{"nodeId": server.NodeIDString()})
		require.Equal(t, http.StatusOK, status, body)

		all := groupNamed(t, client, ownerKey, v1, "All")

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-rule", map[string]any{
			"name":                "SSH from current clients",
			"protocol":            "tcp",
			"ports":               "22",
			"sourceGroupIds":      []string{all},
			"destinationGroupIds": []string{serversID},
			"postureIds":          []string{currentID},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{currentID}, field(t, body, "rule", "postureIds"))

		id, ok = field(t, body, "rule", "id").(string)
		require.True(t, ok)

		ruleID = id

		// The mac reaches the server; the old client is nobody's peer.
		mac.WaitForCondition(t, "the server as the only peer", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1 && nm.Peers[0].ID() == server.Netmap().SelfNode.ID()
		})
		old.WaitForCondition(t, "no peers", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})
	})

	t.Run("a custom attribute flips the posture live", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPut,
			v1+"/node/"+mac.NodeIDString()+"/attributes/custom:blocked", map[string]any{"value": true})
		require.Equal(t, http.StatusOK, status, body)

		mac.WaitForCondition(t, "no peers once blocked", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})

		status, _ = apiCall(t, client, ownerKey, http.MethodDelete,
			v1+"/node/"+mac.NodeIDString()+"/attributes/custom:blocked", nil)
		require.Equal(t, http.StatusOK, status)

		mac.WaitForCondition(t, "the server back", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1
		})
	})

	t.Run("a posture in use cannot be deleted", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/posture/"+currentID, nil)
		assert.Equal(t, http.StatusConflict, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/access-rule/"+ruleID, nil)
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/posture/"+currentID, nil)
		assert.Equal(t, http.StatusOK, status, body)

		old.WaitForCondition(t, "every peer again", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 2
		})
	})
}

// groupNamed returns the id of the group with the name.
func groupNamed(t *testing.T, client *http.Client, key, v1, name string) string {
	t.Helper()

	status, body := apiCall(t, client, key, http.MethodGet, v1+"/group", nil)
	require.Equal(t, http.StatusOK, status, body)

	groups, ok := field(t, body, "groups").([]any)
	require.True(t, ok)

	for _, g := range groups {
		group, ok := g.(map[string]any)
		require.True(t, ok)

		if group["name"] == name {
			id, ok := group["id"].(string)
			require.True(t, ok)

			return id
		}
	}

	t.Fatalf("no group named %q", name)

	return ""
}
