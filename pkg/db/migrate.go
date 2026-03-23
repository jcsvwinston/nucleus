package db

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gorm.io/gorm"
)

// AutoMigrate runs GORM's AutoMigrate for the given models.
// Intended for development only — it creates tables and adds missing columns
// but does not drop columns or change column types.
func (d *DB) AutoMigrate(models ...interface{}) error {
	if err := d.db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("db.AutoMigrate: %w", err)
	}
	return nil
}

// Migrator manages SQL-based database migrations using timestamped .up.sql
// and .down.sql files. For production use where schema changes must be explicit
// and reversible.
type Migrator struct {
	db             *gorm.DB
	migrationsPath string
	logger         *slog.Logger
}

// NewMigrator creates a Migrator that reads migration files from the given directory.
func NewMigrator(db *DB, migrationsPath string, logger *slog.Logger) *Migrator {
	return &Migrator{
		db:             db.db,
		migrationsPath: migrationsPath,
		logger:         logger,
	}
}

// Create generates a pair of empty migration files with a timestamp prefix.
// Files are created as {timestamp}_{name}.up.sql and {timestamp}_{name}.down.sql.
func (m *Migrator) Create(name string) error {
	if err := os.MkdirAll(m.migrationsPath, 0755); err != nil {
		return fmt.Errorf("db.Migrator.Create mkdir: %w", err)
	}

	ts := time.Now().Format("20060102150405")
	upFile := filepath.Join(m.migrationsPath, fmt.Sprintf("%s_%s.up.sql", ts, name))
	downFile := filepath.Join(m.migrationsPath, fmt.Sprintf("%s_%s.down.sql", ts, name))

	for _, f := range []string{upFile, downFile} {
		if err := os.WriteFile(f, []byte("-- Write your SQL here\n"), 0644); err != nil {
			return fmt.Errorf("db.Migrator.Create write %s: %w", f, err)
		}
	}

	m.logger.Info("migration files created", "up", upFile, "down", downFile)
	return nil
}
