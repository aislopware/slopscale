package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/aislopware/slopscale/hscontrol/db/sqliteconfig"
	"github.com/aislopware/slopscale/hscontrol/types"
)

// Errors reported by the backup, verify and restore operations. They are
// exported so the CLI can tell an operator mistake from a broken file.
var (
	// ErrBackupNotSQLite is returned for a PostgreSQL database, whose
	// backup belongs to the tools that ship with the server.
	ErrBackupNotSQLite = errors.New(
		"only sqlite databases can be copied by slopscale, use pg_dump and pg_restore for postgresql",
	)
	// ErrBackupDestinationExists keeps a backup from overwriting an
	// older one.
	ErrBackupDestinationExists = errors.New("backup destination already exists")
	// ErrIntegrityCheck reports what PRAGMA integrity_check found.
	ErrIntegrityCheck = errors.New("integrity check failed")
	// ErrNoMigrationHistory marks a readable SQLite file that was not
	// written by slopscale.
	ErrNoMigrationHistory = errors.New("no migrations table, the file is not a slopscale database")
	// ErrDatabaseInUse is the write lock a running server holds.
	ErrDatabaseInUse = errors.New("the database is in use, stop slopscale before restoring")
)

// backupTimestampLayout names backup and pre-restore files after the UTC
// time they were made, in an order that sorts.
const backupTimestampLayout = "20060102-150405"

// lockProbeBusyTimeout is how long, in milliseconds, the restore's write
// lock probe waits for the database. It is short on purpose: the question
// is whether another process holds the database, not whether it will let
// go eventually.
const lockProbeBusyTimeout = 500

// DefaultBackupPath is where a backup lands when the operator names no
// destination: next to the database it copies.
func DefaultBackupPath(cfg *types.Config) string {
	return cfg.Database.Sqlite.Path + ".backup-" + time.Now().UTC().Format(backupTimestampLayout)
}

// Backup writes a consistent copy of the configured SQLite database to
// dest while the server keeps running. It opens the database the way the
// server does but runs neither migrations nor the version check, so the
// copy is the schema that is there, and hands the copying to SQLite's own
// VACUUM INTO, which reads one transaction and leaves no write-ahead log
// of its own. The copy is written next to dest and only takes its name
// once it reads back, so an interrupted backup leaves nothing behind.
func Backup(cfg *types.Config, dest string) error {
	if cfg.Database.Type != types.DatabaseSqlite {
		return ErrBackupNotSQLite
	}

	// Opening the database would create an empty one, which is not what
	// an operator who mistyped the config file's path wants back.
	_, err := os.Stat(cfg.Database.Sqlite.Path)
	if err != nil {
		return fmt.Errorf("reading the database at %s: %w", cfg.Database.Sqlite.Path, err)
	}

	_, err = os.Stat(dest)
	if err == nil {
		return fmt.Errorf("%w: %s", ErrBackupDestinationExists, dest)
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", dest, err)
	}

	// SQLite reports a missing backup directory as an unspecific failure
	// to open a file, which is not a useful answer to a mistyped --out.
	dir := filepath.Dir(dest)

	_, err = os.Stat(dir)
	if err != nil {
		return fmt.Errorf("reading the backup directory %s: %w", dir, err)
	}

	// VACUUM INTO refuses to write over a file, so the leftover of an
	// interrupted run has to go before it can start.
	tmp := dest + ".tmp"

	err = os.Remove(tmp)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing the leftover %s: %w", tmp, err)
	}

	pool, err := openSQLite(cfg.Database.Sqlite)
	if err != nil {
		return err
	}

	defer func() { _ = pool.Close() }()

	_, err = pool.ExecContext(context.Background(), "VACUUM INTO $1", tmp)
	if err != nil {
		return fmt.Errorf("copying the database into %s: %w", tmp, err)
	}

	err = checkFile(tmp)
	if err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("reading back %s: %w", tmp, err)
	}

	err = os.Rename(tmp, dest)
	if err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("renaming %s to %s: %w", tmp, dest, err)
	}

	return nil
}

// Verify reports whether the file at path is a slopscale database this
// binary could use: it passes SQLite's integrity check, carries the
// migration history, and was last written by a version this one can take
// over from.
func Verify(path string) error {
	pool, err := openReadOnly(path)
	if err != nil {
		return err
	}

	defer func() { _ = pool.Close() }()

	err = checkIntegrity(pool)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	err = checkStoredVersion(pool)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	return nil
}

// Restore puts the database at src in place of the configured one. The
// current database is moved aside rather than deleted, so a restore that
// turns out to be the wrong file can be undone by moving it back.
func Restore(cfg *types.Config, src string) error {
	if cfg.Database.Type != types.DatabaseSqlite {
		return ErrBackupNotSQLite
	}

	err := Verify(src)
	if err != nil {
		return err
	}

	path := cfg.Database.Sqlite.Path

	err = refuseWhileInUse(path)
	if err != nil {
		return err
	}

	saved := path + ".pre-restore-" + time.Now().UTC().Format(backupTimestampLayout)

	// The write-ahead log and its shared-memory file belong to the
	// database they sit next to; moving the three together keeps the
	// copy that is set aside readable.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err = os.Rename(path+suffix, saved+suffix)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("moving %s aside: %w", path+suffix, err)
		}
	}

	err = copyFile(src, path)
	if err != nil {
		return fmt.Errorf("restoring %s, the previous database is at %s: %w", src, saved, err)
	}

	err = checkFile(path)
	if err != nil {
		return fmt.Errorf("reading back %s, the previous database is at %s: %w", path, saved, err)
	}

	return nil
}

// openReadOnly opens path in the driver's read-only mode, which reports a
// missing file instead of creating an empty database and applies no
// pragma that would write to the file.
func openReadOnly(path string) (*sql.DB, error) {
	pool, err := sqliteconfig.Open(&sqliteconfig.Config{
		Path:        "file:" + path + "?mode=ro",
		BusyTimeout: sqliteconfig.DefaultBusyTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("opening %s read-only: %w", path, err)
	}

	pool.SetMaxOpenConns(1)

	return pool, nil
}

// checkFile runs the structural checks on the file at path.
func checkFile(path string) error {
	pool, err := openReadOnly(path)
	if err != nil {
		return err
	}

	defer func() { _ = pool.Close() }()

	return checkIntegrity(pool)
}

// checkIntegrity asks SQLite whether the file is sound and looks for the
// migration history, the one table every slopscale database has.
func checkIntegrity(pool *sql.DB) error {
	ctx := context.Background()

	var result string

	// A damaged database reports one row per problem; the first is
	// enough to tell the operator the file is unusable.
	err := pool.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
	if err != nil {
		return fmt.Errorf("running the integrity check: %w", err)
	}

	if result != "ok" {
		return fmt.Errorf("%w: %s", ErrIntegrityCheck, result)
	}

	exists, err := tableExists(ctx, pool, "migrations")
	if err != nil {
		return err
	}

	if !exists {
		return ErrNoMigrationHistory
	}

	return nil
}

// checkStoredVersion applies the startup upgrade rules to the version the
// file records, so an operator learns that a backup comes from a newer
// server before restoring it rather than when the server refuses to boot.
func checkStoredVersion(pool *sql.DB) error {
	ctx := context.Background()

	exists, err := tableExists(ctx, pool, "database_versions")
	if err != nil {
		return err
	}

	// Databases that predate the version table record nothing to
	// compare against; the server treats them the same way.
	if !exists {
		return nil
	}

	ex := executor{ctx: ctx, db: pool, dialect: dialectSQLite}

	return checkVersionUpgradePathFromVersions(&ex, types.GetVersionInfo().Version)
}

func tableExists(ctx context.Context, pool *sql.DB, name string) (bool, error) {
	var count int

	err := pool.QueryRowContext(
		ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = $1",
		name,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("looking for the %s table: %w", name, err)
	}

	return count > 0, nil
}

// refuseWhileInUse tries to take the database's write lock. A running
// server does not hold it while it is idle, so this catches the busy
// server rather than every running one; the documentation tells operators
// to stop the server themselves.
func refuseWhileInUse(path string) error {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	// Only the busy timeout and the lock mode are set: every other
	// pragma the server uses would write to a database that is about to
	// be moved aside.
	pool, err := sqliteconfig.Open(&sqliteconfig.Config{
		Path:        path,
		BusyTimeout: lockProbeBusyTimeout,
		TxLock:      sqliteconfig.TxLockImmediate,
	})
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	defer func() { _ = pool.Close() }()

	tx, err := pool.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrDatabaseInUse, path, err)
	}

	err = tx.Rollback()
	if err != nil {
		return fmt.Errorf("releasing the write lock on %s: %w", path, err)
	}

	return nil
}

// copyFile copies src over dst and flushes it, so the restored database
// is on disk when the command returns.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}

	_, err = io.Copy(out, in)
	if err != nil {
		_ = out.Close()

		return fmt.Errorf("writing %s: %w", dst, err)
	}

	err = out.Sync()
	if err != nil {
		_ = out.Close()

		return fmt.Errorf("flushing %s: %w", dst, err)
	}

	err = out.Close()
	if err != nil {
		return fmt.Errorf("closing %s: %w", dst, err)
	}

	return nil
}
