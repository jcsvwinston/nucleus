package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// StatementInfo is a single observed database/sql statement handed to a
// StatementObserver. It is the driver-level counterpart to the model layer's
// SQLQueryEvent: it carries no model name (the driver only sees SQL text), so
// it exists to surface the traffic model.CRUD never sees — direct
// db.QueryContext/ExecContext calls from outbox dispatch, SQL session stores,
// migrations, schema drift checks, and any other code that runs raw SQL.
type StatementInfo struct {
	// Operation is the SQL verb classified from Query (SELECT/INSERT/…),
	// lowercase, or "other"/"unknown".
	Operation string
	// Query is the SQL statement text as sent to the driver.
	Query string
	// Args are the driver arguments, unredacted. The observer MUST sanitize
	// them before exposing the statement anywhere — the driver layer does no
	// redaction.
	Args []any
	// Duration is the wall-clock time the driver call took.
	Duration time.Duration
	// Err is the driver error, if any.
	Err error
	// RowsAffected is the exec row count (0 for queries, and for execs whose
	// driver does not report it).
	RowsAffected int64
}

// StatementObserver is invoked after each direct database/sql statement when
// Config.StatementObserver is set. It runs synchronously on the caller's
// goroutine after the underlying driver call returns, so it MUST be cheap and
// non-blocking; the wiring in pkg/app gates it on the observability bus having
// subscribers before doing any real work.
//
// Statements issued through model.CRUD are NOT delivered here: CRUD already
// observes them at the model layer (enriched with the model name) and marks
// the context so this layer skips them — see observe.CtxWithModelObserved.
//
// Opt-in: when Config.StatementObserver is nil the driver is not wrapped at
// all, so the hot path is byte-for-byte the stock database/sql path.
type StatementObserver func(ctx context.Context, info StatementInfo)

// openInstrumented opens a *sql.DB whose driver is wrapped to report every
// statement to obs. It builds the DB via sql.OpenDB over a wrapping connector.
//
// The base driver instance is obtained from a throwaway sql.Open handle:
// sql.Open is lazy (it opens no connection), so Driver() is a cheap,
// side-effect-free way to reach the registered driver without re-implementing
// driver registration.
func openInstrumented(driverName, dsn string, obs StatementObserver) (*sql.DB, error) {
	probe, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	base := probe.Driver()
	_ = probe.Close()

	connector, err := baseConnector(base, dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(&instrumentedConnector{base: connector, driver: base, obs: obs}), nil
}

// baseConnector returns a driver.Connector for base+dsn, preferring the
// driver's own DriverContext.OpenConnector when available (it may pre-parse
// the DSN) and falling back to a DSN-carrying connector otherwise.
func baseConnector(base driver.Driver, dsn string) (driver.Connector, error) {
	if dc, ok := base.(driver.DriverContext); ok {
		return dc.OpenConnector(dsn)
	}
	return dsnConnector{dsn: dsn, driver: base}, nil
}

type dsnConnector struct {
	dsn    string
	driver driver.Driver
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.driver.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.driver }

// instrumentedConnector wraps a base connector so every connection it opens is
// an instrumentedConn.
type instrumentedConnector struct {
	base   driver.Connector
	driver driver.Driver
	obs    StatementObserver
}

func (c *instrumentedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &instrumentedConn{base: conn, obs: c.obs}, nil
}

func (c *instrumentedConnector) Driver() driver.Driver { return c.driver }

// instrumentedConn wraps a driver.Conn. It implements every optional
// database/sql driver interface unconditionally and forwards to the base conn
// when the base implements the corresponding interface, returning
// driver.ErrSkip (or a benign default) otherwise so database/sql falls back to
// its standard path without losing the base driver's capabilities.
//
// Observation happens in QueryContext/ExecContext (the direct path) and in the
// wrapped statement's QueryContext/ExecContext (the prepared path). For any one
// logical operation database/sql uses exactly one of those paths, so a
// statement is observed once.
type instrumentedConn struct {
	base driver.Conn
	obs  StatementObserver
}

// --- driver.Conn ---

func (c *instrumentedConn) Prepare(query string) (driver.Stmt, error) {
	st, err := c.base.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &instrumentedStmt{base: st, query: query, obs: c.obs}, nil
}

func (c *instrumentedConn) Close() error { return c.base.Close() }

func (c *instrumentedConn) Begin() (driver.Tx, error) { return c.base.Begin() } //nolint:staticcheck // required by driver.Conn; ConnBeginTx is preferred and forwarded below.

// --- driver.ConnPrepareContext ---

func (c *instrumentedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if cpc, ok := c.base.(driver.ConnPrepareContext); ok {
		st, err := cpc.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return &instrumentedStmt{base: st, query: query, obs: c.obs}, nil
	}
	return c.Prepare(query)
}

// --- driver.ConnBeginTx ---

func (c *instrumentedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if cbt, ok := c.base.(driver.ConnBeginTx); ok {
		return cbt.BeginTx(ctx, opts)
	}
	return c.base.Begin() //nolint:staticcheck // fallback when the base lacks ConnBeginTx.
}

// --- driver.QueryerContext ---

func (c *instrumentedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qc, ok := c.base.(driver.QueryerContext)
	if !ok {
		// Base cannot query directly; database/sql will fall back to the
		// prepared path, where instrumentedStmt observes instead.
		return nil, driver.ErrSkip
	}
	started := time.Now()
	rows, err := qc.QueryContext(ctx, query, args)
	c.observe(ctx, query, args, started, err, 0)
	return rows, err
}

// --- driver.ExecerContext ---

func (c *instrumentedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ec, ok := c.base.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	started := time.Now()
	res, err := ec.ExecContext(ctx, query, args)
	c.observe(ctx, query, args, started, err, rowsAffected(res, err))
	return res, err
}

// --- driver.Pinger ---

func (c *instrumentedConn) Ping(ctx context.Context) error {
	if p, ok := c.base.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return driver.ErrSkip
}

// --- driver.SessionResetter ---

func (c *instrumentedConn) ResetSession(ctx context.Context) error {
	if sr, ok := c.base.(driver.SessionResetter); ok {
		return sr.ResetSession(ctx)
	}
	return nil
}

// --- driver.Validator ---

func (c *instrumentedConn) IsValid() bool {
	if v, ok := c.base.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

// --- driver.NamedValueChecker ---

func (c *instrumentedConn) CheckNamedValue(nv *driver.NamedValue) error {
	if nvc, ok := c.base.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

func (c *instrumentedConn) observe(ctx context.Context, query string, args []driver.NamedValue, started time.Time, err error, rows int64) {
	observeStatement(ctx, c.obs, query, args, started, err, rows)
}

// instrumentedStmt wraps a driver.Stmt so prepared-statement execution is
// observed. Query text is captured at prepare time.
type instrumentedStmt struct {
	base  driver.Stmt
	query string
	obs   StatementObserver
}

func (s *instrumentedStmt) Close() error  { return s.base.Close() }
func (s *instrumentedStmt) NumInput() int { return s.base.NumInput() }

func (s *instrumentedStmt) Exec(args []driver.Value) (driver.Result, error) { //nolint:staticcheck // required by driver.Stmt; StmtExecContext is preferred and implemented below.
	started := time.Now()
	res, err := s.base.Exec(args) //nolint:staticcheck
	observeStatement(context.Background(), s.obs, s.query, valuesToNamed(args), started, err, rowsAffected(res, err))
	return res, err
}

func (s *instrumentedStmt) Query(args []driver.Value) (driver.Rows, error) { //nolint:staticcheck // required by driver.Stmt; StmtQueryContext is preferred and implemented below.
	started := time.Now()
	rows, err := s.base.Query(args) //nolint:staticcheck
	observeStatement(context.Background(), s.obs, s.query, valuesToNamed(args), started, err, 0)
	return rows, err
}

// --- driver.StmtExecContext ---

func (s *instrumentedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if sec, ok := s.base.(driver.StmtExecContext); ok {
		started := time.Now()
		res, err := sec.ExecContext(ctx, args)
		observeStatement(ctx, s.obs, s.query, args, started, err, rowsAffected(res, err))
		return res, err
	}
	// Base is context-less; database/sql already converted NamedValue→Value
	// checks, so fall back through the non-context Exec.
	return s.Exec(namedToValues(args))
}

// --- driver.StmtQueryContext ---

func (s *instrumentedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if sqc, ok := s.base.(driver.StmtQueryContext); ok {
		started := time.Now()
		rows, err := sqc.QueryContext(ctx, args)
		observeStatement(ctx, s.obs, s.query, args, started, err, 0)
		return rows, err
	}
	return s.Query(namedToValues(args))
}

// --- driver.NamedValueChecker (statement-level; pgx uses it) ---

func (s *instrumentedStmt) CheckNamedValue(nv *driver.NamedValue) error {
	if nvc, ok := s.base.(driver.NamedValueChecker); ok {
		return nvc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// observeStatement is the single emission point shared by the conn and stmt
// wrappers. It skips CRUD-originated statements (already observed at the model
// layer) and skips when no observer is set.
func observeStatement(ctx context.Context, obs StatementObserver, query string, args []driver.NamedValue, started time.Time, err error, rows int64) {
	if obs == nil || observe.IsModelObserved(ctx) {
		return
	}
	obs(ctx, StatementInfo{
		Operation:    classifySQLOperation(query),
		Query:        query,
		Args:         namedValuesToArgs(args),
		Duration:     time.Since(started),
		Err:          err,
		RowsAffected: rows,
	})
}

func rowsAffected(res driver.Result, err error) int64 {
	if err != nil || res == nil {
		return 0
	}
	// Best-effort: drivers without RowsAffected support (or SELECT-shaped
	// results) report 0, never an error to the caller.
	if n, raErr := res.RowsAffected(); raErr == nil {
		return n
	}
	return 0
}

func namedValuesToArgs(args []driver.NamedValue) []any {
	if len(args) == 0 {
		return nil
	}
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

func valuesToNamed(args []driver.Value) []driver.NamedValue {
	if len(args) == 0 {
		return nil
	}
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

func namedToValues(args []driver.NamedValue) []driver.Value {
	if len(args) == 0 {
		return nil
	}
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// resolveDriver maps a database URL to the (driverName, dsn) pair used to open
// it. It is the extraction of openSQLDB's switch so both the stock and the
// instrumented open paths share one source of truth.
func resolveDriver(rawURL string) (driverName, dsn string, err error) {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://"):
		return "pgx", rawURL, nil
	case strings.HasPrefix(lower, "mysql://"):
		d, e := mysqlURLToDSN(rawURL)
		if e != nil {
			return "", "", e
		}
		return "mysql", d, nil
	case strings.HasPrefix(lower, "sqlserver://"), strings.HasPrefix(lower, "mssql://"):
		return "sqlserver", normalizeMSSQLURL(rawURL), nil
	case strings.HasPrefix(lower, "oracle://"):
		return "oracle", rawURL, nil
	case strings.HasPrefix(lower, "sqlite://"):
		path := strings.TrimPrefix(rawURL, "sqlite://")
		if path == "" {
			path = ":memory:"
		}
		return "sqlite", path, nil
	case strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || rawURL == ":memory:":
		return "sqlite", rawURL, nil
	default:
		return "", "", &unsupportedSchemeError{url: rawURL}
	}
}

type unsupportedSchemeError struct{ url string }

func (e *unsupportedSchemeError) Error() string {
	return "unsupported database URL scheme: " + e.url
}
