package db

import (
	_ "embed"
	"fmt"
)

// schema.sql is the SQLite source of truth for the database schema; squibble
// validates every SQLite database against it after migrations run and
// cmd/gen-jet derives the SQL builder tables from it. schema_postgres.sql
// mirrors it with PostgreSQL types and is validated by
// TestPostgresSchemaMatchesGolden.
var (
	//go:embed schema.sql
	sqliteSchema string
	//go:embed schema_postgres.sql
	postgresSchema string
)

// columnType is a column type spelled for each dialect.
type columnType struct {
	sqlite   string
	postgres string
}

var (
	typeText      = columnType{sqlite: "text", postgres: "text"}
	typeBlob      = columnType{sqlite: "blob", postgres: "bytea"}
	typeInteger   = columnType{sqlite: "integer", postgres: "bigint"}
	typeTimestamp = columnType{sqlite: "datetime", postgres: "timestamptz"}
)

func (t columnType) forDialect(d dialect) string {
	if d == dialectPostgres {
		return t.postgres
	}

	return t.sqlite
}

// schema returns the DDL that creates a complete, empty database.
func (d dialect) schema() string {
	if d == dialectPostgres {
		return postgresSchema
	}

	return sqliteSchema
}

// Schema introspection and DDL helpers used by migrations. Table and column
// names come from migration code, never from user input.

func (e *executor) hasTable(name string) (bool, error) {
	query := `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = $1`
	if e.dialect == dialectPostgres {
		query = `SELECT count(*) FROM information_schema.tables ` +
			`WHERE table_schema = current_schema() AND table_name = $1`
	}

	var count int

	err := e.scanRow(query, []any{name}, &count)
	if err != nil {
		return false, fmt.Errorf("checking for table %s: %w", name, err)
	}

	return count > 0, nil
}

func (e *executor) hasColumn(table, column string) (bool, error) {
	query := `SELECT count(*) FROM pragma_table_info($1) WHERE name = $2`
	if e.dialect == dialectPostgres {
		query = `SELECT count(*) FROM information_schema.columns ` +
			`WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`
	}

	var count int

	err := e.scanRow(query, []any{table, column}, &count)
	if err != nil {
		return false, fmt.Errorf("checking for column %s.%s: %w", table, column, err)
	}

	return count > 0, nil
}

// addColumnIfMissing adds column to table unless it is already there.
func (e *executor) addColumnIfMissing(table, column string, typ columnType) error {
	exists, err := e.hasColumn(table, column)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	_, err = e.execRaw("ALTER TABLE " + table + " ADD COLUMN " + column + " " + typ.forDialect(e.dialect))
	if err != nil {
		return fmt.Errorf("adding column %s.%s: %w", table, column, err)
	}

	return nil
}

func (e *executor) renameColumn(table, from, to string) error {
	_, err := e.execRaw("ALTER TABLE " + table + " RENAME COLUMN " + from + " TO " + to)
	if err != nil {
		return fmt.Errorf("renaming column %s.%s to %s: %w", table, from, to, err)
	}

	return nil
}

func (e *executor) dropTableIfExists(table string) error {
	query := "DROP TABLE IF EXISTS " + table
	if e.dialect == dialectPostgres {
		query += " CASCADE"
	}

	_, err := e.execRaw(query)
	if err != nil {
		return fmt.Errorf("dropping table %s: %w", table, err)
	}

	return nil
}

func (e *executor) dropIndexIfExists(index string) error {
	_, err := e.execRaw("DROP INDEX IF EXISTS " + index)
	if err != nil {
		return fmt.Errorf("dropping index %s: %w", index, err)
	}

	return nil
}

// execAll runs each statement in order, wrapping a failure with what failed.
func (e *executor) execAll(what string, statements []string) error {
	for _, stmt := range statements {
		_, err := e.execRaw(stmt)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}

	return nil
}

// addForeignKeyIfMissing adds a named foreign key constraint on PostgreSQL
// unless it exists. SQLite cannot add constraints in place; its tables are
// rebuilt by 202507021200 instead.
func (e *executor) addForeignKeyIfMissing(table, name, definition string) error {
	var count int

	err := e.scanRow(
		`SELECT count(*) FROM pg_constraint WHERE conname = $1 AND conrelid = $2::regclass`,
		[]any{name, table}, &count,
	)
	if err != nil {
		return fmt.Errorf("checking for constraint %s: %w", name, err)
	}

	if count > 0 {
		return nil
	}

	_, err = e.execRaw("ALTER TABLE " + table + " ADD CONSTRAINT " + name + " " + definition)
	if err != nil {
		return fmt.Errorf("adding constraint %s: %w", name, err)
	}

	return nil
}
