package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// dialect identifies the SQL dialect of the open database. Every statement
// is built once with jet's SQLite dialect; [postgresPlaceholders] adapts the
// rendered text for PostgreSQL, so queries are written a single time.
type dialect uint8

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

// ErrNotFound is returned by lookups that match no row.
var ErrNotFound = errors.New("record not found")

// QueryHook observes every statement before it runs and can abort it by
// returning an error. It exists for tests that need to count or fail
// specific writes; see [HSDatabase.SetQueryHook].
type QueryHook func(query string) error

// Querier is the handle query functions run on: the connection pool
// ([*HSDatabase]) or an open transaction ([*Tx]).
type Querier interface {
	executor() *executor
}

// statement is the part of jet's statement API the executor needs.
type statement interface {
	Sql() (string, []any)
}

// dbtx is the common surface of [*sql.DB] and [*sql.Tx].
type dbtx interface {
	qrm.DB
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// executor runs statements on a pool or a transaction.
type executor struct {
	//nolint:containedctx // scoped to the pool or transaction it belongs to, like database/sql's own Tx
	ctx     context.Context
	db      dbtx
	tx      *sql.Tx // set when db is a transaction
	dialect dialect
	log     *queryLog
	hook    *atomic.Pointer[QueryHook]
	stmts   *stmtCache
}

// stmtCache keeps one prepared [*sql.Stmt] per distinct query text. Built
// statements come from a fixed set of call sites, so the set of texts is
// small and each is reused for the life of the process; preparing once
// saves SQLite a parse and compile per execution. database/sql re-prepares
// a statement on any connection that has not seen it, so the cache is
// safe with any pool size.
type stmtCache struct {
	pool *sql.DB
	mu   sync.Mutex
	m    map[string]*sql.Stmt
}

// stmtCacheLimit bounds the cache; reaching it drops every entry, which
// only costs a re-prepare and keeps an unexpected query shape from growing
// the map without end.
const stmtCacheLimit = 512

func newStmtCache(pool *sql.DB) *stmtCache {
	return &stmtCache{pool: pool, m: make(map[string]*sql.Stmt)}
}

// lookup returns the cached statement for query, or nil.
func (c *stmtCache) lookup(query string) *sql.Stmt {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.m[query]
}

// add prepares query on the pool and caches it. Preparing takes a pool
// connection and may wait for one, so it happens outside the mutex: a
// transaction that already holds the connection must still be able to
// call [stmtCache.lookup], otherwise the two wait on each other forever
// when the pool has a single connection, as it does for SQLite. Two
// callers may prepare the same text at once; the loser closes its copy.
// add must not be called while a transaction holds the only connection;
// see [executor.runner].
func (c *stmtCache) add(ctx context.Context, query string) (*sql.Stmt, error) {
	if stmt := c.lookup(query); stmt != nil {
		return stmt, nil
	}

	stmt, err := c.pool.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("preparing statement: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if cached, ok := c.m[query]; ok {
		// Another caller prepared the same text first; the cache owns
		// that copy and closeLocked releases it.
		//nolint:sqlclosecheck // duplicate is released here, not deferred
		_ = stmt.Close()

		return cached, nil
	}

	if len(c.m) >= stmtCacheLimit {
		c.closeLocked()
	}

	c.m[query] = stmt

	return stmt, nil
}

func (c *stmtCache) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closeLocked()
}

func (c *stmtCache) closeLocked() {
	for query, stmt := range c.m {
		_ = stmt.Close()

		delete(c.m, query)
	}
}

// preparedDB runs a prepared statement behind the [qrm.DB] interface,
// whose query text argument is the one the statement was prepared from.
type preparedDB struct {
	stmt *sql.Stmt
}

// Exec is required by [qrm.DB]; jet only calls the context variant.
//
//nolint:noctx // the interface demands the context-free method
func (p preparedDB) Exec(_ string, args ...any) (sql.Result, error) {
	res, err := p.stmt.Exec(args...)
	if err != nil {
		return nil, fmt.Errorf("executing prepared statement: %w", err)
	}

	return res, nil
}

func (p preparedDB) ExecContext(ctx context.Context, _ string, args ...any) (sql.Result, error) {
	res, err := p.stmt.ExecContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("executing prepared statement: %w", err)
	}

	return res, nil
}

// Query is required by [qrm.DB]; jet only calls the context variant.
//
//nolint:noctx // the interface demands the context-free method
func (p preparedDB) Query(_ string, args ...any) (*sql.Rows, error) {
	rows, err := p.stmt.Query(args...)
	if err != nil {
		return nil, fmt.Errorf("querying prepared statement: %w", err)
	}

	return rows, nil
}

func (p preparedDB) QueryContext(ctx context.Context, _ string, args ...any) (*sql.Rows, error) {
	rows, err := p.stmt.QueryContext(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("querying prepared statement: %w", err)
	}

	return rows, nil
}

// prepare renders stmt for the executor's dialect, runs the query hook and
// returns the prepared statement to run it on. The returned qrm.DB is the
// plain pool or transaction when nothing is cached for the query.
func (e *executor) prepare(stmt statement) (string, []any, qrm.DB, error) {
	query, args := stmt.Sql()

	// jet renders with a leading newline; trimming keeps the hook, the log
	// and the statement cache keyed on the statement itself.
	query = strings.TrimSpace(query)
	if e.dialect == dialectPostgres {
		query = postgresPlaceholders(query)
	}

	err := e.runHook(query)
	if err != nil {
		return "", nil, nil, err
	}

	runner, err := e.runner(query)
	if err != nil {
		return "", nil, nil, err
	}

	return query, args, runner, nil
}

// runner returns what executes query: its cached prepared statement, bound
// to the transaction when there is one, or the plain pool or transaction.
// A miss inside a transaction runs unprepared: preparing on the pool would
// wait for a connection that the transaction itself holds when the pool
// has a single one, as it does for SQLite. The statement is prepared and
// cached by the next use outside a transaction.
func (e *executor) runner(query string) (qrm.DB, error) {
	if e.stmts == nil {
		return e.db, nil
	}

	stmt := e.stmts.lookup(query)

	switch {
	case stmt != nil && e.tx != nil:
		return preparedDB{stmt: e.tx.StmtContext(e.ctx, stmt)}, nil
	case stmt != nil:
		return preparedDB{stmt: stmt}, nil
	case e.tx != nil:
		return e.db, nil
	}

	stmt, err := e.stmts.add(e.ctx, query)
	if err != nil {
		return nil, err
	}

	return preparedDB{stmt: stmt}, nil
}

func (e *executor) runHook(query string) error {
	if e.hook == nil {
		return nil
	}

	hook := e.hook.Load()
	if hook == nil {
		return nil
	}

	return (*hook)(query)
}

// query runs stmt and maps its result set into dest, a pointer to a struct
// or to a slice of structs. A struct destination with no matching row
// yields [ErrNotFound]; a slice destination is left empty.
func (e *executor) query(stmt statement, dest any) error {
	query, args, runner, err := e.prepare(stmt)
	if err != nil {
		return err
	}

	start := time.Now()

	rows, err := qrm.Query(e.ctx, runner, query, args, dest)
	err = translateErr(err)

	e.log.trace(start, query, args, rows, err)

	return err
}

// exec runs stmt and returns the number of rows it affected.
func (e *executor) exec(stmt statement) (int64, error) {
	query, args, runner, err := e.prepare(stmt)
	if err != nil {
		return 0, err
	}

	start := time.Now()

	var affected int64

	res, err := runner.ExecContext(e.ctx, query, args...)
	if err == nil {
		affected, err = rowsAffected(res)
	}

	e.log.trace(start, query, args, affected, err)

	return affected, err
}

func rowsAffected(res sql.Result) (int64, error) {
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reading affected rows: %w", err)
	}

	return affected, nil
}

// execRaw runs hand-written SQL. Placeholders are written as $1..$n, which
// both drivers accept, so raw statements need no dialect rewriting.
func (e *executor) execRaw(query string, args ...any) (int64, error) {
	err := e.runHook(query)
	if err != nil {
		return 0, err
	}

	start := time.Now()

	var affected int64

	res, err := e.db.ExecContext(e.ctx, query, args...)
	if err == nil {
		affected, err = rowsAffected(res)
	}

	e.log.trace(start, query, args, affected, err)

	return affected, err
}

// queryRaw runs hand-written SQL and returns its rows. The caller closes them.
func (e *executor) queryRaw(query string) (*sql.Rows, error) {
	err := e.runHook(query)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	rows, err := e.db.QueryContext(e.ctx, query)
	e.log.trace(start, query, nil, 0, err)

	if err != nil {
		return nil, fmt.Errorf("querying: %w", err)
	}

	return rows, nil
}

// scanRow runs hand-written SQL expected to return one row and scans it
// into dest. No row yields [ErrNotFound].
func (e *executor) scanRow(query string, args []any, dest ...any) error {
	err := e.runHook(query)
	if err != nil {
		return err
	}

	start := time.Now()

	err = translateErr(e.db.QueryRowContext(e.ctx, query, args...).Scan(dest...))
	e.log.trace(start, query, args, 1, err)

	return err
}

// translateErr maps the drivers' and jet's empty-result errors onto
// [ErrNotFound].
func translateErr(err error) error {
	if errors.Is(err, qrm.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}

	return err
}

// postgresPlaceholders rewrites a statement rendered by jet's SQLite dialect
// for PostgreSQL: positional "?" placeholders become "$n" and identifiers
// quoted with backticks use double quotes. Single-quoted literals are
// copied through untouched, with ” as the escaped quote.
func postgresPlaceholders(query string) string {
	var out strings.Builder

	out.Grow(len(query) + len(query)/8)

	n := 0
	inLiteral := false

	for i := range len(query) {
		c := query[i]

		switch {
		case c == '\'':
			inLiteral = !inLiteral

			out.WriteByte(c)
		case inLiteral:
			out.WriteByte(c)
		case c == '?':
			n++

			out.WriteByte('$')
			out.WriteString(strconv.Itoa(n))
		case c == '`':
			out.WriteByte('"')
		default:
			out.WriteByte(c)
		}
	}

	return out.String()
}

// queryLog logs every statement when database debugging is enabled. A nil
// *queryLog logs nothing.
type queryLog struct {
	slowThreshold time.Duration
	logNotFound   bool
	parameterized bool
}

func newQueryLog(cfg types.QueryLogConfig) *queryLog {
	if !cfg.Enabled {
		return nil
	}

	return &queryLog{
		slowThreshold: cfg.SlowThreshold,
		logNotFound:   cfg.LogNotFound,
		parameterized: cfg.Parameterized,
	}
}

func (l *queryLog) trace(start time.Time, query string, args []any, rows int64, err error) {
	if l == nil {
		return
	}

	elapsed := time.Since(start)

	var ev *zerolog.Event

	switch {
	case err != nil && (l.logNotFound || !errors.Is(err, ErrNotFound)):
		ev = log.Error().Err(err)
	case l.slowThreshold != 0 && elapsed > l.slowThreshold:
		ev = log.Warn()
	default:
		ev = log.Debug()
	}

	ev = ev.Dur("duration", elapsed).Str("sql", query).Int64("rows", rows)
	if !l.parameterized {
		ev = ev.Interface("args", args)
	}

	ev.Msg("database statement")
}

// Tx is an open transaction. Every query function that accepts a [Querier]
// runs on it, and the transaction commits or rolls back when the enclosing
// [HSDatabase.Write] or [HSDatabase.Read] returns.
type Tx struct {
	ex executor
	tx *sql.Tx
}

// Exec runs hand-written SQL inside the transaction. Placeholders are
// $1..$n on both databases. It is meant for tests and migrations; regular
// code goes through the typed query functions.
func (t *Tx) Exec(query string, args ...any) (int64, error) {
	return t.ex.execRaw(query, args...)
}

func (t *Tx) executor() *executor { return &t.ex }

func (hsdb *HSDatabase) executor() *executor { return &hsdb.ex }

// begin opens a transaction on the pool's context. Query functions carry no
// context of their own; the pool's is the process lifetime.
func (hsdb *HSDatabase) begin() (*Tx, error) {
	ctx := hsdb.ex.ctx

	tx, err := hsdb.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}

	return &Tx{
		ex: executor{
			ctx:     ctx,
			db:      tx,
			tx:      tx,
			dialect: hsdb.ex.dialect,
			log:     hsdb.ex.log,
			hook:    hsdb.ex.hook,
			stmts:   hsdb.ex.stmts,
		},
		tx: tx,
	}, nil
}

// Read runs fn inside a transaction that is always rolled back, giving a
// consistent snapshot for several reads.
func (hsdb *HSDatabase) Read(fn func(rx *Tx) error) error {
	_, err := Read(hsdb, func(rx *Tx) (struct{}, error) {
		return struct{}{}, fn(rx)
	})

	return err
}

// Read runs fn inside a read transaction on hsdb and returns its result;
// see [HSDatabase.Read].
func Read[T any](hsdb *HSDatabase, fn func(rx *Tx) (T, error)) (T, error) {
	var zero T

	rx, err := hsdb.begin()
	if err != nil {
		return zero, err
	}

	defer func() { _ = rx.tx.Rollback() }()

	return fn(rx)
}

// Write runs fn inside a transaction that commits when fn returns nil and
// rolls back otherwise.
func (hsdb *HSDatabase) Write(fn func(tx *Tx) error) error {
	_, err := Write(hsdb, func(tx *Tx) (struct{}, error) {
		return struct{}{}, fn(tx)
	})

	return err
}

// Write runs fn inside a write transaction on hsdb and returns its result;
// see [HSDatabase.Write].
func Write[T any](hsdb *HSDatabase, fn func(tx *Tx) (T, error)) (T, error) {
	var zero T

	tx, err := hsdb.begin()
	if err != nil {
		return zero, err
	}

	defer func() { _ = tx.tx.Rollback() }()

	ret, err := fn(tx)
	if err != nil {
		return zero, err
	}

	err = tx.tx.Commit()
	if err != nil {
		return zero, fmt.Errorf("committing transaction: %w", err)
	}

	return ret, nil
}

// SetQueryHook installs hook for every statement run through hsdb and its
// transactions, replacing any previous hook; nil removes it. It is only
// available to tests.
func (hsdb *HSDatabase) SetQueryHook(hook QueryHook) {
	if !testing.Testing() {
		panic("SetQueryHook can only be called during tests")
	}

	if hook == nil {
		hsdb.ex.hook.Store(nil)

		return
	}

	hsdb.ex.hook.Store(&hook)
}
