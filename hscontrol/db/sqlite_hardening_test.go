package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSQLiteHardeningReachesProduction opens the database exactly as the
// server does and checks that the connection-level hardening from
// sqliteconfig is in force: the DSN is only honoured because hscontrol/db
// opens SQLite through the modernc driver directly.
func TestSQLiteHardeningReachesProduction(t *testing.T) {
	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	ctx := t.Context()

	// Defensive mode: the request to make the schema writable is refused
	// silently, so the pragma reads back as off.
	_, err = db.DB.ExecContext(ctx, "PRAGMA writable_schema = ON")
	require.NoError(t, err)

	var writable int
	require.NoError(t, db.DB.QueryRowContext(ctx, "PRAGMA writable_schema").Scan(&writable))
	assert.Equal(t, 0, writable, "defensive mode must keep writable_schema off")

	// Strict double quotes: an unknown double-quoted identifier is an error
	// rather than a silently substituted string literal.
	var out string

	err = db.DB.QueryRowContext(ctx, `SELECT "no_such_column"`).Scan(&out)
	require.ErrorContains(t, err, "no such column")

	// Ordinary single-quoted literals keep working through the same path.
	require.NoError(t, db.DB.QueryRowContext(ctx, `SELECT 'literal'`).Scan(&out))
	assert.Equal(t, "literal", out)
}
