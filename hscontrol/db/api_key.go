package db

import (
	"errors"
	"fmt"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
	"golang.org/x/crypto/bcrypt"
	"tailscale.com/util/rands"
)

const (
	apiKeyPrefix       = "hskey-api-" //nolint:gosec // This is a prefix, not a credential
	apiKeyPrefixLength = 12
	apiKeyHashLength   = 64

	// Legacy format constants.
	legacyAPIPrefixLength = 7
	legacyAPIKeyLength    = 32
)

var (
	ErrAPIKeyFailedToParse    = errors.New("failed to parse ApiKey")
	ErrAPIKeyGenerationFailed = errors.New("failed to generate API key")
	ErrAPIKeyExpired          = errors.New("API key expired")
)

// CreateAPIKey creates a new [types.APIKey] in a user, and returns it.
func (hsdb *HSDatabase) CreateAPIKey(
	expiration *time.Time,
) (string, *types.APIKey, error) {
	return hsdb.CreateScopedAPIKey(expiration, nil, "")
}

// newAPIKeySecret mints the credential material for a key and returns, in
// order, the whole key string shown once to the operator, the public prefix
// it is looked up by, and the bcrypt hash of its secret.
func newAPIKeySecret() (string, string, []byte, error) {
	// Public prefix (12 chars) and secret (64 chars).
	prefix := rands.HexString(apiKeyPrefixLength)
	secret := rands.HexString(apiKeyHashLength)

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcryptCost)
	if err != nil {
		return "", "", nil, fmt.Errorf("hashing API key secret: %w", err)
	}

	return apiKeyPrefix + prefix + "-" + secret, prefix, hash, nil
}

// CreateScopedAPIKey creates a key that carries scopes and a description.
// The caller has already narrowed the scopes to what it may delegate.
func (hsdb *HSDatabase) CreateScopedAPIKey(
	expiration *time.Time,
	scopes []string,
	description string,
) (string, *types.APIKey, error) {
	keyStr, prefix, hash, err := newAPIKeySecret()
	if err != nil {
		return "", nil, err
	}

	now := time.Now()
	key := types.APIKey{
		Prefix:      prefix,
		Hash:        hash,
		Scopes:      scopes,
		Description: description,
		CreatedAt:   &now,
		Expiration:  expiration,
	}

	row, err := apiKeyRowFrom(&key)
	if err != nil {
		return "", nil, err
	}

	var inserted idRow

	err = hsdb.ex.query(
		table.APIKeys.INSERT(table.APIKeys.MutableColumns).MODEL(&row).RETURNING(table.APIKeys.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return "", nil, fmt.Errorf("saving API key to database: %w", err)
	}

	key.ID = inserted.ID

	return keyStr, &key, nil
}

// ListAPIKeys returns the list of [types.APIKey] values for a user.
func (hsdb *HSDatabase) ListAPIKeys() ([]types.APIKey, error) {
	var records []apiKeyRecord

	err := hsdb.ex.query(
		jet.SELECT(table.APIKeys.AllColumns).FROM(table.APIKeys).ORDER_BY(table.APIKeys.ID.ASC()),
		&records,
	)
	if err != nil {
		return nil, err
	}

	keys := make([]types.APIKey, 0, len(records))

	for i := range records {
		key, err := records[i].Key.key()
		if err != nil {
			return nil, err
		}

		keys = append(keys, *key)
	}

	return keys, nil
}

// queryAPIKey returns the API key matched by where, or [ErrNotFound].
func selectAPIKey(where jet.BoolExpression) jet.SelectStatement {
	return jet.SELECT(table.APIKeys.AllColumns).FROM(table.APIKeys).WHERE(where).
		ORDER_BY(table.APIKeys.ID.ASC()).LIMIT(1)
}

func queryAPIKey(q Querier, where jet.BoolExpression) (*types.APIKey, error) {
	var record apiKeyRecord

	err := q.executor().query(selectAPIKey(where), &record)
	if err != nil {
		return nil, err
	}

	return record.Key.key()
}

// apiKeyByPrefix is the lookup every authenticated API request makes,
// rendered once; see [fixedSQL].
var apiKeyByPrefix = newFixedSQL(func() statement {
	return selectAPIKey(table.APIKeys.Prefix.EQ(jet.String("")))
})

// GetAPIKey returns a [types.APIKey] for a given key.
func (hsdb *HSDatabase) GetAPIKey(prefix string) (*types.APIKey, error) {
	var record apiKeyRecord

	err := hsdb.ex.queryFixed(apiKeyByPrefix, &record, prefix, limitOne)
	if err != nil {
		return nil, err
	}

	return record.Key.key()
}

// GetAPIKeyByID returns a [types.APIKey] for a given id.
func (hsdb *HSDatabase) GetAPIKeyByID(id uint64) (*types.APIKey, error) {
	return queryAPIKey(hsdb, table.APIKeys.ID.EQ(jet.Uint64(id)))
}

// DestroyAPIKey destroys a [types.APIKey]. Returns error if the [types.APIKey]
// does not exist.
func (hsdb *HSDatabase) DestroyAPIKey(key types.APIKey) error {
	affected, err := hsdb.ex.exec(table.APIKeys.DELETE().WHERE(table.APIKeys.ID.EQ(jet.Uint64(key.ID))))
	if err != nil {
		return err
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

// ExpireAPIKey marks a [types.APIKey] as expired.
func (hsdb *HSDatabase) ExpireAPIKey(key *types.APIKey) error {
	now := time.Now()

	_, err := hsdb.ex.exec(
		table.APIKeys.UPDATE(table.APIKeys.Expiration).SET(now).WHERE(table.APIKeys.ID.EQ(jet.Uint64(key.ID))),
	)
	if err != nil {
		return err
	}

	key.Expiration = &now

	return nil
}

// RotateAPIKey mints a new secret for an existing key and returns the new key
// string, shown once. The row is rewritten in place rather than replaced by a
// new one: the key keeps its id, owner, scopes and description, so the audit
// trail and the key list stay attached to one credential instead of forking
// into a new row plus an expired husk that operators then have to reap. The
// old secret stops working the moment the hash lands, because every request
// looks a key up by its prefix and compares the stored hash. last_seen is
// cleared, since it described the secret that has just been retired. A nil
// expiration keeps the one the key already has.
func (hsdb *HSDatabase) RotateAPIKey(key *types.APIKey, expiration *time.Time) (string, error) {
	keyStr, prefix, hash, err := newAPIKeySecret()
	if err != nil {
		return "", err
	}

	newExpiration := key.Expiration
	if expiration != nil {
		newExpiration = expiration
	}

	var expirationValue any = jet.NULL
	if newExpiration != nil {
		expirationValue = *newExpiration
	}

	affected, err := hsdb.ex.exec(
		table.APIKeys.
			UPDATE(table.APIKeys.Prefix, table.APIKeys.Hash, table.APIKeys.Expiration, table.APIKeys.LastSeen).
			SET(prefix, hash, expirationValue, jet.NULL).
			WHERE(table.APIKeys.ID.EQ(jet.Uint64(key.ID))),
	)
	if err != nil {
		return "", err
	}

	if affected == 0 {
		return "", ErrNotFound
	}

	key.Prefix = prefix
	key.Hash = hash
	key.Expiration = newExpiration
	key.LastSeen = nil

	return keyStr, nil
}

func (hsdb *HSDatabase) ValidateAPIKey(keyStr string) (bool, error) {
	key, err := validateAPIKey(hsdb, keyStr)
	if err != nil {
		return false, err
	}

	if key.Expiration != nil && key.Expiration.Before(time.Now()) {
		return false, nil
	}

	return true, nil
}

// AuthenticateAPIKey validates keyStr and returns the matching, unexpired
// [types.APIKey] (with its owning UserID populated). Unlike ValidateAPIKey it
// returns the key itself, so the v2 API can act as the key's owning user. A
// non-nil error means the key is missing, malformed, or expired.
func (hsdb *HSDatabase) AuthenticateAPIKey(keyStr string) (*types.APIKey, error) {
	key, err := validateAPIKey(hsdb, keyStr)
	if err != nil {
		return nil, err
	}

	if key.Expiration != nil && key.Expiration.Before(time.Now()) {
		return nil, ErrAPIKeyExpired
	}

	return key, nil
}

// SetAPIKeyUser sets the owning user of an API key. Used when an admin mints a
// key on behalf of a user (headscale apikeys create --user).
func (hsdb *HSDatabase) SetAPIKeyUser(keyID uint64, userID types.UserID) error {
	_, err := hsdb.ex.exec(
		table.APIKeys.UPDATE(table.APIKeys.UserID).SET(uint(userID)).WHERE(table.APIKeys.ID.EQ(jet.Uint64(keyID))),
	)

	return err
}

// ParseAPIKeyPrefix extracts the database prefix from a display prefix.
// Handles formats: "hskey-api-{12chars}-***", "hskey-api-{12chars}", or just "{12chars}".
// Returns the 12-character prefix suitable for database lookup.
func ParseAPIKeyPrefix(displayPrefix string) (string, error) {
	// If it's already just the 12-character prefix, return it
	if len(displayPrefix) == apiKeyPrefixLength && isValidBase64URLSafe(displayPrefix) {
		return displayPrefix, nil
	}

	// If it starts with the API key prefix, parse it
	if strings.HasPrefix(displayPrefix, apiKeyPrefix) {
		// Remove the "hskey-api-" prefix
		_, remainder, found := strings.Cut(displayPrefix, apiKeyPrefix)
		if !found {
			return "", fmt.Errorf("%w: invalid display prefix format", ErrAPIKeyFailedToParse)
		}

		// Extract just the first 12 characters (the actual prefix)
		if len(remainder) < apiKeyPrefixLength {
			return "", fmt.Errorf("%w: prefix too short", ErrAPIKeyFailedToParse)
		}

		prefix := remainder[:apiKeyPrefixLength]

		// Validate it's base64 URL-safe
		if !isValidBase64URLSafe(prefix) {
			return "", fmt.Errorf("%w: prefix contains invalid characters", ErrAPIKeyFailedToParse)
		}

		return prefix, nil
	}

	// For legacy 7-character prefixes or other formats, return as-is
	return displayPrefix, nil
}

// validateAPIKey validates an API key and returns the key if valid.
// Handles both new (hskey-api-{prefix}-{secret}) and legacy (prefix.secret) formats.
func validateAPIKey(q Querier, keyStr string) (*types.APIKey, error) {
	// Validate input is not empty
	if keyStr == "" {
		return nil, ErrAPIKeyFailedToParse
	}

	// Check for new format: hskey-api-{prefix}-{secret}
	_, prefixAndSecret, found := strings.Cut(keyStr, apiKeyPrefix)

	if !found {
		// Legacy format: prefix.secret
		return validateLegacyAPIKey(q, keyStr)
	}

	// New format: parse and verify
	prefix, secret, err := parsePrefixedKey(
		prefixAndSecret,
		apiKeyPrefixLength,
		apiKeyHashLength,
		ErrAPIKeyFailedToParse,
	)
	if err != nil {
		return nil, err
	}

	// Look up by prefix (indexed)
	key, err := queryAPIKey(q, table.APIKeys.Prefix.EQ(jet.String(prefix)))
	if err != nil {
		return nil, fmt.Errorf("API key not found: %w", err)
	}

	// Verify bcrypt hash
	err = bcrypt.CompareHashAndPassword(key.Hash, []byte(secret))
	if err != nil {
		return nil, fmt.Errorf("invalid API key: %w", err)
	}

	return key, nil
}

// validateLegacyAPIKey validates a legacy format API key (prefix.secret).
func validateLegacyAPIKey(q Querier, keyStr string) (*types.APIKey, error) {
	// Legacy format uses "." as separator
	prefix, secret, found := strings.Cut(keyStr, ".")
	if !found {
		return nil, ErrAPIKeyFailedToParse
	}

	// Legacy prefix is 7 chars
	if len(prefix) != legacyAPIPrefixLength {
		return nil, fmt.Errorf("%w: legacy prefix length mismatch", ErrAPIKeyFailedToParse)
	}

	key, err := queryAPIKey(q, table.APIKeys.Prefix.EQ(jet.String(prefix)))
	if err != nil {
		return nil, fmt.Errorf("API key not found: %w", err)
	}

	// Verify bcrypt (key.Hash stores bcrypt of full secret)
	err = bcrypt.CompareHashAndPassword(key.Hash, []byte(secret))
	if err != nil {
		return nil, fmt.Errorf("invalid API key: %w", err)
	}

	return key, nil
}
