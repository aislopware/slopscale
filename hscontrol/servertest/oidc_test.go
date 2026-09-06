package servertest_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/oauth2-proxy/mockoidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	oidcLoginTimeout = 30 * time.Second

	// oidcTokenTTL is the lifetime the mock provider stamps on its id tokens.
	// It is deliberately unlike any node expiry default so a node whose
	// expiry was copied from the token is unmistakable.
	oidcTokenTTL = 42 * time.Minute

	// registerConfirmCSRFField is the hidden form field (and cookie) the
	// registration confirmation interstitial uses for its CSRF token.
	registerConfirmCSRFField = "headscale_register_confirm"
)

// csrfInputRE finds the hidden CSRF input on the confirmation interstitial
// regardless of attribute order.
var csrfInputRE = regexp.MustCompile(`<input[^>]*name="` + registerConfirmCSRFField + `"[^>]*>`)

var csrfValueRE = regexp.MustCompile(`value="([^"]*)"`)

// oidcUser builds a mock identity. The provider only releases claims the
// requested scopes cover, so the test config asks for profile, email and
// groups.
func oidcUser(subject, email string, verified bool, groups ...string) mockoidc.MockUser {
	return mockoidc.MockUser{
		Subject:           subject,
		PreferredUsername: subject,
		Email:             email,
		EmailVerified:     verified,
		Groups:            groups,
	}
}

// startMockOIDC runs an in-process OpenID Connect provider on a real loopback
// port, because Headscale reaches the issuer with a plain [http.Client].
// Logins pop users off a queue in order, so every test gets its own provider.
func startMockOIDC(t *testing.T, users ...mockoidc.MockUser) *mockoidc.MockOIDC {
	t.Helper()

	provider, err := mockoidc.NewServer(nil)
	require.NoError(t, err)

	provider.AccessTTL = oidcTokenTTL

	for i := range users {
		provider.QueueUser(&users[i])
	}

	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	require.NoError(t, provider.Start(ln, nil))
	t.Cleanup(func() { assert.NoError(t, provider.Shutdown()) })

	return provider
}

// newOIDCServer starts a mock provider queued with users and a Headscale
// server that authenticates against it. mutate, when non-nil, adjusts the
// OIDC config before the server starts.
func newOIDCServer(
	t *testing.T,
	mutate func(*types.OIDCConfig),
	users ...mockoidc.MockUser,
) (*servertest.TestServer, *mockoidc.MockOIDC) {
	t.Helper()

	provider := startMockOIDC(t, users...)

	cfg := types.OIDCConfig{
		Issuer:       provider.Issuer(),
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Scope:        []string{"openid", "profile", "email", "groups"},
	}
	if mutate != nil {
		mutate(&cfg)
	}

	return servertest.NewServer(t, servertest.WithOIDC(cfg)), provider
}

// browse GETs target with a browser-like client, following the whole
// redirect chain, and returns the status and body the chain ends on.
func browse(t *testing.T, client *http.Client, target string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(body)
}

// csrfTokenFromInterstitial pulls the CSRF token out of the confirmation
// page the OIDC callback rendered.
func csrfTokenFromInterstitial(t *testing.T, body string) string {
	t.Helper()

	input := csrfInputRE.FindString(body)
	require.NotEmpty(t, input, "interstitial has no CSRF input:\n%s", body)

	match := csrfValueRE.FindStringSubmatch(input)
	require.Len(t, match, 2, "CSRF input has no value: %s", input)
	require.NotEmpty(t, match[1])

	return match[1]
}

// submitConfirm POSTs the confirmation form the way the interstitial's
// submit button does, and returns the status and body of the response. Extra
// cookies are added on top of whatever the client's jar sends.
func submitConfirm(
	t *testing.T,
	client *http.Client,
	srv *servertest.TestServer,
	authID types.AuthID,
	form url.Values,
	cookies ...*http.Cookie,
) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		srv.URL+"/register/confirm/"+authID.String(),
		strings.NewReader(form.Encode()),
	)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(body)
}

// completeOIDCLogin drives the browser half of an interactive login for a
// pending registration: open the AuthURL, get bounced through the provider
// and back to the callback, then submit the confirmation interstitial. It
// returns the body of the final success page.
func completeOIDCLogin(
	t *testing.T,
	srv *servertest.TestServer,
	client *http.Client,
	login *servertest.PendingLogin,
) string {
	t.Helper()

	status, body := browse(t, client, login.AuthURL)
	require.Equal(t, http.StatusOK, status, "callback did not render the interstitial:\n%s", body)
	require.Contains(t, body, "Confirm node registration")

	form := url.Values{registerConfirmCSRFField: {csrfTokenFromInterstitial(t, body)}}

	status, body = submitConfirm(t, client, srv, login.AuthID, form)
	require.Equal(t, http.StatusOK, status, "confirm did not succeed:\n%s", body)

	return body
}

// nodeByHostname finds a node in the server's node store by hostname.
func nodeByHostname(t *testing.T, srv *servertest.TestServer, hostname string) types.NodeView {
	t.Helper()

	for _, node := range srv.State().ListNodes().All() {
		if node.Hostname() == hostname {
			return node
		}
	}

	t.Fatalf("no node with hostname %q", hostname)

	return types.NodeView{}
}

// soleUser asserts that exactly one user exists and returns it.
func soleUser(t *testing.T, srv *servertest.TestServer) types.User {
	t.Helper()

	users, err := srv.State().ListAllUsers()
	require.NoError(t, err)
	require.Len(t, users, 1)

	return users[0]
}

// TestOIDCLogin walks a node through a complete OIDC registration: the
// client asks to log in, a browser follows the AuthURL through the identity
// provider and confirms the interstitial, and the client's held follow-up
// request comes back with a registered node.
func TestOIDCLogin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pkce types.PKCEConfig
	}{
		{name: "without_pkce"},
		{name: "pkce_s256", pkce: types.PKCEConfig{Enabled: true, Method: types.PKCEMethodS256}},
		{name: "pkce_plain", pkce: types.PKCEConfig{Enabled: true, Method: types.PKCEMethodPlain}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv, provider := newOIDCServer(t,
				func(cfg *types.OIDCConfig) { cfg.PKCE = tt.pkce },
				oidcUser("alice", "alice@example.com", true, "engineering"),
			)

			login := servertest.NewPendingLogin(t, srv, "alice-laptop")
			_, found := srv.State().GetAuthCacheEntry(login.AuthID)
			require.True(t, found, "the pending registration must be cached")

			body := completeOIDCLogin(t, srv, srv.HTTPClient(t), login)
			assert.Contains(t, body, "Node registered")

			client := login.Wait(t, oidcLoginTimeout)
			nm := client.WaitForUpdate(t, oidcLoginTimeout)
			require.True(t, nm.SelfNode.Valid())
			assert.Equal(t, "alice-laptop", nm.SelfNode.Hostinfo().Hostname())

			profile, ok := nm.UserProfiles[nm.SelfNode.User()]
			require.True(t, ok, "netmap must carry the profile of the OIDC user")
			assert.Equal(t, "alice@example.com", profile.LoginName(),
				"the login name a client sees is the user's email")

			user := soleUser(t, srv)
			assert.Equal(t, "alice", user.Name)
			assert.Equal(t, "alice@example.com", user.Email)
			assert.Equal(t, util.RegisterMethodOIDC, user.Provider)
			assert.Equal(t, provider.Issuer()+"/alice", user.ProviderIdentifier.String)

			node := nodeByHostname(t, srv, "alice-laptop")
			assert.Equal(t, util.RegisterMethodOIDC, node.RegisterMethod())
			assert.False(t, node.IsTagged())
			require.True(t, node.User().Valid())
			assert.Equal(t, user.ID, node.User().ID())

			_, found = srv.State().GetAuthCacheEntry(login.AuthID)
			assert.False(t, found, "a finished registration must leave the auth cache")
		})
	}
}

// TestOIDCReloginUpdatesUser checks that a second login by the same subject
// updates the existing user from the new claims instead of creating another
// one, and that both nodes hang off that single user.
func TestOIDCReloginUpdatesUser(t *testing.T) {
	t.Parallel()

	srv, provider := newOIDCServer(t, nil,
		oidcUser("bob", "bob@example.com", true),
		mockoidc.MockUser{
			Subject:           "bob",
			PreferredUsername: "robert",
			Email:             "robert@example.com",
			EmailVerified:     true,
		},
	)

	first := servertest.NewPendingLogin(t, srv, "bob-desktop")
	completeOIDCLogin(t, srv, srv.HTTPClient(t), first)
	first.Wait(t, oidcLoginTimeout)

	user := soleUser(t, srv)
	assert.Equal(t, "bob", user.Name)
	assert.Equal(t, "bob@example.com", user.Email)

	second := servertest.NewPendingLogin(t, srv, "bob-phone")
	body := completeOIDCLogin(t, srv, srv.HTTPClient(t), second)
	assert.Contains(t, body, "Node registered")
	second.Wait(t, oidcLoginTimeout)

	updated := soleUser(t, srv)
	assert.Equal(t, user.ID, updated.ID, "the same subject must map to the same user")
	assert.Equal(t, "robert", updated.Name)
	assert.Equal(t, "robert@example.com", updated.Email)
	assert.Equal(t, provider.Issuer()+"/bob", updated.ProviderIdentifier.String)

	assert.Equal(t, 2, srv.State().ListNodesByUser(types.UserID(user.ID)).Len())
}

// TestOIDCReloginSameNodeReauthenticates has a registered node log out and
// log in again through OIDC, which must reauthenticate the existing node
// rather than mint a second one.
func TestOIDCReloginSameNodeReauthenticates(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t, nil,
		oidcUser("carol", "carol@example.com", true),
		oidcUser("carol", "carol@example.com", true),
	)

	login := servertest.NewPendingLogin(t, srv, "carol-laptop")
	completeOIDCLogin(t, srv, srv.HTTPClient(t), login)
	client := login.Wait(t, oidcLoginTimeout)
	client.WaitForUpdate(t, oidcLoginTimeout)

	nodeID := nodeByHostname(t, srv, "carol-laptop").ID()

	ctx, cancel := context.WithTimeout(t.Context(), oidcLoginTimeout)
	defer cancel()

	require.NoError(t, client.LogoutAndDisconnect(ctx))

	relogin := client.StartInteractiveRelogin(t)
	completeOIDCLogin(t, srv, srv.HTTPClient(t), relogin)
	relogin.Wait(t, oidcLoginTimeout)
	client.WaitForUpdate(t, oidcLoginTimeout)

	assert.Equal(t, 1, srv.State().ListNodes().Len(), "relogin must not create a second node")

	node := nodeByHostname(t, srv, "carol-laptop")
	assert.Equal(t, nodeID, node.ID())
	assert.False(t, node.IsExpired(), "a reauthenticated node must no longer be expired")
}

// TestOIDCAuthorization covers the allowed_domains, allowed_users and
// allowed_groups gates, and the email verification requirement that guards
// the email-based ones. A denied login must leave no user behind and keep
// the registration pending so the person can retry.
func TestOIDCAuthorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*types.OIDCConfig)
		user       mockoidc.MockUser
		wantStatus int
	}{
		{
			name:       "allowed_domains_accepts_matching_domain",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedDomains = []string{"example.com"} },
			user:       oidcUser("dana", "dana@example.com", true),
			wantStatus: http.StatusOK,
		},
		{
			name:       "allowed_domains_denies_other_domain",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedDomains = []string{"example.com"} },
			user:       oidcUser("dana", "dana@example.org", true),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "allowed_domains_denies_domain_suffix_trick",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedDomains = []string{"example.com"} },
			user:       oidcUser("dana", "dana@notexample.com", true),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "allowed_users_accepts_listed_email",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedUsers = []string{"erin@example.com"} },
			user:       oidcUser("erin", "erin@example.com", true),
			wantStatus: http.StatusOK,
		},
		{
			name:       "allowed_users_denies_unlisted_email",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedUsers = []string{"erin@example.com"} },
			user:       oidcUser("frank", "frank@example.com", true),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "allowed_groups_accepts_member",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedGroups = []string{"admins"} },
			user:       oidcUser("grace", "grace@example.com", true, "staff", "admins"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "allowed_groups_denies_non_member",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedGroups = []string{"admins"} },
			user:       oidcUser("heidi", "heidi@example.com", true, "staff"),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "allowed_groups_denies_user_without_groups",
			mutate:     func(cfg *types.OIDCConfig) { cfg.AllowedGroups = []string{"admins"} },
			user:       oidcUser("ivan", "ivan@example.com", true),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "unverified_email_denied_when_verification_required",
			mutate: func(cfg *types.OIDCConfig) {
				cfg.EmailVerifiedRequired = true
				cfg.AllowedDomains = []string{"example.com"}
			},
			user:       oidcUser("judy", "judy@example.com", false),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "unverified_email_accepted_when_verification_not_required",
			mutate: func(cfg *types.OIDCConfig) {
				cfg.AllowedDomains = []string{"example.com"}
			},
			user:       oidcUser("judy", "judy@example.com", false),
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv, _ := newOIDCServer(t, tt.mutate, tt.user)

			login := servertest.NewPendingLogin(t, srv, "node-"+tt.user.Subject)
			status, body := browse(t, srv.HTTPClient(t), login.AuthURL)
			assert.Equal(t, tt.wantStatus, status, "unexpected callback outcome:\n%s", body)

			users, err := srv.State().ListAllUsers()
			require.NoError(t, err)

			_, stillPending := srv.State().GetAuthCacheEntry(login.AuthID)

			if tt.wantStatus == http.StatusOK {
				assert.Len(t, users, 1, "an authorised login creates the user")
				assert.Contains(t, body, "Confirm node registration")
			} else {
				assert.Empty(t, users, "a denied login must not create a user")
				assert.Contains(t, body, http.StatusText(tt.wantStatus))
			}

			assert.True(t, stillPending, "the registration stays pending until it is confirmed")
		})
	}
}

// TestOIDCUnverifiedEmailNotStored checks that with email verification
// required, an unverified email still lets the user in when no email-based
// gate is configured, but the address is not recorded on the user.
func TestOIDCUnverifiedEmailNotStored(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t,
		func(cfg *types.OIDCConfig) { cfg.EmailVerifiedRequired = true },
		oidcUser("kim", "kim@example.com", false),
	)

	login := servertest.NewPendingLogin(t, srv, "kim-laptop")
	completeOIDCLogin(t, srv, srv.HTTPClient(t), login)

	user := soleUser(t, srv)
	assert.Equal(t, "kim", user.Name)
	assert.Empty(t, user.Email, "an unverified email must not be stored")
}

// TestOIDCNodeExpiryFromToken checks that use_expiry_from_token copies the id
// token's expiry onto the node, and that without it the node's expiry is
// unrelated to the token.
func TestOIDCNodeExpiryFromToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		useExpiryFromToken bool
	}{
		{name: "from_token", useExpiryFromToken: true},
		{name: "not_from_token", useExpiryFromToken: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv, _ := newOIDCServer(t,
				func(cfg *types.OIDCConfig) { cfg.UseExpiryFromToken = tt.useExpiryFromToken },
				oidcUser("leo", "leo@example.com", true),
			)

			login := servertest.NewPendingLogin(t, srv, "leo-laptop")
			completeOIDCLogin(t, srv, srv.HTTPClient(t), login)
			login.Wait(t, oidcLoginTimeout)

			node := nodeByHostname(t, srv, "leo-laptop")
			tokenExpiry := time.Now().Add(oidcTokenTTL)

			if tt.useExpiryFromToken {
				require.True(t, node.Expiry().Valid(), "node must carry the token expiry")
				assert.WithinDuration(t, tokenExpiry, node.Expiry().Get(), 2*time.Minute)

				return
			}

			if node.Expiry().Valid() && !node.Expiry().Get().IsZero() {
				assert.Greater(t, node.Expiry().Get().Sub(tokenExpiry).Abs(), 2*time.Minute,
					"node expiry must not be derived from the token")
			}
		})
	}
}

// TestOIDCCallbackRejectsBadState hits /oidc/callback directly with the ways
// the state parameter can be wrong or unbacked by a browser session. None of
// these may reach the provider with a real login.
func TestOIDCCallbackRejectsBadState(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t, nil)

	tests := []struct {
		name       string
		query      url.Values
		cookies    map[string]string
		wantStatus int
	}{
		{
			name:       "no_parameters",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_state",
			query:      url.Values{"code": {"somecode"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing_code",
			query:      url.Values{"state": {"abcdefghijklmnop"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "state_too_short_for_cookie_name",
			query:      url.Values{"code": {"somecode"}, "state": {"abc"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "state_without_cookie",
			query:      url.Values{"code": {"somecode"}, "state": {"abcdefghijklmnop"}},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "state_cookie_mismatch",
			query:      url.Values{"code": {"somecode"}, "state": {"abcdefghijklmnop"}},
			cookies:    map[string]string{"state_abcdef": "abcdefsomethingelse"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "unknown_code_with_matching_cookie",
			query:      url.Values{"code": {"somecode"}, "state": {"abcdefghijklmnop"}},
			cookies:    map[string]string{"state_abcdef": "abcdefghijklmnop"},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := srv.URL + "/oidc/callback"
			if len(tt.query) > 0 {
				target += "?" + tt.query.Encode()
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
			require.NoError(t, err)

			for name, value := range tt.cookies {
				//nolint:gosec // G124: a request cookie; security attributes only apply to Set-Cookie
				req.AddCookie(&http.Cookie{Name: name, Value: value})
			}

			resp, err := srv.HTTPClient(t).Do(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStatus, resp.StatusCode, "body:\n%s", body)
			assert.Contains(t, string(body), http.StatusText(tt.wantStatus))
		})
	}

	users, err := srv.State().ListAllUsers()
	require.NoError(t, err)
	assert.Empty(t, users, "no bad-state request may create a user")
}

// TestOIDCRegistrationSessionLost covers auth ids the server does not know,
// whether they never existed or expired out of the cache before the browser
// reached the callback or the confirmation step.
func TestOIDCRegistrationSessionLost(t *testing.T) {
	t.Parallel()

	t.Run("malformed_auth_id", func(t *testing.T) {
		t.Parallel()

		srv, _ := newOIDCServer(t, nil)

		status, body := browse(t, srv.HTTPClient(t), srv.URL+"/register/not-an-auth-id")
		assert.Equal(t, http.StatusBadRequest, status, "body:\n%s", body)
	})

	t.Run("unknown_auth_id", func(t *testing.T) {
		t.Parallel()

		srv, _ := newOIDCServer(t, nil, oidcUser("mia", "mia@example.com", true))

		authID, err := types.NewAuthID()
		require.NoError(t, err)

		status, body := browse(t, srv.HTTPClient(t), srv.URL+"/register/"+authID.String())
		assert.Equal(t, http.StatusGone, status, "body:\n%s", body)

		users, err := srv.State().ListAllUsers()
		require.NoError(t, err)
		assert.Len(t, users, 1, "the identity was verified before the registration was looked up")
		assert.Empty(t, srv.State().ListNodes().AsSlice(), "no node may be registered")
	})

	t.Run("expired_before_callback", func(t *testing.T) {
		t.Parallel()

		srv, _ := newOIDCServer(t, nil, oidcUser("nina", "nina@example.com", true))

		login := servertest.NewPendingLogin(t, srv, "nina-laptop")
		srv.State().DeleteAuthCacheEntryForTest(login.AuthID)

		status, body := browse(t, srv.HTTPClient(t), login.AuthURL)
		assert.Equal(t, http.StatusGone, status, "body:\n%s", body)
		assert.Empty(t, srv.State().ListNodes().AsSlice(), "no node may be registered")
	})

	t.Run("expired_before_confirm", func(t *testing.T) {
		t.Parallel()

		srv, _ := newOIDCServer(t, nil, oidcUser("otto", "otto@example.com", true))
		client := srv.HTTPClient(t)

		login := servertest.NewPendingLogin(t, srv, "otto-laptop")

		status, body := browse(t, client, login.AuthURL)
		require.Equal(t, http.StatusOK, status, "body:\n%s", body)

		form := url.Values{registerConfirmCSRFField: {csrfTokenFromInterstitial(t, body)}}

		srv.State().DeleteAuthCacheEntryForTest(login.AuthID)

		status, body = submitConfirm(t, client, srv, login.AuthID, form)
		assert.Equal(t, http.StatusGone, status, "body:\n%s", body)
		assert.Empty(t, srv.State().ListNodes().AsSlice(), "no node may be registered")
	})
}

// TestOIDCRegisterConfirmCSRF checks that the confirmation POST is only
// honoured when the form token matches the cookie the callback set, and
// that failed attempts leave the pending registration usable.
func TestOIDCRegisterConfirmCSRF(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t, nil, oidcUser("pat", "pat@example.com", true))
	browser := srv.HTTPClient(t)

	login := servertest.NewPendingLogin(t, srv, "pat-laptop")

	status, body := browse(t, browser, login.AuthURL)
	require.Equal(t, http.StatusOK, status, "body:\n%s", body)

	token := csrfTokenFromInterstitial(t, body)

	// A form with no token is rejected before any cookie is looked at.
	status, body = submitConfirm(t, browser, srv, login.AuthID, url.Values{})
	assert.Equal(t, http.StatusBadRequest, status, "missing token; body:\n%s", body)

	// A token that does not match the cookie is rejected.
	status, body = submitConfirm(t, browser, srv, login.AuthID,
		url.Values{registerConfirmCSRFField: {"not-the-token"}})
	assert.Equal(t, http.StatusForbidden, status, "mismatched token; body:\n%s", body)

	// The right token from a browser without the cookie is rejected, so a
	// leaked token alone cannot finish someone else's registration.
	status, body = submitConfirm(t, srv.HTTPClient(t), srv, login.AuthID,
		url.Values{registerConfirmCSRFField: {token}})
	assert.Equal(t, http.StatusForbidden, status, "missing cookie; body:\n%s", body)

	// A registration the callback never authorised is rejected even when the
	// cookie and the form agree, so a matching pair minted elsewhere cannot
	// finish an arbitrary pending registration.
	otherLogin := servertest.NewPendingLogin(t, srv, "pat-phone")
	status, body = submitConfirm(t, srv.HTTPClient(t), srv, otherLogin.AuthID,
		url.Values{registerConfirmCSRFField: {token}},
		//nolint:gosec // G124: a request cookie; security attributes only apply to Set-Cookie
		&http.Cookie{Name: registerConfirmCSRFField, Value: token})
	assert.Equal(t, http.StatusForbidden, status, "unauthorised registration; body:\n%s", body)

	assert.Empty(t, srv.State().ListNodes().AsSlice(), "no rejected attempt may register a node")

	// The genuine submission still works after the rejected attempts.
	status, body = submitConfirm(t, browser, srv, login.AuthID,
		url.Values{registerConfirmCSRFField: {token}})
	require.Equal(t, http.StatusOK, status, "body:\n%s", body)
	assert.Contains(t, body, "Node registered")

	login.Wait(t, oidcLoginTimeout)
	assert.Equal(t, 1, srv.State().ListNodes().Len())
}
