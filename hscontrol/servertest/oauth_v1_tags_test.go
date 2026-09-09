package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestOAuthTokenTagBoundaryV1 pins the v1 side of the tag boundary v2 has
// always enforced: an OAuth access token acts for the tailnet through the tags
// it was granted, so on v1 it may mint a key only with those tags (or tags
// they own), never an untagged key, never a key or a node registration for a
// user. An admin API key keeps its historical freedom.
//
//nolint:tparallel // the steps share the server, the policy and the node
func TestOAuthTokenTagBoundaryV1(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "v1-tags-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	// tag:prod exists but is owned by nobody, so asking for it is a grant
	// denial (403) rather than a tag-not-in-policy rejection (400).
	const policy = `{
		"tagOwners": {
			"tag:ci": ["v1-tags-owner@"],
			"tag:ci-child": ["tag:ci"],
			"tag:prod": []
		},
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]
	}`

	st := srv.State()

	_, err := st.SetPolicy([]byte(policy))
	require.NoError(t, err)

	_, err = st.SetPolicyInDB(policy)
	require.NoError(t, err)

	_, err = st.ReloadPolicy()
	require.NoError(t, err)

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
		"description": "CI", "scopes": []string{"devices:core", "auth_keys"}, "tags": []string{"tag:ci"},
	})
	require.Equal(t, http.StatusOK, status, body)

	secret, ok := body["clientSecret"].(string)
	require.True(t, ok)

	token := accessToken(t, client, srv.URL, secret)

	node := servertest.NewClient(t, srv, "v1-tags-node", servertest.WithUser(owner))
	node.WaitForCondition(t, "a netmap with a self node", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm.SelfNode.Valid()
	})

	nodeID := node.NodeIDString()

	t.Run("a token mints a key with a tag it holds", func(t *testing.T) {
		status, body := apiCall(t, client, token, http.MethodPost, v1+"/preauthkey", map[string]any{
			"aclTags": []string{"tag:ci"},
		})
		assert.Equal(t, http.StatusOK, status, body)
	})

	t.Run("a token mints a key with a tag its tag owns", func(t *testing.T) {
		status, body := apiCall(t, client, token, http.MethodPost, v1+"/preauthkey", map[string]any{
			"aclTags": []string{"tag:ci-child"},
		})
		assert.Equal(t, http.StatusOK, status, body)
	})

	t.Run("a token may not mint a key with an unowned tag", func(t *testing.T) {
		status, _ := apiCall(t, client, token, http.MethodPost, v1+"/preauthkey", map[string]any{
			"aclTags": []string{"tag:prod"},
		})
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("a token may not mint a key with a tag the policy does not define", func(t *testing.T) {
		status, _ := apiCall(t, client, token, http.MethodPost, v1+"/preauthkey", map[string]any{
			"aclTags": []string{"tag:undefined"},
		})
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("a token may not mint an untagged key", func(t *testing.T) {
		status, _ := apiCall(t, client, token, http.MethodPost, v1+"/preauthkey", map[string]any{})
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("a token may not mint a key for a user", func(t *testing.T) {
		status, _ := apiCall(t, client, token, http.MethodPost, v1+"/preauthkey", map[string]any{
			"user": userID(owner), "aclTags": []string{"tag:ci"},
		})
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("a token may not register a node for a user", func(t *testing.T) {
		register := v1 + "/node/register?user=v1-tags-owner&key=hskey-authreq-x"

		status, _ := apiCall(t, client, token, http.MethodPost, register, nil)
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("a token may not tag a node with an unowned tag", func(t *testing.T) {
		status, _ := apiCall(t, client, token, http.MethodPost, v1+"/node/"+nodeID+"/tags", map[string]any{
			"tags": []string{"tag:prod"},
		})
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("a token tags a node with a tag it holds", func(t *testing.T) {
		status, body := apiCall(t, client, token, http.MethodPost, v1+"/node/"+nodeID+"/tags", map[string]any{
			"tags": []string{"tag:ci"},
		})
		assert.Equal(t, http.StatusOK, status, body)
	})

	t.Run("an admin key keeps its freedom", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/preauthkey", map[string]any{
			"user": userID(owner),
		})
		assert.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/preauthkey", map[string]any{
			"aclTags": []string{"tag:prod"},
		})
		assert.Equal(t, http.StatusOK, status, body)
	})
}
