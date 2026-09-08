package servertest_test

import (
	"net/http"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOIDCGroupSync mirrors the identity provider's groups claim into
// headscale groups: the first login creates the claimed groups with the
// user in them, a later login with a different claim moves the user, and
// the console cannot edit the users of a synced group.
func TestOIDCGroupSync(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t,
		func(cfg *types.OIDCConfig) { cfg.Groups = types.OIDCGroupsConfig{Sync: true, Prefix: "hs-"} },
		oidcUser("carol", "carol@example.com", true, "hs-eng", "hs-sre", "unrelated"),
		oidcUser("carol", "carol@example.com", true, "hs-eng", "hs-ops"),
	)
	client := srv.HTTPClient(t)
	apiKey := srv.CreateAPIKey(t, nil)
	v1 := srv.URL + "/api/v1"

	first := servertest.NewPendingLogin(t, srv, "carol-laptop")
	completeOIDCLogin(t, srv, client, first)
	first.Wait(t, oidcLoginTimeout)

	user := soleUser(t, srv)

	groups := func() map[string]map[string]any {
		status, body := apiCall(t, client, apiKey, http.MethodGet, v1+"/group", nil)
		require.Equal(t, http.StatusOK, status, body)

		out := map[string]map[string]any{}

		list, ok := body["groups"].([]any)
		require.True(t, ok, "groups list: %v", body)

		for _, item := range list {
			g, ok := item.(map[string]any)
			require.True(t, ok)

			name, ok := g["name"].(string)
			require.True(t, ok)

			out[name] = g
		}

		return out
	}

	got := groups()
	require.Contains(t, got, "eng", "the prefix is stripped")
	require.Contains(t, got, "sre")
	assert.NotContains(t, got, "unrelated", "a claim without the prefix is ignored")
	assert.Equal(t, "oidc", got["eng"]["source"])
	assert.Equal(t, []any{userID(&user)}, got["eng"]["userIds"])
	assert.Equal(t, []any{userID(&user)}, got["sre"]["userIds"])

	engID, ok := got["eng"]["id"].(string)
	require.True(t, ok)

	status, body := apiCall(t, client, apiKey, http.MethodDelete, v1+"/group/"+engID+"/user/"+userID(&user), nil)
	assert.Equal(t, http.StatusBadRequest, status, "the users of a synced group follow the claim: %v", body)

	// A rename is refused, and a stale user list is refused before the
	// description is written.
	status, body = apiCall(t, client, apiKey, http.MethodPatch, v1+"/group/"+engID,
		map[string]any{"name": "engineering", "description": "renamed"})
	assert.Equal(t, http.StatusBadRequest, status, "a synced group keeps its name: %v", body)
	status, body = apiCall(t, client, apiKey, http.MethodPatch, v1+"/group/"+engID,
		map[string]any{"name": "eng", "description": "half written", "userIds": []string{}})
	assert.Equal(t, http.StatusBadRequest, status, "a changed user list is refused: %v", body)
	status, body = apiCall(t, client, apiKey, http.MethodGet, v1+"/group/"+engID, nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Empty(t, field(t, body, "group", "description"), "nothing was written before the refusal")
	status, body = apiCall(t, client, apiKey, http.MethodPatch, v1+"/group/"+engID,
		map[string]any{"name": "eng", "description": "described"})
	assert.Equal(t, http.StatusOK, status, "the description of a synced group can change: %v", body)

	second := servertest.NewPendingLogin(t, srv, "carol-phone")
	completeOIDCLogin(t, srv, client, second)
	second.Wait(t, oidcLoginTimeout)

	got = groups()
	assert.Equal(t, []any{userID(&user)}, got["eng"]["userIds"], "still in eng")
	assert.Equal(t, []any{}, got["sre"]["userIds"], "left sre, which stays for its next member")
	assert.Equal(t, []any{userID(&user)}, got["ops"]["userIds"], "joined ops")

	status, body = apiCall(t, client, apiKey, http.MethodGet, v1+"/audit", nil)
	require.Equal(t, http.StatusOK, status, body)

	events, ok := body["events"].([]any)
	require.True(t, ok)

	synced := 0

	for _, e := range events {
		event, ok := e.(map[string]any)
		require.True(t, ok)

		if event["action"] == "group.sync" {
			synced++
		}
	}

	assert.Equal(t, 2, synced, "each login that moved a membership is logged")
}
