// Package idtoken signs the identity tokens a machine asks for with
// `tailscale id-token`, the way the hosted control plane's workload
// identity does: a JSON Web Token about one node, verifiable through
// OpenID discovery at the server URL. See docs/ref/identity-tokens.md.
package idtoken

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// TokenLifetime is how long a token is valid for; a machine asks for a
// fresh one each time it needs one.
const TokenLifetime = 5 * time.Minute

// Algorithm is the signing algorithm every token uses.
const Algorithm = jose.ES256

// The paths a verifier finds the key at, relative to the issuer.
const (
	DiscoveryPath = "/.well-known/openid-configuration"
	JWKSPath      = "/.well-known/jwks.json"
)

// ErrNotECDSAKey is returned for a stored key that is not an ECDSA key.
var ErrNotECDSAKey = errors.New("identity token key is not an ECDSA key")

// ErrNoKey is returned for stored key material that holds no PEM block.
var ErrNoKey = errors.New("identity token key holds no PEM block")

// Claims is what a token says; the standard claims first, then
// Tailscale's, as [tailcfg.TokenResponse] documents them.
type Claims struct {
	Issuer    string           `json:"iss"`
	Subject   string           `json:"sub"`
	Audience  jwt.Audience     `json:"aud"`
	Expiry    *jwt.NumericDate `json:"exp"`
	IssuedAt  *jwt.NumericDate `json:"iat"`
	NotBefore *jwt.NumericDate `json:"nbf"`
	ID        string           `json:"jti"`

	// Key is the node's public key.
	Key string `json:"key"`
	// Addresses are the node's tailnet addresses.
	Addresses []string `json:"addresses"`
	// NodeID is the node's machine ID.
	NodeID uint64 `json:"nid"`
	// Node is the node's name.
	Node string `json:"node"`
	// Domain is the tailnet's domain, as the map response carries it.
	Domain string `json:"domain"`
	// Tags are the node's tags as <domain>:tag:<name>, on a tagged node.
	Tags []string `json:"tags,omitempty"`
	// User is <domain>:<login> and UserID the user's ID, on a user-owned
	// node.
	User   string `json:"user,omitempty"`
	UserID uint64 `json:"uid,omitempty"`
}

// Configuration is the OpenID discovery document a verifier reads.
type Configuration struct {
	Issuer                           string   `json:"issuer"`
	JWKSURI                          string   `json:"jwks_uri"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
	ClaimsSupported                  []string `json:"claims_supported"`
}

// Discovery returns the discovery document for tokens issued by issuer.
func Discovery(issuer string) Configuration {
	return Configuration{
		Issuer:                           issuer,
		JWKSURI:                          issuer + JWKSPath,
		ResponseTypesSupported:           []string{"id_token"},
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: []string{string(Algorithm)},
		ClaimsSupported: []string{
			"iss", "sub", "aud", "exp", "iat", "nbf", "jti",
			"key", "addresses", "nid", "node", "domain", "tags", "user", "uid",
		},
	}
}

// Signer signs tokens with one key and publishes its public half.
type Signer struct {
	key    *ecdsa.PrivateKey
	keyID  string
	signer jose.Signer
}

// GenerateKey makes a new signing key.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating identity token key: %w", err)
	}

	return key, nil
}

// EncodeKey renders a key as PKCS#8 PEM for storage.
func EncodeKey(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("encoding identity token key: %w", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// DecodeKey reads a key [EncodeKey] rendered.
func DecodeKey(encoded string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(encoded))
	if block == nil {
		return nil, ErrNoKey
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("decoding identity token key: %w", err)
	}

	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrNotECDSAKey, parsed)
	}

	return key, nil
}

// New returns a signer over key.
func New(key *ecdsa.PrivateKey) (*Signer, error) {
	public := jose.JSONWebKey{Key: &key.PublicKey, Algorithm: string(Algorithm), Use: "sig"}

	thumb, err := public.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("computing the key id: %w", err)
	}

	keyID := base64.RawURLEncoding.EncodeToString(thumb)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: Algorithm, Key: jose.JSONWebKey{Key: key, KeyID: keyID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating the token signer: %w", err)
	}

	return &Signer{key: key, keyID: keyID, signer: signer}, nil
}

// KeyID is the id the tokens' header carries and the key set lists.
func (s *Signer) KeyID() string {
	return s.keyID
}

// Sign returns the serialised token for claims.
func (s *Signer) Sign(claims Claims) (string, error) {
	token, err := jwt.Signed(s.signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("signing the identity token: %w", err)
	}

	return token, nil
}

// JWKS is the key set a verifier reads, holding the public key.
func (s *Signer) JWKS() jose.JSONWebKeySet {
	return jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{{
			Key:       &s.key.PublicKey,
			KeyID:     s.keyID,
			Algorithm: string(Algorithm),
			Use:       "sig",
		}},
	}
}

// NewClaims returns the time claims for a token issued now for the
// audience, with a random id; the caller fills in the node.
func NewClaims(issuer, audience string, now time.Time) Claims {
	return Claims{
		Issuer:    issuer,
		Audience:  jwt.Audience{audience},
		Expiry:    jwt.NewNumericDate(now.Add(TokenLifetime)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ID:        newID(),
	}
}

// idBytes is the size of a token id.
const idBytes = 16

func newID() string {
	var b [idBytes]byte

	_, _ = rand.Read(b[:])

	return base64.RawURLEncoding.EncodeToString(b[:])
}
