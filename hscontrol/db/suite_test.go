package db

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"github.com/stapelberg/postgrestest"
)

func newSQLiteTestDB() (*HSDatabase, error) {
	tmpDir, err := os.MkdirTemp("", "slopscale-db-test-*")
	if err != nil {
		return nil, err
	}

	log.Info().Str("path", tmpDir+"/slopscale_test.db").Msg("database path")

	db, err := NewSlopscaleDatabase(
		&types.Config{
			Database: types.DatabaseConfig{
				Type: types.DatabaseSqlite,
				Sqlite: types.SqliteConfig{
					Path: tmpDir + "/slopscale_test.db",
				},
			},
			Policy: types.PolicyConfig{
				Mode: types.PolicyModeDB,
			},
		},
	)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func newPostgresTestDB(t *testing.T) *HSDatabase {
	t.Helper()

	return newSlopscaleDBFromPostgresURL(t, newPostgresDBForTest(t))
}

func newPostgresDBForTest(t *testing.T) *url.URL {
	t.Helper()

	ctx := t.Context()

	srv, err := postgrestest.Start(ctx, postgrestest.WithSQLDriver("pgx"))
	if err != nil {
		t.Skipf("start postgres: %s", err)
	}

	t.Cleanup(srv.Cleanup)

	u, err := srv.CreateDatabase(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("created local postgres: %s", u)
	pu, _ := url.Parse(u)

	return pu
}

func newSlopscaleDBFromPostgresURL(t *testing.T, pu *url.URL) *HSDatabase {
	t.Helper()

	pass, _ := pu.User.Password()
	port, _ := strconv.Atoi(pu.Port())

	// postgrestest listens on a unix socket only and carries its
	// directory in the host query parameter; libpq treats a host
	// starting with / as a socket directory.
	host := pu.Hostname()
	if host == "" {
		host = pu.Query().Get("host")
	}

	db, err := NewSlopscaleDatabase(
		&types.Config{
			Database: types.DatabaseConfig{
				Type: types.DatabasePostgres,
				Postgres: types.PostgresConfig{
					Host: host,
					User: pu.User.Username(),
					Name: strings.TrimLeft(pu.Path, "/"),
					Pass: pass,
					Port: port,
					Ssl:  "disable",
				},
			},
			Policy: types.PolicyConfig{
				Mode: types.PolicyModeDB,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return db
}
