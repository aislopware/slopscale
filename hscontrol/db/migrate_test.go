package db

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/db/sqliteconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateMigrationIDs(t *testing.T) {
	t.Parallel()

	noop := func(*Tx) error { return nil }

	tests := []struct {
		name    string
		ids     []string
		wantErr bool
	}{
		{name: "distinct", ids: []string{"202501010000", "202501020000-x"}},
		{name: "duplicate", ids: []string{"202501010000", "202501010000"}, wantErr: true},
		{name: "reserved", ids: []string{initSchemaMigrationID}, wantErr: true},
		{name: "empty id", ids: []string{""}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			history := make([]migration, 0, len(tt.ids))
			for _, id := range tt.ids {
				history = append(history, migration{id: id, run: noop})
			}

			err := validateMigrationIDs(history)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestProductionMigrationIDsAreValid guards the list against a duplicated
// or reserved id, which would silently skip a migration.
func TestProductionMigrationIDsAreValid(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateMigrationIDs(migrations(nil)))
}

func TestFreshDatabaseIsCreatedFromSchema(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	applied, err := db.appliedMigrations()
	require.NoError(t, err)

	assert.Contains(t, applied, initSchemaMigrationID, "fresh databases record the schema marker")

	for _, m := range migrations(db.cfg) {
		assert.Containsf(t, applied, m.id, "fresh databases mark migration %s as applied", m.id)
	}

	fresh, err := db.isFreshDatabase()
	require.NoError(t, err)
	assert.False(t, fresh)
}

func TestExistingDatabaseWithoutHistoryIsRejected(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	// Drop the history but keep the tables: this is a database the old
	// AutoMigrate path could have produced, which the runner refuses rather
	// than guessing at.
	_, err = db.DB.ExecContext(t.Context(), "DROP TABLE migrations")
	require.NoError(t, err)

	err = db.prepareSchema(migrations(db.cfg))
	require.ErrorIs(t, err, errMigrationHistoryMissing)
}

func TestPendingMigrationsRunOnceEachInOrder(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	var ran []string

	record := func(id string) migration {
		return migration{id: id, run: func(*Tx) error {
			ran = append(ran, id)

			return nil
		}}
	}

	history := append(migrations(db.cfg), record("209901010000-a"), record("209901020000-b"))

	require.NoError(t, db.runMigrations(history))
	assert.Equal(t, []string{"209901010000-a", "209901020000-b"}, ran)

	ran = nil

	require.NoError(t, db.runMigrations(history))
	assert.Empty(t, ran, "applied migrations must not run again")
}

func TestFailedMigrationIsNotRecorded(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	errBoom := errors.New("boom")

	history := append(migrations(db.cfg), migration{id: "209901010000-fail", run: func(tx *Tx) error {
		_, execErr := tx.Exec("CREATE TABLE leftover (id integer)")
		if execErr != nil {
			return execErr
		}

		return errBoom
	}})

	err = db.runMigrations(history)
	require.ErrorIs(t, err, errBoom)

	applied, err := db.appliedMigrations()
	require.NoError(t, err)
	assert.NotContains(t, applied, "209901010000-fail")

	exists, err := db.ex.hasTable("leftover")
	require.NoError(t, err)
	assert.False(t, exists, "a failed migration must roll back its DDL")
}

func TestSchemaHelpersOnBarePool(t *testing.T) {
	t.Parallel()

	pool, err := sql.Open(sqliteconfig.DriverName, "file::memory:")
	require.NoError(t, err)

	t.Cleanup(func() { _ = pool.Close() })

	pool.SetMaxOpenConns(1)

	e := &executor{ctx: t.Context(), db: pool, dialect: dialectSQLite}

	_, err = e.execRaw("CREATE TABLE t (id integer PRIMARY KEY)")
	require.NoError(t, err)

	exists, err := e.hasTable("t")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = e.hasColumn("t", "name")
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, e.addColumnIfMissing("t", "name", typeText))
	require.NoError(t, e.addColumnIfMissing("t", "name", typeText), "adding twice is a no-op")

	exists, err = e.hasColumn("t", "name")
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, e.renameColumn("t", "name", "title"))
	require.NoError(t, e.dropTableIfExists("t"))

	exists, err = e.hasTable("t")
	require.NoError(t, err)
	assert.False(t, exists)
}
