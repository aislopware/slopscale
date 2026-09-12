package mockoidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

const (
	scopeGroups = "groups"
	idBytes     = 16
)

var (
	errRequestNotFound  = errors.New("auth request not found")
	errCodeNotFound     = errors.New("authorization code not found")
	errTokenNotFound    = errors.New("token not found")
	errTokenExpired     = errors.New("token expired")
	errUserNotFound     = errors.New("user not found")
	errClientNotFound   = errors.New("client not found")
	errInvalidSecret    = errors.New("invalid client secret")
	errUnsupportedGrant = errors.New("grant not supported by the mock provider")
)

var (
	_ op.Storage                   = (*store)(nil)
	_ op.CanSetUserinfoFromRequest = (*store)(nil)
	_ op.HasRedirectGlobs          = (*client)(nil)
	_ op.AuthRequest               = (*authRequest)(nil)
	_ op.RefreshTokenRequest       = (*refreshRequest)(nil)
)

// store is the in-memory op.Storage behind [Server]: one client, a login
// queue, and the requests, codes and tokens of the current run.
type store struct {
	mu sync.Mutex

	client     *client
	accessTTL  time.Duration
	refreshTTL time.Duration

	queue         []User
	users         map[string]User
	authRequests  map[string]*authRequest
	codes         map[string]string
	tokens        map[string]*token
	refreshTokens map[string]*refreshRequest

	key *rsa.PrivateKey
}

type token struct {
	subject    string
	scopes     []string
	audience   []string
	expiration time.Time
}

func newStore(key *rsa.PrivateKey) *store {
	return &store{
		users:         make(map[string]User),
		authRequests:  make(map[string]*authRequest),
		codes:         make(map[string]string),
		tokens:        make(map[string]*token),
		refreshTokens: make(map[string]*refreshRequest),
		key:           key,
	}
}

// CreateAuthRequest signs the request in as the next queued user right
// away, so the login redirect the op issues lands on the callback that
// mints the code.
//
//nolint:ireturn // op.Storage returns these interfaces
func (s *store) CreateAuthRequest(_ context.Context, req *oidc.AuthRequest, _ string) (op.AuthRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomID()
	if err != nil {
		return nil, err
	}

	user := s.popUser()

	var challenge *oidc.CodeChallenge
	if req.CodeChallenge != "" {
		challenge = &oidc.CodeChallenge{Challenge: req.CodeChallenge, Method: req.CodeChallengeMethod}
	}

	request := &authRequest{
		id:            id,
		clientID:      req.ClientID,
		redirectURI:   req.RedirectURI,
		state:         req.State,
		nonce:         req.Nonce,
		scopes:        req.Scopes,
		responseType:  req.ResponseType,
		responseMode:  req.ResponseMode,
		codeChallenge: challenge,
		subject:       user.Subject,
		authTime:      time.Now(),
	}
	s.authRequests[id] = request

	return request, nil
}

//nolint:ireturn // op.Storage returns these interfaces
func (s *store) AuthRequestByID(_ context.Context, id string) (op.AuthRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, ok := s.authRequests[id]
	if !ok {
		return nil, errRequestNotFound
	}

	return request, nil
}

//nolint:ireturn // op.Storage returns these interfaces
func (s *store) AuthRequestByCode(_ context.Context, code string) (op.AuthRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, ok := s.codes[code]
	if !ok {
		return nil, errCodeNotFound
	}

	request, ok := s.authRequests[id]
	if !ok {
		return nil, errRequestNotFound
	}

	return request, nil
}

func (s *store) SaveAuthCode(_ context.Context, id, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.codes[code] = id

	return nil
}

func (s *store) DeleteAuthRequest(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.authRequests, id)
	maps.DeleteFunc(s.codes, func(_, requestID string) bool { return requestID == id })

	return nil
}

func (s *store) CreateAccessToken(_ context.Context, req op.TokenRequest) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, err := s.newToken(req)
	if err != nil {
		return "", time.Time{}, err
	}

	return tok, s.tokens[tok].expiration, nil
}

func (s *store) CreateAccessAndRefreshTokens(
	_ context.Context,
	req op.TokenRequest,
	currentRefreshToken string,
) (string, string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if currentRefreshToken != "" {
		if _, ok := s.refreshTokens[currentRefreshToken]; !ok {
			return "", "", time.Time{}, op.ErrInvalidRefreshToken
		}

		delete(s.refreshTokens, currentRefreshToken)
	}

	accessToken, err := s.newToken(req)
	if err != nil {
		return "", "", time.Time{}, err
	}

	refreshToken, err := randomID()
	if err != nil {
		return "", "", time.Time{}, err
	}

	authTime := time.Now()
	if withAuthTime, ok := req.(interface{ GetAuthTime() time.Time }); ok {
		authTime = withAuthTime.GetAuthTime()
	}

	s.refreshTokens[refreshToken] = &refreshRequest{
		subject:    req.GetSubject(),
		audience:   req.GetAudience(),
		scopes:     req.GetScopes(),
		clientID:   s.client.id,
		authTime:   authTime,
		expiration: time.Now().Add(s.refreshTTL),
	}

	return accessToken, refreshToken, s.tokens[accessToken].expiration, nil
}

//nolint:ireturn // op.Storage returns these interfaces
func (s *store) TokenRequestByRefreshToken(_ context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, ok := s.refreshTokens[refreshToken]
	if !ok || request.expiration.Before(time.Now()) {
		return nil, op.ErrInvalidRefreshToken
	}

	return request, nil
}

func (s *store) TerminateSession(_ context.Context, userID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	maps.DeleteFunc(s.tokens, func(_ string, tok *token) bool { return tok.subject == userID })
	maps.DeleteFunc(s.refreshTokens, func(_ string, req *refreshRequest) bool { return req.subject == userID })

	return nil
}

func (s *store) RevokeToken(_ context.Context, tokenOrTokenID, _, _ string) *oidc.Error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tokens, tokenOrTokenID)
	delete(s.refreshTokens, tokenOrTokenID)

	return nil
}

func (s *store) GetRefreshTokenInfo(_ context.Context, _, refreshToken string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	request, ok := s.refreshTokens[refreshToken]
	if !ok {
		return "", "", op.ErrInvalidRefreshToken
	}

	return request.subject, refreshToken, nil
}

//nolint:ireturn // op.Storage returns these interfaces
func (s *store) SigningKey(context.Context) (op.SigningKey, error) {
	return &signingKey{key: s.key}, nil
}

func (s *store) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}

func (s *store) KeySet(context.Context) ([]op.Key, error) {
	return []op.Key{&publicKey{key: &s.key.PublicKey}}, nil
}

//nolint:ireturn // op.Storage returns these interfaces
func (s *store) GetClientByClientID(_ context.Context, clientID string) (op.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client == nil || clientID != s.client.id {
		return nil, errClientNotFound
	}

	return s.client, nil
}

func (s *store) AuthorizeClientIDSecret(_ context.Context, clientID, clientSecret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client == nil || clientID != s.client.id {
		return errClientNotFound
	}

	if clientSecret != s.client.secret {
		return errInvalidSecret
	}

	return nil
}

// SetUserinfoFromScopes is the deprecated hook; SetUserinfoFromRequest
// carries the ID token claims.
func (s *store) SetUserinfoFromScopes(context.Context, *oidc.UserInfo, string, string, []string) error {
	return nil
}

func (s *store) SetUserinfoFromRequest(
	_ context.Context,
	info *oidc.UserInfo,
	req op.IDTokenRequest,
	scopes []string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.setUserinfo(info, req.GetSubject(), scopes)
}

func (s *store) SetUserinfoFromToken(_ context.Context, info *oidc.UserInfo, tokenID, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, ok := s.tokens[tokenID]
	if !ok {
		return errTokenNotFound
	}

	if tok.expiration.Before(time.Now()) {
		return errTokenExpired
	}

	return s.setUserinfo(info, tok.subject, tok.scopes)
}

func (s *store) SetIntrospectionFromToken(
	_ context.Context,
	introspection *oidc.IntrospectionResponse,
	tokenID, _, clientID string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tok, ok := s.tokens[tokenID]
	if !ok {
		return errTokenNotFound
	}

	if tok.expiration.Before(time.Now()) {
		return errTokenExpired
	}

	info := new(oidc.UserInfo)

	err := s.setUserinfo(info, tok.subject, tok.scopes)
	if err != nil {
		return err
	}

	introspection.SetUserInfo(info)
	introspection.Scope = tok.scopes
	introspection.ClientID = clientID
	introspection.Expiration = oidc.FromTime(tok.expiration)

	return nil
}

func (s *store) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return nil, nil //nolint:nilnil // no private claims: the op reads nil as "none"
}

func (s *store) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, errUnsupportedGrant
}

func (s *store) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, errUnsupportedGrant
}

func (s *store) Health(context.Context) error {
	return nil
}

func (s *store) configure(issuer, clientID, clientSecret string, accessTTL, refreshTTL time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.client = &client{issuer: issuer, id: clientID, secret: clientSecret, idTokenTTL: accessTTL}
	s.accessTTL = accessTTL
	s.refreshTTL = refreshTTL
}

func (s *store) queueUser(user User) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.queue = append(s.queue, user)
}

// popUser returns the next queued user, or the default user, and records
// it so later userinfo lookups by subject find its claims.
func (s *store) popUser() User {
	user := DefaultUser()
	if len(s.queue) > 0 {
		user, s.queue = s.queue[0], s.queue[1:]
	}

	s.users[user.Subject] = user

	return user
}

// newToken records an access token and returns its id; the caller holds
// the lock.
func (s *store) newToken(req op.TokenRequest) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}

	s.tokens[id] = &token{
		subject:    req.GetSubject(),
		scopes:     req.GetScopes(),
		audience:   req.GetAudience(),
		expiration: time.Now().Add(s.accessTTL),
	}

	return id, nil
}

// setUserinfo fills the claims the scopes cover; the caller holds the lock.
func (s *store) setUserinfo(info *oidc.UserInfo, subject string, scopes []string) error {
	user, ok := s.users[subject]
	if !ok {
		return errUserNotFound
	}

	for _, scope := range scopes {
		switch scope {
		case oidc.ScopeOpenID:
			info.Subject = user.Subject
		case oidc.ScopeEmail:
			info.Email = user.Email
			info.EmailVerified = oidc.Bool(user.EmailVerified)
		case oidc.ScopeProfile:
			info.PreferredUsername = user.PreferredUsername
		case oidc.ScopePhone:
			info.PhoneNumber = user.Phone
		case oidc.ScopeAddress:
			if user.Address != "" {
				info.Address = &oidc.UserInfoAddress{Formatted: user.Address}
			}
		case scopeGroups:
			info.AppendClaims(scopeGroups, slices.Clone(user.Groups))
		}
	}

	return nil
}

// client is the one relying party the provider knows. It is a confidential
// web client in dev mode that accepts any redirect_uri.
type client struct {
	issuer     string
	id         string
	secret     string
	idTokenTTL time.Duration
}

func (c *client) GetID() string                        { return c.id }
func (c *client) RedirectURIs() []string               { return nil }
func (c *client) RedirectURIGlobs() []string           { return []string{"http://**", "https://**"} }
func (c *client) PostLogoutRedirectURIs() []string     { return nil }
func (c *client) PostLogoutRedirectURIGlobs() []string { return []string{"http://**", "https://**"} }
func (c *client) ApplicationType() op.ApplicationType  { return op.ApplicationTypeWeb }
func (c *client) AuthMethod() oidc.AuthMethod          { return oidc.AuthMethodBasic }
func (c *client) ResponseTypes() []oidc.ResponseType {
	return []oidc.ResponseType{oidc.ResponseTypeCode}
}

func (c *client) AccessTokenType() op.AccessTokenType  { return op.AccessTokenTypeBearer }
func (c *client) IDTokenLifetime() time.Duration       { return c.idTokenTTL }
func (c *client) DevMode() bool                        { return true }
func (c *client) IsScopeAllowed(string) bool           { return true }
func (c *client) IDTokenUserinfoClaimsAssertion() bool { return true }
func (c *client) ClockSkew() time.Duration             { return 0 }

//nolint:revive,staticcheck // op.Client spells the method this way
func (c *client) RestrictAdditionalIdTokenScopes() scopeFilter     { return keepScopes }
func (c *client) RestrictAdditionalAccessTokenScopes() scopeFilter { return keepScopes }

func (c *client) GrantTypes() []oidc.GrantType {
	return []oidc.GrantType{oidc.GrantTypeCode, oidc.GrantTypeRefreshToken}
}

// LoginURL skips the login page: the request is already signed in, so the
// user agent goes straight to the callback that issues the code.
func (c *client) LoginURL(id string) string {
	return c.issuer + "/authorize/callback?id=" + id
}

type scopeFilter = func(scopes []string) []string

func keepScopes(scopes []string) []string { return scopes }

// authRequest is a signed-in authorization request.
type authRequest struct {
	id            string
	clientID      string
	redirectURI   string
	state         string
	nonce         string
	scopes        []string
	responseType  oidc.ResponseType
	responseMode  oidc.ResponseMode
	codeChallenge *oidc.CodeChallenge
	subject       string
	authTime      time.Time
}

func (r *authRequest) GetID() string                         { return r.id }
func (r *authRequest) GetACR() string                        { return "" }
func (r *authRequest) GetAMR() []string                      { return nil }
func (r *authRequest) GetAudience() []string                 { return []string{r.clientID} }
func (r *authRequest) GetAuthTime() time.Time                { return r.authTime }
func (r *authRequest) GetClientID() string                   { return r.clientID }
func (r *authRequest) GetCodeChallenge() *oidc.CodeChallenge { return r.codeChallenge }
func (r *authRequest) GetNonce() string                      { return r.nonce }
func (r *authRequest) GetRedirectURI() string                { return r.redirectURI }
func (r *authRequest) GetResponseType() oidc.ResponseType    { return r.responseType }
func (r *authRequest) GetResponseMode() oidc.ResponseMode    { return r.responseMode }
func (r *authRequest) GetScopes() []string                   { return r.scopes }
func (r *authRequest) GetState() string                      { return r.state }
func (r *authRequest) GetSubject() string                    { return r.subject }
func (r *authRequest) Done() bool                            { return true }

// refreshRequest is what a refresh token stands for.
type refreshRequest struct {
	subject    string
	audience   []string
	scopes     []string
	clientID   string
	authTime   time.Time
	expiration time.Time
}

func (r *refreshRequest) GetAMR() []string                 { return nil }
func (r *refreshRequest) GetAudience() []string            { return r.audience }
func (r *refreshRequest) GetAuthTime() time.Time           { return r.authTime }
func (r *refreshRequest) GetClientID() string              { return r.clientID }
func (r *refreshRequest) GetScopes() []string              { return r.scopes }
func (r *refreshRequest) GetSubject() string               { return r.subject }
func (r *refreshRequest) SetCurrentScopes(scopes []string) { r.scopes = scopes }

const keyID = "mock"

// signingKey is the RSA key that signs tokens.
type signingKey struct {
	key *rsa.PrivateKey
}

func (k *signingKey) ID() string                                  { return keyID }
func (k *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k *signingKey) Key() any                                    { return k.key }

// publicKey is the JWK the keys endpoint publishes.
type publicKey struct {
	key *rsa.PublicKey
}

func (k *publicKey) ID() string                         { return keyID }
func (k *publicKey) Algorithm() jose.SignatureAlgorithm { return jose.RS256 }
func (k *publicKey) Use() string                        { return "sig" }
func (k *publicKey) Key() any                           { return k.key }

func randomID() (string, error) {
	b := make([]byte, idBytes)

	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("generating id: %w", err)
	}

	return hex.EncodeToString(b), nil
}
