package hscontrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newConfirmRequest(t *testing.T, authID types.AuthID, formCSRF, cookieCSRF string) *http.Request {
	t.Helper()

	form := strings.NewReader(registerConfirmCSRFCookie + "=" + formCSRF)
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/register/confirm/"+authID.String(),
		form,
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{
		Name:  registerConfirmCSRFCookie,
		Value: cookieCSRF,
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("auth_id", authID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	return req
}

// TestRegisterConfirmHandler_RejectsCSRFMismatch verifies that the
// /register/confirm POST handler refuses to finalise a pending
// registration when the form CSRF token does not match the cookie.
func TestRegisterConfirmHandler_RejectsCSRFMismatch(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	provider := &AuthProviderOIDC{h: app}

	// Mint a pending registration with a stashed pending-confirmation,
	// as the OIDC callback would have done after resolving the user
	// identity but before the user clicked the interstitial form.
	authID := types.MustAuthID()
	regReq := types.NewRegisterAuthRequest(&types.RegistrationData{
		Hostname: "phish-target",
	})
	regReq.SetPendingConfirmation(&types.PendingRegistrationConfirmation{
		UserID: 1,
		CSRF:   "expected-csrf",
	})
	app.state.SetAuthCacheEntry(authID, regReq)

	rec := httptest.NewRecorder()
	provider.RegisterConfirmHandler(rec,
		newConfirmRequest(t, authID, "wrong-csrf", "expected-csrf"),
	)

	assert.Equal(t, http.StatusForbidden, rec.Code,
		"CSRF cookie/form mismatch must be rejected with 403")

	// And the registration must still be pending — the rejected POST
	// must not have called [AuthProviderOIDC.handleRegistration].
	cached, ok := app.state.GetAuthCacheEntry(authID)
	require.True(t, ok, "rejected POST must not evict the cached registration")
	require.NotNil(t, cached.PendingConfirmation(),
		"rejected POST must not clear the pending confirmation")
}

// TestRegisterConfirmHandler_RejectsWithoutPending verifies that
// /register/confirm refuses to finalise a registration that did not
// first complete the OIDC interstitial. Without this check an attacker
// who knew an auth_id could POST directly to the confirm endpoint and
// claim the device.
func TestRegisterConfirmHandler_RejectsWithoutPending(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	provider := &AuthProviderOIDC{h: app}

	authID := types.MustAuthID()
	// Cached registration with NO pending confirmation set — i.e. the
	// OIDC callback has not run yet.
	app.state.SetAuthCacheEntry(authID, types.NewRegisterAuthRequest(
		&types.RegistrationData{Hostname: "no-oidc-yet"},
	))

	rec := httptest.NewRecorder()
	provider.RegisterConfirmHandler(rec,
		newConfirmRequest(t, authID, "fake", "fake"),
	)

	assert.Equal(t, http.StatusForbidden, rec.Code,
		"confirm without prior OIDC pending state must be rejected with 403")
}

func newConfirmGetRequest(t *testing.T, authID types.AuthID, cookieCSRF string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/register/confirm/"+authID.String(),
		nil,
	)

	if cookieCSRF != "" {
		req.AddCookie(&http.Cookie{
			Name:  registerConfirmCSRFCookie,
			Value: cookieCSRF,
		})
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("auth_id", authID.String())

	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestBeginRegistrationConfirmationRedirects proves the OIDC callback no
// longer renders the interstitial on the URL that carries the single-use
// authorization code. Anything that reloads that URL — the back button,
// pull-to-refresh, an extension — would re-enter the spent code exchange and
// paint an error over the page the user was told to click.
func TestBeginRegistrationConfirmationRedirects(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	provider := &AuthProviderOIDC{h: app, serverURL: "http://localhost:8080"}

	authID := types.MustAuthID()
	app.state.SetAuthCacheEntry(authID, types.NewRegisterAuthRequest(
		&types.RegistrationData{Hostname: "redirect-me"},
	))

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/oidc/callback?code=c&state=s", nil)

	provider.beginRegistrationConfirmation(rec, req, authID, &types.User{ID: 1}, nil)

	require.Equal(t, http.StatusSeeOther, rec.Code,
		"the code-bearing callback URL must be left behind with a 303, not rendered on")
	assert.Equal(t, "http://localhost:8080/register/confirm/"+authID.String(), rec.Header().Get("Location"))

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, registerConfirmCSRFCookie, cookies[0].Name)
	assert.NotEmpty(t, cookies[0].Value)
	assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite,
		"Strict is withheld on the hop out of the IdP redirect chain, so the page would 403")

	cached, ok := app.state.GetAuthCacheEntry(authID)
	require.True(t, ok)
	require.NotNil(t, cached.PendingConfirmation())
	assert.Equal(t, cookies[0].Value, cached.PendingConfirmation().CSRF,
		"the cookie must carry the token cached for the confirmation page")
}

// TestRegisterConfirmGetHandlerIsReloadable proves the confirmation page can
// be fetched, and re-fetched, from its own URL: it only reads the pending
// confirmation and never touches the one-time code exchange.
func TestRegisterConfirmGetHandlerIsReloadable(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	provider := &AuthProviderOIDC{h: app, serverURL: "http://localhost:8080"}

	user, _, err := app.state.CreateUser(types.User{Name: "confirm-user"})
	require.NoError(t, err)

	authID := types.MustAuthID()
	regReq := types.NewRegisterAuthRequest(&types.RegistrationData{Hostname: "reload-me"})
	regReq.SetPendingConfirmation(&types.PendingRegistrationConfirmation{
		UserID: user.ID,
		CSRF:   "expected-csrf",
	})
	app.state.SetAuthCacheEntry(authID, regReq)

	for range 2 {
		rec := httptest.NewRecorder()
		provider.RegisterConfirmGetHandler(rec, newConfirmGetRequest(t, authID, "expected-csrf"))

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"),
			"the page carries the token that finalises the registration")
		assert.Contains(t, rec.Body.String(), "reload-me")
		assert.Contains(t, rec.Body.String(),
			"http://localhost:8080/register/confirm/"+authID.String(),
			"the form must post to the browser-facing URL, which carries any proxy prefix")
	}

	cached, ok := app.state.GetAuthCacheEntry(authID)
	require.True(t, ok, "rendering the page must not consume the registration")
	require.NotNil(t, cached.PendingConfirmation())
}

// TestRegisterConfirmGetHandlerRequiresCSRFCookie asserts the device details,
// and the token that finalises the registration, stay away from anyone who
// merely knows the auth ID — which the node being registered does.
func TestRegisterConfirmGetHandlerRequiresCSRFCookie(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	provider := &AuthProviderOIDC{h: app, serverURL: "http://localhost:8080"}

	authID := types.MustAuthID()
	regReq := types.NewRegisterAuthRequest(&types.RegistrationData{Hostname: "secret-host"})
	regReq.SetPendingConfirmation(&types.PendingRegistrationConfirmation{
		UserID: 1,
		CSRF:   "expected-csrf",
	})
	app.state.SetAuthCacheEntry(authID, regReq)

	missing := httptest.NewRecorder()
	provider.RegisterConfirmGetHandler(missing, newConfirmGetRequest(t, authID, ""))
	assert.Equal(t, http.StatusForbidden, missing.Code, "no cookie must not render the page")
	assert.NotContains(t, missing.Body.String(), "secret-host")

	wrong := httptest.NewRecorder()
	provider.RegisterConfirmGetHandler(wrong, newConfirmGetRequest(t, authID, "other-csrf"))
	assert.Equal(t, http.StatusForbidden, wrong.Code, "a foreign cookie must not render the page")
	assert.NotContains(t, wrong.Body.String(), "secret-host")
}

// TestRegisterConfirmGetHandlerSpentLink covers the common case: the user
// already confirmed and came back to the link. That is not a failure, so the
// page says so rather than showing the generic expired-session error.
func TestRegisterConfirmGetHandlerSpentLink(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)
	provider := &AuthProviderOIDC{h: app, serverURL: "http://localhost:8080"}

	rec := httptest.NewRecorder()
	provider.RegisterConfirmGetHandler(rec, newConfirmGetRequest(t, types.MustAuthID(), "any"))

	assert.Equal(t, http.StatusGone, rec.Code)
	assert.Contains(t, rec.Body.String(), "already been used")
}
