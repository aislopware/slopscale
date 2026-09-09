package servertest_test

import (
	"net/http"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOAuthFederatedV1 covers the console's and the CLI's side of workload
// identity federation: an identity is created, listed, changed and revoked
// through /api/v1, and the identity v1 created is a real one, so the JWT its
// issuer signs is exchanged for an access token at the v2 endpoint. The
// issuer and its claims are the ones the v2 test uses.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestOAuthFederatedV1(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "fed-v1-owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	idp := startFederatedIssuer(t)

	var identityID, clientID string

	t.Run("create a federated identity", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"keyType":          "federated",
			"description":      "github actions",
			"scopes":           []string{"devices:core:read"},
			"issuer":           idp.URL,
			"audience":         fedAudience,
			"subject":          fedSubject,
			"customClaimRules": map[string]string{fedClaimName: fedClaimValue},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.NotContains(t, body, "clientSecret", "a federated identity has no secret to show")

		created, ok := body["oauthClient"].(map[string]any)
		require.True(t, ok)
		identityID, ok = created["clientId"].(string)
		require.True(t, ok)
		assert.Equal(t, "federated", created["keyType"])
		assert.Equal(t, idp.URL, created["issuer"])
		assert.Equal(t, fedAudience, created["audience"])
		assert.Equal(t, fedSubject, created["subject"])
		assert.Equal(t, map[string]any{fedClaimName: fedClaimValue}, created["customClaimRules"])
	})

	t.Run("an identity needs every trust condition", func(t *testing.T) {
		for _, missing := range []string{"issuer", "audience", "subject"} {
			body := map[string]any{
				"keyType": "federated", "scopes": []string{"devices:core:read"},
				"issuer": idp.URL, "audience": fedAudience, "subject": fedSubject,
			}
			body[missing] = ""

			status, _ := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", body)
			assert.Equal(t, http.StatusBadRequest, status, "without %s", missing)
		}
	})

	t.Run("the list shows both kinds", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/oauth-client", map[string]any{
			"description": "deploys", "scopes": []string{"devices:core:read"},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.NotEmpty(t, body["clientSecret"], "a client's secret is shown once")

		created, ok := body["oauthClient"].(map[string]any)
		require.True(t, ok)
		clientID, ok = created["clientId"].(string)
		require.True(t, ok)
		assert.Equal(t, "client", created["keyType"])
		assert.Empty(t, created["issuer"])
		assert.Equal(t, map[string]any{}, created["customClaimRules"])

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/oauth-client", nil)
		require.Equal(t, http.StatusOK, status, body)

		kinds := map[string]string{}

		listed, ok := body["oauthClients"].([]any)
		require.True(t, ok)

		for _, entry := range listed {
			row, isMap := entry.(map[string]any)
			require.True(t, isMap)

			id, isString := row["clientId"].(string)
			require.True(t, isString)

			kind, isString := row["keyType"].(string)
			require.True(t, isString)

			kinds[id] = kind
		}

		assert.Equal(t, "federated", kinds[identityID])
		assert.Equal(t, "client", kinds[clientID])
	})

	t.Run("the identity mints a token at the v2 exchange", func(t *testing.T) {
		status, body := exchangeFederatedToken(t.Context(), t, srv, identityID, idp.sign(t, idp.claims()))
		require.Equal(t, http.StatusOK, status, body)
		assert.NotEmpty(t, body["access_token"])
	})

	const narrowed = "repo:acme/app:ref:refs/heads/release"

	t.Run("a patch changes the subject and the tags", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/oauth-client/"+identityID,
			map[string]any{"subject": narrowed, "tags": []string{"tag:fed-v1"}})
		require.Equal(t, http.StatusOK, status, body)

		updated, ok := body["oauthClient"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, narrowed, updated["subject"])
		assert.Equal(t, []any{"tag:fed-v1"}, updated["tags"])
		assert.Equal(t, idp.URL, updated["issuer"], "an absent field keeps its value")
		assert.Equal(t, []any{"devices:core:read"}, updated["scopes"])
		assert.Equal(t, "github actions", updated["description"])
		assert.Equal(t, map[string]any{fedClaimName: fedClaimValue}, updated["customClaimRules"])

		// The old subject no longer satisfies the identity, the new one does.
		status, body = exchangeFederatedToken(t.Context(), t, srv, identityID, idp.sign(t, idp.claims()))
		assert.Equal(t, "invalid_grant", body["error"], "status %d body %v", status, body)

		signed := idp.sign(t, idp.claimsWith(map[string]any{"sub": narrowed}))
		status, body = exchangeFederatedToken(t.Context(), t, srv, identityID, signed)
		require.Equal(t, http.StatusOK, status, body)
		assert.NotEmpty(t, body["access_token"])
	})

	t.Run("a trust condition on a plain client is refused", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/oauth-client/"+clientID,
			map[string]any{"issuer": idp.URL})
		assert.Equal(t, http.StatusBadRequest, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/oauth-client/"+clientID,
			map[string]any{"description": "deploys, renamed"})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "deploys, renamed", field(t, body, "oauthClient", "description"))
	})

	t.Run("revoking deletes the identity", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/oauth-client/"+identityID, nil)
		require.Equal(t, http.StatusOK, status)

		signed := idp.sign(t, idp.claimsWith(map[string]any{"sub": narrowed}))
		status, body := exchangeFederatedToken(t.Context(), t, srv, identityID, signed)
		assert.Equal(t, http.StatusUnauthorized, status)
		assert.Equal(t, "invalid_client", body["error"], "body: %v", body)

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/oauth-client/"+identityID,
			map[string]any{"subject": fedSubject})
		assert.Equal(t, http.StatusNotFound, status, body)
	})
}
