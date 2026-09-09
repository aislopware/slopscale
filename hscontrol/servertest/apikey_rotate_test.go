package servertest_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiKeyPrefixOf reads the public prefix out of a whole key string
// (hskey-api-<prefix>-<secret>), which is what the rotate path takes.
func apiKeyPrefixOf(t *testing.T, key string) string {
	t.Helper()

	_, rest, ok := strings.Cut(key, "hskey-api-")
	require.True(t, ok, "unexpected key format")
	require.GreaterOrEqual(t, len(rest), types.NewAPIKeyPrefixLength)

	return rest[:types.NewAPIKeyPrefixLength]
}

// apiKeyByDescription finds one key in a listApiKeys body by its description.
func apiKeyByDescription(t *testing.T, body map[string]any, description string) map[string]any {
	t.Helper()

	keys, ok := body["apiKeys"].([]any)
	require.True(t, ok, "apiKeys missing in %v", body)

	for _, raw := range keys {
		key, ok := raw.(map[string]any)
		require.True(t, ok)

		if key["description"] == description {
			return key
		}
	}

	require.FailNowf(t, "key not listed", "no key described %q in %v", description, body)

	return nil
}

// TestRotateAPIKey proves rotation mints a new secret for an existing key
// without forking it: the row keeps its id, scopes and description, the old
// secret is refused at once, an unknown prefix is not found, an expired key
// is refused, a scoped key cannot rotate its way wider than itself, and the
// rotation is audited.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestRotateAPIKey(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "rotate-owner")
	itAdmin := srv.CreateUser(t, "rotate-it")
	ownerKey := srv.CreateAPIKey(t, owner)

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(itAdmin)+"/role",
		map[string]string{"role": string(types.RoleITAdmin)})
	require.Equal(t, http.StatusOK, status, body)

	var rotatedPrefix string

	t.Run("rotating keeps the key and retires the old secret", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"scopes": []string{"dns"}, "description": "Resolver sync",
		})
		require.Equal(t, http.StatusOK, status, body)

		old := mintedKey(t, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/apikey", nil)
		require.Equal(t, http.StatusOK, status, body)

		before := apiKeyByDescription(t, body, "Resolver sync")

		status, body = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/apikey/"+apiKeyPrefixOf(t, old)+"/rotate", nil)
		require.Equal(t, http.StatusOK, status, body)

		rotated := mintedKey(t, body)
		assert.NotEqual(t, old, rotated)
		assert.Equal(t, "hskey-api-"+apiKeyPrefixOf(t, rotated)+"-***", body["prefix"],
			"the response names the key's new prefix as the list does")

		rotatedPrefix = apiKeyPrefixOf(t, rotated)

		status, body = apiCall(t, client, rotated, http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "permissions", "dns"), "the new secret keeps the scopes")

		status, _ = apiCall(t, client, old, http.MethodGet, v1+"/whoami", nil)
		assert.Equal(t, http.StatusUnauthorized, status, "the old secret is refused at once")

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/apikey", nil)
		require.Equal(t, http.StatusOK, status, body)

		after := apiKeyByDescription(t, body, "Resolver sync")
		assert.Equal(t, before["id"], after["id"], "rotation keeps the row, it does not fork it")
		assert.Equal(t, []any{"dns"}, after["scopes"])
		assert.NotEqual(t, before["prefix"], after["prefix"], "the listed prefix follows the new secret")
	})

	t.Run("an unknown prefix is not found", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey/abcdef012345/rotate", nil)
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("an expired key is not rotated", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"description": "Retired",
		})
		require.Equal(t, http.StatusOK, status, body)

		prefix := apiKeyPrefixOf(t, mintedKey(t, body))

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey/expire",
			map[string]any{"prefix": prefix})
		require.Equal(t, http.StatusOK, status, body)

		status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey/"+prefix+"/rotate", nil)
		assert.Equal(t, http.StatusConflict, status, "expiring a key revokes it; rotation must not revive it")
	})

	t.Run("a new expiration replaces the old one", func(t *testing.T) {
		expiry := time.Now().Add(72 * time.Hour).UTC()

		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"description": "Dated", "expiration": time.Now().Add(time.Hour).UTC(),
		})
		require.Equal(t, http.StatusOK, status, body)

		prefix := apiKeyPrefixOf(t, mintedKey(t, body))

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey/"+prefix+"/rotate",
			map[string]any{"expiration": expiry})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/apikey", nil)
		require.Equal(t, http.StatusOK, status, body)

		stored, ok := apiKeyByDescription(t, body, "Dated")["expiration"].(string)
		require.True(t, ok)

		parsed, err := time.Parse(time.RFC3339, stored)
		require.NoError(t, err)
		assert.WithinDuration(t, expiry, parsed, time.Second)
	})

	t.Run("a scoped key never rotates its way wider", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"userId": userID(itAdmin), "scopes": []string{"dns:read"}, "description": "Narrow",
		})
		require.Equal(t, http.StatusOK, status, body)

		narrow := mintedKey(t, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"userId": userID(itAdmin), "scopes": []string{"users"}, "description": "Wide",
		})
		require.Equal(t, http.StatusOK, status, body)

		wide := apiKeyPrefixOf(t, mintedKey(t, body))

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/apikey", map[string]any{
			"userId": userID(itAdmin), "description": "Whole role",
		})
		require.Equal(t, http.StatusOK, status, body)

		whole := apiKeyPrefixOf(t, mintedKey(t, body))

		status, _ = apiCall(t, client, narrow, http.MethodPost, v1+"/apikey/"+wide+"/rotate", nil)
		assert.Equal(t, http.StatusForbidden, status, "a dns:read key cannot re-mint a users key")

		status, _ = apiCall(t, client, narrow, http.MethodPost, v1+"/apikey/"+whole+"/rotate", nil)
		assert.Equal(t, http.StatusForbidden, status, "nor one standing for the whole role")

		status, _ = apiCall(t, client, narrow, http.MethodPost, v1+"/apikey/"+rotatedPrefix+"/rotate", nil)
		assert.Equal(t, http.StatusNotFound, status, "nor another owner's key")

		// Its own key it may rotate, and what comes back is no wider.
		status, body = apiCall(t, client, narrow, http.MethodPost,
			v1+"/apikey/"+apiKeyPrefixOf(t, narrow)+"/rotate", nil)
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, mintedKey(t, body), http.MethodGet, v1+"/whoami", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "permissions", "dns:read"))
		assert.Equal(t, false, field(t, body, "permissions", "users"))
	})

	t.Run("the rotation is audited", func(t *testing.T) {
		// The query narrows the log; the loop checks the action again, so
		// the case reads the rotations whatever else the log holds.
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/audit?action=apikey.rotate", nil)
		require.Equal(t, http.StatusOK, status, body)

		events, ok := body["events"].([]any)
		require.True(t, ok)
		require.NotEmpty(t, events, "apikey.rotate is recorded")

		var found bool

		for _, raw := range events {
			event, ok := raw.(map[string]any)
			require.True(t, ok)

			if event["action"] != "apikey.rotate" {
				continue
			}

			assert.Equal(t, "apikey", event["targetKind"])
			assert.NotEmpty(t, event["targetId"], "the event names the key that was rotated")

			if field(t, event, "detail", "newPrefix") == rotatedPrefix {
				found = true

				assert.NotEqual(t, rotatedPrefix, event["targetId"],
					"the target is the prefix the request addressed, the detail the new one")
			}
		}

		assert.True(t, found, "the event carries the prefix the key now answers to: %v", events)
	})
}
