package idtoken_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/idtoken"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignAndVerify(t *testing.T) {
	t.Parallel()

	key, err := idtoken.GenerateKey()
	require.NoError(t, err)

	signer, err := idtoken.New(key)
	require.NoError(t, err)

	now := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	claims := idtoken.NewClaims("https://control.example.com", "https://vault.example.com", now)
	claims.Subject = "laptop.ts.example.com"
	claims.Key = "nodekey:abcd"
	claims.Addresses = []string{"100.64.0.1", "fd7a:115c:a1e0::1"}
	claims.NodeID = 7
	claims.Node = "laptop"
	claims.Domain = "control.example.com"
	claims.User = "control.example.com:alice"
	claims.UserID = 3

	token, err := signer.Sign(claims)
	require.NoError(t, err)

	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	require.Len(t, parsed.Headers, 1)
	assert.Equal(t, signer.KeyID(), parsed.Headers[0].KeyID)

	// The key set verifies what the signer signed, and nothing else.
	jwks := signer.JWKS()
	require.Len(t, jwks.Keys, 1)
	assert.Equal(t, signer.KeyID(), jwks.Keys[0].KeyID)
	assert.True(t, jwks.Keys[0].IsPublic())

	var out idtoken.Claims

	require.NoError(t, parsed.Claims(jwks.Keys[0].Key, &out))
	assert.Equal(t, claims, out)

	other, err := idtoken.GenerateKey()
	require.NoError(t, err)
	require.Error(t, parsed.Claims(&other.PublicKey, &out))

	// The audience is a string on the wire, as the client expects.
	var raw map[string]any

	require.NoError(t, parsed.UnsafeClaimsWithoutVerification(&raw))
	assert.Equal(t, "https://vault.example.com", raw["aud"])
	assert.InDelta(t, float64(now.Add(idtoken.TokenLifetime).Unix()), raw["exp"], 0)
	assert.NotContains(t, raw, "tags", "a user-owned node has no tags claim")
}

func TestKeyRoundTrip(t *testing.T) {
	t.Parallel()

	key, err := idtoken.GenerateKey()
	require.NoError(t, err)

	encoded, err := idtoken.EncodeKey(key)
	require.NoError(t, err)
	assert.Contains(t, encoded, "PRIVATE KEY")

	decoded, err := idtoken.DecodeKey(encoded)
	require.NoError(t, err)
	assert.True(t, key.Equal(decoded))

	_, err = idtoken.DecodeKey("not a key")
	require.ErrorIs(t, err, idtoken.ErrNoKey)
}

func TestDiscovery(t *testing.T) {
	t.Parallel()

	doc := idtoken.Discovery("https://control.example.com")

	raw, err := json.Marshal(doc)
	require.NoError(t, err)

	var out map[string]any

	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, "https://control.example.com", out["issuer"])
	assert.Equal(t, "https://control.example.com/.well-known/jwks.json", out["jwks_uri"])
	assert.Equal(t, []any{"ES256"}, out["id_token_signing_alg_values_supported"])
	assert.Contains(t, out["claims_supported"], "nid")
}
