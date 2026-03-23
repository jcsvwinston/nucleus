// Package db provides database connectivity for the GoFrame framework.
// It wraps GORM with automatic driver detection from the DatabaseURL scheme,
// connection pool management, health checks, and transaction helpers.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/goframe/goframe/pkg/app"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	gormsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB wraps a GORM database connection with logging and convenience methods.
type DB struct {
	db     *gorm.DB
	logger *slog.Logger
}

// New opens a database connection based on the DatabaseURL in the config.
// Supported URL schemes: postgres://, postgresql://, mysql://, sqlite://.
// A plain file path ending in .db or .sqlite is treated as SQLite.
func New(cfg *app.Config, logger *slog.Logger) (*DB, error) {
	dialector, err := dialectorFromURL(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db.New: %w", err)
	}

	gormCfg := &gorm.Config{
		Logger: newSlogAdapter(logger),
	}

	gormDB, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		return nil, fmt.Errorf("db.New open: %w", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("db.New underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.DatabaseMaxOpen)
	sqlDB.SetMaxIdleConns(cfg.DatabaseMaxIdle)
	sqlDB.SetConnMaxLifetime(cfg.DatabaseMaxLifetime)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("db.New ping: %w", err)
	}

	return &DB{db: gormDB, logger: logger}, nil
}

// GormDB returns the underlying *gorm.DB for direct GORM operations.
func (d *DB) GormDB() *gorm.DB {
	return d.db
}

// Health verifies the database is reachable.
func (d *DB) Health(ctx context.Context) error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("db.Health: %w", err)
	}
	return sqlDB.PingContext(ctx)
}

// Tx runs fn inside a database transaction. If fn returns an error the
// transaction is rolled back; otherwise it is committed.
func (d *DB) Tx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return d.db.WithContext(ctx).Transaction(fn)
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("db.Close: %w", err)
	}
	return sqlDB.Close()
}

// SqlDB returns the underlying *sql.DB for low-level access.
func (d *DB) SqlDB() (*sql.DB, error) {
	return d.db.DB()
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

	// Append any extra query params from the original URL
	if u.RawQuery != "" {
		dsn += "&" + u.RawQuery
	}

	return dsn, nil
}

// slogAdapter bridges GORM's logger interface to slog.
type slogAdapter struct {
	logger *slog.Logger
}

func newSlogAdapter(logger *slog.Logger) gormlogger.Interface {
	return &slogAdapter{logger: logger}
}

func (a *slogAdapter) LogMode(gormlogger.LogLevel) gormlogger.Interface {
	return a
}

func (a *slogAdapter) Info(_ context.Context, msg string, data ...interface{}) {
	a.logger.Info(fmt.Sprintf(msg, data...))
}

func (a *slogAdapter) Warn(_ context.Context, msg string, data ...interface{}) {
	a.logger.Warn(fmt.Sprintf(msg, data...))
}

func (a *slogAdapter) Error(_ context.Context, msg string, data ...interface{}) {
	a.logger.Error(fmt.Sprintf(msg, data...))
}

func (a *slogAdapter) Trace(_ context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	elapsed := time.Since(begin)
	sqlStr, rows := fc()

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
