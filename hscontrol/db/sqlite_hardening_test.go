package db

import (
	"os"
	"strings"
	"testing"

	"github.com/juanfont/headscale/hscontrol/db/sqliteconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSQLiteHardeningReachesProduction opens the database exactly as the
// server does and checks that the compile-time hardening from sqlite.cflags
// is in force. The flags reach the compiler through CGO_CFLAGS, which make
// and the Nix flake export; a bare `go test` cannot see them and skips.
func TestSQLiteHardeningReachesProduction(t *testing.T) {
	if !strings.Contains(os.Getenv("CGO_CFLAGS"), "SQLITE_DQS=0") {
		t.Skip("CGO_CFLAGS does not carry sqlite.cflags; run through make test")
	}

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	ctx := t.Context()

	hardening, err := sqliteconfig.ProbeHardening(ctx, db.DB)
	require.NoError(t, err)
	assert.True(t, hardening.Defensive, "defensive mode must keep writable_schema off")
	assert.True(t, hardening.StrictDoubleQuotes, "double-quoted string literals must be rejected")

	// The probe leaves the connection usable and ordinary literals work.
	var out string

	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT 'literal'`).Scan(&out))
	assert.Equal(t, "literal", out)
}
