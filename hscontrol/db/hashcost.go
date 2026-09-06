package db

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Credential hashing costs. The production values are OWASP's minimum
// recommendations; they are encoded into every stored hash, so raising them
// later still verifies credentials stored under the old cost.
//
// Under `go test` they drop to the cheapest settings the algorithms accept.
// The hashes still travel through the same code paths and parsers, but a test
// that mints hundreds of keys no longer spends minutes inside a KDF, and the
// race detector stops amplifying memory-hard work tenfold. Integration tests
// exercise the real binary and therefore the real cost.
var (
	bcryptCost = bcrypt.DefaultCost

	argon2Time   uint32 = 2
	argon2Memory uint32 = 19 * 1024
)

// argon2MinMemoryPerThread is the smallest per-thread memory cost (in KiB)
// the argon2 implementation accepts.
const argon2MinMemoryPerThread = 8

//nolint:gochecknoinits // lowers credential-hashing cost for tests before any hash is computed
func init() {
	if !testing.Testing() {
		return
	}

	bcryptCost = bcrypt.MinCost
	argon2Time = 1
	argon2Memory = argon2MinMemoryPerThread * argon2Threads // the smallest argon2 accepts
}
