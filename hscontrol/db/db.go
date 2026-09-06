package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/juanfont/headscale/hscontrol/db/sqliteconfig"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/rs/zerolog/log"
	"github.com/tailscale/squibble"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

//go:generate go run ../../cmd/gen-jet -schema schema.sql -out ../../gen/jet

var errDatabaseNotSupported = errors.New("database type not supported")

const (
	maxIdleConns   = 100
	maxOpenConns   = 100
	contextTimeout = 10 * time.Second

	// postgresLifetimeJitter spreads connection expiry so the pool is not
	// emptied all at once when the connections opened at startup reach
	// their lifetime together.
	postgresLifetimeJitter = 5 * time.Minute
)

// HSDatabase is the open database. Typed query functions and
// [HSDatabase.Read]/[HSDatabase.Write] cover regular use; DB is the
// underlying pool for callers that need raw SQL, such as tests.
type HSDatabase struct {
	DB  *sql.DB
	ex  executor
	cfg *types.Config
}

// NewHeadscaleDatabase opens the configured database, creates the schema on
// a new database or applies the pending migrations on an existing one, and
// validates the result against schema.sql on SQLite.
func NewHeadscaleDatabase(cfg *types.Config) (*HSDatabase, error) {
	hsdb, err := openDB(cfg)
	if err != nil {
		return nil, err
	}

	err = hsdb.prepareSchema(migrations(cfg))
	if err != nil {
		_ = hsdb.DB.Close()

		return nil, err
	}

	return hsdb, nil
}

func openDB(cfg *types.Config) (*HSDatabase, error) {
	var (
		pool *sql.DB
		d    dialect
		err  error
	)

	switch cfg.Database.Type {
	case types.DatabaseSqlite:
		pool, err = openSQLite(cfg.Database.Sqlite)
		d = dialectSQLite
	case types.DatabasePostgres:
		pool, err = openPostgres(cfg.Database.Postgres)
		d = dialectPostgres
	default:
		return nil, fmt.Errorf(
			"database of type %s is not supported: %w",
			cfg.Database.Type,
			errDatabaseNotSupported,
		)
	}

	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()

	err = pool.PingContext(ctx)
	if err != nil {
		_ = pool.Close()

		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	return &HSDatabase{
		DB: pool,
		ex: executor{
			ctx:     context.Background(),
			db:      pool,
			dialect: d,
			log:     newQueryLog(cfg.Database.QueryLog),
			hook:    new(atomic.Pointer[QueryHook]),
			stmts:   newStmtCache(pool),
		},
		cfg: cfg,
	}, nil
}

func openSQLite(cfg types.SqliteConfig) (*sql.DB, error) {
	err := util.EnsureDir(filepath.Dir(cfg.Path))
	if err != nil {
		return nil, fmt.Errorf("creating directory for sqlite: %w", err)
	}

	log.Info().
		Str("database", types.DatabaseSqlite).
		Str("path", cfg.Path).
		Msg("Opening database")

	// Connection-level hardening and pragmas are applied through the URL.
	sqliteConfig := sqliteconfig.Default(cfg.Path)
	if cfg.WriteAheadLog {
		sqliteConfig.JournalMode = sqliteconfig.JournalModeWAL
		sqliteConfig.WALAutocheckpoint = cfg.WALAutoCheckPoint
	}

	connectionURL, err := sqliteConfig.ToURL()
	if err != nil {
		return nil, fmt.Errorf("building sqlite connection URL: %w", err)
	}

	pool, err := sql.Open(sqliteconfig.DriverName, connectionURL)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}

	// SQLite allows one writer at a time and the pragmas above are
	// per-connection, so a single connection is both the fastest and the
	// only configuration in which every statement sees the same settings.
	pool.SetMaxIdleConns(1)
	pool.SetMaxOpenConns(1)
	pool.SetConnMaxIdleTime(time.Hour)

	return pool, nil
}

func openPostgres(cfg types.PostgresConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s dbname=%s user=%s",
		cfg.Host,
		cfg.Name,
		cfg.User,
	)

	log.Info().
		Str("database", types.DatabasePostgres).
		Str("path", dsn).
		Msg("Opening database")

	sslEnabled, err := strconv.ParseBool(cfg.Ssl)
	if err == nil {
		if !sslEnabled {
			dsn += " sslmode=disable"
		}
	} else {
		dsn += " sslmode=" + cfg.Ssl
	}

	if cfg.Port != 0 {
		dsn += " port=" + strconv.Itoa(cfg.Port)
	}

	if cfg.Pass != "" {
		dsn += " password=" + cfg.Pass
	}

	// The pool is pgx's own rather than database/sql's: it keeps a floor of
	// open connections, health-checks idle ones and jitters their lifetime,
	// none of which database/sql does. The *sql.DB on top only adapts the
	// interface; its idle limit is set to zero by OpenDBFromPool.
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres connection string: %w", err)
	}

	//nolint:gosec // G115: bounded by the config validation, far below int32
	poolCfg.MaxConns = int32(max(cfg.MaxOpenConnections, 1))
	//nolint:gosec // G115: bounded by MaxConns
	poolCfg.MinConns = int32(min(cfg.MaxIdleConnections, cfg.MaxOpenConnections))
	poolCfg.MaxConnIdleTime = time.Duration(cfg.ConnMaxIdleTimeSecs) * time.Second
	poolCfg.MaxConnLifetimeJitter = postgresLifetimeJitter

	pgPool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("opening postgres database: %w", err)
	}

	pool := stdlib.OpenDBFromPool(pgPool)
	pool.SetMaxOpenConns(cfg.MaxOpenConnections)

	return pool, nil
}

// PingDB checks that the database answers.
func (hsdb *HSDatabase) PingDB(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	err := hsdb.DB.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}

	return nil
}

// Close vacuums a WAL-mode SQLite database on a best-effort basis and closes
// the pool.
func (hsdb *HSDatabase) Close() error {
	if hsdb.cfg.Database.Type == types.DatabaseSqlite && hsdb.cfg.Database.Sqlite.WriteAheadLog {
		// The vacuum is best effort: a failure must not keep the
		// connection open, so it is logged rather than returned.
		_, err := hsdb.ex.execRaw("VACUUM")
		if err != nil {
			log.Warn().Err(err).Msg("vacuuming sqlite database before close")
		}
	}

	hsdb.ex.stmts.close()

	err := hsdb.DB.Close()
	if err != nil {
		return fmt.Errorf("closing database: %w", err)
	}

	return nil
}

func (hsdb *HSDatabase) prepareSchema(history []migration) error {
	fresh, err := hsdb.isFreshDatabase()
	if err != nil {
		return err
	}

	if fresh {
		err = hsdb.initSchema(history)
		if err != nil {
			return fmt.Errorf("creating schema: %w", err)
		}
	} else {
		err = hsdb.ensureDatabaseVersionTable()
		if err != nil {
			return err
		}

		err = hsdb.checkVersionUpgradePath()
		if err != nil {
			return fmt.Errorf("version check: %w", err)
		}

		err = hsdb.runMigrations(history)
		if err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// Store the current version after migrations succeed. Dev builds skip
	// this to preserve the stored version for the next real versioned
	// binary.
	currentVersion := types.GetVersionInfo().Version
	if !isDev(currentVersion) {
		err = setDatabaseVersion(&hsdb.ex, currentVersion)
		if err != nil {
			return fmt.Errorf("storing database version: %w", err)
		}
	}

	if hsdb.ex.dialect == dialectSQLite {
		err = hsdb.validateSQLiteSchema()
		if err != nil {
			return fmt.Errorf("validating schema: %w", err)
		}
	}

	return nil
}

// validateSQLiteSchema checks the live schema against schema.sql. squibble
// only supports SQLite, which is also the source of truth.
func (hsdb *HSDatabase) validateSQLiteSchema() error {
	// squibble opens several connections; widen the pool for the check.
	hsdb.DB.SetMaxIdleConns(maxIdleConns)
	hsdb.DB.SetMaxOpenConns(maxOpenConns)

	defer hsdb.DB.SetMaxIdleConns(1)
	defer hsdb.DB.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()

	opts := squibble.DigestOptions{
		IgnoreTables: []string{
			// Litestream tables, these are inserted by
			// litestream and not part of our schema
			// https://litestream.io/how-it-works
			"_litestream_lock",
			"_litestream_seq",
		},
	}

	err := squibble.Validate(ctx, hsdb.DB, sqliteSchema, &opts)
	if err != nil {
		return fmt.Errorf("comparing with schema.sql: %w", err)
	}

	return nil
}
