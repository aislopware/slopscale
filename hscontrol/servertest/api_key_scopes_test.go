package servertest_test

import (
	"net/http"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScopedAPIKeys proves an API key minted with scopes acts within
// them: the owner's DNS-only key reads and writes DNS and nothing else,
// a network admin's key asking for more than the role holds gets only
// what the role grants, a caller that can delegate none of the scopes
// is refused, an unknown scope is a client error, and the list shows
// the scopes and description.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestScopedAPIKeys(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "scoped-owner")
	netAdmin := srv.CreateUser(t, "scoped-net")
	member := srv.CreateUser(t, "scoped-member")
	ownerKey := srv.CreateAPIKey(t, owner)
	netKey := srv.CreateAPIKey(t, netAdmin)
	memberKey := srv.CreateAPIKey(t, member)

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(netAdmin)+"/role",
		map[string]string{"role": string(types.RoleNetworkAdmin)})
	require.Equal(t, http.StatusOK, status, body)

	t.Run("a dns-only key does dns and nothing else", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"scopes": []string{"dns"}, "description": "Resolver sync",
		})
		require.Equal(t, http.StatusOK, status, body)

		key := mintedKey(t, body)

		status, body = apiCall(t, client, key, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["allAccess"])
		assert.Equal(t, true, field(t, body, "permissions", "dns"))
		assert.Equal(t, true, field(t, body, "permissions", "dns:read"))
		assert.Equal(t, false, field(t, body, "permissions", "users"))

		status, _ = apiCall(t, client, key, http.MethodGet, v1+"/dns", nil)
		assert.Equal(t, http.StatusOK, status)

		status, _ = apiCall(
			t,
			client,
			key,
			http.MethodPut,
			v1+"/dns",
			map[string]any{"nameservers": []string{"9.9.9.9"}},
		)
		assert.Equal(t, http.StatusOK, status)

		status, _ = apiCall(t, client, key, http.MethodGet, v1+"/user", nil)
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = apiCall(t, client, key, http.MethodPost, v1+"/user", map[string]any{"name": "sneaky"})
		assert.Equal(t, http.StatusForbidden, status)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/apikey", nil)
		require.Equal(t, http.StatusOK, status, body)

		keys, ok := body["apiKeys"].([]any)
		require.True(t, ok)

		var found bool

		for _, k := range keys {
			if field(t, k, "description") == "Resolver sync" {
				found = true

				assert.Equal(t, []any{"dns"}, field(t, k, "scopes"))
			}
		}

		assert.True(t, found, "the list shows the scoped key")
	})

	t.Run("scopes never outgrow the minter", func(t *testing.T) {
		status, body := apiCall(t, client, netKey, http.MethodPost, v1+"/apikey", map[string]any{
			"scopes": []string{"users", "devices:routes"},
		})
		require.Equal(t, http.StatusOK, status, body)

		key := mintedKey(t, body)

		status, body = apiCall(t, client, key, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "permissions", "devices:routes"))
		assert.Equal(t, false, field(t, body, "permissions", "users"), "users is outside a network admin's role")
		assert.Equal(t, false, field(t, body, "permissions", "policy_file"), "the key asked for routes alone")

		status, body = apiCall(t, client, memberKey, http.MethodPost, v1+"/apikey", map[string]any{
			"scopes": []string{"users"},
		})
		assert.Equal(t, http.StatusForbidden, status, "a member can delegate none of it: %v", body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"scopes": []string{"kitchen:sink"},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)
	})

	t.Run("a scoped key mints keys no wider than itself", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"scopes": []string{"dns:read"},
		})
		require.Equal(t, http.StatusOK, status, body)

		// The owner is unbounded, so the key has no owner: its scopes alone
		// bound it, and so they bound what it mints.
		status, body = apiCall(t, client, mintedKey(t, body), http.MethodPost, v1+"/apikey", map[string]any{
			"scopes": []string{"dns", "dns:read"},
		})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, mintedKey(t, body), http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "permissions", "dns:read"))
		assert.Nil(t, body["user"], "a key minted by an ownerless key has no owner either")
		assert.Equal(t, false, field(t, body, "permissions", "dns"))
	})
}
