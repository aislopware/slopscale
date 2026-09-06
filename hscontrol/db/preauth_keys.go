package db

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
	"golang.org/x/crypto/bcrypt"
	"tailscale.com/util/rands"
	"tailscale.com/util/set"
)

var (
	// ErrPreAuthKeyNotFound wraps [ErrNotFound] so an unknown or deleted
	// key is treated as a missing record by callers, which the registration
	// handler maps to a 401 rather than a raw server error.
	ErrPreAuthKeyNotFound          = fmt.Errorf("auth-key not found: %w", ErrNotFound)
	ErrPreAuthKeyExpired           = errors.New("auth-key expired")
	ErrSingleUseAuthKeyHasBeenUsed = errors.New("auth-key has already been used")
	ErrUserMismatch                = errors.New("user mismatch")
	ErrPreAuthKeyACLTagInvalid     = errors.New("auth-key tag is invalid")
)

// validateACLTags deduplicates, sorts, and checks that every tag carries the
// "tag:" prefix. Shared by the pre-auth-key and OAuth credential paths so both
// enforce the same tag shape.
func validateACLTags(tags []string) ([]string, error) {
	tags = set.SetOf(tags).Slice()
	slices.Sort(tags)

	for _, tag := range tags {
		if !strings.HasPrefix(tag, "tag:") {
			return nil, fmt.Errorf(
				"%w: '%s' did not begin with 'tag:'",
				ErrPreAuthKeyACLTagInvalid,
				tag,
			)
		}
	}

	return tags, nil
}

func (hsdb *HSDatabase) CreatePreAuthKey(
	uid *types.UserID,
	reusable bool,
	ephemeral bool,
	expiration *time.Time,
	aclTags []string,
) (*types.PreAuthKeyNew, error) {
	return Write(hsdb, func(tx *Tx) (*types.PreAuthKeyNew, error) {
		return CreatePreAuthKey(tx, uid, reusable, ephemeral, expiration, aclTags)
	})
}

// selectPreAuthKeys is the base query for pre-auth keys with their user
// joined in, ordered by id.
func selectPreAuthKeys() jet.SelectStatement {
	return jet.SELECT(table.PreAuthKeys.AllColumns, table.Users.AllColumns).
		FROM(table.PreAuthKeys.LEFT_JOIN(
			table.Users,
			table.PreAuthKeys.UserID.EQ(table.Users.ID).AND(table.Users.DeletedAt.IS_NULL()),
		)).
		ORDER_BY(table.PreAuthKeys.ID.ASC())
}

func queryPreAuthKeys(q Querier, stmt jet.SelectStatement) ([]types.PreAuthKey, error) {
	var records []preAuthKeyRecord

	err := q.executor().query(stmt, &records)
	if err != nil {
		return nil, err
	}

	return preAuthKeyRecordsToKeys(records)
}

func queryPreAuthKey(q Querier, where jet.BoolExpression) (*types.PreAuthKey, error) {
	var record preAuthKeyRecord

	err := q.executor().query(selectPreAuthKeys().WHERE(where).LIMIT(1), &record)
	if err != nil {
		return nil, err
	}

	return record.preAuthKey()
}

// Pre-auth key lookups on the registration path, rendered once; see
// [fixedSQL].
var (
	preAuthKeyByKey = newFixedSQL(func() statement {
		return selectPreAuthKeys().WHERE(table.PreAuthKeys.Key.EQ(jet.String(""))).LIMIT(1)
	})
	preAuthKeyByPrefix = newFixedSQL(func() statement {
		return selectPreAuthKeys().WHERE(table.PreAuthKeys.Prefix.EQ(jet.String(""))).LIMIT(1)
	})
)

func fixedPreAuthKey(q Querier, stmt *fixedSQL, args ...any) (*types.PreAuthKey, error) {
	var record preAuthKeyRecord

	err := q.executor().queryFixed(stmt, &record, args...)
	if err != nil {
		return nil, err
	}

	return record.preAuthKey()
}

// insertPreAuthKey inserts key and sets its ID.
func insertPreAuthKey(q Querier, key *types.PreAuthKey) error {
	row, err := preAuthKeyRowFrom(key)
	if err != nil {
		return err
	}

	columns := table.PreAuthKeys.MutableColumns
	if key.ID != 0 {
		columns = table.PreAuthKeys.AllColumns
	}

	var inserted idRow

	err = q.executor().query(
		table.PreAuthKeys.INSERT(columns).MODEL(row).RETURNING(table.PreAuthKeys.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return err
	}

	key.ID = inserted.ID

	return nil
}

// updatePreAuthKeyColumn sets a single column on the keys matched by where
// and returns how many rows changed.
func updatePreAuthKeyColumn(q Querier, where jet.BoolExpression, column jet.Column, value any) (int64, error) {
	return q.executor().exec(table.PreAuthKeys.UPDATE(column).SET(value).WHERE(where))
}

const (
	authKeyPrefix       = "hskey-auth-"
	authKeyPrefixLength = 12
	authKeyLength       = 64
)

// CreatePreAuthKey creates a new [types.PreAuthKey] in a user, and returns it.
// The uid parameter can be nil for system-created tagged keys.
// For tagged keys, uid tracks "created by" (who created the key).
// For user-owned keys, uid tracks the node owner.
func CreatePreAuthKey(
	q Querier,
	uid *types.UserID,
	reusable bool,
	ephemeral bool,
	expiration *time.Time,
	aclTags []string,
) (*types.PreAuthKeyNew, error) {
	// Validate: must be tagged OR user-owned, not neither
	if uid == nil && len(aclTags) == 0 {
		return nil, ErrPreAuthKeyNotTaggedOrOwned
	}

	var (
		user   *types.User
		userID *uint
	)

	if uid != nil {
		var err error

		user, err = GetUserByID(q, *uid)
		if err != nil {
			return nil, err
		}

		userID = &user.ID
	}

	aclTags, err := validateACLTags(aclTags)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	prefix := rands.HexString(authKeyPrefixLength)

	toBeHashed := rands.HexString(authKeyLength)

	keyStr := authKeyPrefix + prefix + "-" + toBeHashed

	hash, err := bcrypt.GenerateFromPassword([]byte(toBeHashed), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing pre-auth key: %w", err)
	}

	key := types.PreAuthKey{
		UserID:     userID, // nil for system-created keys, or "created by" for tagged keys
		User:       user,   // nil for system-created keys
		Reusable:   reusable,
		Ephemeral:  ephemeral,
		CreatedAt:  &now,
		Expiration: expiration,
		Tags:       aclTags, // empty for user-owned keys
		Prefix:     prefix,  // Store prefix
		Hash:       hash,    // Store hash
	}

	err = insertPreAuthKey(q, &key)
	if err != nil {
		return nil, fmt.Errorf("creating key in database: %w", err)
	}

	return &types.PreAuthKeyNew{
		ID:         key.ID,
		Key:        keyStr,
		Reusable:   key.Reusable,
		Ephemeral:  key.Ephemeral,
		Tags:       key.Tags,
		Expiration: key.Expiration,
		CreatedAt:  key.CreatedAt,
		User:       key.User,
	}, nil
}

// SetPreAuthKeyDescription sets the free-text description on a pre-auth key.
// The v2 keys API sets it after creation rather than threading it through the
// many-armed CreatePreAuthKey signature shared by every other caller.
func (hsdb *HSDatabase) SetPreAuthKeyDescription(id uint64, description string) error {
	_, err := updatePreAuthKeyColumn(
		hsdb, table.PreAuthKeys.ID.EQ(jet.Uint64(id)), table.PreAuthKeys.Description, description,
	)

	return err
}

func (hsdb *HSDatabase) ListPreAuthKeys() ([]types.PreAuthKey, error) {
	return Read(hsdb, func(rx *Tx) ([]types.PreAuthKey, error) {
		return ListPreAuthKeys(rx)
	})
}

// ListPreAuthKeys returns all [types.PreAuthKey] values in the database.
func ListPreAuthKeys(q Querier) ([]types.PreAuthKey, error) {
	return queryPreAuthKeys(q, selectPreAuthKeys())
}

// ListPreAuthKeysByUser returns all [types.PreAuthKey] values belonging to a specific user.
func ListPreAuthKeysByUser(q Querier, uid types.UserID) ([]types.PreAuthKey, error) {
	return queryPreAuthKeys(q, selectPreAuthKeys().WHERE(table.PreAuthKeys.UserID.EQ(jet.Uint64(uint64(uid)))))
}

var (
	ErrPreAuthKeyFailedToParse    = errors.New("failed to parse auth-key")
	ErrPreAuthKeyNotTaggedOrOwned = errors.New("auth-key must be either tagged or owned by user")
)

func findAuthKey(q Querier, keyStr string) (*types.PreAuthKey, error) {
	// Validate input is not empty
	if keyStr == "" {
		return nil, ErrPreAuthKeyFailedToParse
	}

	_, prefixAndHash, found := strings.Cut(keyStr, authKeyPrefix)

	if !found {
		// Legacy format (plaintext) - backwards compatibility
		pak, err := fixedPreAuthKey(q, preAuthKeyByKey, keyStr, limitOne)
		if err != nil {
			return nil, ErrPreAuthKeyNotFound
		}

		return pak, nil
	}

	// New format: hskey-auth-{12-char-prefix}-{64-char-hash}
	prefix, hash, err := parsePrefixedKey(
		prefixAndHash,
		authKeyPrefixLength,
		authKeyLength,
		ErrPreAuthKeyFailedToParse,
	)
	if err != nil {
		return nil, err
	}

	// Look up key by prefix
	pak, err := fixedPreAuthKey(q, preAuthKeyByPrefix, prefix, limitOne)
	if err != nil {
		return nil, ErrPreAuthKeyNotFound
	}

	// Verify hash matches
	err = bcrypt.CompareHashAndPassword(pak.Hash, []byte(hash))
	if err != nil {
		return nil, fmt.Errorf("invalid auth key: %w", err)
	}

	return pak, nil
}

// parsePrefixedKey splits the prefix-and-secret portion of a new-format key
// (the part after the "hskey-*-" prefix) into its fixed-length prefix and
// secret components, validating the length, separator position, and that both
// components are base64 URL-safe. Fixed-length parsing is used instead of
// separator-based to handle dashes in base64 URL-safe characters.
func parsePrefixedKey(
	prefixAndSecret string,
	//nolint:unparam // kept explicit though every credential kind uses a 12-char prefix and 64-char secret today
	prefixLen, secretLen int,
	parseErr error,
) (string, string, error) {
	expectedMinLength := prefixLen + 1 + secretLen
	if len(prefixAndSecret) < expectedMinLength {
		return "", "", fmt.Errorf(
			"%w: key too short, expected at least %d chars after prefix, got %d",
			parseErr,
			expectedMinLength,
			len(prefixAndSecret),
		)
	}

	prefix := prefixAndSecret[:prefixLen]

	// Validate separator at expected position
	if prefixAndSecret[prefixLen] != '-' {
		return "", "", fmt.Errorf(
			"%w: expected separator '-' at position %d, got '%c'",
			parseErr,
			prefixLen,
			prefixAndSecret[prefixLen],
		)
	}

	secret := prefixAndSecret[prefixLen+1:]

	// Validate secret length
	if len(secret) != secretLen {
		return "", "", fmt.Errorf(
			"%w: secret length mismatch, expected %d chars, got %d",
			parseErr,
			secretLen,
			len(secret),
		)
	}

	// Validate prefix contains only base64 URL-safe characters
	if !isValidBase64URLSafe(prefix) {
		return "", "", fmt.Errorf(
			"%w: prefix contains invalid characters (expected base64 URL-safe: A-Za-z0-9_-)",
			parseErr,
		)
	}

	// Validate secret contains only base64 URL-safe characters
	if !isValidBase64URLSafe(secret) {
		return "", "", fmt.Errorf(
			"%w: secret contains invalid characters (expected base64 URL-safe: A-Za-z0-9_-)",
			parseErr,
		)
	}

	return prefix, secret, nil
}

// isValidBase64URLSafe reports whether s contains only base64 URL-safe
// characters (A-Za-z0-9-_). Key material is now generated as hex, a subset of
// this alphabet, so this accepts both current hex keys and any legacy keys
// still stored in the database.
func isValidBase64URLSafe(s string) bool {
	return !strings.ContainsFunc(s, func(c rune) bool {
		return (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_'
	})
}

func (hsdb *HSDatabase) GetPreAuthKey(key string) (*types.PreAuthKey, error) {
	return GetPreAuthKey(hsdb, key)
}

// GetPreAuthKey returns a [types.PreAuthKey] for a given key. The caller is responsible
// for checking if the key is usable (expired or used).
func GetPreAuthKey(q Querier, key string) (*types.PreAuthKey, error) {
	return findAuthKey(q, key)
}

// GetPreAuthKeyByID returns a [types.PreAuthKey] by its primary key, with the
// owning user preloaded.
func (hsdb *HSDatabase) GetPreAuthKeyByID(id uint64) (*types.PreAuthKey, error) {
	return queryPreAuthKey(hsdb, table.PreAuthKeys.ID.EQ(jet.Uint64(id)))
}

// DestroyPreAuthKey destroys a preauthkey. Returns error if the [types.PreAuthKey]
// does not exist. This also clears the auth_key_id on any nodes that reference
// this key.
func DestroyPreAuthKey(q Querier, id uint64) error {
	// First, clear the foreign key reference on any nodes using this key
	_, err := q.executor().exec(
		table.Nodes.UPDATE(table.Nodes.AuthKeyID).SET(jet.NULL).
			WHERE(table.Nodes.AuthKeyID.EQ(jet.Uint64(id))),
	)
	if err != nil {
		return fmt.Errorf("destroying pre-auth key: clearing auth_key_id on nodes: %w", err)
	}

	// Then delete the pre-auth key
	affected, err := q.executor().exec(table.PreAuthKeys.DELETE().WHERE(table.PreAuthKeys.ID.EQ(jet.Uint64(id))))
	if err != nil {
		return fmt.Errorf("destroying pre-auth key: deleting pre-auth key: %w", err)
	}

	if affected == 0 {
		return fmt.Errorf("destroying pre-auth key: %w", ErrPreAuthKeyNotFound)
	}

	return nil
}

func (hsdb *HSDatabase) ExpirePreAuthKey(id uint64) error {
	return hsdb.Write(func(tx *Tx) error {
		return ExpirePreAuthKey(tx, id)
	})
}

func (hsdb *HSDatabase) DeletePreAuthKey(id uint64) error {
	return hsdb.Write(func(tx *Tx) error {
		return DestroyPreAuthKey(tx, id)
	})
}

func (hsdb *HSDatabase) RevokePreAuthKey(id uint64) error {
	return hsdb.Write(func(tx *Tx) error {
		return RevokePreAuthKey(tx, id)
	})
}

// RevokePreAuthKey soft-revokes a key (the v2 API's DELETE): the row is kept and
// stays retrievable with its invalid flag set, but the key can no longer
// authorize nodes. The background collector hard-deletes it after the retention
// window. An already-revoked or unknown id returns [ErrPreAuthKeyNotFound], so a
// repeated DELETE is a clean 404.
func RevokePreAuthKey(q Querier, id uint64) error {
	affected, err := updatePreAuthKeyColumn(
		q,
		table.PreAuthKeys.ID.EQ(jet.Uint64(id)).AND(table.PreAuthKeys.Revoked.IS_NULL()),
		table.PreAuthKeys.Revoked, time.Now(),
	)
	if err != nil {
		return err
	}

	if affected == 0 {
		return ErrPreAuthKeyNotFound
	}

	return nil
}

// DestroyRevokedPreAuthKeysBefore hard-deletes every key revoked before cutoff,
// returning how many were removed. The background collector calls this to reap
// soft-revoked keys after the retention window.
func (hsdb *HSDatabase) DestroyRevokedPreAuthKeysBefore(cutoff time.Time) (int, error) {
	var count int

	err := hsdb.Write(func(tx *Tx) error {
		var ids []idRow

		err := tx.ex.query(
			jet.SELECT(table.PreAuthKeys.ID.AS("id_row.id")).FROM(table.PreAuthKeys).
				WHERE(table.PreAuthKeys.Revoked.IS_NOT_NULL().
					AND(table.PreAuthKeys.Revoked.LT(jet.TimestampExp(timeArg(cutoff))))),
			&ids,
		)
		if err != nil {
			return err
		}

		for _, id := range ids {
			err := DestroyPreAuthKey(tx, id.ID)
			if err != nil {
				return err
			}
		}

		count = len(ids)

		return nil
	})

	return count, err
}

// UsePreAuthKey atomically marks a [types.PreAuthKey] as used. The UPDATE is
// guarded by `used = false` so two concurrent registrations racing for
// the same single-use key cannot both succeed: the first commits and
// the second returns [types.PAKError]("authkey already used"). Without the
// guard the previous code (Update("used", true) with no WHERE) would
// silently let both transactions claim the key.
func UsePreAuthKey(q Querier, k *types.PreAuthKey) error {
	affected, err := updatePreAuthKeyColumn(
		q,
		table.PreAuthKeys.ID.EQ(jet.Uint64(k.ID)).AND(table.PreAuthKeys.Used.EQ(jet.Bool(false))),
		table.PreAuthKeys.Used, true,
	)
	if err != nil {
		return fmt.Errorf("updating key used status in database: %w", err)
	}

	if affected == 0 {
		return types.PAKError("authkey already used")
	}

	k.Used = true

	return nil
}

// ExpirePreAuthKey marks a [types.PreAuthKey] as expired, returning
// [ErrPreAuthKeyNotFound] rather than succeeding silently when no such key exists.
func ExpirePreAuthKey(q Querier, id uint64) error {
	affected, err := updatePreAuthKeyColumn(
		q, table.PreAuthKeys.ID.EQ(jet.Uint64(id)), table.PreAuthKeys.Expiration, time.Now(),
	)
	if err != nil {
		return err
	}

	if affected == 0 {
		return ErrPreAuthKeyNotFound
	}

	return nil
}
