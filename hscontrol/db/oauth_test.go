package db

import (
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOAuthClientCreateAndAuthenticate(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	secret, client, err := db.CreateOAuthClient(
		[]string{"auth_keys", "devices:core"},
		[]string{"tag:ci"},
		"my client",
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Secret carries the public client id as its middle segment, so it can be
	// derived from the secret alone (the Tailscale get-authkey trick).
	assert.True(t, strings.HasPrefix(secret, "hskey-client-"+client.ClientID+"-"))
	// Scopes/tags are deduplicated and sorted for stable storage.
	assert.Equal(t, []string{"auth_keys", "devices:core"}, client.Scopes)
	assert.Equal(t, []string{"tag:ci"}, client.Tags)
	// Only the SHA-256 hash is stored, never the plaintext.
	assert.True(t, strings.HasPrefix(string(client.SecretHash), hashPrefixSHA256))

	// The secret authenticates, deriving the client id from the secret itself.
	got, err := db.AuthenticateOAuthClient(secret)
	require.NoError(t, err)
	assert.Equal(t, client.ClientID, got.ClientID)

	// A truncated/garbage secret does not.
	_, err = db.AuthenticateOAuthClient("hskey-client-deadbeef-nope")
	require.Error(t, err)

	// Wrong secret for a real client id is rejected by the constant-time compare.
	_, err = db.AuthenticateOAuthClient("hskey-client-" + client.ClientID + "-" + strings.Repeat("0", 64))
	require.Error(t, err)
}

// TestOAuthClientAuthenticateTailscalePrefix asserts the same stored client
// authenticates under the tskey-client- alias, and that only a leading prefix
// is recognised.
func TestOAuthClientAuthenticateTailscalePrefix(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	secret, client, err := db.CreateOAuthClient([]string{"auth_keys"}, []string{"tag:ci"}, "", nil)
	require.NoError(t, err)

	rest := strings.TrimPrefix(secret, types.OAuthClientPrefix)
	tsSecret := types.TailscaleOAuthClientPrefix + rest

	for _, s := range []string{
		tsSecret,
		// Callers may pass the raw auth-key form; ?attributes are stripped.
		tsSecret + "?baseURL=http://127.0.0.1:8080&ephemeral=true",
	} {
		got, authErr := db.AuthenticateOAuthClient(s)
		require.NoError(t, authErr, s)
		assert.Equal(t, client.ClientID, got.ClientID)
	}

	// A wrong secret under the alias parses but fails verification.
	_, err = db.AuthenticateOAuthClient(
		types.TailscaleOAuthClientPrefix + client.ClientID + "-" + strings.Repeat("0", 64),
	)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrOAuthClientFailedToParse)

	for _, s := range []string{
		types.TailscaleOAuthClientPrefix,
		"tskey-auth-" + rest,
		"tskey-" + rest,
		"junk-" + tsSecret,
		"junk-" + secret,
		types.TailscaleOAuthClientPrefix + secret,
	} {
		_, authErr := db.AuthenticateOAuthClient(s)
		require.ErrorIs(t, authErr, ErrOAuthClientFailedToParse, s)
	}
}

func TestOAuthClientRevoke(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	secret, client, err := db.CreateOAuthClient([]string{"auth_keys"}, []string{"tag:ci"}, "", nil)
	require.NoError(t, err)

	// A token minted by the client survives only until the client is revoked.
	_, _, err = db.MintAccessToken(client.ClientID, client.Scopes, client.Tags, nil)
	require.NoError(t, err)

	require.NoError(t, db.RevokeOAuthClient(client.ClientID))

	// The client no longer authenticates and a repeated revoke is a clean 404.
	_, err = db.AuthenticateOAuthClient(secret)
	require.Error(t, err)
	require.ErrorIs(t, db.RevokeOAuthClient(client.ClientID), ErrOAuthClientNotFound)
}

func TestOAuthAccessTokenMintAuthenticateExpire(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	_, client, err := db.CreateOAuthClient([]string{"auth_keys"}, []string{"tag:ci"}, "", nil)
	require.NoError(t, err)

	future := time.Now().Add(time.Hour)
	tokenStr, token, err := db.MintAccessToken(
		client.ClientID,
		[]string{"auth_keys"},
		[]string{"tag:ci"},
		&future,
	)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(tokenStr, "hskey-oauthtok-"))

	got, err := db.AuthenticateAccessToken(tokenStr)
	require.NoError(t, err)
	assert.Equal(t, client.ClientID, got.ClientID)
	assert.Equal(t, []string{"auth_keys"}, got.Scopes)
	assert.Equal(t, []string{"tag:ci"}, got.Tags)

	// An expired token is rejected even though the row still exists.
	past := time.Now().Add(-time.Hour)
	expiredStr, _, err := db.MintAccessToken(client.ClientID, nil, nil, &past)
	require.NoError(t, err)
	_, err = db.AuthenticateAccessToken(expiredStr)
	require.ErrorIs(t, err, ErrAccessTokenExpired)

	// The reaper deletes the expired row; the live token is untouched.
	n, err := db.DeleteExpiredAccessTokens(time.Now())
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	_ = token

	_, err = db.AuthenticateAccessToken(tokenStr)
	require.NoError(t, err)
}

// TestAccessTokenRejectedWhenClientGone asserts a token whose issuing client no
// longer exists (orphaned by a delete/revoke race) is rejected, even though the
// token row itself is valid and unexpired.
func TestAccessTokenRejectedWhenClientGone(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	_, client, err := db.CreateOAuthClient([]string{"auth_keys"}, []string{"tag:ci"}, "", nil)
	require.NoError(t, err)

	future := time.Now().Add(time.Hour)
	tokenStr, _, err := db.MintAccessToken(client.ClientID, []string{"auth_keys"}, []string{"tag:ci"}, &future)
	require.NoError(t, err)

	_, err = db.AuthenticateAccessToken(tokenStr)
	require.NoError(t, err)

	// Delete only the client row, leaving the token orphaned (the state a
	// mint/revoke race or manual deletion would produce).
	_, err = db.DB.ExecContext(t.Context(), "DELETE FROM oauth_clients WHERE client_id = $1", client.ClientID)
	require.NoError(t, err)

	_, err = db.AuthenticateAccessToken(tokenStr)
	require.ErrorIs(t, err, ErrAccessTokenClientRevoked)

	// A soft-revoked client (row present, Revoked set) is likewise rejected.
	_, client2, err := db.CreateOAuthClient([]string{"auth_keys"}, []string{"tag:ci"}, "", nil)
	require.NoError(t, err)

	tokenStr2, _, err := db.MintAccessToken(client2.ClientID, []string{"auth_keys"}, []string{"tag:ci"}, &future)
	require.NoError(t, err)

	now := time.Now()
	_, err = db.DB.ExecContext(t.Context(),
		"UPDATE oauth_clients SET revoked = $1 WHERE client_id = $2", now, client2.ClientID)
	require.NoError(t, err)

	_, err = db.AuthenticateAccessToken(tokenStr2)
	require.ErrorIs(t, err, ErrAccessTokenClientRevoked)
}
