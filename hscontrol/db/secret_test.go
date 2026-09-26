package db

import (
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// legacyArgon2idHash encodes secret the way OAuth clients and access tokens
// were stored before SHA-256, at the cheapest cost argon2 accepts.
func legacyArgon2idHash(secret string) []byte {
	const (
		memory   = 8
		timeCost = 1
		threads  = 1
	)

	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte(secret), salt, timeCost, memory, threads, argon2KeyLen)

	return fmt.Appendf(nil, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, timeCost, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// legacyBcryptHash encodes secret the way API and pre-auth keys were stored
// before SHA-256.
func legacyBcryptHash(t *testing.T, secret string) []byte {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	require.NoError(t, err)

	return hash
}

func TestHashSecretRoundTrip(t *testing.T) {
	t.Parallel()

	const secret = "a-high-entropy-credential-secret"

	encoded := hashSecret(secret)
	assert.True(t, strings.HasPrefix(string(encoded), hashPrefixSHA256))
	assert.Equal(t, encoded, hashSecret(secret), "an unsalted digest is deterministic")

	needsRehash, err := verifySecret(encoded, secret)
	require.NoError(t, err)
	assert.False(t, needsRehash)

	_, err = verifySecret(encoded, "wrong-secret")
	require.ErrorIs(t, err, errSecretMismatch)

	for _, malformed := range []string{
		hashPrefixSHA256 + "zz",
		hashPrefixSHA256 + strings.Repeat("ab", 16),
		"not-a-hash",
		"",
	} {
		_, err = verifySecret([]byte(malformed), secret)
		require.ErrorIs(t, err, errSecretHashMalformed, malformed)
	}
}

func TestVerifySecretLegacyFormats(t *testing.T) {
	t.Parallel()

	const secret = "s3cr3t"

	for name, encoded := range map[string][]byte{
		"bcrypt":   legacyBcryptHash(t, secret),
		"argon2id": legacyArgon2idHash(secret),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			needsRehash, err := verifySecret(encoded, secret)
			require.NoError(t, err)
			assert.True(t, needsRehash, "a legacy hash asks to be upgraded")

			needsRehash, err = verifySecret(encoded, "wrong")
			require.ErrorIs(t, err, errSecretMismatch)
			assert.False(t, needsRehash)
		})
	}

	// Parameters argon2 would panic on, or that would allocate without bound,
	// are refused before hashing.
	valid := string(legacyArgon2idHash(secret))
	for name, encoded := range map[string]string{
		"huge memory":  strings.Replace(valid, "m=8,", "m=4194304,", 1),
		"zero time":    strings.Replace(valid, "t=1,", "t=0,", 1),
		"zero threads": strings.Replace(valid, "p=1$", "p=0$", 1),
		"short key": valid[:strings.LastIndex(valid, "$")+1] +
			base64.RawStdEncoding.EncodeToString(make([]byte, 16)),
	} {
		_, err := verifySecret([]byte(encoded), secret)
		require.ErrorIs(t, err, errSecretHashMalformed, name)
	}
}

// TestVerifySecretConcurrent runs more concurrent legacy verifications than
// the limiter admits, asserting it releases correctly (no deadlock) and stays
// correct under contention. Run with -race.
func TestVerifySecretConcurrent(t *testing.T) {
	t.Parallel()

	hash := legacyArgon2idHash("s3cr3t")

	const n = 64

	var wg sync.WaitGroup

	errs := make([]error, n)

	for i := range n {
		wg.Go(func() {
			secret := "s3cr3t"
			if i%2 == 1 {
				secret = "wrong"
			}

			_, errs[i] = verifySecret(hash, secret)
		})
	}

	wg.Wait()

	for i, e := range errs {
		if i%2 == 0 {
			assert.NoError(t, e, "correct secret must verify")
		} else {
			assert.Error(t, e, "wrong secret must fail")
		}
	}
}

// credentialKind drives one stored credential kind through the legacy hash
// upgrade.
type credentialKind struct {
	name string
	// create stores a credential and returns the string its holder presents,
	// the row id and the part of that string that is hashed.
	create       func(t *testing.T, db *HSDatabase) (string, uint64, string)
	hashCol      hashColumn
	legacyHash   func(t *testing.T, secret string) []byte
	authenticate func(db *HSDatabase, presented string) error
}

func credentialKinds() []credentialKind {
	bcryptHash := legacyBcryptHash
	argon2Hash := func(_ *testing.T, secret string) []byte { return legacyArgon2idHash(secret) }
	lastSegment := func(s string) string { return s[strings.LastIndex(s, "-")+1:] }

	return []credentialKind{
		{
			name: "api key",
			create: func(t *testing.T, db *HSDatabase) (string, uint64, string) {
				t.Helper()

				keyStr, key, err := db.CreateAPIKey(nil)
				require.NoError(t, err)

				return keyStr, key.ID, lastSegment(keyStr)
			},
			hashCol:    apiKeyHash,
			legacyHash: bcryptHash,
			authenticate: func(db *HSDatabase, presented string) error {
				_, err := db.AuthenticateAPIKey(presented)

				return err
			},
		},
		{
			name: "legacy-format api key",
			create: func(t *testing.T, db *HSDatabase) (string, uint64, string) {
				t.Helper()

				var inserted idRow

				err := db.ex.query(
					table.APIKeys.INSERT(table.APIKeys.Prefix, table.APIKeys.Hash, table.APIKeys.CreatedAt).
						VALUES("abcdefg", jet.Blob(hashSecret("placeholder")), time.Now()).
						RETURNING(table.APIKeys.ID.AS("id_row.id")),
					&inserted,
				)
				require.NoError(t, err)

				secret := strings.Repeat("x", 32)

				return "abcdefg." + secret, inserted.ID, secret
			},
			hashCol:    apiKeyHash,
			legacyHash: bcryptHash,
			authenticate: func(db *HSDatabase, presented string) error {
				_, err := db.AuthenticateAPIKey(presented)

				return err
			},
		},
		{
			name: "pre-auth key",
			create: func(t *testing.T, db *HSDatabase) (string, uint64, string) {
				t.Helper()

				user := db.CreateUserForTest("legacy-hash")

				pak, err := db.CreatePreAuthKey(user.TypedID(), true, false, nil, nil)
				require.NoError(t, err)

				return pak.Key, pak.ID, lastSegment(pak.Key)
			},
			hashCol:    preAuthKeyHash,
			legacyHash: bcryptHash,
			authenticate: func(db *HSDatabase, presented string) error {
				_, err := db.GetPreAuthKey(presented)

				return err
			},
		},
		{
			name: "oauth client",
			create: func(t *testing.T, db *HSDatabase) (string, uint64, string) {
				t.Helper()

				secret, client, err := db.CreateOAuthClient([]string{"auth_keys"}, []string{"tag:ci"}, "", nil)
				require.NoError(t, err)

				return secret, client.ID, lastSegment(secret)
			},
			hashCol:    oauthClientHash,
			legacyHash: argon2Hash,
			authenticate: func(db *HSDatabase, presented string) error {
				_, err := db.AuthenticateOAuthClient(presented)

				return err
			},
		},
		{
			name: "oauth access token",
			create: func(t *testing.T, db *HSDatabase) (string, uint64, string) {
				t.Helper()

				_, client, err := db.CreateOAuthClient([]string{"auth_keys"}, []string{"tag:ci"}, "", nil)
				require.NoError(t, err)

				expiry := time.Now().Add(time.Hour)

				tokenStr, token, err := db.MintAccessToken(client.ClientID, client.Scopes, client.Tags, &expiry)
				require.NoError(t, err)

				return tokenStr, token.ID, lastSegment(tokenStr)
			},
			hashCol:    oauthAccessTokenHash,
			legacyHash: argon2Hash,
			authenticate: func(db *HSDatabase, presented string) error {
				_, err := db.AuthenticateAccessToken(presented)

				return err
			},
		},
	}
}

func setStoredHash(t *testing.T, db *HSDatabase, col hashColumn, id uint64, hash []byte) {
	t.Helper()

	_, err := db.ex.exec(col.table.UPDATE(col.hash).SET(jet.Blob(hash)).WHERE(col.id.EQ(jet.Uint64(id))))
	require.NoError(t, err)
}

func storedHash(t *testing.T, db *HSDatabase, col hashColumn, id uint64) []byte {
	t.Helper()

	var row struct {
		Hash []byte `alias:"stored.hash"`
	}

	err := db.ex.query(
		jet.SELECT(col.hash.AS("stored.hash")).FROM(col.table).WHERE(col.id.EQ(jet.Uint64(id))),
		&row,
	)
	require.NoError(t, err)

	return row.Hash
}

// TestLegacyCredentialHashesUpgradeOnUse stores each credential kind under
// the hash it had before SHA-256 and asserts it keeps authenticating, that
// the first successful use rewrites the hash to SHA-256, and that a wrong
// secret neither authenticates nor touches the stored hash.
func TestLegacyCredentialHashesUpgradeOnUse(t *testing.T) {
	t.Parallel()

	dbs := map[string]func(t *testing.T) *HSDatabase{
		"sqlite": func(t *testing.T) *HSDatabase {
			t.Helper()

			db, err := newSQLiteTestDB()
			require.NoError(t, err)

			return db
		},
		"postgres": newPostgresTestDB,
	}

	for dbName, newDB := range dbs {
		t.Run(dbName, func(t *testing.T) {
			t.Parallel()

			db := newDB(t)

			for _, kind := range credentialKinds() {
				t.Run(kind.name, func(t *testing.T) {
					presented, id, secret := kind.create(t, db)

					legacy := kind.legacyHash(t, secret)
					setStoredHash(t, db, kind.hashCol, id, legacy)

					wrong := presented[:len(presented)-len(secret)] + strings.Repeat("0", len(secret))
					require.Error(t, kind.authenticate(db, wrong))
					assert.Equal(t, legacy, storedHash(t, db, kind.hashCol, id),
						"a failed attempt must not touch the hash")

					require.NoError(t, kind.authenticate(db, presented))
					assert.Equal(t, hashSecret(secret), storedHash(t, db, kind.hashCol, id),
						"a verified legacy hash is rewritten as SHA-256")

					require.NoError(t, kind.authenticate(db, presented), "the upgraded hash verifies")
				})
			}
		})
	}
}

// TestHashUpgradeKeepsRotatedAPIKey races a legacy-hash upgrade against a
// rotation: an authentication that verified the old secret before the key
// was rotated must not write that secret's digest over the new hash.
func TestHashUpgradeKeepsRotatedAPIKey(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	oldKey, key, err := db.CreateAPIKey(nil)
	require.NoError(t, err)

	oldSecret := oldKey[strings.LastIndex(oldKey, "-")+1:]
	legacy := legacyBcryptHash(t, oldSecret)
	setStoredHash(t, db, apiKeyHash, key.ID, legacy)

	newKey, err := db.RotateAPIKey(key, nil)
	require.NoError(t, err)

	upgradeHash(db, apiKeyHash, key.ID, legacy, oldSecret)

	_, err = db.AuthenticateAPIKey(newKey)
	require.NoError(t, err, "the rotated key must keep working")
	assert.NotEqual(t, hashSecret(oldSecret), storedHash(t, db, apiKeyHash, key.ID))
}
