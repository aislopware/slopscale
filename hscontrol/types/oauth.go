package types

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const (
	// OAuthClientPrefix prefixes an OAuth client secret:
	// hskey-client-<clientID>-<secret>.
	OAuthClientPrefix = "hskey-client-"

	// AccessTokenPrefix prefixes an OAuth access token:
	// hskey-oauthtok-<prefix>-<secret>. The v2 auth middleware dispatches a
	// scope-limited token from an all-access admin key on this prefix alone, so
	// it is one canonical constant shared by the db and api layers.
	AccessTokenPrefix = "hskey-oauthtok-" //nolint:gosec // prefix, not a credential
)

// The kinds a row of the oauth_clients table can be, Tailscale's keyType
// values. A client holds a secret and mints tokens with the
// client-credentials grant; a federated identity holds none and mints them
// by presenting a JWT its own identity provider signed.
const (
	OAuthKeyTypeClient    = "client"
	OAuthKeyTypeFederated = "federated"
)

// FederatedIdentitySpec is what a federated identity is created or
// replaced with. It is one struct rather than a run of string arguments
// because every field is a trust condition and mixing two of them up would
// be silent.
type FederatedIdentitySpec struct {
	// Scopes the identity may grant and Tags it may assign, as for an
	// [OAuthClient].
	Scopes []string
	Tags   []string

	Description string

	// Issuer is the OIDC issuer whose discovery document and JWKS the
	// presented JWT is verified against; Audience and Subject are the aud
	// entry it must carry and the sub it must equal.
	Issuer   string
	Audience string
	Subject  string

	// CustomClaimRules are further claims the JWT must carry, claim name
	// to the value it must have. A claim holding a list satisfies a rule
	// when the list contains the value.
	CustomClaimRules map[string]string

	// CreatorUserID records who created the identity, as for an
	// [OAuthClient].
	CreatorUserID *uint
}

// OAuthClient is a long-lived OAuth 2.0 client-credentials principal. It mints
// short-lived [OAuthAccessToken]s limited to its Scopes and Tags. The secret is
// stored only as an Argon2id hash. ClientID is public and embedded in the secret
// string (hskey-client-<ClientID>-<secret>) so the token endpoint can derive it
// from the secret alone, matching Tailscale, where the client id is a substring
// of the client secret.
//
// An OAuth client is always tag/tailnet-scoped, never user-owned: the access
// tokens it mints, and the auth keys those tokens create, produce tagged nodes.
// UserID only records who created the client (informational), mirroring
// [APIKey].
type OAuthClient struct {
	ID       uint64
	ClientID string
	// SecretHash is empty for a federated identity, which authenticates
	// with a JWT its identity provider signed rather than a stored secret.
	SecretHash []byte

	// KeyType is [OAuthKeyTypeClient] or [OAuthKeyTypeFederated]. An empty
	// value reads as a client: rows written before federated identities
	// existed carry none.
	KeyType string

	// Issuer, Audience, Subject and CustomClaimRules are the trust
	// conditions of a federated identity and are empty on a client. See
	// [FederatedIdentitySpec].
	Issuer           string
	Audience         string
	Subject          string
	CustomClaimRules map[string]string

	// Scopes the client may grant. Tags the client may assign to access tokens
	// (and, transitively, to the auth keys and nodes those tokens create).
	Scopes []string
	Tags   []string

	Description string

	// UserID records who created the client. Kept as a plain column with no
	// foreign key so an upgraded database matches a freshly-migrated one.
	UserID *uint

	CreatedAt *time.Time
	Revoked   *time.Time
}

// OAuthAccessToken is a short-lived bearer token minted by an [OAuthClient] via
// the client-credentials grant. It carries the scope/tag set granted at mint
// time (a subset of the issuing client's), is stored as an Argon2id hash of its
// secret, and authenticates v2 API requests as Authorization: Bearer.
type OAuthAccessToken struct {
	ID     uint64
	Prefix string
	Hash   []byte

	// ClientID links back to the issuing [OAuthClient].
	ClientID string
	// ClientUserID is the user the issuing client belongs to, filled at
	// authentication from that client and never stored on the token: the
	// token is bounded by its owner's current role, not the role held when
	// the client was made.
	ClientUserID *uint

	Scopes []string
	Tags   []string

	Expiration *time.Time
	CreatedAt  *time.Time
}

// MarshalZerologObject implements [zerolog.LogObjectMarshaler] for safe logging.
// SECURITY: intentionally does NOT log the secret or hash.
func (c *OAuthClient) MarshalZerologObject(e *zerolog.Event) {
	if c == nil {
		return
	}

	e.Uint64("oauth_client_id", c.ID)

	if masked := c.maskedClientID(); masked != "" {
		e.Str("oauth_client", masked)
	}

	if len(c.Scopes) > 0 {
		e.Strs("oauth_client_scopes", c.Scopes)
	}

	if len(c.Tags) > 0 {
		e.Strs("oauth_client_tags", c.Tags)
	}

	if c.Revoked != nil {
		e.Time("oauth_client_revoked", *c.Revoked)
	}
}

// MarshalZerologObject implements [zerolog.LogObjectMarshaler] for safe logging.
// SECURITY: intentionally does NOT log the secret or hash.
func (t *OAuthAccessToken) MarshalZerologObject(e *zerolog.Event) {
	if t == nil {
		return
	}

	e.Uint64("oauth_token_id", t.ID)

	if masked := t.maskedPrefix(); masked != "" {
		e.Str("oauth_token", masked)
	}

	if t.ClientID != "" {
		e.Str("oauth_token_client", t.ClientID)
	}

	if len(t.Scopes) > 0 {
		e.Strs("oauth_token_scopes", t.Scopes)
	}

	if len(t.Tags) > 0 {
		e.Strs("oauth_token_tags", t.Tags)
	}

	if t.Expiration != nil {
		e.Time("oauth_token_expiration", *t.Expiration)
	}
}

// maskedPrefix returns the token prefix in masked form for safe logging.
// SECURITY: never log the secret or its hash.
func (t *OAuthAccessToken) maskedPrefix() string {
	if t.Prefix != "" {
		return AccessTokenPrefix + t.Prefix + "-***"
	}

	return ""
}

// IsFederated reports whether the row is a federated identity rather than
// a client-credentials client. A row written before federated identities
// existed has no key type and is a client.
func (c *OAuthClient) IsFederated() bool {
	return c != nil && c.KeyType == OAuthKeyTypeFederated
}

// Kind is the row's keyType as the API reports it, filling in "client"
// for a row written before federated identities existed.
func (c *OAuthClient) Kind() string {
	if c.IsFederated() {
		return OAuthKeyTypeFederated
	}

	return OAuthKeyTypeClient
}

// maskedClientID returns the client id in masked form for safe logging.
// SECURITY: never log the secret or its hash.
func (c *OAuthClient) maskedClientID() string {
	if c.ClientID != "" {
		return OAuthClientPrefix + c.ClientID + "-***"
	}

	return ""
}

// Errors from [ParseFederatedIssuer].
var (
	ErrIssuerURL      = errors.New("issuer must be the https URL of an OpenID Connect issuer")
	ErrIssuerInsecure = errors.New("issuer must use https; plain http is only allowed on a loopback address")
)

// ParseFederatedIssuer normalises an OpenID Connect issuer URL, without
// its trailing slash, and returns it with the host for the egress check. The
// discovery document and the key set are fetched from it, so anything
// but https would let a network on the path hand the server a key set of
// its own; a loopback issuer is the exception, for tests and a provider
// on the same machine.
func ParseFederatedIssuer(raw string) (string, string, error) {
	issuer := strings.TrimSuffix(strings.TrimSpace(raw), "/")

	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", ErrIssuerURL
	}

	if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
		return "", "", ErrIssuerInsecure
	}

	return issuer, u.Host, nil
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}

	ip, err := netip.ParseAddr(host)

	return err == nil && ip.IsLoopback()
}
