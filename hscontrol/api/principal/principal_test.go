package principal

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errUnknown = errors.New("unknown credential")

type fakeAuth struct {
	keys     map[string]*types.APIKey
	tokens   map[string]*types.OAuthAccessToken
	sessions map[string]*types.Session
	users    map[types.UserID]*types.User
}

func (f fakeAuth) AuthenticateSession(token string) (*types.Session, error) {
	if s, ok := f.sessions[token]; ok {
		return s, nil
	}

	return nil, errUnknown
}

func (f fakeAuth) AuthenticateAPIKey(key string) (*types.APIKey, error) {
	if k, ok := f.keys[key]; ok {
		return k, nil
	}

	return nil, errUnknown
}

func (f fakeAuth) AuthenticateAccessToken(token string) (*types.OAuthAccessToken, error) {
	if t, ok := f.tokens[token]; ok {
		return t, nil
	}

	return nil, errUnknown
}

func (f fakeAuth) GetUserByID(id types.UserID) (*types.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}

	return nil, errUnknown
}

func newFakeAuth() fakeAuth {
	return fakeAuth{
		keys: map[string]*types.APIKey{
			"legacy":  {ID: 1},
			"owner":   {ID: 2, UserID: new(uint(1))},
			"netadm":  {ID: 3, UserID: new(uint(2))},
			"member":  {ID: 4, UserID: new(uint(3))},
			"orphan":  {ID: 5, UserID: new(uint(99))},
			"auditor": {ID: 6, UserID: new(uint(4))},
			// Scoped keys: the owner's key keeps only DNS; the network
			// admin's key asked for users, which the role lacks, and
			// routes, which it holds; the legacy key gets exactly its
			// scopes.
			"owner-dns":     {ID: 7, UserID: new(uint(1)), Scopes: []string{"dns"}},
			"netadm-scoped": {ID: 8, UserID: new(uint(2)), Scopes: []string{"users", "devices:routes"}},
			"legacy-read":   {ID: 9, Scopes: []string{"all:read"}},
		},
		tokens: map[string]*types.OAuthAccessToken{
			types.AccessTokenPrefix + "routes": {Scopes: []string{"devices:routes"}, Tags: []string{"tag:web"}},
		},
		sessions: map[string]*types.Session{
			"owner-session":  {ID: 7, UserID: 1},
			"member-session": {ID: 8, UserID: 3},
			"orphan-session": {ID: 9, UserID: 99},
		},
		users: map[types.UserID]*types.User{
			1: {ID: 1, Role: types.RoleOwner},
			2: {ID: 2, Role: types.RoleNetworkAdmin},
			3: {ID: 3, Role: types.RoleMember},
			4: {ID: 4, Role: types.RoleAuditor},
		},
	}
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()

	auth := newFakeAuth()

	tests := []struct {
		token     string
		wantErr   bool
		wantKind  Kind
		wantUser  types.UserID
		bounded   bool
		allows    []scope.Scope
		denies    []scope.Scope
		wantTags  []string
		wantRole  types.Role
		wantOAuth bool
	}{
		{token: "", wantErr: true},
		{token: "nope", wantErr: true},
		{token: types.AccessTokenPrefix + "nope", wantErr: true},
		{
			token: "legacy", wantKind: APIKey,
			allows: []scope.Scope{scope.All, scope.Users, scope.PolicyFile},
		},
		{
			token: "owner", wantKind: APIKey, wantUser: 1, wantRole: types.RoleOwner,
			allows: []scope.Scope{scope.All, scope.Users},
		},
		{
			token: "netadm", wantKind: APIKey, wantUser: 2, wantRole: types.RoleNetworkAdmin, bounded: true,
			allows: []scope.Scope{scope.PolicyFile, scope.DevicesRoutes, scope.UsersRead},
			denies: []scope.Scope{scope.Users, scope.All, scope.DevicesCore},
		},
		{
			token: "member", wantKind: APIKey, wantUser: 3, wantRole: types.RoleMember, bounded: true,
			denies: []scope.Scope{scope.AllRead, scope.UsersRead},
		},
		{
			token: "orphan", wantKind: APIKey, wantUser: 99, bounded: true,
			denies: []scope.Scope{scope.AllRead, scope.UsersRead},
		},
		{
			token: "auditor", wantKind: APIKey, wantUser: 4, wantRole: types.RoleAuditor, bounded: true,
			allows: []scope.Scope{scope.AllRead, scope.UsersRead, scope.PolicyFileRead},
			denies: []scope.Scope{scope.Users, scope.PolicyFile},
		},
		{
			token: "owner-dns", wantKind: APIKey, wantUser: 1, wantRole: types.RoleOwner, bounded: true,
			allows: []scope.Scope{scope.DNS, scope.DNSRead},
			denies: []scope.Scope{scope.All, scope.Users, scope.PolicyFile},
		},
		{
			token: "netadm-scoped", wantKind: APIKey, wantUser: 2, wantRole: types.RoleNetworkAdmin, bounded: true,
			allows: []scope.Scope{scope.DevicesRoutes, scope.DevicesRoutesRead},
			denies: []scope.Scope{scope.Users, scope.UsersRead, scope.PolicyFile, scope.DNS},
		},
		{
			token: "legacy-read", wantKind: APIKey, bounded: true,
			allows: []scope.Scope{scope.AllRead, scope.UsersRead, scope.PolicyFileRead},
			denies: []scope.Scope{scope.All, scope.Users},
		},
		{
			token: types.AccessTokenPrefix + "routes", wantKind: AccessToken, bounded: true, wantOAuth: true,
			allows:   []scope.Scope{scope.DevicesRoutes, scope.DevicesRoutesRead},
			denies:   []scope.Scope{scope.DevicesCore, scope.UsersRead},
			wantTags: []string{"tag:web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			t.Parallel()

			p, err := Authenticate(auth, tt.token)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrUnauthenticated)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, p.Kind)
			assert.Equal(t, tt.wantUser, p.UserID)
			assert.Equal(t, tt.wantUser != 0, p.HasUser())
			assert.Equal(t, tt.bounded, p.Bounded)
			assert.Equal(t, tt.wantRole, p.Role)
			assert.Equal(t, tt.wantTags, p.Tags)
			assert.Equal(t, tt.wantOAuth, p.IsOAuth())

			for _, s := range tt.allows {
				assert.True(t, p.Allows(s), "should allow %s", s)
			}

			for _, s := range tt.denies {
				assert.False(t, p.Allows(s), "should deny %s", s)
			}
		})
	}
}

func TestNarrow(t *testing.T) {
	t.Parallel()

	wanted := []scope.Scope{scope.All, scope.PolicyFile, scope.UsersRead}

	assert.Equal(t, wanted, Local().Narrow(wanted), "an unbounded principal delegates anything")

	auth := newFakeAuth()
	p, err := Authenticate(auth, "netadm")
	require.NoError(t, err)
	assert.Equal(t, []scope.Scope{scope.PolicyFile, scope.UsersRead}, p.Narrow(wanted))
}

func TestToken(t *testing.T) {
	t.Parallel()

	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("key-abc:"))

	tests := []struct {
		header string
		want   string
		ok     bool
	}{
		{"Bearer abc", "abc", true},
		{"Bearer ", "", false},
		{basic, "key-abc", true},
		{"Basic !!!", "", false},
		{"Basic " + base64.StdEncoding.EncodeToString([]byte(":pw")), "", false},
		{"Digest abc", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		got, ok := Token(tt.header)
		assert.Equal(t, tt.ok, ok, tt.header)
		assert.Equal(t, tt.want, got, tt.header)
	}
}

func TestRequireScopeRoundTrip(t *testing.T) {
	t.Parallel()

	op := RequireScope(huma.Operation{Description: "Lists things."}, scope.UsersRead)

	got, ok := RequiredScope(&op)
	require.True(t, ok)
	assert.Equal(t, scope.UsersRead, got)
	assert.Equal(t, "users:read", op.Extensions["x-required-scope"])
	assert.Contains(t, op.Description, "Lists things.\n\nRequires the `users:read` scope")

	_, ok = RequiredScope(&huma.Operation{})
	assert.False(t, ok)
	_, ok = RequiredScope(nil)
	assert.False(t, ok)
}

// newTestAPI mounts three operations behind the middleware: a public one, a
// scoped one and one that reads the principal itself.
func newTestAPI(t *testing.T, auth Authenticator) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("t", "1"))
	api.UseMiddleware(Middleware(api, auth))

	security := []map[string][]string{{"bearer": {}}}

	type out struct {
		Body struct {
			Kind    Kind         `json:"kind"`
			User    types.UserID `json:"user"`
			Bounded bool         `json:"bounded"`
		}
	}

	echo := func(ctx context.Context, _ *struct{}) (*out, error) {
		p, ok := From(ctx)
		if !ok {
			return nil, huma.Error500InternalServerError("no principal")
		}

		o := &out{}
		o.Body.Kind = p.Kind
		o.Body.User = p.UserID
		o.Body.Bounded = p.Bounded

		return o, nil
	}

	huma.Register(api, huma.Operation{
		OperationID: "public", Method: http.MethodGet, Path: "/public",
	}, func(context.Context, *struct{}) (*struct{}, error) { return new(struct{}), nil })
	huma.Register(api, RequireScope(huma.Operation{
		OperationID: "users", Method: http.MethodGet, Path: "/users", Security: security,
	}, scope.Users), echo)
	huma.Register(api, huma.Operation{
		OperationID: "self", Method: http.MethodGet, Path: "/self", Security: security,
	}, echo)
	huma.Register(api, RequireScope(huma.Operation{
		OperationID: "write", Method: http.MethodPost, Path: "/users", Security: security,
	}, scope.Users), echo)

	return mux
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	auth := newFakeAuth()
	handler := newTestAPI(t, auth)
	trusted := WithLocalTrust(handler)

	tests := []struct {
		name   string
		path   string
		header string
		local  bool
		want   int
		body   string
	}{
		{name: "public needs nothing", path: "/public", want: http.StatusNoContent},
		{name: "missing credential", path: "/users", want: http.StatusUnauthorized},
		{name: "bad credential", path: "/users", header: "Bearer nope", want: http.StatusUnauthorized},
		{
			name:   "legacy key passes",
			path:   "/users",
			header: "Bearer legacy",
			want:   http.StatusOK,
			body:   `"bounded":false`,
		},
		{name: "owner key passes", path: "/users", header: "Bearer owner", want: http.StatusOK, body: `"user":1`},
		{name: "network admin lacks users", path: "/users", header: "Bearer netadm", want: http.StatusForbidden},
		{name: "member lacks users", path: "/users", header: "Bearer member", want: http.StatusForbidden},
		{
			name:   "member reaches self-checked op",
			path:   "/self",
			header: "Bearer member",
			want:   http.StatusOK,
			body:   `"bounded":true`,
		},
		{
			name: "token lacks users", path: "/users",
			header: "Bearer " + types.AccessTokenPrefix + "routes", want: http.StatusForbidden,
		},
		{name: "socket bypasses auth", path: "/users", local: true, want: http.StatusOK, body: `"kind":0`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			rec := httptest.NewRecorder()

			if tt.local {
				trusted.ServeHTTP(rec, req)
			} else {
				handler.ServeHTTP(rec, req)
			}

			assert.Equal(t, tt.want, rec.Code, rec.Body.String())

			if tt.body != "" {
				assert.Contains(t, rec.Body.String(), tt.body)
			}
		})
	}
}

func TestAuthenticateSession(t *testing.T) {
	t.Parallel()

	auth := newFakeAuth()

	p, err := AuthenticateSession(auth, "owner-session")
	require.NoError(t, err)
	assert.Equal(t, Session, p.Kind)
	assert.Equal(t, types.UserID(1), p.UserID)
	assert.Equal(t, types.RoleOwner, p.Role)
	assert.False(t, p.Bounded, "an owner's session is all-access")
	assert.Equal(t, uint64(7), p.SessionID)
	assert.Equal(t, "7", p.Credential)

	p, err = AuthenticateSession(auth, "member-session")
	require.NoError(t, err)
	assert.True(t, p.Bounded)
	assert.Empty(t, p.Scopes)

	p, err = AuthenticateSession(auth, "orphan-session")
	require.NoError(t, err)
	assert.True(t, p.Bounded, "a session whose user is gone grants nothing")
	assert.Empty(t, p.Scopes)

	_, err = AuthenticateSession(auth, "")
	require.ErrorIs(t, err, ErrUnauthenticated)

	_, err = AuthenticateSession(auth, "nope")
	require.ErrorIs(t, err, ErrUnauthenticated)
}

func TestSessionCookie(t *testing.T) {
	t.Parallel()

	token, ok := SessionCookie("other=1; " + types.SessionCookieName + "=abc; x=y")
	assert.True(t, ok)
	assert.Equal(t, "abc", token)

	_, ok = SessionCookie("other=1")
	assert.False(t, ok)

	_, ok = SessionCookie("")
	assert.False(t, ok)

	_, ok = SessionCookie(types.SessionCookieName + "=")
	assert.False(t, ok)
}

// TestMiddlewareCookie pins the same-site rule: a session cookie signs a
// request in only when the browser vouches that the page came from this
// server, and a bearer token always wins over a cookie.
func TestMiddlewareCookie(t *testing.T) {
	t.Parallel()

	handler := newTestAPI(t, newFakeAuth())
	cookie := types.SessionCookieName + "=owner-session"

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
		body    string
	}{
		{
			name: "same-origin read", method: http.MethodGet,
			headers: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "same-origin"},
			want:    http.StatusOK, body: `"kind":3`,
		},
		{
			name: "same-origin write", method: http.MethodPost,
			headers: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "same-origin"},
			want:    http.StatusOK,
		},
		{
			name: "navigation read", method: http.MethodGet,
			headers: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "none"},
			want:    http.StatusOK,
		},
		{
			name: "cross-site read refused", method: http.MethodGet,
			headers: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusForbidden,
		},
		{
			name: "cross-site write refused", method: http.MethodPost,
			headers: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "cross-site"},
			want:    http.StatusForbidden,
		},
		{
			name: "same-site subdomain refused", method: http.MethodPost,
			headers: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "same-site"},
			want:    http.StatusForbidden,
		},
		{
			name: "old browser with matching origin", method: http.MethodPost,
			headers: map[string]string{"Cookie": cookie, "Origin": "http://example.com"},
			want:    http.StatusOK,
		},
		{
			name: "old browser with foreign origin", method: http.MethodPost,
			headers: map[string]string{"Cookie": cookie, "Origin": "https://evil.example"},
			want:    http.StatusForbidden,
		},
		{
			name: "no browser headers read", method: http.MethodGet,
			headers: map[string]string{"Cookie": cookie},
			want:    http.StatusOK,
		},
		{
			name: "no browser headers write refused", method: http.MethodPost,
			headers: map[string]string{"Cookie": cookie},
			want:    http.StatusForbidden,
		},
		{
			name: "unknown cookie", method: http.MethodGet,
			headers: map[string]string{"Cookie": types.SessionCookieName + "=nope", "Sec-Fetch-Site": "same-origin"},
			want:    http.StatusUnauthorized,
		},
		{
			name:   "bearer wins over cookie",
			method: http.MethodGet,
			headers: map[string]string{
				"Cookie":         cookie,
				"Sec-Fetch-Site": "cross-site",
				"Authorization":  "Bearer legacy",
			},
			want: http.StatusOK,
			body: `"kind":1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "http://example.com/users", http.NoBody)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.want, rec.Code, rec.Body.String())

			if tt.body != "" {
				assert.Contains(t, rec.Body.String(), tt.body)
			}
		})
	}
}
