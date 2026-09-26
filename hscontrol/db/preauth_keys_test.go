package db

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePreAuthKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		test func(*testing.T, *HSDatabase)
	}{
		{
			name: "error_invalid_user_id",
			test: func(t *testing.T, db *HSDatabase) {
				t.Helper()

				_, err := db.CreatePreAuthKey(new(types.UserID(12345)), true, false, nil, nil)
				assert.Error(t, err)
			},
		},
		{
			name: "success_create_and_list",
			test: func(t *testing.T, db *HSDatabase) {
				t.Helper()

				user, err := db.CreateUser(types.User{Name: "test"})
				require.NoError(t, err)

				key, err := db.CreatePreAuthKey(user.TypedID(), true, false, nil, nil)
				require.NoError(t, err)
				assert.NotEmpty(t, key.Key)

				// List keys for the user
				keys, err := db.ListPreAuthKeys()
				require.NoError(t, err)
				assert.Len(t, keys, 1)

				// Verify User association is populated
				assert.Equal(t, user.ID, keys[0].User.ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := newSQLiteTestDB()
			require.NoError(t, err)

			tt.test(t, db)
		})
	}
}

func TestPreAuthKeyACLTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		test func(*testing.T, *HSDatabase)
	}{
		{
			name: "reject_malformed_tags",
			test: func(t *testing.T, db *HSDatabase) {
				t.Helper()

				user, err := db.CreateUser(types.User{Name: "test-tags-1"})
				require.NoError(t, err)

				_, err = db.CreatePreAuthKey(user.TypedID(), false, false, nil, []string{"badtag"})
				assert.Error(t, err)
			},
		},
		{
			name: "deduplicate_and_sort_tags",
			test: func(t *testing.T, db *HSDatabase) {
				t.Helper()

				user, err := db.CreateUser(types.User{Name: "test-tags-2"})
				require.NoError(t, err)

				expectedTags := []string{"tag:test1", "tag:test2"}
				tagsWithDuplicate := []string{"tag:test1", "tag:test2", "tag:test2"}

				_, err = db.CreatePreAuthKey(user.TypedID(), false, false, nil, tagsWithDuplicate)
				require.NoError(t, err)

				listedPaks, err := db.ListPreAuthKeys()
				require.NoError(t, err)
				require.Len(t, listedPaks, 1)

				gotTags := slices.Clone(listedPaks[0].Tags)
				slices.Sort(gotTags)
				assert.Equal(t, expectedTags, gotTags)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := newSQLiteTestDB()
			require.NoError(t, err)

			tt.test(t, db)
		})
	}
}

func TestCannotDeleteAssignedPreAuthKey(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)
	user, err := db.CreateUser(types.User{Name: "test8"})
	require.NoError(t, err)

	key, err := db.CreatePreAuthKey(user.TypedID(), false, false, nil, []string{"tag:good"})
	require.NoError(t, err)

	node := types.Node{
		ID:             0,
		Hostname:       "testest",
		UserID:         &user.ID,
		RegisterMethod: util.RegisterMethodAuthKey,
		AuthKeyID:      new(key.ID),
	}
	require.NoError(t, CreateNode(db, &node))

	_, err = db.DB.ExecContext(t.Context(), "DELETE FROM pre_auth_keys WHERE id = $1", key.ID)
	require.ErrorContains(t, err, "FOREIGN KEY constraint failed")
}

func TestPreAuthKeyAuthentication(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	user := db.CreateUserForTest("test-user")

	const legacyKey = "abc123def456ghi789jkl012mno345pqr678stu901vwx234yz"

	tests := []struct {
		name            string
		setupKey        func() string // Returns key string to test
		wantFindErr     bool          // Error when finding the key
		wantValidateErr bool          // Error when validating the key
		validateResult  func(*testing.T, *types.PreAuthKey)
	}{
		{
			name: "legacy_key_plaintext",
			setupKey: func() string {
				insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{key: legacyKey, reusable: true})
				hashLegacyPreAuthKeysForTest(t, db)

				return legacyKey
			},
			wantFindErr:     false,
			wantValidateErr: false,
			validateResult: func(t *testing.T, pak *types.PreAuthKey) {
				t.Helper()

				assert.Equal(t, user.ID, *pak.UserID)
				assert.Equal(t, legacyAuthKeyIdentifier(legacyKey), pak.Prefix)
				assert.Equal(t, hashSecret(legacyKey), pak.Hash)
			},
		},
		{
			name: "legacy_key_wrong_secret",
			setupKey: func() string {
				// A row under the key's derived prefix holding the hash of
				// another secret: the prefix finds it, the hash rejects it.
				key := "wrongsecretlegacykey000000000000000000000000000"
				id := insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{key: "", reusable: true})
				_, err := db.DB.ExecContext(
					t.Context(), `UPDATE pre_auth_keys SET prefix = $1, hash = $2 WHERE id = $3`,
					legacyAuthKeyIdentifier(key), hashSecret("another secret"), id,
				)
				require.NoError(t, err)

				return key
			},
			wantFindErr: true,
		},
		{
			name: "legacy_key_never_stored",
			setupKey: func() string {
				return "neverstoredlegacykey00000000000000000000000000"
			},
			wantFindErr: true,
		},
		{
			name: "legacy_prefix_is_not_a_key",
			setupKey: func() string {
				key := "prefixonlylegacykey000000000000000000000000000"
				insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{key: key, reusable: true})
				hashLegacyPreAuthKeysForTest(t, db)

				return legacyAuthKeyIdentifier(key)
			},
			wantFindErr: true,
		},
		{
			name: "revoked_legacy_key",
			setupKey: func() string {
				key := "revokedlegacykey00000000000000000000000000000"
				insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{
					key: key, reusable: true, revoked: new(time.Now()),
				})
				hashLegacyPreAuthKeysForTest(t, db)

				return key
			},
			wantValidateErr: true,
		},
		{
			name: "new_key_hashed",
			setupKey: func() string {
				// Create new key via API
				keyStr, err := db.CreatePreAuthKey(
					user.TypedID(),
					true, false, nil, []string{"tag:test"},
				)
				require.NoError(t, err)

				return keyStr.Key
			},
			wantFindErr:     false,
			wantValidateErr: false,
			validateResult: func(t *testing.T, pak *types.PreAuthKey) {
				t.Helper()

				assert.Equal(t, user.ID, *pak.UserID)
				assert.NotEmpty(t, pak.Prefix) // New keys have Prefix
				assert.NotNil(t, pak.Hash)     // New keys have Hash
				assert.Len(t, pak.Prefix, 12)  // Prefix is 12 chars
			},
		},
		{
			name: "new_key_format_validation",
			setupKey: func() string {
				keyStr, err := db.CreatePreAuthKey(
					user.TypedID(),
					true, false, nil, nil,
				)
				require.NoError(t, err)

				// Verify format: hskey-auth-{12-char-prefix}-{64-char-hash}
				// Use fixed-length parsing since prefix/hash can contain dashes (base64 URL-safe)
				assert.True(t, strings.HasPrefix(keyStr.Key, "hskey-auth-"))

				// Extract prefix and hash using fixed-length parsing like the real code does
				_, prefixAndHash, found := strings.Cut(keyStr.Key, "hskey-auth-")
				assert.True(t, found)
				assert.GreaterOrEqual(t, len(prefixAndHash), 12+1+64) // prefix + '-' + hash minimum

				prefix := prefixAndHash[:12]
				assert.Len(t, prefix, 12)                     // Prefix is 12 chars
				assert.Equal(t, byte('-'), prefixAndHash[12]) // Separator
				hash := prefixAndHash[13:]
				assert.Len(t, hash, 64) // Hash is 64 chars

				return keyStr.Key
			},
			wantFindErr:     false,
			wantValidateErr: false,
		},
		{
			name: "wrong_secret",
			setupKey: func() string {
				// Create valid key
				key, err := db.CreatePreAuthKey(
					user.TypedID(),
					true, false, nil, nil,
				)
				require.NoError(t, err)

				keyStr := key.Key

				// Return key with tampered hash using fixed-length parsing
				_, prefixAndHash, _ := strings.Cut(keyStr, "hskey-auth-")
				prefix := prefixAndHash[:12]

				wrongHash := "wrong_hash_here_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

				return "hskey-auth-" + prefix + "-" + wrongHash
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "empty_key",
			setupKey: func() string {
				return ""
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "key_too_short",
			setupKey: func() string {
				return "hskey-auth-short"
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "missing_separator",
			setupKey: func() string {
				return "hskey-auth-ABCDEFGHIJKLabcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "hash_too_short",
			setupKey: func() string {
				return "hskey-auth-ABCDEFGHIJKL-short"
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "prefix_with_invalid_chars",
			setupKey: func() string {
				return "hskey-auth-ABC$EF@HIJKL-" + strings.Repeat("a", 64)
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "hash_with_invalid_chars",
			setupKey: func() string {
				return "hskey-auth-ABCDEFGHIJKL-" + "invalid$chars" + strings.Repeat("a", 54)
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "prefix_not_found_in_db",
			setupKey: func() string {
				// Create a validly formatted key but with a prefix that doesn't exist
				return "hskey-auth-NotInDB12345-" + strings.Repeat("a", 64)
			},
			wantFindErr:     true,
			wantValidateErr: false,
		},
		{
			name: "expired_legacy_key",
			setupKey: func() string {
				legacyKey := "expired_legacy_key_123456789012345678901234"
				insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{
					key: legacyKey, reusable: true, expiration: new(time.Now().Add(-time.Hour)),
				})
				hashLegacyPreAuthKeysForTest(t, db)

				return legacyKey
			},
			wantFindErr:     false,
			wantValidateErr: true,
		},
		{
			name: "used_single_use_legacy_key",
			setupKey: func() string {
				legacyKey := "used_legacy_key_123456789012345678901234567"
				insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{key: legacyKey, used: true})
				hashLegacyPreAuthKeysForTest(t, db)

				return legacyKey
			},
			wantFindErr:     false,
			wantValidateErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			keyStr := tt.setupKey()

			pak, err := db.GetPreAuthKey(keyStr)

			if tt.wantFindErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, pak)

			// Check validation if needed
			if tt.wantValidateErr {
				err := pak.Validate()
				assert.Error(t, err)

				return
			}

			if tt.validateResult != nil {
				tt.validateResult(t, pak)
			}
		})
	}
}

func TestMultipleLegacyKeysAllowed(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	user, err := db.CreateUser(types.User{Name: "test-legacy"})
	require.NoError(t, err)

	// Create multiple legacy keys by directly inserting with empty prefix
	// This simulates the migration scenario where existing databases have multiple
	// plaintext keys without prefix/hash fields
	now := time.Now()

	for i := range 5 {
		legacyKey := fmt.Sprintf("legacy_key_%d_%s", i, strings.Repeat("x", 40))

		_, execErr := db.DB.ExecContext(t.Context(), `
			INSERT INTO pre_auth_keys (key, prefix, hash, user_id, reusable, ephemeral, used, created_at)
			VALUES ($1, '', NULL, $2, $3, $4, $5, $6)
		`, legacyKey, user.ID, true, false, false, now)
		require.NoError(t, execErr, "should allow multiple legacy keys with empty prefix")
	}

	// Verify all legacy keys can be retrieved
	allPaks, err := ListPreAuthKeys(db)
	require.NoError(t, err)

	var legacyKeys []types.PreAuthKey

	for _, pak := range allPaks {
		if pak.Prefix == "" {
			legacyKeys = append(legacyKeys, pak)
		}
	}

	assert.Len(t, legacyKeys, 5, "should have created 5 legacy keys")

	// Now create new hashed keys - these should have unique prefixes
	key1, err := db.CreatePreAuthKey(user.TypedID(), true, false, nil, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, key1.Key)

	key2, err := db.CreatePreAuthKey(user.TypedID(), true, false, nil, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, key2.Key)

	// Verify the new keys have different prefixes
	pak1, err := db.GetPreAuthKey(key1.Key)
	require.NoError(t, err)
	assert.NotEmpty(t, pak1.Prefix)

	pak2, err := db.GetPreAuthKey(key2.Key)
	require.NoError(t, err)
	assert.NotEmpty(t, pak2.Prefix)

	assert.NotEqual(t, pak1.Prefix, pak2.Prefix, "new keys should have unique prefixes")

	// Verify we cannot manually insert duplicate non-empty prefixes
	duplicatePrefix := "test_prefix1"
	hash1 := []byte("hash1")
	hash2 := []byte("hash2")

	// First insert should succeed
	_, err = db.DB.ExecContext(t.Context(), `
		INSERT INTO pre_auth_keys (key, prefix, hash, user_id, reusable, ephemeral, used, created_at)
		VALUES ('', $1, $2, $3, $4, $5, $6, $7)
	`, duplicatePrefix, hash1, user.ID, true, false, false, now)
	require.NoError(t, err, "first key with prefix should succeed")

	// Second insert with same prefix should fail
	_, err = db.DB.ExecContext(t.Context(), `
		INSERT INTO pre_auth_keys (key, prefix, hash, user_id, reusable, ephemeral, used, created_at)
		VALUES ('', $1, $2, $3, $4, $5, $6, $7)
	`, duplicatePrefix, hash2, user.ID, true, false, false, now)
	require.Error(t, err, "duplicate non-empty prefix should be rejected")
	assert.Contains(t, err.Error(), "UNIQUE constraint failed", "should fail with UNIQUE constraint error")
}

// TestUsePreAuthKeyAtomicCAS verifies that UsePreAuthKey is an atomic
// compare-and-set: a second call against an already-used key reports
// PAKError("authkey already used") rather than silently succeeding.
func TestUsePreAuthKeyAtomicCAS(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	user, err := db.CreateUser(types.User{Name: "atomic-cas"})
	require.NoError(t, err)

	pakNew, err := db.CreatePreAuthKey(user.TypedID(), false /* reusable */, false, nil, nil)
	require.NoError(t, err)

	pak, err := db.GetPreAuthKey(pakNew.Key)
	require.NoError(t, err)
	require.False(t, pak.Reusable, "test sanity: key must be single-use")

	// First Use should commit cleanly.
	err = db.Write(func(tx *Tx) error {
		return UsePreAuthKey(tx, pak)
	})
	require.NoError(t, err, "first UsePreAuthKey should succeed")

	// Reload from disk to drop the in-memory Used=true the first call
	// set on the struct, simulating a second concurrent transaction
	// that loaded the same row before the first one committed.
	stale, err := db.GetPreAuthKey(pakNew.Key)
	require.NoError(t, err)

	stale.Used = false

	err = db.Write(func(tx *Tx) error {
		return UsePreAuthKey(tx, stale)
	})
	require.Error(t, err, "second UsePreAuthKey on the same single-use key must fail")

	var pakErr types.PAKError
	require.ErrorAs(t, err, &pakErr,
		"second UsePreAuthKey error must be a PAKError, got: %v", err)
	assert.Equal(t, "authkey already used", pakErr.Error())
}

// TestGetPreAuthKeyUnknownMapsToRecordNotFound ensures an unknown (or deleted)
// pre-auth key resolves to a record-not-found error, which the registration
// handler maps to a 401 rather than a raw server error.
func TestGetPreAuthKeyUnknownMapsToRecordNotFound(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	_, err = db.GetPreAuthKey("nonexistent-key")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound,
		"unknown pre-auth key must map to record-not-found (handled as 401)")
}

// TestDestroyRevokedPreAuthKeysKeepsKeysBackingNodes asserts the collector
// reaps a revoked key only once no node references it: an ephemeral node's
// ephemerality lives on its key, so reaping the key would make the node
// permanent after the next restart.
func TestDestroyRevokedPreAuthKeysKeepsKeysBackingNodes(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	user := db.CreateUserForTest("revoked-reaper")

	backing, err := db.CreatePreAuthKey(user.TypedID(), false, true, nil, nil)
	require.NoError(t, err)

	unused, err := db.CreatePreAuthKey(user.TypedID(), false, true, nil, nil)
	require.NoError(t, err)

	node := types.Node{
		Hostname:       "ephemeral",
		UserID:         &user.ID,
		RegisterMethod: util.RegisterMethodAuthKey,
		AuthKeyID:      new(backing.ID),
	}
	require.NoError(t, CreateNode(db, &node))

	require.NoError(t, db.RevokePreAuthKey(backing.ID))
	require.NoError(t, db.RevokePreAuthKey(unused.ID))

	cutoff := time.Now().Add(time.Minute)

	reaped, err := db.DestroyRevokedPreAuthKeysBefore(cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, reaped)

	_, err = db.GetPreAuthKeyByID(unused.ID)
	require.ErrorIs(t, err, ErrNotFound)

	kept, err := GetNodeByID(db, node.ID)
	require.NoError(t, err)
	require.NotNil(t, kept.AuthKeyID, "the node keeps its key")
	assert.True(t, kept.IsEphemeral())

	require.NoError(t, db.DeleteNode(&node))

	reaped, err = db.DestroyRevokedPreAuthKeysBefore(cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, reaped, "the key goes once its node is gone")

	_, err = db.GetPreAuthKeyByID(backing.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

// plaintextPreAuthKey is a pre-auth key as headscale stored it before 0.28:
// the key itself in the key column, no prefix and no hash.
type plaintextPreAuthKey struct {
	key        string
	reusable   bool
	used       bool
	expiration *time.Time
	revoked    *time.Time
}

func insertPlaintextPreAuthKey(t *testing.T, db *HSDatabase, userID uint, k plaintextPreAuthKey) uint64 {
	t.Helper()

	var id uint64

	err := db.DB.QueryRowContext(t.Context(), `INSERT INTO pre_auth_keys
  (key, user_id, reusable, ephemeral, used, created_at, expiration, revoked)
VALUES ($1, $2, $3, false, $4, $5, $6, $7) RETURNING id`,
		k.key, userID, k.reusable, k.used, time.Now(), k.expiration, k.revoked,
	).Scan(&id)
	require.NoError(t, err)

	return id
}

// hashLegacyPreAuthKeysForTest runs the migration that hashes plaintext
// pre-auth keys over what the test inserted after the database was created.
func hashLegacyPreAuthKeysForTest(t *testing.T, db *HSDatabase) {
	t.Helper()

	require.NoError(t, db.Write(migrateHashLegacyPreAuthKeys))
}

// TestHashLegacyPreAuthKeysMigration upgrades a database holding plaintext
// pre-auth keys from before headscale 0.28: each keeps authenticating with
// the original string and keeps its state, and no plaintext is left.
func TestHashLegacyPreAuthKeysMigration(t *testing.T) {
	t.Parallel()

	const (
		migrationID = "202609261000-hash-legacy-pre-auth-keys"
		activeKey   = "0123456789abcdef0123456789abcdef0123456789abcdef"
		revokedKey  = "1123456789abcdef0123456789abcdef0123456789abcdef"
		expiredKey  = "2123456789abcdef0123456789abcdef0123456789abcdef"
		usedKey     = "3123456789abcdef0123456789abcdef0123456789abcdef"
	)

	forEachDialect(t, func(t *testing.T, db *HSDatabase) {
		// Back to before the migration: out of the history, and the
		// plaintext column indexed again.
		_, err := db.DB.ExecContext(t.Context(), `DELETE FROM migrations WHERE id = $1`, migrationID)
		require.NoError(t, err)
		_, err = db.DB.ExecContext(t.Context(), `CREATE INDEX idx_pre_auth_keys_key ON pre_auth_keys(key)`)
		require.NoError(t, err)

		user := db.CreateUserForTest("legacy")

		current, err := db.CreatePreAuthKey(user.TypedID(), true, false, nil, nil)
		require.NoError(t, err)

		active := insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{key: activeKey, reusable: true})
		revoked := insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{
			key: revokedKey, reusable: true, revoked: new(time.Now()),
		})
		expired := insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{
			key: expiredKey, reusable: true, expiration: new(time.Now().Add(-time.Hour)),
		})
		used := insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{key: usedKey, used: true})
		// A later row holding the same key never authenticated, since the
		// plaintext lookup returned the lowest id.
		duplicate := insertPlaintextPreAuthKey(t, db, user.ID, plaintextPreAuthKey{key: activeKey, reusable: true})

		require.NoError(t, db.runMigrations(migrations(db.cfg)))

		applied, err := db.appliedMigrations()
		require.NoError(t, err)
		assert.Contains(t, applied, migrationID)

		var plaintext int

		err = db.DB.QueryRowContext(t.Context(),
			`SELECT count(*) FROM pre_auth_keys WHERE key IS NOT NULL AND key != ''`).Scan(&plaintext)
		require.NoError(t, err)
		assert.Zero(t, plaintext, "no key may be left in plaintext")

		_, err = db.DB.ExecContext(t.Context(), `DROP INDEX idx_pre_auth_keys_key`)
		require.Error(t, err, "the index on the plaintext column is dropped")

		pak, err := db.GetPreAuthKey(activeKey)
		require.NoError(t, err)
		assert.Equal(t, active, pak.ID)
		assert.Equal(t, legacyAuthKeyIdentifier(activeKey), pak.Prefix)
		assert.Equal(t, hashSecret(activeKey), pak.Hash)
		require.NoError(t, pak.Validate())
		assert.Equal(t, user.ID, pak.User.ID)

		for key, want := range map[string]struct {
			id  uint64
			err error
		}{
			revokedKey: {revoked, types.PAKError("authkey revoked")},
			expiredKey: {expired, types.PAKError("authkey expired")},
			usedKey:    {used, types.PAKError("authkey already used")},
		} {
			legacy, findErr := db.GetPreAuthKey(key)
			require.NoError(t, findErr)
			assert.Equal(t, want.id, legacy.ID)
			assert.Equal(t, want.err, legacy.Validate())
		}

		dup, err := db.GetPreAuthKeyByID(duplicate)
		require.NoError(t, err)
		assert.Empty(t, dup.Prefix)
		assert.Empty(t, dup.Hash)
		assert.NotNil(t, dup.Revoked, "the unreachable copy is revoked")

		pak, err = db.GetPreAuthKey(current.Key)
		require.NoError(t, err, "a current key is untouched")
		assert.Equal(t, current.ID, pak.ID)

		_, err = db.GetPreAuthKey(activeKey[:len(activeKey)-1] + "0")
		require.ErrorIs(t, err, ErrPreAuthKeyNotFound)

		_, err = db.GetPreAuthKey(legacyAuthKeyIdentifier(activeKey))
		require.ErrorIs(t, err, ErrPreAuthKeyNotFound, "the stored prefix is not a key")
	})
}
