package servertest_test

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOIDCMatchByEmail covers a provider switch: a login whose iss/sub is
// unknown takes over the existing OIDC user with the same verified email
// when oidc.match_by_email is on, keeping the user's machines, and creates
// a fresh user when it is off.
func TestOIDCMatchByEmail(t *testing.T) {
	t.Parallel()

	t.Run("the login takes over the user", func(t *testing.T) {
		t.Parallel()

		srv, provider := newOIDCServer(t,
			func(cfg *types.OIDCConfig) { cfg.MatchByEmail = true },
			oidcUser("dana", "dana@example.com", true),
			oidcUser("dana", "dana@example.com", true),
		)

		first := servertest.NewPendingLogin(t, srv, "dana-laptop")
		completeOIDCLogin(t, srv, srv.HTTPClient(t), first)
		first.Wait(t, oidcLoginTimeout)

		user := soleUser(t, srv)
		require.Equal(t, provider.Issuer()+"/dana", user.ProviderIdentifier.String)

		// The old provider is gone: the user carries its identifier.
		_, _, err := srv.State().UpdateUser(types.UserID(user.ID), func(u *types.User) error {
			u.ProviderIdentifier = sql.NullString{String: "https://old.example/dana", Valid: true}

			return nil
		})
		require.NoError(t, err)

		second := servertest.NewPendingLogin(t, srv, "dana-phone")
		body := completeOIDCLogin(t, srv, srv.HTTPClient(t), second)
		assert.Contains(t, body, "Node registered")
		second.Wait(t, oidcLoginTimeout)

		updated := soleUser(t, srv)
		assert.Equal(t, user.ID, updated.ID, "the same person, not a second user")
		assert.Equal(t, provider.Issuer()+"/dana", updated.ProviderIdentifier.String, "now known by the new provider")
		assert.Equal(t, 2, srv.State().ListNodesByUser(types.UserID(user.ID)).Len())

		apiKey := srv.CreateAPIKey(t, nil)
		status, audit := apiCall(t, srv.HTTPClient(t), apiKey, http.MethodGet, srv.URL+"/api/v1/audit", nil)
		require.Equal(t, http.StatusOK, status, audit)

		events, ok := audit["events"].([]any)
		require.True(t, ok)

		switched := false

		for _, e := range events {
			event, ok := e.(map[string]any)
			require.True(t, ok)

			if event["action"] == "user.provider.switch" {
				switched = true
			}
		}

		assert.True(t, switched, "the switch is audited")
	})

	t.Run("off, the login is a new user", func(t *testing.T) {
		t.Parallel()

		srv, _ := newOIDCServer(t, nil, oidcUser("erin", "erin@example.com", true))

		existing, _, err := srv.State().CreateUser(types.User{
			Name:               "erin-old",
			Email:              "erin@example.com",
			Provider:           "oidc",
			ProviderIdentifier: sql.NullString{String: "https://old.example/erin", Valid: true},
		})
		require.NoError(t, err)

		login := servertest.NewPendingLogin(t, srv, "erin-laptop")
		completeOIDCLogin(t, srv, srv.HTTPClient(t), login)
		login.Wait(t, oidcLoginTimeout)

		users, err := srv.State().ListAllUsers()
		require.NoError(t, err)
		assert.Len(t, users, 2, "a second user was created next to %s", existing.Name)
	})
}
