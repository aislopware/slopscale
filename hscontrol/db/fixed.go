package db

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/go-jet/jet/v2/qrm"
)

// fixedSQL is a statement whose shape never changes between calls: only
// its argument values do. jet serialises a statement on every Sql() call,
// and that serialisation was a third of the allocations of a node fetch,
// so the hot call sites render their text once and pass arguments
// directly. build produces the statement with a placeholder literal for
// every argument, in the order callers later supply them; the argument
// count is checked on every use, and fixed_test.go pins each statement to
// the jet rendering it replaces.
type fixedSQL struct {
	build    func() statement
	once     sync.Once
	sqlite   string
	postgres string
	nargs    int
}

func newFixedSQL(build func() statement) *fixedSQL {
	return &fixedSQL{build: build}
}

// text returns the statement rendered for target and the number of
// arguments it takes.
func (f *fixedSQL) text(target dialect) (string, int) {
	f.once.Do(func() {
		query, args := f.build().Sql()
		f.sqlite = strings.TrimSpace(query)
		f.postgres = postgresPlaceholders(f.sqlite)
		f.nargs = len(args)
	})

	if target == dialectPostgres {
		return f.postgres, f.nargs
	}

	return f.sqlite, f.nargs
}

var errFixedSQLArgs = errors.New("fixed statement argument count")

// prepareFixed runs the query hook for stmt and returns its text and the
// runner to execute it on.
func (e *executor) prepareFixed(stmt *fixedSQL, args []any) (string, qrm.DB, error) {
	query, nargs := stmt.text(e.dialect)
	if len(args) != nargs {
		return "", nil, fmt.Errorf("%w: statement takes %d, got %d", errFixedSQLArgs, nargs, len(args))
	}

	err := e.runHook(query)
	if err != nil {
		return "", nil, err
	}

	runner, err := e.runner(query)
	if err != nil {
		return "", nil, err
	}

	return query, runner, nil
}

// queryFixed is [executor.query] for a fixed statement.
func (e *executor) queryFixed(stmt *fixedSQL, dest any, args ...any) error {
	query, runner, err := e.prepareFixed(stmt, args)
	if err != nil {
		return err
	}

	return e.runQuery(runner, query, args, dest)
}

// execFixed is [executor.exec] for a fixed statement whose affected row
// count the caller does not need.
func (e *executor) execFixed(stmt *fixedSQL, args ...any) error {
	query, runner, err := e.prepareFixed(stmt, args)
	if err != nil {
		return err
	}

	_, err = e.runExec(runner, query, args)

	return err
}

// optional unboxes a nullable column value the way jet does when it reads
// a model: a nil pointer binds NULL, anything else binds the value.
func optional[T any](value *T) any {
	if value == nil {
		return nil
	}

	return *value
}
