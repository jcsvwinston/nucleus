// Package db provides database connectivity for the GoFrame framework.
// GoFrame is Bun-first at runtime and keeps a temporary GORM compatibility path.
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

	gormsqlite "github.com/glebarez/sqlite"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/schema"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Engine identifies the SQL runtime used by DB.
type Engine string

const (
	// EngineGORM keeps backwards-compatible behavior.
	EngineGORM Engine = "gorm"
	// EngineBun enables the Bun-based SQL runtime.
	EngineBun Engine = "bun"
)

var (
	// ErrGORMRequired indicates a GORM-specific operation was requested while
	// the current DB runtime is not GORM.
	ErrGORMRequired = errors.New("gorm engine is required")
	// ErrBunRequired indicates a Bun-specific operation was requested while the
	// current DB runtime is not Bun.
	ErrBunRequired = errors.New("bun engine is required")
)

// Config contains the database-specific settings needed to open a connection.
// It intentionally avoids depending on app.Config to keep packages decoupled.
type Config struct {
	Engine              Engine
	DatabaseURL         string
	DatabaseMaxOpen     int
	DatabaseMaxIdle     int
	DatabaseMaxLifetime time.Duration
}

// DB wraps the SQL runtime and exposes compatibility helpers for both engines.
type DB struct {
	engine Engine
	db     *gorm.DB
	bun    *bun.DB
	sql    *sql.DB
	logger *slog.Logger

	telemetryCleanup func()
}

// New opens a database connection based on config.
// Supported URL schemes: postgres://, postgresql://, mysql://, sqlite://.
// A plain file path ending in .db or .sqlite is treated as SQLite.
// Default engine is Bun.
func New(cfg Config, logger *slog.Logger) (*DB, error) {
	engine := cfg.Engine
	if engine == "" {
		engine = EngineBun
	}

	switch engine {
	case EngineGORM:
		return newGORM(cfg, logger)
	case EngineBun:
		return newBun(cfg, logger)
	default:
		return nil, fmt.Errorf("db.New: unsupported engine: %s", engine)
	}
}

func newGORM(cfg Config, logger *slog.Logger) (*DB, error) {
	dialector, err := dialectorFromURL(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db.New gorm dialector: %w", err)
	}

	gormCfg := &gorm.Config{
		Logger: newSlogAdapter(logger, cfg.DatabaseURL, string(EngineGORM)),
	}

	gormDB, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		return nil, fmt.Errorf("db.New gorm open: %w", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("db.New gorm sql.DB: %w", err)
	}

	applyPoolConfig(sqlDB, cfg)
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("db.New gorm ping: %w", err)
	}

	dbSystem := dbSystemFromURL(cfg.DatabaseURL)
	telemetryCleanup := registerDBPoolTelemetry(sqlDB, dbSystem, string(EngineGORM))

	return &DB{
		engine:           EngineGORM,
		db:               gormDB,
		sql:              sqlDB,
		logger:           logger,
		telemetryCleanup: telemetryCleanup,
	}, nil
}

func newBun(cfg Config, logger *slog.Logger) (*DB, error) {
	sqlDB, err := openSQLDB(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db.New bun sql open: %w", err)
	}

	applyPoolConfig(sqlDB, cfg)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db.New bun ping: %w", err)
	}

	dialect, err := bunDialectFromURL(cfg.DatabaseURL)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("db.New bun dialect: %w", err)
	}

	bunDB := bun.NewDB(sqlDB, dialect)
	bunDB.AddQueryHook(newBunTelemetryHook(cfg.DatabaseURL, string(EngineBun)))

	dbSystem := dbSystemFromURL(cfg.DatabaseURL)
	telemetryCleanup := registerDBPoolTelemetry(sqlDB, dbSystem, string(EngineBun))

	return &DB{
		engine:           EngineBun,
		bun:              bunDB,
		sql:              sqlDB,
		logger:           logger,
		telemetryCleanup: telemetryCleanup,
	}, nil
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

// GormDB returns the underlying *gorm.DB when running with EngineGORM.
func (d *DB) GormDB() *gorm.DB {
	return d.db
}

// BunDB returns the underlying *bun.DB when running with EngineBun.
func (d *DB) BunDB() *bun.DB {
	return d.bun
}

// Health verifies the database is reachable.
func (d *DB) Health(ctx context.Context) error {
	sqlDB, err := d.SqlDB()
	if err != nil {
		return fmt.Errorf("db.Health: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

// Tx runs fn inside a GORM transaction.
// If the current runtime is not GORM, ErrGORMRequired is returned.
func (d *DB) Tx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if d.db == nil {
		return fmt.Errorf("db.Tx: %w", ErrGORMRequired)
	}
	return d.db.WithContext(ctx).Transaction(fn)
}

// TxBun runs fn inside a Bun transaction.
// If the current runtime is not Bun, ErrBunRequired is returned.
func (d *DB) TxBun(ctx context.Context, fn func(tx bun.Tx) error) error {
	if d.bun == nil {
		return fmt.Errorf("db.TxBun: %w", ErrBunRequired)
	}
	return d.bun.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		return fn(tx)
	})
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
	if d.sql != nil {
		return d.sql, nil
	}
	if d.db != nil {
		return d.db.DB()
	}
	return nil, errors.New("db.SqlDB: no sql handle available")
}

// dialectorFromURL parses a database URL and returns the appropriate GORM dialector.
func dialectorFromURL(rawURL string) (gorm.Dialector, error) {
	lower := strings.ToLower(rawURL)

	switch {
	case strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://"):
		return postgres.Open(rawURL), nil

	case strings.HasPrefix(lower, "mysql://"):
		dsn, err := mysqlURLToDSN(rawURL)
		if err != nil {
			return nil, err
		}
		return mysql.Open(dsn), nil

	case strings.HasPrefix(lower, "sqlite://"):
		path := strings.TrimPrefix(rawURL, "sqlite://")
		if path == "" {
			path = ":memory:"
		}
		return gormsqlite.Open(path), nil

	case strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || rawURL == ":memory:":
		return gormsqlite.Open(rawURL), nil

	default:
		return nil, fmt.Errorf("unsupported database URL scheme: %s", rawURL)
	}
}

func openSQLDB(rawURL string) (*sql.DB, error) {
	lower := strings.ToLower(rawURL)

	switch {
	case strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://"):
		return sql.Open("pgx", rawURL)

	case strings.HasPrefix(lower, "mysql://"):
		dsn, err := mysqlURLToDSN(rawURL)
		if err != nil {
			return nil, err
		}
		return sql.Open("mysql", dsn)

	case strings.HasPrefix(lower, "sqlite://"):
		path := strings.TrimPrefix(rawURL, "sqlite://")
		if path == "" {
			path = ":memory:"
		}
		return sql.Open("sqlite", path)

	case strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || rawURL == ":memory:":
		return sql.Open("sqlite", rawURL)

	default:
		return nil, fmt.Errorf("unsupported database URL scheme: %s", rawURL)
	}
}

func bunDialectFromURL(rawURL string) (schema.Dialect, error) {
	lower := strings.ToLower(rawURL)

	switch {
	case strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://"):
		return pgdialect.New(), nil
	case strings.HasPrefix(lower, "mysql://"):
		return mysqldialect.New(), nil
	case strings.HasPrefix(lower, "sqlite://"), strings.HasSuffix(lower, ".db"), strings.HasSuffix(lower, ".sqlite"), rawURL == ":memory:":
		return sqlitedialect.New(), nil
	default:
		return nil, fmt.Errorf("unsupported database URL scheme: %s", rawURL)
	}
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

// slogAdapter bridges GORM's logger interface to slog.
type slogAdapter struct {
	logger   *slog.Logger
	dbSystem string
	dbEngine string
}

func newSlogAdapter(logger *slog.Logger, rawURL, dbEngine string) gormlogger.Interface {
	return &slogAdapter{
		logger:   logger,
		dbSystem: dbSystemFromURL(rawURL),
		dbEngine: dbEngine,
	}
}

func (a *slogAdapter) LogMode(gormlogger.LogLevel) gormlogger.Interface {
	return a
}

func (a *slogAdapter) Info(_ context.Context, msg string, data ...interface{}) {
	if a.logger == nil {
		return
	}
	a.logger.Info(fmt.Sprintf(msg, data...))
}

func (a *slogAdapter) Warn(_ context.Context, msg string, data ...interface{}) {
	if a.logger == nil {
		return
	}
	a.logger.Warn(fmt.Sprintf(msg, data...))
}

func (a *slogAdapter) Error(_ context.Context, msg string, data ...interface{}) {
	if a.logger == nil {
		return
	}
	a.logger.Error(fmt.Sprintf(msg, data...))
}

func (a *slogAdapter) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sqlStr, rows := fc()

	if a.logger != nil {
		switch {
		case err != nil:
			a.logger.Error("gorm query error",
				"error", err.Error(),
				"duration_ms", float64(elapsed.Nanoseconds())/1e6,
				"rows", rows,
				"sql", sqlStr,
			)
		case elapsed > 200*time.Millisecond:
			a.logger.Warn("gorm slow query",
				"duration_ms", float64(elapsed.Nanoseconds())/1e6,
				"rows", rows,
				"sql", sqlStr,
			)
		default:
			a.logger.Debug("gorm query",
				"duration_ms", float64(elapsed.Nanoseconds())/1e6,
				"rows", rows,
				"sql", sqlStr,
			)
		}
	}

	recordDBQueryTelemetry(ctx, a.dbSystem, a.dbEngine, classifySQLOperation(sqlStr), elapsed, err)
}
