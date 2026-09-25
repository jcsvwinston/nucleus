// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/db"
)

// Transactional makes every database in dbs run the whole test inside ONE
// transaction that is rolled back when the test ends — Rails' transactional
// tests, for an application whose routes use the pool.
//
// It works one level below the pool: each alias is opened through a driver
// registered for this test alone, which hands database/sql a single
// underlying connection with a transaction already begun, however many
// connections the pool asks for. Transactions the application begins become
// savepoints. At cleanup the outer transaction is rolled back and the
// connection closed, so the database is as it was — schema included, on the
// engines whose DDL is transactional (PostgreSQL, SQLite) — and two tests
// can share one database without seeing each other:
//
//	dbs := nucleustest.Transactional(t, map[string]app.DatabaseConfig{
//	    "default": {URL: os.Getenv("TEST_DATABASE_URL")},
//	})
//	srv := nucleustest.Start(t, nucleus.New().WithDatabases(dbs).Mount(...))
//
// Two things follow from "one connection". Statements are serialised — a
// handler that queries concurrently on the pool queues here instead — and
// the connection is busy while a result set is open: reading rows and
// issuing another statement on the same connection at the same time is the
// one shape of code that behaves differently under Transactional (SQLite
// interleaves, PostgreSQL does not). TempSQLite remains the right tool for
// a test that needs the pool's real concurrency.
func Transactional(tb testing.TB, dbs map[string]app.DatabaseConfig) map[string]app.DatabaseConfig {
	tb.Helper()
	out := make(map[string]app.DatabaseConfig, len(dbs))
	for alias, cfg := range dbs {
		name, dsn, err := db.ResolveDriver(cfg.URL)
		if err != nil {
			tb.Fatalf("nucleustest: Transactional: database %q: %v", alias, err)
		}
		probe, err := sql.Open(name, dsn)
		if err != nil {
			tb.Fatalf("nucleustest: Transactional: database %q: %v", alias, err)
		}
		base := probe.Driver()
		_ = probe.Close()
		d := &txDriver{inner: base, dsn: dsn}
		regName := fmt.Sprintf("nucleustest-tx-%d", txSeq.Add(1))
		sql.Register(regName, d)
		tb.Cleanup(func() {
			if err := d.rollback(); err != nil {
				tb.Errorf("nucleustest: Transactional: roll back database %q: %v", alias, err)
			}
		})
		c := cfg
		c.Driver = regName
		out[alias] = c
	}
	return out
}

var txSeq atomic.Int64

// txDriver is the per-test driver: one real connection, one open
// transaction, shared by every connection database/sql opens through it.
type txDriver struct {
	inner driver.Driver
	dsn   string

	mu     sync.Mutex
	base   driver.Conn
	tx     driver.Tx
	sp     int
	closed bool
}

var errRolledBack = errors.New("nucleustest: the transactional database was rolled back when its test ended")

// Open hands out another view of the one connection, opening it — and
// beginning the transaction — on the first call.
func (d *txDriver) Open(string) (driver.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, errRolledBack
	}
	if d.base == nil {
		conn, err := d.inner.Open(d.dsn)
		if err != nil {
			return nil, err
		}
		var tx driver.Tx
		if bt, ok := conn.(driver.ConnBeginTx); ok {
			tx, err = bt.BeginTx(context.Background(), driver.TxOptions{})
		} else {
			tx, err = conn.Begin()
		}
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("nucleustest: begin the test transaction: %w", err)
		}
		d.base, d.tx = conn, tx
	}
	return &txConn{d: d}, nil
}

func (d *txDriver) rollback() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	var err error
	if d.tx != nil {
		err = d.tx.Rollback()
	}
	if d.base != nil {
		if cerr := d.base.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

// exec runs a statement on the base connection. The caller holds d.mu.
func (d *txDriver) exec(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if d.closed {
		return nil, errRolledBack
	}
	if e, ok := d.base.(driver.ExecerContext); ok {
		res, err := e.ExecContext(ctx, query, args)
		if !errors.Is(err, driver.ErrSkip) {
			return res, err
		}
	}
	st, err := d.base.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = st.Close() }()
	return execStmt(ctx, st, args)
}

// query runs a query on the base connection. The caller holds d.mu.
func (d *txDriver) query(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if d.closed {
		return nil, errRolledBack
	}
	if q, ok := d.base.(driver.QueryerContext); ok {
		rows, err := q.QueryContext(ctx, query, args)
		if !errors.Is(err, driver.ErrSkip) {
			return rows, err
		}
	}
	st, err := d.base.Prepare(query)
	if err != nil {
		return nil, err
	}
	rows, err := queryStmt(ctx, st, args)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	return &stmtRows{Rows: rows, st: st}, nil
}

// stmtRows closes the statement a fallback query prepared once its rows
// are closed.
type stmtRows struct {
	driver.Rows
	st driver.Stmt
}

func (r *stmtRows) Close() error {
	err := r.Rows.Close()
	_ = r.st.Close()
	return err
}

func execStmt(ctx context.Context, st driver.Stmt, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := st.(driver.StmtExecContext); ok {
		return e.ExecContext(ctx, args)
	}
	return st.Exec(plainValues(args)) //nolint:staticcheck // the fallback for drivers without the Context form
}

func queryStmt(ctx context.Context, st driver.Stmt, args []driver.NamedValue) (driver.Rows, error) {
	if q, ok := st.(driver.StmtQueryContext); ok {
		return q.QueryContext(ctx, args)
	}
	return st.Query(plainValues(args)) //nolint:staticcheck // the fallback for drivers without the Context form
}

func plainValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// txConn is one of the pool's connections: a view of the shared one.
type txConn struct{ d *txDriver }

var (
	_ driver.Conn               = (*txConn)(nil)
	_ driver.ConnBeginTx        = (*txConn)(nil)
	_ driver.ExecerContext      = (*txConn)(nil)
	_ driver.QueryerContext     = (*txConn)(nil)
	_ driver.Pinger             = (*txConn)(nil)
	_ driver.SessionResetter    = (*txConn)(nil)
	_ driver.Validator          = (*txConn)(nil)
	_ driver.NamedValueChecker  = (*txConn)(nil)
	_ driver.ConnPrepareContext = (*txConn)(nil)
)

func (c *txConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *txConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	if c.d.closed {
		return nil, errRolledBack
	}
	var (
		st  driver.Stmt
		err error
	)
	if p, ok := c.d.base.(driver.ConnPrepareContext); ok {
		st, err = p.PrepareContext(ctx, query)
	} else {
		st, err = c.d.base.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &txStmt{d: c.d, base: st}, nil
}

// Close releases nothing: the connection outlives the pool's idea of it.
func (c *txConn) Close() error { return nil }

func (c *txConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx opens a savepoint: the application's transaction inside the
// test's.
func (c *txConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	c.d.sp++
	name := fmt.Sprintf("nucleustest_sp_%d", c.d.sp)
	if _, err := c.d.exec(ctx, "SAVEPOINT "+name, nil); err != nil {
		return nil, err
	}
	return &txSavepoint{d: c.d, name: name}, nil
}

func (c *txConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	return c.d.exec(ctx, query, args)
}

func (c *txConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	return c.d.query(ctx, query, args)
}

func (c *txConn) Ping(ctx context.Context) error {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	if c.d.closed {
		return errRolledBack
	}
	if p, ok := c.d.base.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *txConn) ResetSession(context.Context) error {
	if c.d.closed {
		return driver.ErrBadConn
	}
	return nil
}

func (c *txConn) IsValid() bool { return !c.d.closed }

func (c *txConn) CheckNamedValue(nv *driver.NamedValue) error {
	if ck, ok := c.d.base.(driver.NamedValueChecker); ok {
		return ck.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// txSavepoint is the application's transaction, as a savepoint.
type txSavepoint struct {
	d    *txDriver
	name string
}

func (s *txSavepoint) Commit() error {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	_, err := s.d.exec(context.Background(), "RELEASE SAVEPOINT "+s.name, nil)
	return err
}

func (s *txSavepoint) Rollback() error {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	_, err := s.d.exec(context.Background(), "ROLLBACK TO SAVEPOINT "+s.name, nil)
	return err
}

// txStmt is a prepared statement on the shared connection.
type txStmt struct {
	d    *txDriver
	base driver.Stmt
}

func (s *txStmt) Close() error {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	return s.base.Close()
}

func (s *txStmt) NumInput() int { return s.base.NumInput() }

func (s *txStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), namedValues(args))
}

func (s *txStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), namedValues(args))
}

func (s *txStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	if s.d.closed {
		return nil, errRolledBack
	}
	return execStmt(ctx, s.base, args)
}

func (s *txStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	if s.d.closed {
		return nil, errRolledBack
	}
	return queryStmt(ctx, s.base, args)
}

func namedValues(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, a := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	return out
}
