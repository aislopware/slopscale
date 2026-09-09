package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/db/sqliteconfig"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newBackupTestDB opens a write-ahead-log database in its own directory,
// the shape the backup commands are written for.
func newBackupTestDB(t *testing.T) (*HSDatabase, *types.Config) {
	t.Helper()

	cfg := &types.Config{
		Database: types.DatabaseConfig{
			Type: types.DatabaseSqlite,
			Sqlite: types.SqliteConfig{
				Path:          filepath.Join(t.TempDir(), "db.sqlite"),
				WriteAheadLog: true,
			},
		},
		Policy: types.PolicyConfig{Mode: types.PolicyModeDB},
	}

	hsdb, err := NewSlopscaleDatabase(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hsdb.Close() })

	return hsdb, cfg
}

func userNames(t *testing.T, hsdb *HSDatabase) []string {
	t.Helper()

	users, err := hsdb.ListUsers(nil)
	require.NoError(t, err)

	names := make([]string, 0, len(users))
	for _, user := range users {
		names = append(names, user.Name)
	}

	return names
}

// TestBackupRestoreRoundTrip backs up a database that is open and being
// written to, then restores it over a database that has moved on, and
// expects the state of the backup to be back.
func TestBackupRestoreRoundTrip(t *testing.T) {
	hsdb, cfg := newBackupTestDB(t)
	hsdb.CreateUserForTest("in-the-backup")

	dir := filepath.Dir(cfg.Database.Sqlite.Path)
	dest := filepath.Join(dir, "db.sqlite.backup")

	require.NoError(t, Backup(cfg, dest))
	assert.FileExists(t, dest)
	assert.NoFileExists(t, dest+".tmp", "the temporary copy is renamed, not left behind")
	require.NoError(t, Verify(dest))

	require.ErrorIs(t, Backup(cfg, dest), ErrBackupDestinationExists)

	hsdb.CreateUserForTest("after-the-backup")
	require.NoError(t, hsdb.Close())

	require.NoError(t, Restore(cfg, dest))

	saved, err := filepath.Glob(cfg.Database.Sqlite.Path + ".pre-restore-*")
	require.NoError(t, err)
	assert.NotEmpty(t, saved, "the replaced database is kept")

	restored, err := NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	t.Cleanup(func() { _ = restored.Close() })

	names := userNames(t, restored)
	assert.Contains(t, names, "in-the-backup")
	assert.NotContains(t, names, "after-the-backup")
}

// TestVerifyRejectsBadFiles covers the two ways a file fails to be a
// slopscale database: it is not a database at all, or it is one somebody
// else wrote.
func TestVerifyRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()

	garbage := filepath.Join(dir, "garbage.sqlite")
	require.NoError(t, os.WriteFile(garbage, []byte("not a database at all"), 0o600))
	require.Error(t, Verify(garbage))

	require.Error(t, Verify(filepath.Join(dir, "missing.sqlite")))

	foreign := filepath.Join(dir, "foreign.sqlite")

	pool, err := sqliteconfig.Open(sqliteconfig.Default(foreign))
	require.NoError(t, err)

	_, err = pool.ExecContext(t.Context(), "CREATE TABLE something (id integer primary key)")
	require.NoError(t, err)
	require.NoError(t, pool.Close())

	require.ErrorIs(t, Verify(foreign), ErrNoMigrationHistory)
}

// TestRestoreRefusesLockedDatabase holds the write lock the way a busy
// server does and expects the restore to leave everything alone.
func TestRestoreRefusesLockedDatabase(t *testing.T) {
	hsdb, cfg := newBackupTestDB(t)
	hsdb.CreateUserForTest("still-here")

	dest := filepath.Join(filepath.Dir(cfg.Database.Sqlite.Path), "db.sqlite.backup")
	require.NoError(t, Backup(cfg, dest))

	tx, err := hsdb.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)

	_, err = tx.ExecContext(t.Context(), "INSERT INTO migrations (id) VALUES ('lock-probe')")
	require.NoError(t, err)

	require.ErrorIs(t, Restore(cfg, dest), ErrDatabaseInUse)

	saved, err := filepath.Glob(cfg.Database.Sqlite.Path + ".pre-restore-*")
	require.NoError(t, err)
	assert.Empty(t, saved, "a refused restore moves nothing")

	require.NoError(t, tx.Rollback())
	assert.Contains(t, userNames(t, hsdb), "still-here")
}

// TestBackupRefusesPostgres points the operator at pg_dump rather than
// pretending to copy a database it cannot reach.
func TestBackupRefusesPostgres(t *testing.T) {
	cfg := &types.Config{
		Database: types.DatabaseConfig{Type: types.DatabasePostgres},
	}

	dest := filepath.Join(t.TempDir(), "db.sqlite.backup")
	require.ErrorIs(t, Backup(cfg, dest), ErrBackupNotSQLite)
	require.ErrorIs(t, Restore(cfg, dest), ErrBackupNotSQLite)
	assert.NoFileExists(t, dest)
}
