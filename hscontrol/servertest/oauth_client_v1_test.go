package servertest_test

import (
	"net/http"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOAuthClientsV1 covers the console's OAuth client page: the owner
// creates a client and gets its secret once, the secret mints a v2 token,
// the list names the client, a role that only reads OAuth clients cannot
// create one, a client cannot be granted more than its creator holds,
// tags are required with machine scopes, and revoking removes the client.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestOAuthClientsV1(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "oauth-owner")
	netAdmin := srv.CreateUser(t, "oauth-net")
	itAdmin := srv.CreateUser(t, "oauth-it")
	ownerKey := srv.CreateAPIKey(t, owner)
	netKey := srv.CreateAPIKey(t, netAdmin)
	itKey := srv.CreateAPIKey(t, itAdmin)

	for user, role := range map[*types.User]types.Role{netAdmin: types.RoleNetworkAdmin, itAdmin: types.RoleITAdmin} {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(user)+"/role",
			map[string]string{"role": string(role)})
		require.Equal(t, http.StatusOK, status, body)
	}

	var clientID string

	t.Run("the owner creates a client and its secret mints a token", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"description": "CI deploys", "scopes": []string{"devices:core:read"},
		})
		require.Equal(t, http.StatusOK, status, body)

		secret, ok := body["clientSecret"].(string)
		require.True(t, ok)
		require.NotEmpty(t, secret)

		created, ok := body["oauthClient"].(map[string]any)
		require.True(t, ok)
		clientID, ok = created["clientId"].(string)
		require.True(t, ok)
		assert.Equal(t, "CI deploys", created["description"])
		assert.Equal(t, userID(owner), created["userId"])

		token := accessToken(t, client, srv.URL, secret)
		status, _ = apiCall(t, client, token, http.MethodGet, srv.URL+"/api/v2/tailnet/-/devices", nil)
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("the list names the client without its secret", func(t *testing.T) {
		status, body := apiCall(t, client, netKey, http.MethodGet, v1+"/oauth-client", nil)
		require.Equal(t, http.StatusOK, status, body)

		clients, ok := body["oauthClients"].([]any)
		require.True(t, ok)
		require.Len(t, clients, 1)

		first, ok := clients[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, clientID, first["clientId"])
		assert.Equal(t, []any{"devices:core:read"}, first["scopes"])
		assert.NotContains(t, first, "clientSecret")
	})

	t.Run("a reader cannot create", func(t *testing.T) {
		status, _ := apiCall(t, client, netKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"dns:read"},
		})
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("a client cannot exceed its creator", func(t *testing.T) {
		status, _ := apiCall(t, client, itKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"policy_file"},
		})
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("null or empty scopes are refused", func(t *testing.T) {
		for _, scopes := range []any{nil, []string{}} {
			status, _ := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
				"scopes": scopes,
			})
			assert.Equal(t, http.StatusUnprocessableEntity, status)
		}
	})

	t.Run("a token cannot hand out tags it does not own", func(t *testing.T) {
		setTagPolicy(t, srv, owner.Name)

		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"oauth_keys", "auth_keys"}, "tags": []string{"tag:ci"},
		})
		require.Equal(t, http.StatusOK, status, body)

		secret, ok := body["clientSecret"].(string)
		require.True(t, ok)
		token := accessToken(t, client, srv.URL, secret)

		status, _ = apiCall(t, client, token, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"auth_keys"}, "tags": []string{"tag:prod"},
		})
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = apiCall(t, client, token, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"auth_keys"}, "tags": []string{"tag:nowhere"},
		})
		assert.Equal(t, http.StatusBadRequest, status)

		status, _ = apiCall(t, client, token, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"auth_keys"}, "tags": []string{"tag:ci-child"},
		})
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("machine scopes need tags", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"devices:core"},
		})
		assert.Equal(t, http.StatusBadRequest, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"scopes": []string{"devices:core"}, "tags": []string{"tag:ci"},
		})
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("revoking removes the client", func(t *testing.T) {
		status, _ := apiCall(t, client, itKey, http.MethodDelete, v1+"/oauth-client/"+clientID, nil)
		assert.Equal(t, http.StatusOK, status)

		status, _ = apiCall(t, client, itKey, http.MethodDelete, v1+"/oauth-client/"+clientID, nil)
		assert.Equal(t, http.StatusNotFound, status)

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/oauth-client", nil)
		require.Equal(t, http.StatusOK, status, body)

		clients, ok := body["oauthClients"].([]any)
		require.True(t, ok)
		assert.NotEmpty(t, clients)

		for _, entry := range clients {
			listed, isMap := entry.(map[string]any)
			require.True(t, isMap)
			assert.NotEqual(t, clientID, listed["clientId"])
		}
	})
}

// setTagPolicy defines tag:ci (owned by owner) with tag:ci-child under it, and
// tag:prod owned by nobody, so a token holding tag:ci may delegate tag:ci-child
// and nothing else.
func setTagPolicy(t *testing.T, srv *servertest.TestServer, owner string) {
	t.Helper()

	policy := `{
		"tagOwners": {
			"tag:ci": ["` + owner + `@"],
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
}
