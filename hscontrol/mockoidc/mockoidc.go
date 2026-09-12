// Package mockoidc runs an OpenID Connect provider for tests and local
// development. Every authorization request signs in as the next queued
// [User], or [DefaultUser] once the queue is empty, without a login page,
// and any redirect_uri is accepted. The protocol side is zitadel/oidc's
// op package over an in-memory store; the provider is served under
// /oidc so the issuer is <addr>/oidc, like the mock this one replaced.
package mockoidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"time"

	"github.com/zitadel/oidc/v3/pkg/op"
)

const (
	// IssuerBase is the path the provider is served under.
	IssuerBase = "/oidc"

	// DefaultAccessTTL and DefaultRefreshTTL are the token lifetimes a
	// [NewServer] starts with.
	DefaultAccessTTL  = 10 * time.Minute
	DefaultRefreshTTL = time.Hour

	credentialBytes   = 24
	signingKeyBits    = 2048
	readHeaderTimeout = 10 * time.Second
)

var errAlreadyStarted = errors.New("mock OIDC server already started")

// User is an identity the provider signs in as. The zero fields are left
// out of the claims; which claims a token carries follows the requested
// scopes (profile, email, phone, address, groups).
type User struct {
	Subject           string   `json:"sub"`
	Email             string   `json:"email,omitempty"`
	EmailVerified     bool     `json:"email_verified,omitempty"`
	PreferredUsername string   `json:"preferred_username,omitempty"`
	Phone             string   `json:"phone_number,omitempty"`
	Address           string   `json:"address,omitempty"`
	Groups            []string `json:"groups,omitempty"`
}

// DefaultUser is who the provider signs in as when nothing is queued.
func DefaultUser() User {
	return User{
		Subject:           "1234567890",
		Email:             "jane.doe@example.com",
		PreferredUsername: "jane.doe",
		Phone:             "555-987-6543",
		Address:           "123 Main Street",
		Groups:            []string{"engineering", "design"},
		EmailVerified:     true,
	}
}

// Server is a mock provider. ClientID, ClientSecret and the lifetimes may
// be changed before Start.
type Server struct {
	ClientID     string
	ClientSecret string

	// AccessTTL is the lifetime of access and ID tokens.
	AccessTTL time.Duration
	// RefreshTTL is the lifetime of refresh tokens.
	RefreshTTL time.Duration

	store      *store
	server     *http.Server
	scheme     string
	middleware []func(http.Handler) http.Handler
}

// NewServer prepares a provider with random client credentials and a
// fresh RSA signing key. It serves nothing until Start.
func NewServer() (*Server, error) {
	clientID, err := randomToken()
	if err != nil {
		return nil, err
	}

	clientSecret, err := randomToken()
	if err != nil {
		return nil, err
	}

	key, err := rsa.GenerateKey(rand.Reader, signingKeyBits)
	if err != nil {
		return nil, fmt.Errorf("generating signing key: %w", err)
	}

	return &Server{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AccessTTL:    DefaultAccessTTL,
		RefreshTTL:   DefaultRefreshTTL,
		store:        newStore(key),
	}, nil
}

// QueueUser appends a user to the login queue. Authorization requests
// pop users in order.
func (s *Server) QueueUser(user User) {
	s.store.queueUser(user)
}

// Use wraps the provider's handler; the first middleware added is the
// outermost. It has no effect after Start.
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.middleware = append(s.middleware, mw)
}

// Start serves the provider on ln in the background. A nil cfg serves
// plain HTTP.
func (s *Server) Start(ln net.Listener, cfg *tls.Config) error {
	if s.server != nil {
		return errAlreadyStarted
	}

	s.scheme = "http"
	if cfg != nil {
		s.scheme = "https"
	}

	issuer := s.scheme + "://" + ln.Addr().String() + IssuerBase

	s.store.configure(issuer, s.ClientID, s.ClientSecret, s.AccessTTL, s.RefreshTTL)

	cryptoKey, err := randomKey()
	if err != nil {
		return err
	}

	provider, err := op.NewProvider(&op.Config{
		CryptoKey:             cryptoKey,
		CodeMethodS256:        true,
		AuthMethodPost:        true,
		GrantTypeRefreshToken: true,
	}, s.store, op.StaticIssuer(issuer), op.WithAllowInsecure())
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle(IssuerBase+"/", http.StripPrefix(IssuerBase, provider))

	var handler http.Handler = mux
	for _, mw := range slices.Backward(s.middleware) {
		handler = mw(handler)
	}

	s.server = &http.Server{
		Addr:              ln.Addr().String(),
		Handler:           handler,
		TLSConfig:         cfg,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		var serveErr error
		if cfg != nil {
			serveErr = s.server.ServeTLS(ln, "", "")
		} else {
			serveErr = s.server.Serve(ln)
		}

		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			panic(serveErr)
		}
	}()

	return nil
}

// Shutdown stops the provider.
func (s *Server) Shutdown() error {
	if s.server == nil {
		return nil
	}

	err := s.server.Shutdown(context.Background())
	if err != nil {
		return fmt.Errorf("shutting down mock OIDC server: %w", err)
	}

	return nil
}

// Addr returns the base URL the provider listens on, empty before Start.
func (s *Server) Addr() string {
	if s.server == nil {
		return ""
	}

	return s.scheme + "://" + s.server.Addr
}

// Issuer returns the issuer, the value of the iss claim, empty before Start.
func (s *Server) Issuer() string {
	if s.server == nil {
		return ""
	}

	return s.Addr() + IssuerBase
}

func randomToken() (string, error) {
	b := make([]byte, credentialBytes)

	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomKey() ([32]byte, error) {
	var key [32]byte

	_, err := rand.Read(key[:])
	if err != nil {
		return key, fmt.Errorf("generating crypto key: %w", err)
	}

	return key, nil
}
