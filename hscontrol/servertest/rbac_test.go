package servertest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/netmap"
)

// apiCall performs one authenticated JSON request and returns the status and
// decoded body (nil for an empty body).
func apiCall(t *testing.T, client *http.Client, key, method, url string, body any) (int, map[string]any) {
	t.Helper()

	var reqBody io.Reader = http.NoBody

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)

		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, url, reqBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var decoded map[string]any
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &decoded), "body: %s", raw)
	}

	return resp.StatusCode, decoded
}

// field walks a decoded JSON body: maps by key, slices by decimal index.
func field(t *testing.T, body any, path ...string) any {
	t.Helper()

	cur := body

	for _, key := range path {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[key]
		case []any:
			i, err := strconv.Atoi(key)
			require.NoError(t, err)
			require.Less(t, i, len(v), "index %d out of range at %q", i, key)
			cur = v[i]
		default:
			require.Failf(t, "cannot descend", "into %T at %q", cur, key)
		}
	}

	return cur
}

// mintedKey reads the key string out of a createApiKey response.
func mintedKey(t *testing.T, body map[string]any) string {
	t.Helper()

	key, ok := body["apiKey"].(string)
	require.True(t, ok, "apiKey missing in %v", body)

	return key
}

func userID(u *types.User) string {
	return strconv.FormatUint(uint64(u.ID), 10)
}

// TestRBACRolesEndToEnd proves the role model through the HTTP surface the
// admin console and CLI use: the first user owns the tailnet, roles are set
// through v1 under the owner rules, a user-owned API key is bounded by its
// user's role on v1 and v2 alike, whoami reports the caller, and the role
// caps reach the user's nodes. The subtests build on one another's state,
// so they run in order.
//
//nolint:tparallel // ownership transfer at the end depends on every earlier step
func TestRBACRolesEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "rbac-owner")
	itAdmin := srv.CreateUser(t, "rbac-it")
	member := srv.CreateUser(t, "rbac-member")
	admin := srv.CreateUser(t, "rbac-admin")

	assert.Equal(t, types.RoleOwner, owner.Role, "the first user owns the tailnet")
	assert.Equal(t, types.RoleMember, itAdmin.Role)

	ownerKey := srv.CreateAPIKey(t, owner)
	legacyKey := srv.CreateAPIKey(t, nil)
	itKey := srv.CreateAPIKey(t, itAdmin)
	memberKey := srv.CreateAPIKey(t, member)
	adminKey := srv.CreateAPIKey(t, admin)

	setRole := func(key string, target *types.User, role types.Role) (int, map[string]any) {
		return apiCall(t, client, key, http.MethodPost, v1+"/user/"+userID(target)+"/role",
			map[string]string{"role": string(role)})
	}

	t.Run("whoami", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "api_key", body["kind"])
		assert.Equal(t, "owner", body["role"])
		assert.Equal(t, true, body["allAccess"])
		assert.Equal(t, "rbac-owner", field(t, body, "user", "name"))

		status, body = apiCall(t, client, legacyKey, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, true, body["allAccess"])
		assert.Nil(t, body["user"], "a legacy key belongs to nobody")

		status, body = apiCall(t, client, memberKey, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status, "any authenticated caller may ask who it is")
		assert.Equal(t, "member", body["role"])
		assert.Equal(t, false, body["allAccess"])
		assert.Empty(t, body["scopes"])
		assert.Equal(t, false, field(t, body, "permissions", "users:read"))

		status, _ = apiCall(t, client, "not-a-key", http.MethodGet, v1+"/whoami", nil)
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("member is denied everything else", func(t *testing.T) {
		status, _ := apiCall(t, client, memberKey, http.MethodGet, v1+"/user", nil)
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = apiCall(t, client, memberKey, http.MethodGet, v1+"/policy", nil)
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = setRole(memberKey, itAdmin, types.RoleAdmin)
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = apiCall(t, client, memberKey, http.MethodGet, srv.URL+"/api/v2/tailnet/-/users", nil)
		assert.Equal(t, http.StatusForbidden, status, "v2 is bounded by the same role")
	})

	t.Run("owner assigns roles", func(t *testing.T) {
		status, body := setRole(ownerKey, itAdmin, types.RoleITAdmin)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "it-admin", field(t, body, "user", "role"))

		status, _ = setRole(ownerKey, admin, types.RoleAdmin)
		require.Equal(t, http.StatusOK, status)

		status, _ = setRole(ownerKey, itAdmin, "billing-admin")
		assert.Equal(t, http.StatusBadRequest, status)

		status, _ = setRole(ownerKey, owner, types.RoleAdmin)
		assert.Equal(t, http.StatusForbidden, status, "nobody changes their own role")
	})

	t.Run("it-admin is bounded by its role", func(t *testing.T) {
		status, body := apiCall(t, client, itKey, http.MethodGet, v1+"/user", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Len(t, body["users"], 4)

		status, _ = apiCall(t, client, itKey, http.MethodGet, v1+"/policy", nil)
		assert.NotEqual(t, http.StatusForbidden, status, "policy_file:read is granted")

		status, _ = apiCall(t, client, itKey, http.MethodPut, v1+"/policy",
			map[string]string{"policy": `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`})
		assert.Equal(t, http.StatusForbidden, status, "policy_file write is not")

		// The users scope lets the request through the middleware; the state
		// layer then applies the role rules.
		status, _ = setRole(itKey, member, types.RoleAuditor)
		assert.Equal(t, http.StatusForbidden, status, "only owner or admin assign roles")

		status, body = apiCall(t, client, itKey, http.MethodGet, srv.URL+"/api/v2/tailnet/-/users?role=it-admin", nil)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, body["users"], 1)
		assert.Equal(t, "it-admin", field(t, body, "users", "0", "role"))
	})

	t.Run("admin cannot touch the owner", func(t *testing.T) {
		status, _ := setRole(adminKey, member, types.RoleAuditor)
		require.Equal(t, http.StatusOK, status, "an admin assigns non-owner roles")

		status, _ = setRole(adminKey, owner, types.RoleMember)
		assert.Equal(t, http.StatusForbidden, status, "the owner cannot be demoted")

		status, _ = setRole(adminKey, member, types.RoleOwner)
		assert.Equal(t, http.StatusForbidden, status, "only the owner transfers ownership")

		status, _ = apiCall(t, client, adminKey, http.MethodDelete, v1+"/user/"+userID(owner), nil)
		assert.Equal(t, http.StatusForbidden, status, "the owner cannot be deleted")

		status, _ = apiCall(t, client, legacyKey, http.MethodDelete, v1+"/user/"+userID(owner), nil)
		assert.Equal(t, http.StatusForbidden, status, "not even by a legacy key")
	})

	t.Run("api keys are minted within the caller's authority", func(t *testing.T) {
		status, _ := apiCall(t, client, memberKey, http.MethodPost, v1+"/apikey",
			map[string]string{"userId": userID(owner)})
		assert.Equal(t, http.StatusForbidden, status, "a member cannot mint a key for the owner")

		status, body := apiCall(t, client, memberKey, http.MethodPost, v1+"/apikey", map[string]string{})
		require.Equal(t, http.StatusOK, status)

		minted := mintedKey(t, body)
		status, body = apiCall(t, client, minted, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "auditor", body["role"], "a self-minted key follows the user's current role")

		status, body = apiCall(t, client, minted, http.MethodGet, v1+"/apikey", nil)
		require.Equal(t, http.StatusOK, status)

		keys, ok := body["apiKeys"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, keys)

		for i := range keys {
			assert.Equal(t, userID(member), field(t, keys, strconv.Itoa(i), "userId"),
				"a bounded caller lists only its own keys")
		}

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey",
			map[string]string{"userId": userID(itAdmin)})
		require.Equal(t, http.StatusOK, status)
		status, body = apiCall(t, client, mintedKey(t, body), http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "it-admin", body["role"], "the owner mints keys for anyone")

		status, body = apiCall(t, client, legacyKey, http.MethodPost, v1+"/apikey", map[string]string{})
		require.Equal(t, http.StatusOK, status)
		status, body = apiCall(t, client, mintedKey(t, body), http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, true, body["allAccess"], "a legacy key mints legacy keys")
		assert.Nil(t, body["user"])
	})

	t.Run("role caps reach the nodes", func(t *testing.T) {
		ownerNode := servertest.NewClient(t, srv, "rbac-owner-node", servertest.WithUser(owner))
		ownerNode.WaitForCondition(t, "owner node carries is-admin and is-owner", 10*time.Second,
			func(nm *netmap.NetworkMap) bool {
				return hasCap(nm, nodecap.Admin) && hasCap(nm, nodecap.Owner)
			})

		memberNode := servertest.NewClient(t, srv, "rbac-member-node", servertest.WithUser(member))
		memberNode.WaitForCondition(t, "auditor node carries neither", 10*time.Second,
			func(nm *netmap.NetworkMap) bool {
				return nm != nil && nm.SelfNode.Valid() &&
					hasCap(nm, nodecap.SSH) && !hasCap(nm, nodecap.Admin) && !hasCap(nm, nodecap.Owner)
			})

		status, _ := setRole(ownerKey, member, types.RoleAdmin)
		require.Equal(t, http.StatusOK, status)

		memberNode.WaitForCondition(t, "promotion reaches the node", 10*time.Second,
			func(nm *netmap.NetworkMap) bool {
				return hasCap(nm, nodecap.Admin) && !hasCap(nm, nodecap.Owner)
			})
	})

	t.Run("ownership transfer", func(t *testing.T) {
		status, body := setRole(ownerKey, admin, types.RoleOwner)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "owner", field(t, body, "user", "role"))

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "admin", body["role"], "the previous owner is now an admin")

		status, _ = setRole(ownerKey, member, types.RoleOwner)
		assert.Equal(t, http.StatusForbidden, status, "and can no longer hand out ownership")

		status, body = apiCall(t, client, adminKey, http.MethodGet, srv.URL+"/api/v2/tailnet/-/users?role=owner", nil)
		require.Equal(t, http.StatusOK, status)
		require.Len(t, body["users"], 1, "exactly one owner")
		assert.Equal(t, userID(admin), field(t, body, "users", "0", "id"))
	})
}
