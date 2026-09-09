package db

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/db/sqliteconfig"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "numbers placeholders in order",
			query: "SELECT * FROM nodes WHERE id = ? AND user_id = ?",
			want:  "SELECT * FROM nodes WHERE id = $1 AND user_id = $2",
		},
		{
			name:  "rewrites backtick quoting",
			query: "SELECT pre_auth_keys.`key` FROM pre_auth_keys WHERE `key` = ?",
			want:  `SELECT pre_auth_keys."key" FROM pre_auth_keys WHERE "key" = $1`,
		},
		{
			name:  "leaves placeholders inside string literals alone",
			query: "SELECT * FROM nodes WHERE hostname = 'what?' AND id = ?",
			want:  "SELECT * FROM nodes WHERE hostname = 'what?' AND id = $1",
		},
		{
			name:  "keeps escaped quotes inside literals",
			query: "SELECT * FROM nodes WHERE hostname = 'it''s?' AND id = ?",
			want:  "SELECT * FROM nodes WHERE hostname = 'it''s?' AND id = $1",
		},
		{
			name:  "no placeholders",
			query: "SELECT count(*) FROM nodes",
			want:  "SELECT count(*) FROM nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, postgresPlaceholders(tt.query))
		})
	}
}

func TestStmtCacheEvictsAtLimit(t *testing.T) {
	t.Parallel()

	pool, err := sql.Open(sqliteconfig.DriverName, "file::memory:")
	require.NoError(t, err)

	t.Cleanup(func() { _ = pool.Close() })

	cache := newStmtCache(pool)
	ctx := t.Context()

	first, err := cache.add(ctx, "SELECT 1")
	require.NoError(t, err)

	again, err := cache.add(ctx, "SELECT 1")
	require.NoError(t, err)
	assert.Same(t, first, again, "same query text must reuse the prepared statement")
	assert.Same(t, first, cache.lookup("SELECT 1"))
	assert.Nil(t, cache.lookup("SELECT 2"))

	for i := range stmtCacheLimit {
		//nolint:sqlclosecheck // owned and closed by the cache
		_, addErr := cache.add(ctx, "SELECT "+strconv.Itoa(i+2))
		require.NoError(t, addErr)
	}

	cache.mu.Lock()
	size := len(cache.m)
	cache.mu.Unlock()

	assert.LessOrEqual(t, size, stmtCacheLimit, "cache must stay bounded")

	// Evicted statements are closed; a fresh prepare must still work.
	//nolint:sqlclosecheck // owned and closed by the cache
	_, err = cache.add(ctx, "SELECT 1")
	require.NoError(t, err)

	cache.close()
}

func TestQueryHookAbortsStatement(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	errBlocked := errors.New("blocked by hook")

	var seen []string

	db.SetQueryHook(func(query string) error {
		seen = append(seen, query)

		return errBlocked
	})

	_, err = db.ListUsers(nil)
	require.ErrorIs(t, err, errBlocked)
	require.NotEmpty(t, seen)

	db.SetQueryHook(nil)

	_, err = db.ListUsers(nil)
	require.NoError(t, err)
}

// TestWriteUsesStatementsInsideTransaction guards against the executor
// preparing on the pool while a transaction holds SQLite's only connection,
// which would block forever.
func TestWriteUsesStatementsInsideTransaction(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	done := make(chan error, 1)

	go func() {
		done <- db.Write(func(tx *Tx) error {
			// Both a cache miss (first use) and a hit (second use) on the
			// same statement inside one transaction.
			for range 2 {
				_, listErr := ListUsers(tx, nil)
				if listErr != nil {
					return listErr
				}
			}

			_, createErr := CreateUser(tx, types.User{Name: "in-tx"})

			return createErr
		})
	}()

	select {
	case runErr := <-done:
		require.NoError(t, runErr)
	case <-time.After(30 * time.Second):
		t.Fatal("transaction blocked: statement preparation waited for the pool connection")
	}

	// Outside the transaction the statement is prepared and cached.
	users, err := db.ListUsers(nil)
	require.NoError(t, err)
	assert.Len(t, users, 1)

	_, err = db.ListUsers(nil)
	require.NoError(t, err)

	query, _ := selectUsers().Sql()
	assert.NotNil(t, db.ex.stmts.lookup(strings.TrimSpace(query)))
}

// TestStatementCacheDoesNotBlockTransactionLookup pins the lock order
// between the statement cache and SQLite's single pool connection. A
// pool-level query that misses the cache waits for the connection; a
// transaction that holds the connection must still be able to look up
// its statements while that wait is in progress.
func TestStatementCacheDoesNotBlockTransactionLookup(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })

	inTx := make(chan struct{})
	poolStarted := make(chan struct{})
	txDone := make(chan error, 1)
	poolDone := make(chan error, 1)

	go func() {
		txDone <- db.Write(func(tx *Tx) error {
			close(inTx)
			<-poolStarted

			// Wait until the pool-level query is queued for the connection
			// this transaction holds; database/sql counts that wait.
			waiting := assert.EventuallyWithT(t, func(c *assert.CollectT) {
				assert.Positive(c, db.DB.Stats().WaitCount)
			}, 10*time.Second, 5*time.Millisecond)
			if !waiting {
				return errors.New("pool query never waited for the connection")
			}

			_, listErr := ListUsers(tx, nil)

			return listErr
		})
	}()

	<-inTx

	go func() {
		close(poolStarted)

		_, apiErr := db.ListAPIKeys()
		poolDone <- apiErr
	}()

	for _, done := range []chan error{txDone, poolDone} {
		select {
		case runErr := <-done:
			require.NoError(t, runErr)
		case <-time.After(30 * time.Second):
			t.Fatal("deadlock: statement cache held while waiting for the pool connection")
		}
	}
}
