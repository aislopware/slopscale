package db

import (
	"errors"
	"fmt"
	"slices"

	"github.com/rs/zerolog/log"
)

// migration is one step of the schema history. IDs are
// YYYYMMDDHHMM-short-description, the order of the list is immutable and
// new migrations go at the end.
type migration struct {
	id  string
	run func(tx *Tx) error
}

// initSchemaMigrationID marks a database whose schema was created from the
// full DDL rather than by replaying the history. The value is inherited from
// gormigrate, which recorded it in the same table.
const initSchemaMigrationID = "SCHEMA_INIT"

// lastMigrationRequiringFKDisabled is the last migration that SQLite runs
// with foreign keys off, because its table rewrites predate enforced
// constraints. The list of such migrations has been frozen since 2025-07-02:
// no new migration may run with foreign keys disabled.
const lastMigrationRequiringFKDisabled = "202501311657"

var (
	errMigrationHistoryMissing = errors.New(
		"database has tables but no migration history; " +
			"it was not created by headscale 0.25 or later",
	)
	errForeignKeyConstraintsViolated = errors.New("foreign key constraints violated")
)

// runMigrations replays the pending migrations in order, each in its own
// transaction. Fresh databases are created by [HSDatabase.initSchema]
// instead, which records the whole history as applied.
func (hsdb *HSDatabase) runMigrations(migrations []migration) error {
	err := validateMigrationIDs(migrations)
	if err != nil {
		return err
	}

	applied, err := hsdb.appliedMigrations()
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		return errMigrationHistoryMissing
	}

	if hsdb.ex.dialect == dialectSQLite {
		return hsdb.runSQLiteMigrations(migrations, applied)
	}

	_, err = hsdb.applyPending(migrations, applied)

	return err
}

func validateMigrationIDs(migrations []migration) error {
	seen := make(map[string]struct{}, len(migrations))

	for _, m := range migrations {
		if m.id == "" {
			return errors.New("migration with empty id") //nolint:err113 // programming error caught by tests
		}

		if m.id == initSchemaMigrationID {
			return fmt.Errorf("migration id %q is reserved", m.id) //nolint:err113 // programming error caught by tests
		}

		if _, dup := seen[m.id]; dup {
			return fmt.Errorf("duplicate migration id %q", m.id) //nolint:err113 // programming error caught by tests
		}

		seen[m.id] = struct{}{}
	}

	return nil
}

// initSchema creates every table from the dialect's schema file, which
// includes the migrations table, and records the whole history as applied.
func (hsdb *HSDatabase) initSchema(migrations []migration) error {
	tables, err := hsdb.userTableCount()
	if err != nil {
		return err
	}

	if tables > 0 {
		return errMigrationHistoryMissing
	}

	log.Info().Msg("creating database schema")

	return hsdb.Write(func(tx *Tx) error {
		_, err := tx.ex.execRaw(hsdb.ex.dialect.schema())
		if err != nil {
			return fmt.Errorf("creating schema: %w", err)
		}

		err = tx.markApplied(initSchemaMigrationID)
		if err != nil {
			return err
		}

		for _, m := range migrations {
			err = tx.markApplied(m.id)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// userTableCount counts the tables that are not SQLite or litestream
// internals, to tell an empty database from a foreign one.
func (hsdb *HSDatabase) userTableCount() (int, error) {
	query := `SELECT count(*) FROM sqlite_master WHERE type = 'table' ` +
		`AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '\_litestream\_%' ESCAPE '\'`
	if hsdb.ex.dialect == dialectPostgres {
		query = `SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema()`
	}

	var count int

	err := hsdb.ex.scanRow(query, nil, &count)
	if err != nil {
		return 0, fmt.Errorf("counting tables: %w", err)
	}

	return count, nil
}

func (hsdb *HSDatabase) appliedMigrations() (map[string]struct{}, error) {
	rows, err := hsdb.ex.queryRaw(`SELECT id FROM migrations`)
	if err != nil {
		return nil, fmt.Errorf("reading migration history: %w", err)
	}

	defer func() { _ = rows.Close() }()

	applied := map[string]struct{}{}

	for rows.Next() {
		var id string

		err = rows.Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("scanning migration id: %w", err)
		}

		applied[id] = struct{}{}
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("reading migration history: %w", err)
	}

	return applied, nil
}

// runSQLiteMigrations replays the early, constraint-rewriting migrations
// with foreign keys off, then the rest with foreign keys on, and finally
// checks that no constraint was left violated.
func (hsdb *HSDatabase) runSQLiteMigrations(migrations []migration, applied map[string]struct{}) error {
	split := 1 + slices.IndexFunc(migrations, func(m migration) bool {
		return m.id == lastMigrationRequiringFKDisabled
	})

	_, err := hsdb.ex.execRaw("PRAGMA foreign_keys = OFF")
	if err != nil {
		return fmt.Errorf("disabling foreign keys: %w", err)
	}

	ranWithoutFKs, err := hsdb.applyPending(migrations[:split], applied)
	if err != nil {
		return err
	}

	_, err = hsdb.ex.execRaw("PRAGMA foreign_keys = ON")
	if err != nil {
		return fmt.Errorf("restoring foreign keys: %w", err)
	}

	ranWithFKs, err := hsdb.applyPending(migrations[split:], applied)
	if err != nil {
		return err
	}

	// The check scans every table that carries a foreign key, so a start
	// that ran no migration skips it: the previous start already checked.
	if ranWithoutFKs == 0 && ranWithFKs == 0 {
		return nil
	}

	return hsdb.checkForeignKeyViolations()
}

// applyPending runs, in order, every migration not yet recorded as applied,
// each inside its own transaction together with its history row, and
// reports how many ran.
func (hsdb *HSDatabase) applyPending(migrations []migration, applied map[string]struct{}) (int, error) {
	ran := 0

	for _, m := range migrations {
		if _, done := applied[m.id]; done {
			continue
		}

		log.Info().Str("migration", m.id).Msg("running database migration")

		err := hsdb.Write(func(tx *Tx) error {
			err := m.run(tx)
			if err != nil {
				return err
			}

			return tx.markApplied(m.id)
		})
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", m.id, err)
		}

		applied[m.id] = struct{}{}
		ran++
	}

	return ran, nil
}

func (t *Tx) markApplied(id string) error {
	_, err := t.ex.execRaw(`INSERT INTO migrations (id) VALUES ($1)`, id)
	if err != nil {
		return fmt.Errorf("recording migration %s: %w", id, err)
	}

	return nil
}

// checkForeignKeyViolations reports an error when the SQLite migrations
// left any foreign key constraint violated.
func (hsdb *HSDatabase) checkForeignKeyViolations() error {
	rows, err := hsdb.ex.queryRaw("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("checking foreign key constraints: %w", err)
	}

	defer func() { _ = rows.Close() }()

	violations := 0

	for rows.Next() {
		var (
			table, parent   string
			rowID           *int64
			constraintIndex int
		)

		err = rows.Scan(&table, &rowID, &parent, &constraintIndex)
		if err != nil {
			return fmt.Errorf("scanning foreign key violation: %w", err)
		}

		violations++

		e := log.Error().Str("table", table).Str("parent", parent)
		if rowID != nil {
			e = e.Int64("row_id", *rowID)
		}

		e.Msg("Foreign key constraint violated")
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterating foreign key check rows: %w", err)
	}

	if violations > 0 {
		return errForeignKeyConstraintsViolated
	}

	return nil
}

// ensureDatabaseVersionTable creates the database_versions table on
// databases that predate it. It runs before the migrations, so it cannot be
// one of them.
func (hsdb *HSDatabase) ensureDatabaseVersionTable() error {
	return hsdb.ex.ensureDatabaseVersionTable()
}

func (e *executor) ensureDatabaseVersionTable() error {
	exists, err := e.hasTable("database_versions")
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	ddl := `CREATE TABLE database_versions(
  id integer PRIMARY KEY,
  version text NOT NULL,
  updated_at datetime
)`
	if e.dialect == dialectPostgres {
		ddl = `CREATE TABLE database_versions(
  id bigserial PRIMARY KEY,
  version text NOT NULL,
  updated_at timestamptz
)`
	}

	_, err = e.execRaw(ddl)
	if err != nil {
		return fmt.Errorf("creating database version table: %w", err)
	}

	return nil
}

// isFreshDatabase reports whether the database has no migration history
// yet, so the schema will be created from scratch.
func (hsdb *HSDatabase) isFreshDatabase() (bool, error) {
	exists, err := hsdb.ex.hasTable("migrations")
	if err != nil {
		return false, err
	}

	return !exists, nil
}
