package apiv2

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
)

const (
	// tokenExchangeGrantType is RFC 8693's grant. The Tailscale client
	// omits grant_type entirely on this endpoint, so an empty value means
	// the same thing.
	tokenExchangeGrantType = "urn:ietf:params:oauth:grant-type:token-exchange" //nolint:gosec // a grant name

	// exchangeAuditAction is the audit action of one token exchange, the
	// federated counterpart of grantAuditAction.
	exchangeAuditAction = "oauth.token.exchange"

	// jwtLeeway is the clock skew allowed on the presented JWT's exp, nbf
	// and iat.
	jwtLeeway = 60 * time.Second

	// The issuer cache holds the discovery document (and, through the
	// provider, the JWKS) of each issuer a federated identity names.
	// issuerCacheTTL bounds how stale a discovery document may get;
	// issuerCacheMax bounds how many issuers one server tracks, since the
	// issuer is operator input.
	issuerCacheTTL = time.Hour
	issuerCacheMax = 32

	// issuerFetchTimeout bounds one discovery fetch, so a slow issuer
	// cannot hold the token endpoint open.
	issuerFetchTimeout = 15 * time.Second
)

// The conditions a presented token can fail. The text reaches the caller
// as the OAuth error_description, wrapped with what was actually seen.
var (
	errTokenSubject     = errors.New("token subject does not match the identity")
	errTokenFromFuture  = errors.New("token was issued in the future")
	errTokenClaimAbsent = errors.New("token is missing a claim the identity requires")
	errTokenClaimValue  = errors.New("token claim does not satisfy the identity's rule")
	errIssuerDiscovery  = errors.New("issuer discovery failed")
)

// registerOAuthTokenExchange mounts POST /api/v2/oauth/token-exchange. Like
// the token endpoint it is a plain handler: form in, RFC 6749 error bodies
// out, and it authenticates the caller itself.
func registerOAuthTokenExchange(router chi.Router, b Backend) {
	router.Post("/api/v2/oauth/token-exchange", oauthTokenExchangeHandler(b))
}

// oauthTokenExchangeHandler implements workload identity federation: the
// caller presents the OIDC JWT its own platform signed, and gets a
// slopscale access token carrying the named identity's scopes and tags.
func oauthTokenExchangeHandler(b Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deny := func(clientID string, status int, code, desc string) {
			writeOAuthError(w, status, code, desc)
			recordExchangeEvent(b, r, clientID, status, map[string]any{"error": code})
		}

		err := r.ParseForm()
		if err != nil {
			deny("", http.StatusBadRequest, "invalid_request", "could not parse request body")

			return
		}

		if gt := r.PostForm.Get("grant_type"); gt != "" && gt != tokenExchangeGrantType {
			deny("", http.StatusBadRequest, "unsupported_grant_type",
				"only the token-exchange grant is supported here")

			return
		}

		clientID := r.PostForm.Get("client_id")
		rawJWT := r.PostForm.Get("jwt")

		if clientID == "" || rawJWT == "" {
			deny(clientID, http.StatusBadRequest, "invalid_request",
				"client_id and jwt are required")

			return
		}

		identity, err := b.State.GetOAuthClientByClientID(clientID)
		if err != nil || !identity.IsFederated() || identity.Revoked != nil {
			deny(clientID, http.StatusUnauthorized, "invalid_client", "unknown federated identity")

			return
		}

		err = verifyFederatedJWT(r.Context(), identity, rawJWT)
		if err != nil {
			deny(clientID, http.StatusBadRequest, "invalid_grant", err.Error())

			return
		}

		expiry := time.Now().Add(accessTokenTTL)

		tokenStr, _, err := b.State.MintAccessToken(clientID, identity.Scopes, identity.Tags, &expiry)
		if err != nil {
			deny(clientID, http.StatusInternalServerError, "server_error", "could not mint access token")

			return
		}

		recordExchangeEvent(b, r, clientID, http.StatusOK,
			map[string]any{"scopes": identity.Scopes, "tags": identity.Tags, "issuer": identity.Issuer})

		writeJSON(w, http.StatusOK, tokenResponse{
			AccessToken: tokenStr,
			TokenType:   "Bearer",
			ExpiresIn:   int(accessTokenTTL.Seconds()),
			Scope:       strings.Join(identity.Scopes, " "),
		})
	}
}

// recordExchangeEvent writes the audit entry for one exchange. A refused
// attempt is recorded too: the endpoint is unauthenticated, so a run of
// failures against one identity is what a credential hunt looks like.
func recordExchangeEvent(b Backend, r *http.Request, clientID string, outcome int, detail map[string]any) {
	audit.Record(b.State, &types.AuditEvent{
		Action:     exchangeAuditAction,
		ActorKind:  types.ActorOAuth,
		ActorName:  clientID,
		TargetKind: "oauth_client",
		TargetID:   clientID,
		Outcome:    outcome,
		Detail:     detail,
		RemoteAddr: r.RemoteAddr,
	})
}

// verifyFederatedJWT checks the presented token against every condition
// the identity was created with. The error text reaches the caller as the
// OAuth error_description, so it names what failed without echoing the
// token.
func verifyFederatedJWT(ctx context.Context, identity *types.OAuthClient, rawJWT string) error {
	provider, err := issuerProvider(ctx, identity.Issuer)
	if err != nil {
		return fmt.Errorf("issuer %s could not be reached: %w", identity.Issuer, err)
	}

	// go-oidc checks the signature, the issuer, that aud contains the
	// audience, and exp/nbf; Now is shifted back so exp gets the leeway.
	verifier := provider.Verifier(&oidc.Config{
		ClientID: identity.Audience,
		Now:      func() time.Time { return time.Now().Add(-jwtLeeway) },
	})

	token, err := verifier.Verify(ctx, rawJWT)
	if err != nil {
		return fmt.Errorf("token rejected: %w", err)
	}

	if token.Subject != identity.Subject {
		return fmt.Errorf("%w: %q is not %q", errTokenSubject, token.Subject, identity.Subject)
	}

	if !token.IssuedAt.IsZero() && token.IssuedAt.After(time.Now().Add(jwtLeeway)) {
		return fmt.Errorf("%w: %s", errTokenFromFuture, token.IssuedAt.Format(time.RFC3339))
	}

	if len(identity.CustomClaimRules) == 0 {
		return nil
	}

	claims := map[string]any{}

	err = token.Claims(&claims)
	if err != nil {
		return fmt.Errorf("reading token claims: %w", err)
	}

	for name, want := range identity.CustomClaimRules {
		value, ok := claims[name]
		if !ok {
			return fmt.Errorf("%w: %s", errTokenClaimAbsent, name)
		}

		if !claimSatisfies(value, want) {
			return fmt.Errorf("%w: %s is not %q", errTokenClaimValue, name, want)
		}
	}

	return nil
}

// claimSatisfies reports whether a claim meets a rule. A claim holding a
// list is satisfied when the list contains the value, which is how a
// repeated claim (groups, a repository list) is written.
func claimSatisfies(value any, want string) bool {
	switch v := value.(type) {
	case string:
		return v == want
	case bool:
		return strconv.FormatBool(v) == want
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64) == want
	case []any:
		for _, item := range v {
			if claimSatisfies(item, want) {
				return true
			}
		}
	}

	return false
}

// issuerProviders caches one [oidc.Provider] per issuer. The provider
// holds the discovery document and, once used, a key set that refreshes
// itself when it meets an unknown key id, so a rotation needs no eviction.
var issuerProviders = struct {
	mu      sync.Mutex
	entries map[string]issuerEntry
}{entries: map[string]issuerEntry{}}

type issuerEntry struct {
	provider *oidc.Provider
	fetched  time.Time
}

// issuerProvider returns the cached provider for issuer, fetching its
// discovery document when there is none or the cached one has aged out.
// The fetch dials through the egress guard, since the issuer is operator
// input.
func issuerProvider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	issuerProviders.mu.Lock()
	defer issuerProviders.mu.Unlock()

	entry, ok := issuerProviders.entries[issuer]
	if ok && time.Since(entry.fetched) < issuerCacheTTL {
		return entry.provider, nil
	}

	// The issuer list is bounded because an operator with oauth_keys can
	// name any issuer; when it is full the whole cache is dropped, which
	// costs one refetch each rather than tracking use order for a map this
	// small.
	if len(issuerProviders.entries) >= issuerCacheMax {
		clear(issuerProviders.entries)
	}

	client := &http.Client{Transport: egress.Transport(), Timeout: issuerFetchTimeout}

	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), issuerFetchTimeout)
	defer cancel()

	provider, err := oidc.NewProvider(oidc.ClientContext(fetchCtx, client), issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errIssuerDiscovery, err)
	}

	issuerProviders.entries[issuer] = issuerEntry{provider: provider, fetched: time.Now()}

	return provider, nil
}
