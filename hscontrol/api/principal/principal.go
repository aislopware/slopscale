// Package principal identifies who is calling the admin API and what they
// may do. Both API versions authenticate through it, so a credential means
// the same thing everywhere: a locally trusted transport (the unix socket)
// may do anything; an API key without a user is the historical all-access
// admin key; an API key owned by a user is bounded by the user's role, and
// by its own scopes when it was minted with some; an OAuth access token is
// bounded by its scopes; a console session cookie is bounded by the
// signed-in user's role.
package principal

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

// Kind is how a principal authenticated.
type Kind uint8

const (
	// LocalTrust is the unix socket or an in-process test: no credential.
	LocalTrust Kind = iota
	// APIKey is an admin API key, with or without an owning user.
	APIKey
	// AccessToken is an OAuth access token minted by an OAuth client.
	AccessToken
	// Session is a browser signed in to the admin console through the
	// identity provider; the credential is a cookie.
	Session
)

var (
	// ErrUnauthenticated is returned when no credential, or an invalid one, is
	// presented.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrCrossSite is returned when a cookie-authenticated request comes
	// from another site: the browser sent the cookie, the page did not
	// come from this server.
	ErrCrossSite = errors.New("cross-site request refused")
)

// Principal is the authenticated caller of one request.
type Principal struct {
	Kind Kind

	// UserID is the user the credential belongs to; zero when it belongs to
	// nobody (a legacy admin key, a token, the socket).
	UserID types.UserID
	// Role is the user's role at authentication time; empty without a user.
	Role types.Role

	// Bounded reports whether Scopes limits the principal. An all-access
	// principal (socket, legacy key, owner or admin key) is unbounded.
	Bounded bool
	Scopes  []scope.Scope
	// Scoped reports whether the credential carries its own scope list (an
	// OAuth token, an API key minted with scopes) rather than standing for
	// a user's whole role. A credential it mints without naming scopes
	// inherits that list, so omitting scopes never widens.
	Scoped bool
	// Tags an OAuth token may assign; nil for every other kind.
	Tags []string

	// Credential names the credential for the audit log without exposing
	// it: an API key's prefix, an OAuth token's client, a session's ID.
	Credential string
	// SessionID is the console session behind a Session principal, so
	// signing out can end exactly that session.
	SessionID uint64
}

// Authenticator resolves credentials. *state.State satisfies it.
type Authenticator interface {
	AuthenticateAPIKey(key string) (*types.APIKey, error)
	AuthenticateAccessToken(token string) (*types.OAuthAccessToken, error)
	AuthenticateSession(token string) (*types.Session, error)
	GetUserByID(id types.UserID) (*types.User, error)
}

// Local is the principal of a locally trusted request.
func Local() Principal {
	return Principal{Kind: LocalTrust}
}

// HasUser reports whether the credential belongs to a user.
func (p Principal) HasUser() bool {
	return p.UserID != 0
}

// IsOAuth reports whether the caller holds an OAuth access token, whose tag
// grant additionally bounds the tags it may hand out.
func (p Principal) IsOAuth() bool {
	return p.Kind == AccessToken
}

// Allows reports whether the principal may perform an operation requiring s.
func (p Principal) Allows(s scope.Scope) bool {
	if !p.Bounded {
		return true
	}

	return scope.Grants(p.Scopes, s)
}

// Narrow returns the subset of wanted the principal may delegate, so a
// credential minted by this one never carries more authority than it has.
func (p Principal) Narrow(wanted []scope.Scope) []scope.Scope {
	if !p.Bounded {
		return wanted
	}

	return scope.Narrow(p.Scopes, wanted)
}

// Authenticate resolves a raw credential. Access tokens and API keys are
// told apart by prefix so a scoped token can never pass as an all-access
// key. A key owned by a user is bounded by [scope.ForRole] of the user's
// current role; a key whose user is gone grants nothing.
func Authenticate(auth Authenticator, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}

	if strings.HasPrefix(token, types.AccessTokenPrefix) {
		at, err := auth.AuthenticateAccessToken(token)
		if err != nil {
			return Principal{}, ErrUnauthenticated
		}

		return Principal{
			Kind:       AccessToken,
			Bounded:    true,
			Scoped:     true,
			Scopes:     scope.Parse(at.Scopes),
			Tags:       at.Tags,
			Credential: at.ClientID,
		}, nil
	}

	key, err := auth.AuthenticateAPIKey(token)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}

	p := Principal{Kind: APIKey, Credential: key.Prefix}
	if key.UserID != nil {
		p.UserID = types.UserID(*key.UserID)
		p.Bounded = true
		p = applyRole(auth, p)
	}

	return applyKeyScopes(p, key), nil
}

// applyKeyScopes narrows p to the scopes minted on the key, within what
// the owner's role grants: a key without scopes keeps the role, a key
// without an owner gets exactly its scopes.
func applyKeyScopes(p Principal, key *types.APIKey) Principal {
	if len(key.Scopes) == 0 {
		return p
	}

	wanted := scope.Parse(key.Scopes)
	if p.Bounded {
		wanted = scope.Narrow(p.Scopes, wanted)
	}

	p.Bounded = true
	p.Scoped = true
	p.Scopes = wanted

	return p
}

// AuthenticateSession resolves a console session cookie. The session is
// bounded by its user's current role, read on every request, so a role
// change or a deleted user takes effect without signing the browser out
// by hand.
func AuthenticateSession(auth Authenticator, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}

	session, err := auth.AuthenticateSession(token)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}

	p := Principal{
		Kind:       Session,
		UserID:     session.UserID,
		Bounded:    true,
		Credential: strconv.FormatUint(session.ID, 10),
		SessionID:  session.ID,
	}

	return applyRole(auth, p), nil
}

// applyRole bounds p by its user's current role; a missing user (deleted
// after the credential was issued) leaves p bounded with no scopes.
func applyRole(auth Authenticator, p Principal) Principal {
	role, ok := userRole(auth, p.UserID)
	if !ok {
		return p
	}

	p.Role = role
	p.Bounded = !role.IsAdmin()
	p.Scopes = scope.ForRole(role)

	return p
}

// userRole looks up a key owner's current role. A missing user (deleted
// after the key was minted) reports false; such a key grants nothing.
func userRole(auth Authenticator, id types.UserID) (types.Role, bool) {
	user, err := auth.GetUserByID(id)
	if err != nil {
		return "", false
	}

	if user.Role == "" {
		return types.RoleMember, true
	}

	return user.Role, true
}

// Token extracts the credential from an Authorization header: Bearer, or
// HTTP Basic with the credential as the username and an empty password,
// which is what the Tailscale SDK sends.
func Token(header string) (string, bool) {
	if token, ok := strings.CutPrefix(header, "Bearer "); ok {
		return token, token != ""
	}

	if encoded, ok := strings.CutPrefix(header, "Basic "); ok {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", false
		}

		username, _, _ := strings.Cut(string(raw), ":")

		return username, username != ""
	}

	return "", false
}

type contextKey struct{}

// With attaches p to ctx.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// From returns the request's principal, if the middleware attached one.
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)

	return p, ok
}

// WithLocalTrust marks every request through next as locally trusted, so the
// middleware skips authentication. The unix socket uses this; access to the
// socket is the trust boundary.
func WithLocalTrust(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req.WithContext(With(req.Context(), Local())))
	})
}

// scopeMetaKey keys the per-operation required scope in huma.Operation.Metadata.
const scopeMetaKey = "headscale.scope"

// RequireScope records op's required scope, both in its Metadata (where the
// middleware reads it back) and in the generated OpenAPI document: an
// x-required-scope extension for machine consumers and a Description line
// so the rendered docs state what each operation needs.
func RequireScope(op huma.Operation, s scope.Scope) huma.Operation {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}

	op.Metadata[scopeMetaKey] = s

	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}

	op.Extensions["x-required-scope"] = string(s)

	note := "Requires the `" + string(s) + "` scope (granted by an OAuth token's scopes " +
		"or the API key owner's role; a legacy API key without a user is all-access)."
	if op.Description == "" {
		op.Description = note
	} else {
		op.Description += "\n\n" + note
	}

	return op
}

// RequiredScope returns the scope an operation declared via RequireScope.
func RequiredScope(op *huma.Operation) (scope.Scope, bool) {
	if op == nil || op.Metadata == nil {
		return "", false
	}

	s, ok := op.Metadata[scopeMetaKey].(scope.Scope)

	return s, ok
}

// SessionCookie extracts the console session token from a Cookie header.
func SessionCookie(header string) (string, bool) {
	if header == "" {
		return "", false
	}

	cookies, err := http.ParseCookie(header)
	if err != nil {
		return "", false
	}

	for _, c := range cookies {
		if c.Name == types.SessionCookieName && c.Value != "" {
			return c.Value, true
		}
	}

	return "", false
}

// Middleware authenticates every operation that declares security and
// enforces the scope it declared. Operations without a declared scope (the
// v2 keys handlers, which pick the scope from the request body) receive the
// principal and finish the check themselves.
//
// A bearer token wins over a cookie. A cookie is honoured only for a
// same-site request: the browser attaches it to any request aimed at this
// server, so without the check a page on another site could act as the
// signed-in operator.
func Middleware(api huma.API, auth Authenticator) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if p, ok := From(ctx.Context()); ok && p.Kind == LocalTrust {
			next(ctx)

			return
		}

		if len(ctx.Operation().Security) == 0 {
			next(ctx)

			return
		}

		p, err := authenticateRequest(ctx, auth)
		if errors.Is(err, ErrCrossSite) {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, err.Error())

			return
		}

		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")

			return
		}

		if want, ok := RequiredScope(ctx.Operation()); ok && !p.Allows(want) {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden,
				"credential is missing the required scope "+string(want))

			return
		}

		next(huma.WithValue(ctx, contextKey{}, p))
	}
}

// authenticateRequest resolves the request's credential: the Authorization
// header when present, else the session cookie under the same-site rule.
func authenticateRequest(ctx huma.Context, auth Authenticator) (Principal, error) {
	if token, ok := Token(ctx.Header("Authorization")); ok {
		return Authenticate(auth, token)
	}

	token, ok := SessionCookie(ctx.Header("Cookie"))
	if !ok {
		return Principal{}, ErrUnauthenticated
	}

	if !sameSite(ctx) {
		return Principal{}, ErrCrossSite
	}

	return AuthenticateSession(auth, token)
}

// sameSite reports whether a cookie-bearing request was made by a page this
// server served. Browsers state it in Sec-Fetch-Site; an older browser
// that omits the header is judged by Origin, which every cross-origin
// request and every non-GET request carries. A request with neither is a
// plain navigation or a non-browser client, which is fine for a read and
// refused for a write.
func sameSite(ctx huma.Context) bool {
	switch ctx.Header("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "same-site", "cross-site":
		return false
	}

	origin := ctx.Header("Origin")
	if origin == "" {
		return isSafeMethod(ctx.Method())
	}

	return strings.EqualFold(strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://"), ctx.Host())
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
