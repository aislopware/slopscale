package hscontrol

import (
	"bytes"
	"cmp"
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/db"
	hsstate "github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/templates"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	"tailscale.com/util/rands"
)

const (
	randomByteSize           = 16
	defaultOAuthOptionsCount = 3
	authCacheExpiration      = time.Minute * 15

	// authCacheMaxEntries bounds the OIDC state→[AuthInfo] cache to prevent
	// unauthenticated cache-fill DoS via repeated /register/{auth_id} or
	// /auth/{auth_id} GETs that mint OIDC state cookies.
	authCacheMaxEntries = 1024

	// cookieNamePrefixLen is the number of leading characters from a
	// state/nonce value that [getCookieName] splices into the cookie name.
	// State and nonce values that are shorter than this are rejected at
	// the callback boundary so [getCookieName] cannot panic on a slice
	// out-of-range.
	cookieNamePrefixLen = 6

	// registerConfirmFormBodyLimit caps the /register/confirm form body. The
	// confirmation form is a single CSRF token, so this is generous and
	// prevents an unauthenticated client from submitting an arbitrarily
	// large body to ParseForm.
	registerConfirmFormBodyLimit = 4 * 1024

	// registerConfirmCSRFTokenLength is the byte length of the CSRF token
	// generated for the registration confirmation interstitial.
	registerConfirmCSRFTokenLength = 32

	// oidcCookieValueLength is the byte length of the OIDC state/nonce
	// cookie values set by [setCSRFCookie].
	oidcCookieValueLength = 64
)

var errOIDCStateTooShort = errors.New("oidc state parameter is too short")

var (
	errEmptyOIDCCallbackParams = errors.New("empty OIDC callback params")
	errNoOIDCIDToken           = errors.New("extracting ID token")
	errNoOIDCRegistrationInfo  = errors.New("registration info not in cache")
	errOIDCAllowedDomains      = errors.New(
		"authenticated principal does not match any allowed domain",
	)
	errOIDCAllowedGroups  = errors.New("authenticated principal is not in any allowed group")
	errOIDCEmailAmbiguous = errors.New("several existing users carry the login's email")
	errOIDCAllowedUsers   = errors.New(
		"authenticated principal does not match any allowed user",
	)
	errOIDCUnverifiedEmail = errors.New("authenticated principal has an unverified email")
	errInvalidPKCEMethod   = errors.New("invalid pkce.method")
)

// AuthInfo contains both auth ID and verifier information for OIDC validation.
type AuthInfo struct {
	AuthID       types.AuthID
	Verifier     *string
	Registration bool

	// Console marks a sign-in to the admin console: no node is involved,
	// the callback opens a session and sends the browser to Redirect.
	Console  bool
	Redirect string

	// InviteToken is the token of the invite link the sign-in started
	// from. It is kept here rather than in a cookie so it survives the
	// round trip through the identity provider without leaving the
	// server.
	InviteToken string
}

type AuthProviderOIDC struct {
	h         *Slopscale
	serverURL string
	cfg       *types.OIDCConfig

	// authCache holds auth information between the auth and the callback
	// steps. It is a bounded [expirable.LRU] keyed by OIDC state, evicting oldest
	// entries to keep the cache footprint constant under attack.
	authCache *expirable.LRU[string, AuthInfo]

	oidcProvider *oidc.Provider
	oauth2Config *oauth2.Config
}

func NewAuthProviderOIDC(
	ctx context.Context,
	h *Slopscale,
	serverURL string,
	cfg *types.OIDCConfig,
) (*AuthProviderOIDC, error) {
	// Use the caller's context (bounded, see app.go) so a slow or unreachable
	// issuer fails discovery within the timeout instead of hanging startup.
	oidcProvider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		if mismatchErr, ok := errors.AsType[*oidc.IssuerMismatchError](err); ok {
			return nil, fmt.Errorf(
				"OIDC issuer mismatch: configured %q, discovered %q: %w",
				mismatchErr.Provided,
				mismatchErr.Discovered,
				err,
			)
		}

		return nil, fmt.Errorf("creating OIDC provider from issuer config: %w", err)
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     oidcProvider.Endpoint(),
		RedirectURL:  oidcCallbackURL(serverURL),
		Scopes:       cfg.Scope,
	}

	authCache := expirable.NewLRU[string, AuthInfo](
		authCacheMaxEntries,
		nil,
		authCacheExpiration,
	)

	return &AuthProviderOIDC{
		h:         h,
		serverURL: serverURL,
		cfg:       cfg,
		authCache: authCache,

		oidcProvider: oidcProvider,
		oauth2Config: oauth2Config,
	}, nil
}

func (a *AuthProviderOIDC) AuthURL(authID types.AuthID) string {
	return authPathURL(a.serverURL, "auth", authID)
}

func (a *AuthProviderOIDC) AuthHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	a.authHandler(writer, req, false)
}

func (a *AuthProviderOIDC) RegisterURL(authID types.AuthID) string {
	return authPathURL(a.serverURL, "register", authID)
}

// RegisterHandler registers the OIDC callback handler with the given router.
// It puts NodeKey in cache so the callback can retrieve it using the oidc state param.
// Listens in /register/:auth_id.
func (a *AuthProviderOIDC) RegisterHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	a.authHandler(writer, req, true)
}

// OIDCCallbackHandler handles the callback from the OIDC endpoint
// Retrieves the nkey from the state cache and adds the node to the users email user
// TODO: A confirmation page for new nodes should be added to avoid phishing vulnerabilities
// TODO: Add groups information from OIDC tokens into node HostInfo
// Listens in /oidc/callback.
func (a *AuthProviderOIDC) OIDCCallbackHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	code, state, err := extractCodeAndStateParamFromRequest(req)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	stateCookieName, err := a.validateStateCookie(req, state)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	oauth2Token, err := a.getOauth2Token(req.Context(), code, state)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	idToken, err := a.extractIDToken(req.Context(), oauth2Token)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	nonceCookieName, err := a.validateNonceCookie(req, idToken)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	// The state/nonce cookies have served their CSRF purpose; clear them so a
	// single-use pair does not linger in the browser until MaxAge.
	a.clearOIDCCallbackCookie(writer, stateCookieName)
	a.clearOIDCCallbackCookie(writer, nonceCookieName)

	nodeExpiry := a.determineNodeExpiry(idToken.Expiry)

	claims, err := a.resolveOIDCClaims(req.Context(), idToken, oauth2Token)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	// The user claims are now updated from the userinfo endpoint so we can verify the user
	// against allowed emails, email domains, and groups.
	err = doOIDCAuthorization(a.cfg, claims)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	// TODO(kradalby): Is this comment right?
	// If the node exists, then the node should be reauthenticated,
	// if the node does not exist, and the machine key exists, then
	// this is a new node that should be registered.
	//
	// The flow is picked up before the user is resolved, because an
	// invite the sign-in carries decides how the user is created.
	authInfo := a.getAuthInfoFromState(state)
	if authInfo == nil {
		log.Debug().Caller().Str("state", state).Msg("state not found in cache, login session may have expired")
		httpUserError(writer, NewHTTPError(http.StatusGone, "login session expired, try again", nil))

		return
	}

	invite, err := a.resolveInvite(authInfo, claims)
	if err != nil {
		renderInviteRefused(writer, err)

		return
	}

	user, err := a.userFromClaims(claims, invite)
	if err != nil {
		httpUserError(writer, NewHTTPError(
			http.StatusInternalServerError,
			"could not create or update user",
			err,
		))

		return
	}

	user, err = a.promoteConfiguredAdmin(user, claims)
	if err != nil {
		httpUserError(writer, NewHTTPError(http.StatusInternalServerError, "could not apply admin_users", err))

		return
	}

	err = a.syncConfiguredGroups(user, claims)
	if err != nil {
		httpUserError(writer, NewHTTPError(http.StatusInternalServerError, "could not sync groups", err))

		return
	}

	if authInfo.Console {
		a.handleConsoleCallback(writer, req, authInfo, user)

		return
	}

	// If this is a registration flow, send the browser to the confirmation
	// interstitial instead of finalising the registration immediately.
	// Without an explicit user click, a single GET to
	// /register/{auth_id} could silently complete a registration when
	// the IdP allows silent SSO.
	if authInfo.Registration {
		a.beginRegistrationConfirmation(writer, req, authInfo.AuthID, user, nodeExpiry)

		return
	}

	// If this is not a registration callback, then it is an SSH
	// check-mode auth callback. Confirm the OIDC identity is the owner
	// of the SSH source node before recording approval; without this
	// check any tailnet user could approve a check-mode prompt for any
	// other user's node, defeating the stolen-key protection that
	// check-mode is meant to provide.
	a.handleSSHCheckCallback(writer, authInfo, user)
}

func extractCodeAndStateParamFromRequest(
	req *http.Request,
) (string, string, error) {
	code := req.URL.Query().Get("code")
	state := req.URL.Query().Get("state")

	if code == "" || state == "" {
		return "", "", NewHTTPError(
			http.StatusBadRequest,
			"missing code or state parameter",
			errEmptyOIDCCallbackParams,
		)
	}

	// Reject states that are too short for [getCookieName] to splice
	// into a cookie name. Without this guard a request with
	// ?state=abc panics on the slice out-of-range and is recovered by
	// chi's [middleware.Recoverer], amplifying small-DoS log noise.
	if len(state) < cookieNamePrefixLen {
		return "", "", NewHTTPError(http.StatusBadRequest, "invalid state parameter", errOIDCStateTooShort)
	}

	return code, state, nil
}

// validateOIDCAllowedDomains checks that if AllowedDomains is provided,
// that the authenticated principal ends with @<alloweddomain>.
func validateOIDCAllowedDomains(
	allowedDomains []string,
	claims *types.OIDCClaims,
) error {
	if len(allowedDomains) > 0 {
		if _, domain, found := strings.CutLast(claims.Email, "@"); !found ||
			!slices.Contains(allowedDomains, domain) {
			return NewHTTPError(http.StatusUnauthorized, "unauthorised domain", errOIDCAllowedDomains)
		}
	}

	return nil
}

// validateOIDCAllowedGroups checks if AllowedGroups is provided,
// and that the user has one group in the list.
// claims.Groups can be populated by adding a client scope named
// 'groups' that contains group membership.
func validateOIDCAllowedGroups(
	allowedGroups []string,
	claims *types.OIDCClaims,
) error {
	for _, group := range allowedGroups {
		if slices.Contains(claims.Groups, group) {
			return nil
		}
	}

	return NewHTTPError(http.StatusUnauthorized, "unauthorised group", errOIDCAllowedGroups)
}

// validateOIDCAllowedUsers checks that if AllowedUsers is provided,
// that the authenticated principal is part of that list.
func validateOIDCAllowedUsers(
	allowedUsers []string,
	claims *types.OIDCClaims,
) error {
	if !slices.Contains(allowedUsers, claims.Email) {
		return NewHTTPError(http.StatusUnauthorized, "unauthorised user", errOIDCAllowedUsers)
	}

	return nil
}

// doOIDCAuthorization applies authorization tests to claims.
//
// The following tests are always applied:
//
// - [validateOIDCAllowedGroups]
//
// The following tests are applied if cfg.EmailVerifiedRequired=false
// or claims.email_verified=true:
//
// - [validateOIDCAllowedDomains]
// - [validateOIDCAllowedUsers]
//
// NOTE that, contrary to the function name, [validateOIDCAllowedUsers]
// only checks the email address -- not the username.
func doOIDCAuthorization(
	cfg *types.OIDCConfig,
	claims *types.OIDCClaims,
) error {
	if len(cfg.AllowedGroups) > 0 {
		err := validateOIDCAllowedGroups(cfg.AllowedGroups, claims)
		if err != nil {
			return err
		}
	}

	trustEmail := !cfg.EmailVerifiedRequired || bool(claims.EmailVerified)

	hasEmailTests := len(cfg.AllowedDomains) > 0 || len(cfg.AllowedUsers) > 0
	if !trustEmail && hasEmailTests {
		return NewHTTPError(http.StatusUnauthorized, "unverified email", errOIDCUnverifiedEmail)
	}

	if len(cfg.AllowedDomains) > 0 {
		err := validateOIDCAllowedDomains(cfg.AllowedDomains, claims)
		if err != nil {
			return err
		}
	}

	if len(cfg.AllowedUsers) > 0 {
		err := validateOIDCAllowedUsers(cfg.AllowedUsers, claims)
		if err != nil {
			return err
		}
	}

	return nil
}

// registerConfirmCSRFCookie is the cookie name used to bind the
// /register/confirm POST handler's CSRF token to the OIDC callback that
// rendered the interstitial. It includes a per-session prefix derived
// from the auth ID so cookies for unrelated registrations on the same
// browser do not collide.
const registerConfirmCSRFCookie = "slopscale_register_confirm"

// registrationLinkSpentMsg is logged when a user returns to a registration
// link whose session is gone, which is usually a reload or a back button
// after they already confirmed.
const registrationLinkSpentMsg = "registration link already used or expired"

// registrationLinkSpentUserMsg is what the browser shows for it: the generic
// 410 page reads as a failure, and this one almost never is.
const registrationLinkSpentUserMsg = "This link has already been used or has expired. " +
	"If your device is connected you are done; otherwise start the login again."

var errRegistrationLinkSpent = newHTTPUserError(
	http.StatusGone,
	registrationLinkSpentMsg,
	registrationLinkSpentUserMsg,
	nil,
)

// RegisterConfirmGetHandler renders the OIDC registration confirmation
// interstitial. It is reached via the redirect that
// [AuthProviderOIDC.beginRegistrationConfirmation] issues from the OIDC
// callback, and it is safe to reload: it only reads the pending confirmation
// captured on the cached [types.AuthRequest] and never touches the one-time
// code exchange.
//
// Listens in GET /register/confirm/:auth_id.
func (a *AuthProviderOIDC) RegisterConfirmGetHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	authID, err := authIDFromRequest(req)
	if err != nil {
		httpUserError(writer, err)

		return
	}

	authReq, ok := a.h.state.GetAuthCacheEntry(authID)
	if !ok {
		httpUserError(writer, errRegistrationLinkSpent)

		return
	}

	pending := authReq.PendingConfirmation()
	if pending == nil {
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "registration not OIDC-authorized", nil))

		return
	}

	// Only the browser that completed the OIDC flow holds this cookie, and
	// holding it is what authorises the confirm POST. Requiring it here too
	// keeps the device details, and the token that finalises the
	// registration, away from anyone who merely knows the auth ID — which
	// the node being registered does.
	cookie, err := req.Cookie(registerConfirmCSRFCookie)
	if err != nil {
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "missing csrf cookie", err))

		return
	}

	// Constant time: the token is a secret the request must prove it has.
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(pending.CSRF)) != 1 {
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "csrf token mismatch", nil))

		return
	}

	user, err := a.h.state.GetUserByID(types.UserID(pending.UserID))
	if err != nil {
		httpUserError(writer, fmt.Errorf("looking up user: %w", err))

		return
	}

	regData := authReq.RegistrationData()

	info := templates.RegisterConfirmInfo{
		FormAction:    a.registerConfirmURL(authID),
		CSRFTokenName: registerConfirmCSRFCookie,
		CSRFToken:     pending.CSRF,
		User:          user.Display(),
		Hostname:      regData.Hostname,
		MachineKey:    regData.MachineKey.ShortString(),
	}
	if regData.Hostinfo != nil {
		info.OS = regData.Hostinfo.OS
	}

	// The page carries the token that finalises the registration, so no
	// shared cache or history restore may serve it back.
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)

	_, err = writer.Write([]byte(templates.RegisterConfirm(info).Render()))
	if err != nil {
		util.LogErr(err, "Failed to write HTTP response")
	}
}

// RegisterConfirmHandler is the POST endpoint behind the OIDC
// registration confirmation interstitial. It validates the CSRF cookie
// against the form-submitted token, finalises the registration via
// [AuthProviderOIDC.handleRegistration], and renders the success page.
func (a *AuthProviderOIDC) RegisterConfirmHandler(
	writer http.ResponseWriter,
	req *http.Request,
) {
	if req.Method != http.MethodPost {
		httpUserError(writer, errMethodNotAllowed)

		return
	}

	authID, err := authIDFromRequest(req)
	if err != nil {
		httpUserError(writer, err)

		return
	}

	// Cap the form body. The confirmation form is a single CSRF token,
	// so 4 KiB is generous and prevents an unauthenticated client from
	// submitting an arbitrarily large body to ParseForm.
	req.Body = http.MaxBytesReader(writer, req.Body, registerConfirmFormBodyLimit)

	parseErr := req.ParseForm()
	if parseErr != nil {
		httpUserError(writer, NewHTTPError(http.StatusBadRequest, "invalid form", parseErr))

		return
	}

	formCSRF := req.PostFormValue(registerConfirmCSRFCookie)
	if formCSRF == "" {
		httpUserError(writer, NewHTTPError(http.StatusBadRequest, "missing csrf token", nil))

		return
	}

	cookie, err := req.Cookie(registerConfirmCSRFCookie)
	if err != nil {
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "missing csrf cookie", err))

		return
	}

	// Constant time: the token is a secret the request must prove it has.
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(formCSRF)) != 1 {
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "csrf token mismatch", nil))

		return
	}

	authReq, ok := a.h.state.GetAuthCacheEntry(authID)
	if !ok {
		httpUserError(writer, errRegistrationLinkSpent)

		return
	}

	pending := authReq.PendingConfirmation()
	if pending == nil {
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "registration not OIDC-authorized", nil))

		return
	}

	if subtle.ConstantTimeCompare([]byte(pending.CSRF), []byte(cookie.Value)) != 1 {
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "csrf token does not match cached registration", nil))

		return
	}

	user, err := a.h.state.GetUserByID(types.UserID(pending.UserID))
	if err != nil {
		httpUserError(writer, fmt.Errorf("looking up user: %w", err))

		return
	}

	newNode, err := a.handleRegistration(user, authID, pending.NodeExpiry)
	if err != nil {
		if errors.Is(err, db.ErrNodeNotFoundRegistrationCache) {
			httpUserError(writer, newHTTPUserError(
				http.StatusGone,
				registrationLinkSpentMsg,
				registrationLinkSpentUserMsg,
				err,
			))

			return
		}

		if errors.Is(err, hsstate.ErrUserNotApproved) {
			httpUserError(writer, NewHTTPError(
				http.StatusForbidden,
				"your account is waiting for an administrator's approval; try again once it has been approved",
				err,
			))

			return
		}

		httpUserError(writer, err)

		return
	}

	// Clear the CSRF cookie now that the registration is final.
	a.setRegisterConfirmCookie(writer, req, authID, "", -1)

	content := renderRegistrationSuccessTemplate(user, newNode)

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)

	// [renderRegistrationSuccessTemplate]'s output only embeds
	// HTML-escaped values from a server-side template, so the gosec
	// XSS warning is a false positive here.
	//nolint:gosec // G705: template output only embeds HTML-escaped values
	_, err = writer.Write(content.Bytes())
	if err != nil {
		util.LogErr(err, "Failed to write HTTP response")
	}
}

// registerConfirmURL is the browser-facing URL of the confirmation page. It
// is built from server_url, like [AuthProviderOIDC.RegisterURL] and the OIDC
// redirect URI, so a Slopscale that a reverse proxy serves under a path
// prefix hands the browser a URL that resolves.
func (a *AuthProviderOIDC) registerConfirmURL(authID types.AuthID) string {
	return authPathURL(a.serverURL, "register/confirm", authID)
}

// setRegisterConfirmCookie writes the per-session register-confirm CSRF
// cookie. Pass the CSRF token and authCacheExpiration seconds to set it;
// pass ("", -1) to clear it after the registration is finalised.
func (a *AuthProviderOIDC) setRegisterConfirmCookie(
	writer http.ResponseWriter,
	req *http.Request,
	authID types.AuthID,
	value string,
	maxAge int,
) {
	// Scope the cookie to the browser-facing path, which carries the
	// reverse proxy's prefix; the routed path does not.
	path := "/register/confirm/" + authID.String()

	u, err := url.Parse(a.registerConfirmURL(authID))
	if err == nil {
		path = u.Path
	}

	//nolint:gosec // G124: Secure from server_url scheme or req.TLS; HttpOnly + SameSite already set
	http.SetCookie(writer, &http.Cookie{
		Name:     registerConfirmCSRFCookie,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		Secure:   a.cookiesSecure() || req.TLS != nil,
		HttpOnly: true,
		// Lax, not Strict: the callback sets this cookie and immediately
		// redirects to the confirmation page. That hop ends a redirect
		// chain which began cross-site at the IdP, and Firefox evaluates
		// the whole chain, so a Strict cookie is withheld and the
		// confirmation page 403s. Lax still never rides a cross-site
		// POST, so the confirm submission stays protected.
		SameSite: http.SameSiteLaxMode,
	})
}

// cookiesSecure reports whether the OIDC cookies should carry the Secure flag.
// It keys off the configured server_url scheme, not req.TLS, so cookies stay
// Secure behind a TLS-terminating reverse proxy (where the proxy→Slopscale hop
// is plain HTTP and req.TLS is nil). Deriving it from config avoids trusting a
// spoofable X-Forwarded-Proto header.
func (a *AuthProviderOIDC) cookiesSecure() bool {
	return strings.HasPrefix(a.serverURL, "https://")
}

// oidcCallbackURL is the browser-facing URL the identity provider redirects
// back to. It is built from server_url so a Slopscale behind a reverse proxy
// that serves it under a path prefix hands the IdP a URL that resolves.
func oidcCallbackURL(serverURL string) string {
	return strings.TrimSuffix(serverURL, "/") + "/oidc/callback"
}

// oidcCallbackPath is the path the callback cookies are scoped to: the
// browser-facing path, which carries the reverse proxy's prefix, not the
// routed path.
func (a *AuthProviderOIDC) oidcCallbackPath() string {
	u, err := url.Parse(oidcCallbackURL(a.serverURL))
	if err != nil {
		return "/oidc/callback"
	}

	return u.Path
}

// authHandler takes an incoming request that needs to be authenticated and
// validates and prepares it for the OIDC flow.
func (a *AuthProviderOIDC) authHandler(
	writer http.ResponseWriter,
	req *http.Request,
	registration bool,
) {
	authID, err := authIDFromRequest(req)
	if err != nil {
		httpUserError(writer, err)
		return
	}

	a.startAuth(writer, req, AuthInfo{AuthID: authID, Registration: registration})
}

// startAuth sends the browser to the identity provider with info cached
// under a fresh state, so the callback can pick the flow up again.
func (a *AuthProviderOIDC) startAuth(writer http.ResponseWriter, req *http.Request, registrationInfo AuthInfo) {
	// Set the state and nonce cookies to protect against CSRF attacks
	state := a.setCSRFCookie(writer, req, "state")

	// Set the state and nonce cookies to protect against CSRF attacks
	nonce := a.setCSRFCookie(writer, req, "nonce")

	extras := make([]oauth2.AuthCodeOption, 0, len(a.cfg.ExtraParams)+defaultOAuthOptionsCount)
	// Add PKCE verification if enabled
	if a.cfg.PKCE.Enabled {
		verifier := oauth2.GenerateVerifier()
		registrationInfo.Verifier = &verifier

		extras = append(extras, oauth2.AccessTypeOffline)

		switch a.cfg.PKCE.Method {
		case types.PKCEMethodS256:
			extras = append(extras, oauth2.S256ChallengeOption(verifier))
		case types.PKCEMethodPlain:
			// oauth2 does not have a plain challenge option, so we add it manually
			extras = append(
				extras,
				oauth2.SetAuthURLParam("code_challenge_method", "plain"),
				oauth2.SetAuthURLParam("code_challenge", verifier),
			)
		default:
			// An unknown method must not silently emit no challenge: a
			// verifier was generated and is sent at token exchange, so a
			// missing challenge degrades to no-PKCE without anyone noticing.
			httpError(
				writer,
				NewHTTPError(
					http.StatusInternalServerError,
					"internal server error",
					fmt.Errorf("%w: %q", errInvalidPKCEMethod, a.cfg.PKCE.Method),
				),
			)

			return
		}
	}

	// Add any extra parameters from configuration
	for k, v := range a.cfg.ExtraParams {
		extras = append(extras, oauth2.SetAuthURLParam(k, v))
	}

	extras = append(extras, oidc.Nonce(nonce))

	// Cache the registration info
	a.authCache.Add(state, registrationInfo)

	authURL := a.oauth2Config.AuthCodeURL(state, extras...)
	log.Debug().Caller().Msgf("redirecting to %s for authentication", authURL)

	http.Redirect(writer, req, authURL, http.StatusFound)
}

func (a *AuthProviderOIDC) determineNodeExpiry(idTokenExpiration time.Time) *time.Time {
	if a.cfg.UseExpiryFromToken {
		return &idTokenExpiration
	}

	return nil
}

// getOauth2Token exchanges the code from the callback for an oauth2 token.
func (a *AuthProviderOIDC) getOauth2Token(
	ctx context.Context,
	code string,
	state string,
) (*oauth2.Token, error) {
	var exchangeOpts []oauth2.AuthCodeOption

	if a.cfg.PKCE.Enabled {
		regInfo, ok := a.authCache.Get(state)
		if !ok {
			return nil, NewHTTPError(http.StatusNotFound, "registration not found", errNoOIDCRegistrationInfo)
		}

		if regInfo.Verifier != nil {
			exchangeOpts = []oauth2.AuthCodeOption{oauth2.VerifierOption(*regInfo.Verifier)}
		}
	}

	oauth2Token, err := a.oauth2Config.Exchange(ctx, code, exchangeOpts...)
	if err != nil {
		return nil, NewHTTPError(http.StatusForbidden, "invalid code", fmt.Errorf("exchanging code for token: %w", err))
	}

	return oauth2Token, nil
}

// extractIDToken extracts the ID token from the oauth2 token.
func (a *AuthProviderOIDC) extractIDToken(
	ctx context.Context,
	oauth2Token *oauth2.Token,
) (*oidc.IDToken, error) {
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, NewHTTPError(http.StatusBadRequest, "no id_token", errNoOIDCIDToken)
	}

	verifier := a.oidcProvider.Verifier(&oidc.Config{ClientID: a.cfg.ClientID})

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, NewHTTPError(
			http.StatusForbidden,
			"failed to verify id_token",
			fmt.Errorf("verifying ID token: %w", err),
		)
	}

	return idToken, nil
}

// getAuthInfoFromState retrieves and consumes the auth info for a state. The
// entry is removed on read so a state is single-use: a replayed callback cannot
// resolve the same auth session twice, even within the cache TTL.
func (a *AuthProviderOIDC) getAuthInfoFromState(state string) *AuthInfo {
	authInfo, ok := a.authCache.Get(state)
	if !ok {
		return nil
	}

	a.authCache.Remove(state)

	return &authInfo
}

func (a *AuthProviderOIDC) createOrUpdateUserFromClaim(
	claims *types.OIDCClaims,
) (*types.User, change.Change, error) {
	var (
		user    *types.User
		err     error
		newUser bool
		c       change.Change
	)

	user, err = a.lookupUser(claims)
	if err != nil {
		return nil, change.Change{}, err
	}

	// if the user is still not found, create a new empty user.
	// TODO(kradalby): This context is not inherited from the request, which is probably not ideal.
	// However, we need a context to use the OIDC provider.
	if user == nil {
		newUser = true
		user = &types.User{}
	}

	user.FromClaim(claims, a.cfg.EmailVerifiedRequired)

	if newUser {
		user, c, err = a.h.state.CreateUserFromLogin(*user)
		if err != nil {
			return nil, change.Change{}, fmt.Errorf("creating user: %w", err)
		}
	} else {
		_, c, err = a.h.state.UpdateUser(types.UserID(user.ID), func(u *types.User) error {
			*u = *user
			return nil
		})
		if err != nil {
			return nil, change.Change{}, fmt.Errorf("updating user: %w", err)
		}
	}

	return user, c, nil
}

// lookupUser finds the user a login already belongs to: the one carrying
// the same iss/sub identifier, or the one the email matches when
// oidc.match_by_email is on. A nil user is a first login.
func (a *AuthProviderOIDC) lookupUser(claims *types.OIDCClaims) (*types.User, error) {
	user, err := a.h.state.GetUserByOIDCIdentifier(claims.Identifier())
	if err != nil && !errors.Is(err, db.ErrUserNotFound) {
		return nil, fmt.Errorf("looking up user: %w", err)
	}

	if user != nil {
		return user, nil
	}

	return a.matchUserByEmail(claims)
}

// matchUserByEmail finds the OIDC user a login with an unknown iss/sub
// identifier stands for when oidc.match_by_email is on: the one existing
// user that signed in through OIDC with the same email, which the login
// then takes over, identifier and all. The email must be verified unless
// verification is not required. Two candidates are refused rather than
// guessed at.
func (a *AuthProviderOIDC) matchUserByEmail(claims *types.OIDCClaims) (*types.User, error) {
	if !a.cfg.MatchByEmail || claims.Email == "" {
		return nil, nil //nolint:nilnil // no match is not an error
	}

	if !claims.EmailVerified && types.FlexibleBoolean(a.cfg.EmailVerifiedRequired) {
		return nil, nil //nolint:nilnil // no match is not an error
	}

	candidates, err := a.h.state.ListUsersWithFilter(&types.User{
		Email:    claims.Email,
		Provider: util.RegisterMethodOIDC,
	})
	if err != nil {
		return nil, fmt.Errorf("matching user by email: %w", err)
	}

	switch len(candidates) {
	case 0:
		return nil, nil //nolint:nilnil // no match is not an error
	case 1:
	default:
		return nil, NewHTTPError(http.StatusConflict, "several users share this email", errOIDCEmailAmbiguous)
	}

	matched := candidates[0]

	// A candidate this issuer already knows is not migrating anywhere: the
	// login is a second identity at the same provider, and letting it take
	// the account over turns "set a user's email" into "become that user".
	// Only an identifier left behind by another provider is taken over.
	if identifierFromIssuer(matched.ProviderIdentifier, claims.Iss) {
		log.Warn().
			Str("user", matched.Name).
			Str("identifier", claims.Identifier()).
			Msg("login matched a user by email but that user already belongs to this provider; not matched")

		return nil, nil //nolint:nilnil // no match is not an error
	}

	log.Info().
		Str("user", matched.Name).
		Str("previous", matched.ProviderIdentifier.String).
		Str("identifier", claims.Identifier()).
		Msg("login matched an existing user by email; the user follows the new identity provider")

	audit.Record(a.h.state, &types.AuditEvent{
		Action:     "user.provider.switch",
		TargetKind: "user",
		TargetID:   strconv.FormatUint(uint64(matched.ID), 10),
		TargetName: matched.Name,
		Detail: map[string]any{
			"previous":   matched.ProviderIdentifier.String,
			"identifier": claims.Identifier(),
			"source":     "oidc.match_by_email",
		},
	})

	return &matched, nil
}

// identifierFromIssuer reports whether a stored provider identifier was
// issued by iss. Identifiers read "<issuer>/<subject>" (see
// [types.OIDCClaims.Identifier]), so the issuer is a prefix of the stored
// value.
func identifierFromIssuer(identifier sql.NullString, iss string) bool {
	if !identifier.Valid || identifier.String == "" || iss == "" {
		return false
	}

	prefix := types.CleanIdentifier(strings.TrimSuffix(iss, "/"))
	if prefix == "" {
		return false
	}

	return strings.HasPrefix(identifier.String, prefix+"/")
}

// beginRegistrationConfirmation captures the resolved OIDC identity and node
// expiry into the cached [types.AuthRequest], sets the CSRF cookie, and
// redirects the browser to the confirmation page.
//
// The interstitial is served from its own URL rather than written inline
// here, because this request carries the single-use OAuth authorization code.
// A page rendered on this response leaves the browser parked on the
// code-bearing URL, and anything that reloads it — an extension calling
// window.location.reload(), the back button, pull-to-refresh, a prerender —
// re-enters the callback with a spent code and paints an error over the
// interstitial. Redirecting keeps the code exchange one-shot and makes the
// page the user waits on safe to reload.
func (a *AuthProviderOIDC) beginRegistrationConfirmation(
	writer http.ResponseWriter,
	req *http.Request,
	authID types.AuthID,
	user *types.User,
	nodeExpiry *time.Time,
) {
	authReq, ok := a.h.state.GetAuthCacheEntry(authID)
	if !ok {
		log.Debug().
			Caller().
			Str("auth_id", authID.String()).
			Msg("registration session expired before authorization completed")
		httpUserError(writer, NewHTTPError(http.StatusGone, "login session expired, try again", nil))

		return
	}

	if !authReq.IsRegistration() {
		log.Warn().Caller().
			Str("auth_id", authID.String()).
			Msg("OIDC callback hit registration path with auth request that is not a node registration")
		httpUserError(writer, NewHTTPError(http.StatusBadRequest, "auth session is not for node registration", nil))

		return
	}

	csrf := rands.HexString(registerConfirmCSRFTokenLength)

	authReq.SetPendingConfirmation(&types.PendingRegistrationConfirmation{
		UserID:     user.ID,
		NodeExpiry: nodeExpiry,
		CSRF:       csrf,
	})

	a.setRegisterConfirmCookie(writer, req, authID, csrf, int(authCacheExpiration.Seconds()))

	// 303 See Other so the browser issues a fresh GET for the confirmation
	// page and leaves the code-bearing URL behind as a transient hop rather
	// than a history entry it can return to.
	http.Redirect(writer, req, a.registerConfirmURL(authID), http.StatusSeeOther)
}

func (a *AuthProviderOIDC) handleRegistration(
	user *types.User,
	registrationID types.AuthID,
	expiry *time.Time,
) (bool, error) {
	// Decide "registered" versus "reauthenticated" before the registration
	// runs: it always produces a non-empty change, so the change cannot
	// tell the two apart afterwards.
	newNode := true

	if entry, ok := a.h.state.GetAuthCacheEntry(registrationID); ok {
		all := a.h.state.GetNodesByMachineKeyAllUsers(entry.RegistrationData().MachineKey)
		_, sameUser := all[types.UserID(user.ID)]
		tagged, hasTagged := all[0]
		convertingTagged := hasTagged && tagged.IsTagged()
		newNode = !sameUser && !convertingTagged
	}

	node, nodeChange, err := a.h.state.HandleNodeFromAuthPath(
		registrationID,
		types.UserID(user.ID),
		expiry,
		util.RegisterMethodOIDC,
	)
	if err != nil {
		return false, fmt.Errorf("registering node: %w", err)
	}

	// This is a bit of a back and forth, but we have a bit of a chicken and egg
	// dependency here.
	// Because the way the policy manager works, we need to have the node
	// in the database, then add it to the policy manager and then we can
	// approve the route. This means we get this dance where the node is
	// first added to the database, then we add it to the policy manager via
	// SaveNode (which automatically updates the policy manager) and then we can auto approve the routes.
	// As that only approves the struct object, we need to save it again and
	// ensure we send an update.
	// This works, but might be another good candidate for doing some sort of
	// eventbus.
	routesChange, err := a.h.state.AutoApproveRoutes(node)
	if err != nil {
		return false, fmt.Errorf("auto approving routes: %w", err)
	}

	// Send both changes. Empty changes are ignored by Change().
	a.h.Change(nodeChange, routesChange)

	return newNode, nil
}

// validateStateCookie checks the OIDC callback's state parameter against the
// state cookie set by [AuthProviderOIDC.authHandler], and returns the
// cookie's name so the caller can clear it. This guards against CSRF: an
// attacker completing their own OIDC flow and redirecting the victim's
// browser to the callback URL cannot reproduce the victim's state cookie.
func (a *AuthProviderOIDC) validateStateCookie(req *http.Request, state string) (string, error) {
	stateCookieName := getCookieName("state", state)

	cookieState, err := req.Cookie(stateCookieName)
	if err != nil {
		return "", NewHTTPError(http.StatusBadRequest, "state not found", err)
	}

	if state != cookieState.Value {
		return "", NewHTTPError(http.StatusForbidden, "state did not match", nil)
	}

	return stateCookieName, nil
}

// validateNonceCookie checks the ID token's nonce against the nonce cookie
// set by [AuthProviderOIDC.authHandler], and returns the cookie's name so the
// caller can clear it. This guards against ID token replay across sessions.
func (a *AuthProviderOIDC) validateNonceCookie(req *http.Request, idToken *oidc.IDToken) (string, error) {
	if idToken.Nonce == "" {
		return "", NewHTTPError(http.StatusBadRequest, "nonce not found in IDToken", nil)
	}

	nonceCookieName := getCookieName("nonce", idToken.Nonce)

	nonce, err := req.Cookie(nonceCookieName)
	if err != nil {
		return "", NewHTTPError(http.StatusBadRequest, "nonce not found", err)
	}

	if idToken.Nonce != nonce.Value {
		return "", NewHTTPError(http.StatusForbidden, "nonce did not match", nil)
	}

	return nonceCookieName, nil
}

// resolveOIDCClaims decodes the ID token's claims and, best-effort, folds in
// richer claims (groups, in particular) fetched from the userinfo endpoint.
// A userinfo fetch failure is logged and otherwise ignored: the ID token
// claims remain usable on their own.
func (a *AuthProviderOIDC) resolveOIDCClaims(
	ctx context.Context,
	idToken *oidc.IDToken,
	oauth2Token *oauth2.Token,
) (*types.OIDCClaims, error) {
	var claims types.OIDCClaims

	err := idToken.Claims(&claims)
	if err != nil {
		return nil, fmt.Errorf("decoding ID token claims: %w", err)
	}

	// Fetch user information (email, groups, name, etc) from the userinfo endpoint
	// https://openid.net/specs/openid-connect-core-1_0.html#UserInfo
	userinfo, err := a.oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(oauth2Token))
	if err != nil {
		util.LogErr(err, "could not get userinfo; only using claims from id token")
	}

	// The [oidc.UserInfo] type only decodes some fields (Subject, Profile, Email, EmailVerified).
	// We are interested in other fields too (e.g. groups are required for allowedGroups) so we
	// decode into our own [types.OIDCUserInfo] type using the underlying claims struct.
	var userinfo2 types.OIDCUserInfo
	if userinfo != nil && userinfo.Claims(&userinfo2) == nil && userinfo2.Sub == claims.Sub {
		// Update the user with the userinfo claims (with id token claims as fallback).
		// TODO(kradalby): there might be more interesting fields here that we have not found yet.
		claims.Email = cmp.Or(userinfo2.Email, claims.Email)
		claims.EmailVerified = cmp.Or(userinfo2.EmailVerified, claims.EmailVerified)
		claims.Username = cmp.Or(userinfo2.PreferredUsername, claims.Username)
		claims.Name = cmp.Or(userinfo2.Name, claims.Name)

		claims.ProfilePictureURL = cmp.Or(userinfo2.Picture, claims.ProfilePictureURL)
		if userinfo2.Groups != nil {
			claims.Groups = userinfo2.Groups
		}
	}

	return &claims, nil
}

// handleSSHCheckCallback finishes an SSH check-mode OIDC callback (a
// non-registration [AuthInfo]). It verifies the authenticated OIDC identity
// owns the SSH source node before recording the check-mode approval;
// otherwise any tailnet user could approve a check-mode prompt for any other
// user's node, defeating the stolen-key protection that check-mode provides.
func (a *AuthProviderOIDC) handleSSHCheckCallback(
	writer http.ResponseWriter,
	authInfo *AuthInfo,
	user *types.User,
) {
	authReq, ok := a.h.state.GetAuthCacheEntry(authInfo.AuthID)
	if !ok {
		log.Debug().
			Caller().
			Str("auth_id", authInfo.AuthID.String()).
			Msg("auth session expired before authorization completed")
		httpUserError(writer, NewHTTPError(http.StatusGone, "login session expired, try again", nil))

		return
	}

	if !authReq.IsSSHCheck() {
		log.Warn().Caller().
			Str("auth_id", authInfo.AuthID.String()).
			Msg("OIDC callback hit non-registration path with auth request that is not an SSH check binding")
		httpUserError(writer, NewHTTPError(http.StatusBadRequest, "auth session is not for SSH check", nil))

		return
	}

	binding := authReq.SSHCheckBinding()

	srcNode, ok := a.h.state.GetNodeByID(binding.SrcNodeID)
	if !ok {
		log.Warn().Caller().
			Str("auth_id", authInfo.AuthID.String()).
			Uint64("src_node_id", binding.SrcNodeID.Uint64()).
			Msg("SSH check src node no longer exists")
		httpUserError(writer, NewHTTPError(http.StatusGone, "src node no longer exists", nil))

		return
	}

	// Strict identity binding: only the user that owns the src node
	// may approve an SSH check for that node. Tagged source nodes are
	// rejected because they have no user owner to compare against.
	if srcNode.IsTagged() || !srcNode.UserID().Valid() {
		log.Warn().Caller().
			Str("auth_id", authInfo.AuthID.String()).
			Uint64("src_node_id", binding.SrcNodeID.Uint64()).
			Bool("src_is_tagged", srcNode.IsTagged()).
			Str("oidc_user", user.Username()).
			Msg("SSH check rejected: src node has no user owner")
		httpUserError(writer, NewHTTPError(http.StatusForbidden, "src node has no user owner", nil))

		return
	}

	if srcNode.UserID().Get() != user.ID {
		log.Warn().Caller().
			Str("auth_id", authInfo.AuthID.String()).
			Uint64("src_node_id", binding.SrcNodeID.Uint64()).
			Uint("src_owner_id", srcNode.UserID().Get()).
			Uint("oidc_user_id", user.ID).
			Str("oidc_user", user.Username()).
			Msg("SSH check rejected: OIDC user is not the owner of src node")
		httpUserError(
			writer,
			NewHTTPError(http.StatusForbidden, "OIDC user is not the owner of the SSH source node", nil),
		)

		return
	}

	// Identity verified — record the verdict for the waiting follow-up.
	authReq.FinishAuth(types.AuthVerdict{})

	content := renderAuthSuccessTemplate(user)

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)

	_, err := writer.Write(content.Bytes())
	if err != nil {
		util.LogErr(err, "Failed to write HTTP response")
	}
}

func renderRegistrationSuccessTemplate(
	user *types.User,
	newNode bool,
) *bytes.Buffer {
	result := templates.AuthSuccessResult{
		Title:   "Slopscale - Node Reauthenticated",
		Heading: "Node reauthenticated",
		Verb:    "Reauthenticated",
		User:    user.Display(),
		Message: "You can now close this window.",
	}
	if newNode {
		result.Title = "Slopscale - Node Registered"
		result.Heading = "Node registered"
		result.Verb = "Registered"
	}

	return bytes.NewBufferString(templates.AuthSuccess(result).Render())
}

func renderAuthSuccessTemplate(
	user *types.User,
) *bytes.Buffer {
	result := templates.AuthSuccessResult{
		Title:   "Slopscale - SSH Session Authorized",
		Heading: "SSH session authorized",
		Verb:    "Authorized",
		User:    user.Display(),
		Message: "You may return to your terminal.",
	}

	return bytes.NewBufferString(templates.AuthSuccess(result).Render())
}

// getCookieName generates a unique cookie name based on a cookie value. It
// uses at most [cookieNamePrefixLen] bytes of value, and fewer if value is
// shorter, so a short value (e.g. a malformed nonce from a misbehaving IdP)
// yields a non-matching name rather than panicking with slice-out-of-range.
func getCookieName(baseName, value string) string {
	n := min(len(value), cookieNamePrefixLen)

	return fmt.Sprintf("%s_%s", baseName, value[:n])
}

// clearOIDCCallbackCookie expires a /oidc/callback cookie by name. Matching the
// path the cookie was set with is required for the browser to drop it.
func (a *AuthProviderOIDC) clearOIDCCallbackCookie(w http.ResponseWriter, name string) {
	//nolint:gosec // G124: a deletion cookie (empty value, MaxAge<0); security attributes are moot
	http.SetCookie(w, &http.Cookie{
		Name:   name,
		Path:   a.oidcCallbackPath(),
		MaxAge: -1,
	})
}

func (a *AuthProviderOIDC) setCSRFCookie(w http.ResponseWriter, r *http.Request, name string) string {
	val := rands.HexString(oidcCookieValueLength)

	//nolint:gosec // G124: Secure from server_url scheme or req.TLS; HttpOnly + SameSite set below
	c := &http.Cookie{
		Path:     a.oidcCallbackPath(),
		Name:     getCookieName(name, val),
		Value:    val,
		MaxAge:   int(time.Hour.Seconds()),
		Secure:   a.cookiesSecure() || r.TLS != nil,
		HttpOnly: true,
		// Lax, not Strict: the OIDC callback is a cross-site top-level GET
		// redirect from the IdP that must still carry this cookie. Strict
		// would drop it and break login. Setting it explicitly also stops
		// pre-Lax-default browsers from sending it on other cross-site
		// requests.
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, c)

	return val
}
