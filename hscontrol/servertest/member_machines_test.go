package servertest_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// nodeIDs reads the ids out of a listNodes response.
func nodeIDs(t *testing.T, body map[string]any) []string {
	t.Helper()

	nodes, ok := body["nodes"].([]any)
	require.True(t, ok, "nodes missing in %v", body)

	ids := make([]string, 0, len(nodes))

	for _, n := range nodes {
		id, ok := field(t, n, "id").(string)
		require.True(t, ok)

		ids = append(ids, id)
	}

	return ids
}

// TestMemberSeesOwnMachines proves the self-service view a member gets
// through the v1 API, which is what the console shows them: their own
// machines and the ones shared with them, with the live reads of a
// machine's page; rename, expire and remove on their own machines only;
// the user directory by name; and nothing of anyone else's. A key minted
// with a scope list stands for its scopes, not for the user, so it sees
// none of it. The subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the share the earlier one makes
func TestMemberSeesOwnMachines(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "machines-owner")
	alice := srv.CreateUser(t, "alice")
	bob := srv.CreateUser(t, "bob")
	ownerKey := srv.CreateAPIKey(t, owner)
	aliceKey := srv.CreateAPIKey(t, alice)
	bobKey := srv.CreateAPIKey(t, bob)

	aliceNode := servertest.NewClient(t, srv, "alice-1", servertest.WithUser(alice))
	bobNode := servertest.NewClient(t, srv, "bob-1", servertest.WithUser(bob))
	tagged := servertest.NewClient(t, srv, "tagged-1", servertest.WithUser(owner), servertest.WithTags("tag:test"))

	for _, c := range []*servertest.TestClient{aliceNode, bobNode, tagged} {
		c.WaitForCondition(t, "self node", sharingWait, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid()
		})
	}

	aliceID := aliceNode.NodeIDString()
	bobID := bobNode.NodeIDString()

	t.Run("a member lists their own machines and nothing else", func(t *testing.T) {
		status, body := apiCall(t, client, aliceKey, http.MethodGet, v1+"/node", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []string{aliceID}, nodeIDs(t, body))

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Len(t, nodeIDs(t, body), 3, "the owner lists every machine")

		status, body = apiCall(t, client, aliceKey, http.MethodGet, v1+"/node?user=bob", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Empty(t, nodeIDs(t, body), "the user filter never widens the list")
	})

	t.Run("another user's machine is not found", func(t *testing.T) {
		status, _ := apiCall(t, client, aliceKey, http.MethodGet, v1+"/node/"+bobID, nil)
		assert.Equal(t, http.StatusNotFound, status)

		status, _ = apiCall(t, client, aliceKey, http.MethodGet, v1+"/node/"+tagged.NodeIDString(), nil)
		assert.Equal(t, http.StatusNotFound, status, "a tagged machine belongs to nobody")

		status, _ = apiCall(t, client, aliceKey, http.MethodGet, v1+"/node/"+bobID+"/health", nil)
		assert.Equal(t, http.StatusNotFound, status)

		status, _ = apiCall(t, client, aliceKey, http.MethodPost, v1+"/node/"+bobID+"/rename/mine", nil)
		assert.Equal(t, http.StatusNotFound, status, "a write on an invisible machine reveals nothing")

		status, _ = apiCall(t, client, aliceKey, http.MethodDelete, v1+"/node/"+bobID, nil)
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("a member looks after their own machine", func(t *testing.T) {
		status, body := apiCall(t, client, aliceKey, http.MethodGet, v1+"/node/"+aliceID, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "alice-1", field(t, body, "node", "givenName"))

		status, body = apiCall(t, client, aliceKey, http.MethodGet, v1+"/node/"+aliceID+"/health", nil)
		assert.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, aliceKey, http.MethodPost, v1+"/node/"+aliceID+"/rename/alice-laptop", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "alice-laptop", field(t, body, "node", "givenName"))

		status, body = apiCall(t, client, aliceKey, http.MethodPost, v1+"/node/"+aliceID+"/expire",
			map[string]any{"disableExpiry": true})
		require.Equal(t, http.StatusOK, status, body)
		assert.Nil(t, field(t, body, "node", "expiry"), "key expiry is off")

		status, _ = apiCall(t, client, aliceKey, http.MethodPost, v1+"/node/"+aliceID+"/tags",
			map[string]any{"tags": []string{"tag:test"}})
		assert.Equal(t, http.StatusForbidden, status, "tags stay an administrator's call")

		status, _ = apiCall(t, client, aliceKey, http.MethodPost, v1+"/node/"+aliceID+"/approve_routes",
			map[string]any{"routes": []string{"10.0.0.0/24"}})
		assert.Equal(t, http.StatusForbidden, status, "so do routes")
	})

	t.Run("a shared machine is visible but not theirs to change", func(t *testing.T) {
		status, body := apiCall(t, client, bobKey, http.MethodPost, v1+"/node/"+bobID+"/share",
			map[string]string{"userId": strconv.FormatUint(uint64(alice.ID), 10)})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, aliceKey, http.MethodGet, v1+"/node", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.ElementsMatch(t, []string{aliceID, bobID}, nodeIDs(t, body))

		status, body = apiCall(t, client, aliceKey, http.MethodGet, v1+"/node/"+bobID, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "bob", field(t, body, "node", "user", "name"))

		status, _ = apiCall(t, client, aliceKey, http.MethodPost, v1+"/node/"+bobID+"/rename/stolen", nil)
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = apiCall(t, client, aliceKey, http.MethodDelete, v1+"/node/"+bobID, nil)
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = apiCall(t, client, aliceKey, http.MethodPost, v1+"/node/"+bobID+"/expire", nil)
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("the user directory names everyone and says nothing more", func(t *testing.T) {
		status, body := apiCall(t, client, aliceKey, http.MethodGet, v1+"/user", nil)
		require.Equal(t, http.StatusOK, status, body)

		users, ok := body["users"].([]any)
		require.True(t, ok)
		assert.Len(t, users, 3)

		names := make([]string, 0, len(users))

		for _, u := range users {
			name, ok := field(t, u, "name").(string)
			require.True(t, ok)

			names = append(names, name)

			assert.Empty(t, field(t, u, "email"))
			assert.Empty(t, field(t, u, "providerId"))
			assert.Equal(t, "member", field(t, u, "role"), "roles are not disclosed")
		}

		assert.ElementsMatch(t, []string{"machines-owner", "alice", "bob"}, names)

		status, _ = apiCall(t, client, aliceKey, http.MethodGet, v1+"/user?name=bob", nil)
		assert.Equal(t, http.StatusForbidden, status, "no filter, so the list is not an oracle")
	})

	t.Run("a scoped key stands for its scopes, not for the user", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"userId": userID(alice), "scopes": []string{"dns:read"}, "description": "narrow",
		})
		require.Equal(t, http.StatusOK, status, body)

		narrow := mintedKey(t, body)

		status, body = apiCall(t, client, narrow, http.MethodGet, v1+"/node", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Empty(t, nodeIDs(t, body), "alice's own machine is not the key's to see")

		status, _ = apiCall(t, client, narrow, http.MethodGet, v1+"/node/"+aliceID, nil)
		assert.Equal(t, http.StatusNotFound, status)

		status, _ = apiCall(t, client, narrow, http.MethodGet, v1+"/user", nil)
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("a member removes their own machine", func(t *testing.T) {
		status, _ := apiCall(t, client, aliceKey, http.MethodDelete, v1+"/node/"+aliceID, nil)
		require.Equal(t, http.StatusOK, status)

		status, body := apiCall(t, client, aliceKey, http.MethodGet, v1+"/node", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []string{bobID}, nodeIDs(t, body), "only the shared machine is left")
	})
}
