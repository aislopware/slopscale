// Command gen-jet regenerates the go-jet SQL builder tables in gen/jet from
// hscontrol/db/schema.sql, the SQLite source of truth for the database
// schema. It loads the schema into a scratch SQLite database with the same
// pure-Go driver the server uses and runs jet's generator against it, so the
// table definitions can never drift from schema.sql.
//
// Only the SQL builder is generated. Row models are hand-written in
// hscontrol/db, where the domain types and their (de)serialisation live.
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/go-jet/jet/v2/generator/metadata"
	sqlitegen "github.com/go-jet/jet/v2/generator/sqlite"
	"github.com/go-jet/jet/v2/generator/template"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/hscontrol/db/sqliteconfig"
)

var errEmptySchema = errors.New("schema file is empty")

func main() {
	schemaPath := flag.String("schema", "hscontrol/db/schema.sql", "SQLite schema to generate tables from")
	outDir := flag.String("out", "gen/jet", "directory the table package is written to")
	flag.Parse()

	err := run(*schemaPath, *outDir)
	if err != nil {
		log.Fatal(err)
	}
}

func run(schemaPath, outDir string) error {
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return fmt.Errorf("reading schema: %w", err)
	}

	if len(schema) == 0 {
		return fmt.Errorf("%s: %w", schemaPath, errEmptySchema)
	}

	dir, err := os.MkdirTemp("", "gen-jet-*")
	if err != nil {
		return fmt.Errorf("creating scratch directory: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	db, err := sql.Open(sqliteconfig.DriverName, filepath.Join(dir, "schema.sqlite"))
	if err != nil {
		return fmt.Errorf("opening scratch database: %w", err)
	}

	defer func() { _ = db.Close() }()

	_, err = db.Exec(string(schema))
	if err != nil {
		return fmt.Errorf("loading schema: %w", err)
	}

	err = sqlitegen.GenerateDB(db, outDir, generatorTemplate())
	if err != nil {
		return fmt.Errorf("generating tables: %w", err)
	}

	return nil
}

// generatorTemplate keeps jet's default SQL builder output but skips the
// model package and types SQLite's untyped "numeric" boolean columns as
// booleans, so comparisons take Go bools instead of floats.
func generatorTemplate() template.Template {
	return template.Default(sqlite.Dialect).
		UseSchema(func(schema metadata.Schema) template.Schema {
			return template.DefaultSchema(schema).
				UseModel(template.DefaultModel().ShouldSkip(true)).
				UseSQLBuilder(template.DefaultSQLBuilder().
					UseTable(func(table metadata.Table) template.TableSQLBuilder {
						return template.DefaultTableSQLBuilder(table).
							UseColumn(func(column metadata.Column) template.TableSQLBuilderColumn {
								col := template.DefaultTableSQLBuilderColumn(column)
								if column.DataType.Name == "numeric" {
									col.Type = "Bool"
								}

								return col
							})
					}))
		})
}
