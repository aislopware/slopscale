package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

// postgresGoldenPath holds the schema GORM created on PostgreSQL before the
// move to jet, dumped with [dumpPostgresSchema]. schema_postgres.sql must
// produce exactly the same columns, indexes and constraints so databases
// created before and after the move are interchangeable.
const postgresGoldenPath = "testdata/postgres/gorm_schema_golden.txt"

func TestPostgresSchemaMatchesGolden(t *testing.T) {
	t.Parallel()

	db := newPostgresTestDB(t)

	got, err := dumpPostgresSchema(t.Context(), db.DB)
	require.NoError(t, err)

	want, err := os.ReadFile(postgresGoldenPath)
	require.NoError(t, err)

	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("schema_postgres.sql differs from the GORM golden schema (-want +got):\n%s", diff)
	}
}

// dumpPostgresSchema renders every column, index and constraint of the
// current schema in a stable text form.
func dumpPostgresSchema(ctx context.Context, pool *sql.DB) (string, error) {
	var out strings.Builder

	err := dumpPostgresColumns(ctx, pool, &out)
	if err != nil {
		return "", err
	}

	err = dumpPostgresRows(ctx, pool, &out, "IDX",
		`SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() ORDER BY indexdef`)
	if err != nil {
		return "", err
	}

	err = dumpPostgresRows(ctx, pool, &out, "CON",
		`SELECT conrelid::regclass::text || ' ' || conname || ' ' || pg_get_constraintdef(oid) `+
			`FROM pg_constraint WHERE connamespace = current_schema()::regnamespace `+
			`ORDER BY conrelid::regclass::text, conname`)
	if err != nil {
		return "", err
	}

	return out.String(), nil
}

func dumpPostgresColumns(ctx context.Context, pool *sql.DB, out *strings.Builder) error {
	rows, err := pool.QueryContext(
		ctx,
		`SELECT table_name, column_name, data_type, character_maximum_length, is_nullable, column_default `+
			`FROM information_schema.columns WHERE table_schema = current_schema() `+
			`ORDER BY table_name, ordinal_position`,
	)
	if err != nil {
		return err
	}

	defer rows.Close()

	for rows.Next() {
		var (
			table, column, typ, nullable string
			length                       sql.NullInt64
			def                          sql.NullString
		)

		err := rows.Scan(&table, &column, &typ, &length, &nullable, &def)
		if err != nil {
			return err
		}

		fmt.Fprintf(out, "COL %s.%s %s", table, column, typ)

		if length.Valid {
			fmt.Fprintf(out, " len=%d", length.Int64)
		}

		fmt.Fprintf(out, " null=%s", nullable)

		if def.Valid {
			fmt.Fprintf(out, " default=%s", def.String)
		}

		out.WriteByte('\n')
	}

	return rows.Err()
}

func dumpPostgresRows(ctx context.Context, pool *sql.DB, out *strings.Builder, prefix, query string) error {
	rows, err := pool.QueryContext(ctx, query)
	if err != nil {
		return err
	}

	defer rows.Close()

	for rows.Next() {
		var line string

		err := rows.Scan(&line)
		if err != nil {
			return err
		}

		fmt.Fprintf(out, "%s %s\n", prefix, line)
	}

	return rows.Err()
}
