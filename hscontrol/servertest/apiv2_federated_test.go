package servertest_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tsclient "tailscale.com/client/tailscale/v2"
)

// The audience, subject and claim the federated identity under test
// trusts. They are shaped like a GitHub Actions OIDC token, which is what
// workload identity federation is mostly used for.
const (
	fedAudience   = "slopscale"
	fedSubject    = "repo:acme/app:ref:refs/heads/main"
	fedClaimName  = "repository"
	fedClaimValue = "acme/app"
)

// federatedPolicy declares the tags the exchanged token may and may not
// assign. tag:fed-forbidden exists in policy but is not on the identity,
// so a token that tries to mint a key with it must be refused for the tag
// rather than for the tag being unknown.
const federatedPolicy = `{
  "tagOwners": {"tag:fed-ci": ["federated@"], "tag:fed-forbidden": ["federated@"]},
  "acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]
}`

// TestAPIv2FederatedIdentity proves workload identity federation end to
// end against the official Go SDK: a federated identity is created through
// the keys resource, a JWT a real in-process OIDC issuer signed is
// exchanged for an access token at /api/v2/oauth/token-exchange, and the
// token mints an auth key with a tag the identity holds but not with one
// it does not. Every trust condition is then exercised from the failing
// side, because an identity that accepts a token it should not is the one
// failure that matters here.
//
// It needs neither tscli nor tofu, so it runs on a bare checkout.
func TestAPIv2FederatedIdentity(t *testing.T) {
	srv := servertest.NewServer(t, servertest.WithRealListener())
	owner := srv.CreateUser(t, "federated")
	apiKey := srv.CreateAPIKey(t, owner)

	setStatePolicy(t, srv, federatedPolicy)

	idp := startFederatedIssuer(t)
	keys := goClient(t, srv.URL, apiKey).Keys()
	ctx := t.Context()

	identity, err := keys.CreateFederatedIdentity(ctx, tsclient.CreateFederatedIdentityRequest{
		Scopes:           []string{"auth_keys"},
		Tags:             []string{"tag:fed-ci"},
		Audience:         fedAudience,
		Issuer:           idp.URL,
		Subject:          fedSubject,
		CustomClaimRules: map[string]string{fedClaimName: fedClaimValue},
		Description:      "ci",
	})
	require.NoError(t, err)
	require.NotEmpty(t, identity.ID)
	assert.Empty(t, identity.Key, "a federated identity has no secret to show")

	// Get and list echo the trust conditions back, which the Terraform
	// provider reads to decide whether its resource has drifted.
	got, err := keys.Get(ctx, identity.ID)
	require.NoError(t, err)
	assert.Equal(t, "federated", got.KeyType)
	assert.Equal(t, fedAudience, got.Audience)
	assert.Equal(t, idp.URL, got.Issuer)
	assert.Equal(t, fedSubject, got.Subject)
	assert.Equal(t, map[string]string{fedClaimName: fedClaimValue}, got.CustomClaimRules)
	assert.Equal(t, []string{"tag:fed-ci"}, got.Tags)

	list, err := keys.List(ctx, true)
	require.NoError(t, err)
	assert.True(t, containsKeyID(list, identity.ID), "the identity is listed with the keys")

	t.Run("ExchangeAndUse", func(t *testing.T) {
		fedClient := &tsclient.Client{
			BaseURL: mustParseURL(t, srv.URL),
			Tailnet: "-",
			Auth: &tsclient.IdentityFederation{
				ClientID:    identity.ID,
				IDTokenFunc: func() (string, error) { return idp.sign(t, idp.claims()), nil },
			},
		}

		var req tsclient.CreateKeyRequest

		req.Description = "from-federation"
		req.ExpirySeconds = 3600
		req.Capabilities.Devices.Create.Tags = []string{"tag:fed-ci"}

		created, err := fedClient.Keys().CreateAuthKey(ctx, req)
		require.NoError(t, err, "the exchanged token mints a key with a tag the identity holds")
		assert.NotEmpty(t, created.Key)

		req.Capabilities.Devices.Create.Tags = []string{"tag:fed-forbidden"}

		_, err = fedClient.Keys().CreateAuthKey(ctx, req)
		require.Error(t, err, "the token must not mint a key with a tag outside its grant")
	})

	t.Run("Refusals", func(t *testing.T) {
		other := startFederatedIssuer(t)

		for _, tc := range []struct {
			name   string
			claims map[string]any
			want   string
		}{
			{
				name:   "wrong audience",
				claims: idp.claimsWith(map[string]any{"aud": "someone-else"}),
				want:   "invalid_grant",
			},
			{
				name:   "wrong subject",
				claims: idp.claimsWith(map[string]any{"sub": "repo:acme/app:ref:refs/heads/other"}),
				want:   "invalid_grant",
			},
			{
				name: "expired",
				claims: idp.claimsWith(map[string]any{
					"exp": jwt.NewNumericDate(time.Now().Add(-time.Hour)),
					"iat": jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
				}),
				want: "invalid_grant",
			},
			{
				name:   "unknown issuer",
				claims: idp.claimsWith(map[string]any{"iss": other.URL}),
				want:   "invalid_grant",
			},
			{
				name:   "failing claim rule",
				claims: idp.claimsWith(map[string]any{fedClaimName: "acme/other"}),
				want:   "invalid_grant",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				status, body := exchangeFederatedToken(ctx, t, srv, identity.ID, idp.sign(t, tc.claims))
				assert.GreaterOrEqual(t, status, http.StatusBadRequest)
				assert.Equal(t, tc.want, body["error"], "body: %v", body)
			})
		}

		t.Run("token signed by another issuer", func(t *testing.T) {
			// The claims name the trusted issuer but the signature is the
			// other one's, so only the JWKS check can catch it.
			status, body := exchangeFederatedToken(ctx, t, srv, identity.ID, other.sign(t, idp.claims()))
			assert.GreaterOrEqual(t, status, http.StatusBadRequest)
			assert.Equal(t, "invalid_grant", body["error"], "body: %v", body)
		})

		t.Run("unknown identity", func(t *testing.T) {
			status, body := exchangeFederatedToken(ctx, t, srv, "0123456789ab", idp.sign(t, idp.claims()))
			assert.Equal(t, http.StatusUnauthorized, status)
			assert.Equal(t, "invalid_client", body["error"], "body: %v", body)
		})
	})

	t.Run("SetAndDelete", func(t *testing.T) {
		updated, err := keys.SetFederatedIdentity(ctx, identity.ID, tsclient.SetFederatedIdentityRequest{
			Scopes:      []string{"devices:core:read"},
			Audience:    fedAudience,
			Issuer:      idp.URL,
			Subject:     "repo:acme/app:ref:refs/heads/release",
			Description: "ci, narrowed",
		})
		require.NoError(t, err)
		assert.Equal(t, "federated", updated.KeyType)
		assert.Equal(t, []string{"devices:core:read"}, updated.Scopes)
		assert.Equal(t, "repo:acme/app:ref:refs/heads/release", updated.Subject)
		assert.Empty(t, updated.CustomClaimRules, "an omitted rule set clears the rules")

		// The old subject no longer satisfies the identity.
		status, body := exchangeFederatedToken(ctx, t, srv, identity.ID, idp.sign(t, idp.claims()))
		assert.Equal(t, "invalid_grant", body["error"], "status %d body %v", status, body)

		require.NoError(t, keys.Delete(ctx, identity.ID))

		_, err = keys.Get(ctx, identity.ID)
		require.Error(t, err)
		assert.True(t, tsclient.IsNotFound(err), "a deleted identity is gone: %v", err)
	})
}

// setStatePolicy installs a policy in both the policy manager and the
// database, as the ACL endpoint does, so tag ownership is in force.
func setStatePolicy(t *testing.T, srv *servertest.TestServer, policy string) {
	t.Helper()

	st := srv.State()

	_, err := st.SetPolicy([]byte(policy))
	require.NoError(t, err)

	_, err = st.SetPolicyInDB(policy)
	require.NoError(t, err)

	_, err = st.ReloadPolicy()
	require.NoError(t, err)
}

// exchangeFederatedToken posts one exchange and returns the status and the
// decoded OAuth body, so a refusal can be asserted on its RFC 6749 error
// code rather than on the SDK's wrapped text.
func exchangeFederatedToken(
	ctx context.Context, t *testing.T, srv *servertest.TestServer, clientID, rawJWT string,
) (int, map[string]any) {
	t.Helper()

	form := url.Values{"client_id": {clientID}, "jwt": {rawJWT}}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, srv.URL+"/api/v2/oauth/token-exchange",
		strings.NewReader(form.Encode()),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := srv.HTTPClient(t).Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body := map[string]any{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	return resp.StatusCode, body
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	require.NoError(t, err)

	return u
}

// federatedIssuer is an in-process OpenID Connect issuer: a discovery
// document and a JWKS over one ES256 key, which is all the token exchange
// reads from an issuer.
type federatedIssuer struct {
	URL string

	signer jose.Signer
	jwks   jose.JSONWebKeySet
}

func startFederatedIssuer(t *testing.T) *federatedIssuer {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	const keyID = "test"

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: key, KeyID: keyID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	require.NoError(t, err)

	issuer := &federatedIssuer{
		signer: signer,
		jwks: jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: &key.PublicKey, KeyID: keyID, Algorithm: string(jose.ES256), Use: "sig",
		}}},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeIssuerJSON(t, w, map[string]any{
			"issuer":                                issuer.URL,
			"jwks_uri":                              issuer.URL + "/jwks",
			"response_types_supported":              []string{"id_token"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{string(jose.ES256)},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeIssuerJSON(t, w, issuer.jwks)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	issuer.URL = server.URL

	return issuer
}

func writeIssuerJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(v))
}

// claims is a token the identity under test accepts.
func (i *federatedIssuer) claims() map[string]any {
	now := time.Now()

	return map[string]any{
		"iss":        i.URL,
		"sub":        fedSubject,
		"aud":        fedAudience,
		"exp":        jwt.NewNumericDate(now.Add(10 * time.Minute)),
		"iat":        jwt.NewNumericDate(now),
		"nbf":        jwt.NewNumericDate(now),
		fedClaimName: fedClaimValue,
	}
}

// claimsWith is [federatedIssuer.claims] with some claims replaced.
func (i *federatedIssuer) claimsWith(overrides map[string]any) map[string]any {
	claims := i.claims()
	maps.Copy(claims, overrides)

	return claims
}

func (i *federatedIssuer) sign(t *testing.T, claims map[string]any) string {
	t.Helper()

	raw, err := jwt.Signed(i.signer).Claims(claims).Serialize()
	require.NoError(t, err)

	return raw
}
