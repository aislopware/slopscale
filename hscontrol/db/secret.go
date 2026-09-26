package db

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// hashPrefixSHA256 marks the current hash format of every stored credential
// secret: API keys, pre-auth keys, OAuth clients and access tokens. Their
// secrets hold 256 bits of crypto/rand entropy and are never user-chosen, so
// recovering one from its SHA-256 digest means searching that whole space;
// security comes from the entropy, not from hash cost. Password stretching
// (bcrypt, Argon2id) defends guessable secrets and would only add cost to
// every authentication. The same reasoning underlies NIST SP 800-63B-4
// §3.1.2.2 (look-up secrets need a salted password hash only below 112 bits)
// and RFC 6819 §5.1.4.1.3.
const hashPrefixSHA256 = "$sha256$"

// Bounds for Argon2id hashes read back from storage, so a corrupt row cannot
// panic argon2 or allocate unbounded memory.
const (
	argon2KeyLen    = 32
	argon2MaxMemory = 64 * 1024
)

var (
	errSecretHashMalformed = errors.New("malformed secret hash")
	errSecretMismatch      = errors.New("secret does not match hash")
)

// legacyHashLimiter bounds concurrent bcrypt and Argon2id verifications. Both
// are deliberately expensive (an Argon2id hash costs ~19 MiB) and reachable
// from unauthenticated endpoints; they run only until each stored hash has
// been upgraded to SHA-256 by its first successful use.
var legacyHashLimiter = make(chan struct{}, max(2, runtime.GOMAXPROCS(0)))

// hashSecret returns the storage form of a credential secret.
func hashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))

	return []byte(hashPrefixSHA256 + hex.EncodeToString(sum[:]))
}

// verifySecret reports whether secret matches a stored hash. Hashes written
// before SHA-256 (bcrypt for API and pre-auth keys, Argon2id for OAuth
// clients and tokens) still verify and return needsRehash, so the caller can
// upgrade them.
func verifySecret(encoded []byte, secret string) (bool, error) {
	if hexSum, ok := bytes.CutPrefix(encoded, []byte(hashPrefixSHA256)); ok {
		want, err := hex.DecodeString(string(hexSum))
		if err != nil || len(want) != sha256.Size {
			return false, errSecretHashMalformed
		}

		got := sha256.Sum256([]byte(secret))
		if subtle.ConstantTimeCompare(got[:], want) != 1 {
			return false, errSecretMismatch
		}

		return false, nil
	}

	legacyHashLimiter <- struct{}{}
	// Deferred: a panic in the hash must not leak the slot for good.
	defer func() { <-legacyHashLimiter }()

	if bytes.HasPrefix(encoded, []byte("$argon2id$")) {
		err := verifyArgon2id(encoded, secret)

		return err == nil, err
	}

	switch err := bcrypt.CompareHashAndPassword(encoded, []byte(secret)); {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return false, errSecretMismatch
	default:
		return false, errSecretHashMalformed
	}
}

// verifyArgon2id checks secret against a PHC-encoded Argon2id hash, rejecting
// parameters argon2 would panic on or that would allocate unbounded memory.
func verifyArgon2id(encoded []byte, secret string) error {
	parts := strings.Split(string(encoded), "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errSecretHashMalformed
	}

	var version int

	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil || version != argon2.Version {
		return errSecretHashMalformed
	}

	var (
		memory, timeCost uint32
		threads          uint8
	)

	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads)
	if err != nil || timeCost < 1 || threads < 1 || memory > argon2MaxMemory {
		return errSecretHashMalformed
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return errSecretHashMalformed
	}

	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) != argon2KeyLen {
		return errSecretHashMalformed
	}

	got := argon2.IDKey([]byte(secret), salt, timeCost, memory, threads, argon2KeyLen)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return errSecretMismatch
	}

	return nil
}

// hashColumn names where one credential kind keeps its hash, so a verified
// legacy hash can be upgraded in place.
type hashColumn struct {
	table jet.Table
	id    jet.ColumnInteger
	hash  jet.ColumnBlob
}

// verifyCredential checks secret against stored, the hash of row id in col,
// and upgrades a legacy hash to SHA-256 once the secret has verified.
func verifyCredential(q Querier, col hashColumn, id uint64, stored []byte, secret string) error {
	needsRehash, err := verifySecret(stored, secret)
	if err != nil {
		return err
	}

	if needsRehash {
		upgradeHash(q, col, id, stored, secret)
	}

	return nil
}

// upgradeHash replaces a verified legacy hash with the SHA-256 form. It only
// replaces the hash it verified: an API key rotated meanwhile holds a new
// hash, which the retired secret's digest must not overwrite. Best effort: on
// failure the legacy hash stays and the next authentication retries.
func upgradeHash(q Querier, col hashColumn, id uint64, stored []byte, secret string) {
	_, err := q.executor().exec(
		col.table.UPDATE(col.hash).SET(jet.Blob(hashSecret(secret))).
			WHERE(col.id.EQ(jet.Uint64(id)).AND(col.hash.EQ(jet.Blob(stored)))),
	)
	if err != nil {
		log.Warn().Err(err).Str("table", col.table.TableName()).Uint64("id", id).
			Msg("upgrading legacy credential hash")
	}
}
