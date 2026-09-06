package sqliteconfig

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// ErrNotSQLiteConn is returned when the driver hands back a connection of an
// unexpected type; it cannot happen with mattn/go-sqlite3 and exists so the
// type assertion is not a silent panic.
var ErrNotSQLiteConn = errors.New("sqlite driver returned a non-sqlite connection")

// Connector opens connections for one [Config]. Every connection it returns
// has the configuration's pragmas applied, so a pool that reconnects after
// an error or an idle timeout keeps the same settings.
type Connector struct {
	dsn     string
	pragmas []string
	driver  *sqlite3.SQLiteDriver
}

// NewConnector validates cfg and returns a [Connector] for it.
func NewConnector(cfg *Config) (*Connector, error) {
	err := cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Connector{
		dsn:     cfg.DSN(),
		pragmas: cfg.Pragmas(),
		driver:  &sqlite3.SQLiteDriver{},
	}, nil
}

// Open returns a database/sql pool for cfg. It never touches the file; the
// first connection is made on first use, as with [sql.Open].
func Open(cfg *Config) (*sql.DB, error) {
	connector, err := NewConnector(cfg)
	if err != nil {
		return nil, err
	}

	return sql.OpenDB(connector), nil
}

// DSN returns the data source name the connector opens.
func (c *Connector) DSN() string {
	return c.dsn
}

// Pragmas returns the statements run on every new connection.
func (c *Connector) Pragmas() []string {
	return c.pragmas
}

// Connect implements [driver.Connector].
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite connection: %w", err)
	}

	sqliteConn, ok := conn.(*sqlite3.SQLiteConn)
	if !ok {
		_ = conn.Close()

		return nil, fmt.Errorf("%w: %T", ErrNotSQLiteConn, conn)
	}

	for _, pragma := range c.pragmas {
		_, err = sqliteConn.ExecContext(ctx, pragma, nil)
		if err != nil {
			_ = conn.Close()

			return nil, fmt.Errorf("applying %q: %w", pragma, err)
		}
	}

	return conn, nil
}

// Driver implements [driver.Connector].
func (c *Connector) Driver() driver.Driver {
	return c.driver
}

// Hardening reports which compile-time hardening the linked SQLite library
// carries. Both switches come from sqlite.cflags at build time; a binary
// built without them still works, it just accepts SQL that a hardened one
// refuses.
type Hardening struct {
	// Defensive is SQLITE_DEFAULT_DEFENSIVE: PRAGMA writable_schema,
	// journal_mode=OFF, schema_version writes and direct shadow-table
	// writes are refused, closing the SQL-level corruption vectors.
	Defensive bool
	// StrictDoubleQuotes is SQLITE_DQS=0: an unknown double-quoted
	// identifier is an error instead of a silently substituted string.
	StrictDoubleQuotes bool
}

// Complete reports whether every hardening switch is present.
func (h Hardening) Complete() bool {
	return h.Defensive && h.StrictDoubleQuotes
}

// ProbeHardening checks the behaviour of one pooled connection to find out
// which hardening the library was compiled with. It leaves the connection
// as it found it.
func ProbeHardening(ctx context.Context, db *sql.DB) (Hardening, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return Hardening{}, fmt.Errorf("acquiring connection: %w", err)
	}

	defer func() { _ = conn.Close() }()

	defensive, err := probeDefensive(ctx, conn)
	if err != nil {
		return Hardening{}, err
	}

	strict, err := probeStrictDoubleQuotes(ctx, conn)
	if err != nil {
		return Hardening{}, err
	}

	return Hardening{Defensive: defensive, StrictDoubleQuotes: strict}, nil
}

// probeDefensive asks for a writable schema; a defensive library refuses
// silently, so the pragma reads back as off.
func probeDefensive(ctx context.Context, conn *sql.Conn) (bool, error) {
	_, err := conn.ExecContext(ctx, "PRAGMA writable_schema = ON")
	if err != nil {
		return false, fmt.Errorf("probing writable_schema: %w", err)
	}

	var writable int

	err = conn.QueryRowContext(ctx, "PRAGMA writable_schema").Scan(&writable)
	if err != nil {
		return false, fmt.Errorf("reading writable_schema: %w", err)
	}

	if writable == 0 {
		return true, nil
	}

	_, err = conn.ExecContext(ctx, "PRAGMA writable_schema = OFF")
	if err != nil {
		return false, fmt.Errorf("restoring writable_schema: %w", err)
	}

	return false, nil
}

// probeStrictDoubleQuotes selects an unknown double-quoted identifier; a
// strict library reports a missing column, a lax one returns the name as a
// string.
func probeStrictDoubleQuotes(ctx context.Context, conn *sql.Conn) (bool, error) {
	var out string

	err := conn.QueryRowContext(ctx, `SELECT "hardening_probe_no_such_column"`).Scan(&out)
	if err == nil {
		return false, nil
	}

	if strings.Contains(err.Error(), "no such column") {
		return true, nil
	}

	return false, fmt.Errorf("probing double-quoted strings: %w", err)
}
