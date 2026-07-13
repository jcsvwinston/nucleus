// Package db provides database connectivity for the Nucleus framework.
// The runtime is implemented directly on top of database/sql.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Engine identifies the SQL runtime used by DB.
type Engine string

const (
	// EngineSQL is the native database/sql runtime.
	EngineSQL Engine = "sql"
)

var (
	ErrUnsupportedEngine = errors.New("unsupported database engine")
	ErrSQLRequired       = errors.New("sql runtime is required")
	ErrAutoMigrate       = errors.New("automigrate is not supported; use SQL migrations")
)

// Config contains the database-specific settings needed to open a connection.
// It intentionally avoids depending on app.Config to keep packages decoupled.
type Config struct {
	Engine              Engine
	DatabaseURL         string
	DatabaseMaxOpen     int
	DatabaseMaxIdle     int
	DatabaseMaxLifetime time.Duration

	// StatementObserver, when non-nil, enables driver-level SQL
	// instrumentation: the database/sql driver is wrapped so every direct
	// db.QueryContext/ExecContext is reported to the observer AFTER the call
	// returns. Statements issued through model.CRUD are already observed at
	// the model layer and are suppressed here (see StatementObserver's godoc
	// and observe.CtxWithModelObserved). Nil (the default) leaves the stock
	// database/sql path untouched — no wrapping, no hot-path cost.
	StatementObserver StatementObserver
}

// DB wraps the SQL runtime.
type DB struct {
	engine Engine
	system string
	sql    *sql.DB
	logger *slog.Logger

	telemetryCleanup func()
}

// New opens a database connection based on config.
// Supported URL schemes:
// - postgres://, postgresql://
// - mysql://
// - sqlite:// (or .db/.sqlite path)
// - sqlserver://, mssql://
// - oracle://
// A plain file path ending in .db or .sqlite is treated as SQLite.
// Default engine is EngineSQL.
func New(cfg Config, logger *slog.Logger) (*DB, error) {
	engine := normalizeEngine(cfg.Engine)
	if engine == "" {
		return nil, fmt.Errorf("db.New: %w: %s", ErrUnsupportedEngine, cfg.Engine)
	}

	sqlDB, err := openConfiguredDB(cfg)
	if err != nil {
		return nil, fmt.Errorf("db.New sql open: %w", err)
	}

	applyPoolConfig(sqlDB, cfg)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db.New sql ping: %w", err)
	}

	dbSystem := dbSystemFromURL(cfg.DatabaseURL)
	telemetryCleanup := registerDBPoolTelemetry(sqlDB, dbSystem, string(engine))

	return &DB{
		engine:           engine,
		system:           dbSystem,
		sql:              sqlDB,
		logger:           logger,
		telemetryCleanup: telemetryCleanup,
	}, nil
}

func normalizeEngine(engine Engine) Engine {
	switch Engine(strings.ToLower(strings.TrimSpace(string(engine)))) {
	case "":
		return EngineSQL
	case EngineSQL:
		return EngineSQL
	default:
		return ""
	}
}

func applyPoolConfig(sqlDB *sql.DB, cfg Config) {
	if cfg.DatabaseMaxOpen > 0 {
		sqlDB.SetMaxOpenConns(cfg.DatabaseMaxOpen)
	}
	if cfg.DatabaseMaxIdle > 0 {
		sqlDB.SetMaxIdleConns(cfg.DatabaseMaxIdle)
	}
	if cfg.DatabaseMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.DatabaseMaxLifetime)
	}
}

// Engine returns the selected runtime engine.
func (d *DB) Engine() Engine {
	if d == nil {
		return ""
	}
	return d.engine
}

// System returns the underlying SQL system name as resolved from the
// connection URL: one of "postgresql", "mysql", "sqlite", "mssql",
// "oracle", or "unknown". Callers can dispatch dialect-specific code
// off this value — `app.AutoMigrate` uses it to pick a migration
// scaffold builder.
func (d *DB) System() string {
	if d == nil {
		return ""
	}
	return d.system
}

// Health verifies the database is reachable.
func (d *DB) Health(ctx context.Context) error {
	sqlDB, err := d.SqlDB()
	if err != nil {
		return fmt.Errorf("db.Health: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

// Tx runs fn inside a SQL transaction.
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("db.Tx: %w", ErrSQLRequired)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("db.Tx begin: %w", err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db.Tx commit: %w", err)
	}
	return nil
}

// Close closes the underlying sql.DB connection.
func (d *DB) Close() error {
	if d != nil && d.telemetryCleanup != nil {
		d.telemetryCleanup()
		d.telemetryCleanup = nil
	}
	sqlDB, err := d.SqlDB()
	if err != nil {
		return fmt.Errorf("db.Close: %w", err)
	}
	return sqlDB.Close()
}

// SqlDB returns the underlying *sql.DB.
func (d *DB) SqlDB() (*sql.DB, error) {
	if d == nil {
		return nil, errors.New("db.SqlDB: nil receiver")
	}
	if d.sql == nil {
		return nil, errors.New("db.SqlDB: no sql handle available")
	}
	return d.sql, nil
}

// openConfiguredDB opens the *sql.DB for cfg, wrapping the driver with
// statement instrumentation when cfg.StatementObserver is set and using the
// stock database/sql path otherwise.
func openConfiguredDB(cfg Config) (*sql.DB, error) {
	driverName, dsn, err := resolveDriver(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.StatementObserver != nil {
		return openInstrumented(driverName, dsn, cfg.StatementObserver)
	}
	return sql.Open(driverName, dsn)
}

// openSQLDB opens rawURL on the stock (uninstrumented) database/sql path.
// Retained for callers and tests that only need scheme→driver resolution.
func openSQLDB(rawURL string) (*sql.DB, error) {
	driverName, dsn, err := resolveDriver(rawURL)
	if err != nil {
		return nil, err
	}
	return sql.Open(driverName, dsn)
}

func normalizeMSSQLURL(rawURL string) string {
	if strings.HasPrefix(strings.ToLower(rawURL), "mssql://") {
		return "sqlserver://" + strings.TrimPrefix(rawURL, "mssql://")
	}
	return rawURL
}

// mysqlURLToDSN converts a mysql:// URL to the DSN format expected by the Go MySQL driver.
// Example: mysql://user:pass@host:3306/dbname -> user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8mb4
func mysqlURLToDSN(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("db.mysqlURLToDSN parse: %w", err)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "3306"
	}

	user := u.User.Username()
	pass, _ := u.User.Password()

	dbname := strings.TrimPrefix(u.Path, "/")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4",
		user, pass, host, port, dbname)

	// Append any extra query params from the original URL.
	if u.RawQuery != "" {
		dsn += "&" + u.RawQuery
	}

	return dsn, nil
}
