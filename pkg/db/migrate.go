package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	migrationsTable          = "nucleus_schema_migrations"
	migrationsChecksumsTable = "nucleus_schema_migration_checksums"
)

// MigrationStatus describes the migration state for one migration ID.
type MigrationStatus struct {
	ID        string     `json:"id"`
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
	HasUp     bool       `json:"has_up"`
	HasDown   bool       `json:"has_down"`
}

// AutoMigrate is intentionally unsupported at the db.DB layer. Use
// explicit SQL migration files through Migrator, or call the
// application-level App.AutoMigrate (pkg/app), which builds a
// dialect-aware scaffold for SQLite, PostgreSQL, MySQL, MSSQL, and
// Oracle and applies it through the same Migrator pipeline.
func (d *DB) AutoMigrate(models ...interface{}) error {
	return fmt.Errorf("db.AutoMigrate: %w", ErrAutoMigrate)
}

// Migrator manages SQL-based database migrations using timestamped .up.sql
// and .down.sql files. For production use where schema changes must be explicit
// and reversible.
type Migrator struct {
	db             *DB
	migrationsPath string
	logger         *slog.Logger
}

// NewMigrator creates a Migrator that reads migration files from the given directory.
func NewMigrator(db *DB, migrationsPath string, logger *slog.Logger) *Migrator {
	return &Migrator{
		db:             db,
		migrationsPath: migrationsPath,
		logger:         logger,
	}
}

// Up applies all pending migrations.
func (m *Migrator) Up() error {
	return m.Steps(1<<31 - 1)
}

// Down rolls back the latest applied migration.
func (m *Migrator) Down() error {
	return m.Steps(-1)
}

// Steps applies n migrations (n>0) or rolls back n migrations (n<0).
func (m *Migrator) Steps(n int) error {
	if n == 0 {
		return nil
	}
	sqlDB, err := m.sqlDB()
	if err != nil {
		return err
	}
	if err := ensureMigrationsTable(sqlDB, m.db.system); err != nil {
		return err
	}
	if err := ensureChecksumsTable(sqlDB, m.db.system); err != nil {
		return err
	}

	migs, err := m.loadMigrations()
	if err != nil {
		return err
	}
	applied, err := loadApplied(sqlDB)
	if err != nil {
		return err
	}

	if n > 0 {
		appliedCount := 0
		for _, mig := range migs {
			if _, ok := applied[mig.ID]; ok {
				continue
			}
			if err := applyMigration(sqlDB, mig); err != nil {
				return err
			}
			appliedCount++
			if m.logger != nil {
				m.logger.Info("migration applied", "id", mig.ID)
			}
			if appliedCount >= n {
				break
			}
		}
		return nil
	}

	// Rollback path.
	toRollback := -n
	appliedMigs := make([]migrationFile, 0, len(migs))
	for _, mig := range migs {
		if _, ok := applied[mig.ID]; ok {
			appliedMigs = append(appliedMigs, mig)
		}
	}
	rolledBack := 0
	for i := len(appliedMigs) - 1; i >= 0; i-- {
		mig := appliedMigs[i]
		if err := rollbackMigration(sqlDB, mig); err != nil {
			return err
		}
		rolledBack++
		if m.logger != nil {
			m.logger.Info("migration rolled back", "id", mig.ID)
		}
		if rolledBack >= toRollback {
			break
		}
	}
	return nil
}

// Status returns migration state for all discovered migration files.
func (m *Migrator) Status() ([]MigrationStatus, error) {
	sqlDB, err := m.sqlDB()
	if err != nil {
		return nil, err
	}
	if err := ensureMigrationsTable(sqlDB, m.db.system); err != nil {
		return nil, err
	}
	if err := ensureChecksumsTable(sqlDB, m.db.system); err != nil {
		return nil, err
	}
	migs, err := m.loadMigrations()
	if err != nil {
		return nil, err
	}
	applied, err := loadApplied(sqlDB)
	if err != nil {
		return nil, err
	}

	status := make([]MigrationStatus, 0, len(migs))
	for _, mig := range migs {
		st := MigrationStatus{
			ID:      mig.ID,
			HasUp:   mig.UpPath != "",
			HasDown: mig.DownPath != "",
		}
		if at, ok := applied[mig.ID]; ok {
			st.Applied = true
			ts := at
			st.AppliedAt = &ts
		}
		status = append(status, st)
	}
	return status, nil
}

// DriftEntry describes a divergence between the migrations recorded as
// applied in the database and the migration files on disk.
type DriftEntry struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	AppliedAt time.Time `json:"applied_at"`
	// ExpectedChecksum is the SHA-256 hex of the .up.sql file as it
	// existed when the migration was originally applied. Populated only
	// for `checksum_mismatch` drift entries.
	ExpectedChecksum string `json:"expected_checksum,omitempty"`
	// ActualChecksum is the SHA-256 hex of the .up.sql file currently on
	// disk. Populated only for `checksum_mismatch` drift entries.
	ActualChecksum string `json:"actual_checksum,omitempty"`
}

// Drift kinds reported by Migrator.Drift.
const (
	// DriftKindMissingUpFile is reported when a migration ID is recorded as
	// applied in nucleus_schema_migrations but the corresponding .up.sql
	// file is absent from the migrations directory. Typical cause: someone
	// deleted a migration after applying it — the database remembers the
	// row, but no reproducible script exists to recreate that state.
	DriftKindMissingUpFile = "missing_up_file"

	// DriftKindChecksumMismatch is reported when the SHA-256 of the
	// `.up.sql` currently on disk does not match the checksum recorded in
	// nucleus_schema_migration_checksums when the migration was originally
	// applied. Typical cause: someone edited a migration file in place
	// after it had already been applied — the database state reflects the
	// pre-edit script, but the on-disk content claims something else.
	// Migrations applied before checksum tracking was introduced have no
	// recorded checksum and are not reported as drift on that basis.
	DriftKindChecksumMismatch = "checksum_mismatch"
)

// Drift returns entries that indicate the migrations log no longer
// matches the files on disk.
//
// Two kinds are detected:
//
//   - DriftKindMissingUpFile — the migration is recorded as applied but
//     the `.up.sql` file is gone.
//   - DriftKindChecksumMismatch — the migration is recorded as applied,
//     the `.up.sql` is present, but its SHA-256 differs from the
//     checksum stored at apply time.
//
// Schema-level drift (actual `information_schema.columns` shape vs what
// the migration files would have produced) is provided by SchemaDrift,
// which compares a caller-supplied expected schema against the live
// database for SQLite, PostgreSQL, and MySQL.
func (m *Migrator) Drift() ([]DriftEntry, error) {
	if m == nil {
		return nil, fmt.Errorf("db.Migrator.Drift: nil receiver")
	}
	sqlDB, err := m.sqlDB()
	if err != nil {
		return nil, err
	}
	if err := ensureMigrationsTable(sqlDB, m.db.system); err != nil {
		return nil, err
	}
	if err := ensureChecksumsTable(sqlDB, m.db.system); err != nil {
		return nil, err
	}
	migs, err := m.loadMigrations()
	if err != nil {
		return nil, err
	}
	applied, err := loadApplied(sqlDB)
	if err != nil {
		return nil, err
	}
	checksums, err := loadChecksums(sqlDB)
	if err != nil {
		return nil, err
	}

	onDisk := make(map[string]migrationFile, len(migs))
	for _, mig := range migs {
		onDisk[mig.ID] = mig
	}

	drift := make([]DriftEntry, 0)
	for id, at := range applied {
		mig, ok := onDisk[id]
		if !ok || mig.UpPath == "" {
			drift = append(drift, DriftEntry{
				ID:        id,
				Kind:      DriftKindMissingUpFile,
				AppliedAt: at,
			})
			continue
		}
		expected, hasChecksum := checksums[id]
		if !hasChecksum {
			// Pre-checksum-tracking migration; nothing to compare. Not
			// reported as drift — the absence is informational only.
			continue
		}
		actual, err := fileSHA256(mig.UpPath)
		if err != nil {
			return nil, fmt.Errorf("db.Migrator.Drift checksum %s: %w", id, err)
		}
		if actual != expected {
			drift = append(drift, DriftEntry{
				ID:               id,
				Kind:             DriftKindChecksumMismatch,
				AppliedAt:        at,
				ExpectedChecksum: expected,
				ActualChecksum:   actual,
			})
		}
	}
	sort.Slice(drift, func(i, j int) bool { return drift[i].ID < drift[j].ID })
	return drift, nil
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

	if m.logger != nil {
		m.logger.Info("migration files created", "up", upFile, "down", downFile)
	}
	return nil
}

type migrationFile struct {
	ID       string
	UpPath   string
	DownPath string
}

func (m *Migrator) sqlDB() (*sql.DB, error) {
	if m == nil || m.db == nil {
		return nil, fmt.Errorf("db.Migrator: nil database")
	}
	sqlDB, err := m.db.SqlDB()
	if err != nil {
		return nil, fmt.Errorf("db.Migrator sql handle: %w", err)
	}
	return sqlDB, nil
}

func ensureMigrationsTable(db *sql.DB, system string) error {
	if db == nil {
		return fmt.Errorf("db.Migrator ensure table: nil sql DB")
	}
	q := migrationsTableDDL(system)
	if _, err := db.Exec(q); err != nil {
		return fmt.Errorf("db.Migrator ensure table: %w", err)
	}
	return nil
}

// migrationsTableDDL returns the CREATE-TABLE-if-not-exists statement for the
// migrations tracking table in the dialect of the given system. Supported
// systems match dbSystemFromURL: postgresql, mysql, sqlite, mssql, oracle.
// Unknown systems fall back to the portable `CREATE TABLE IF NOT EXISTS` form
// accepted by postgres, mysql, and sqlite.
func migrationsTableDDL(system string) string {
	switch system {
	case "mssql":
		// SQL Server has no `CREATE TABLE IF NOT EXISTS`; guard with OBJECT_ID.
		// NVARCHAR is preferred over TEXT (deprecated LOB in SQL Server).
		return fmt.Sprintf(`IF OBJECT_ID('%s', 'U') IS NULL
	CREATE TABLE %s (
		id NVARCHAR(255) NOT NULL PRIMARY KEY,
		applied_at NVARCHAR(64) NOT NULL
	)`, migrationsTable, migrationsTable)
	case "oracle":
		// Oracle has no `CREATE TABLE IF NOT EXISTS`; swallow ORA-00955
		// ("name is already used by an existing object") in a PL/SQL block.
		// VARCHAR2 is the standard Oracle string type.
		return fmt.Sprintf(`BEGIN
	EXECUTE IMMEDIATE 'CREATE TABLE %s (
		id VARCHAR2(255) NOT NULL PRIMARY KEY,
		applied_at VARCHAR2(64) NOT NULL
	)';
EXCEPTION
	WHEN OTHERS THEN
		IF SQLCODE != -955 THEN RAISE; END IF;
END;`, migrationsTable)
	default:
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`, migrationsTable)
	}
}

func (m *Migrator) loadMigrations() ([]migrationFile, error) {
	if m == nil {
		return nil, fmt.Errorf("db.Migrator: nil receiver")
	}

	if err := os.MkdirAll(m.migrationsPath, 0755); err != nil {
		return nil, fmt.Errorf("db.Migrator load migrations mkdir: %w", err)
	}

	entries, err := os.ReadDir(m.migrationsPath)
	if err != nil {
		return nil, fmt.Errorf("db.Migrator load migrations: %w", err)
	}

	byID := map[string]*migrationFile{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		id, kind, ok := migrationNameParts(name)
		if !ok {
			continue
		}
		mig := byID[id]
		if mig == nil {
			mig = &migrationFile{ID: id}
			byID[id] = mig
		}
		fullPath := filepath.Join(m.migrationsPath, name)
		if kind == "up" {
			mig.UpPath = fullPath
		}
		if kind == "down" {
			mig.DownPath = fullPath
		}
	}

	result := make([]migrationFile, 0, len(byID))
	for _, mig := range byID {
		if mig.UpPath == "" {
			return nil, fmt.Errorf("db.Migrator invalid migration %s: missing .up.sql file", mig.ID)
		}
		result = append(result, *mig)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func migrationNameParts(name string) (id string, kind string, ok bool) {
	switch {
	case strings.HasSuffix(name, ".up.sql"):
		return strings.TrimSuffix(name, ".up.sql"), "up", true
	case strings.HasSuffix(name, ".down.sql"):
		return strings.TrimSuffix(name, ".down.sql"), "down", true
	default:
		return "", "", false
	}
}

func loadApplied(db *sql.DB) (map[string]time.Time, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT id, applied_at FROM %s ORDER BY id", migrationsTable))
	if err != nil {
		return nil, fmt.Errorf("db.Migrator load applied: %w", err)
	}
	defer rows.Close()

	applied := map[string]time.Time{}
	for rows.Next() {
		var (
			id        string
			appliedAt string
		)
		if err := rows.Scan(&id, &appliedAt); err != nil {
			return nil, fmt.Errorf("db.Migrator load applied scan: %w", err)
		}
		ts, err := time.Parse(time.RFC3339Nano, appliedAt)
		if err != nil {
			return nil, fmt.Errorf("db.Migrator load applied parse time for %s: %w", id, err)
		}
		applied[id] = ts
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db.Migrator load applied rows: %w", err)
	}
	return applied, nil
}

func applyMigration(db *sql.DB, mig migrationFile) error {
	script, err := os.ReadFile(mig.UpPath)
	if err != nil {
		return fmt.Errorf("db.Migrator read up migration %s: %w", mig.ID, err)
	}
	sum := sha256.Sum256(script)
	checksum := hex.EncodeToString(sum[:])

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db.Migrator begin apply %s: %w", mig.ID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(string(script)); err != nil {
		return fmt.Errorf("db.Migrator apply %s: %w", mig.ID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	insert := fmt.Sprintf(
		"INSERT INTO %s (id, applied_at) VALUES (%s, %s)",
		migrationsTable,
		quoteSQLString(mig.ID),
		quoteSQLString(now),
	)
	if _, err := tx.Exec(insert); err != nil {
		return fmt.Errorf("db.Migrator mark applied %s: %w", mig.ID, err)
	}

	insertChecksum := fmt.Sprintf(
		"INSERT INTO %s (id, checksum) VALUES (%s, %s)",
		migrationsChecksumsTable,
		quoteSQLString(mig.ID),
		quoteSQLString(checksum),
	)
	if _, err := tx.Exec(insertChecksum); err != nil {
		return fmt.Errorf("db.Migrator record checksum %s: %w", mig.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db.Migrator commit apply %s: %w", mig.ID, err)
	}
	return nil
}

func rollbackMigration(db *sql.DB, mig migrationFile) error {
	if mig.DownPath == "" {
		return fmt.Errorf("db.Migrator rollback %s: missing .down.sql file", mig.ID)
	}

	script, err := os.ReadFile(mig.DownPath)
	if err != nil {
		return fmt.Errorf("db.Migrator read down migration %s: %w", mig.ID, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db.Migrator begin rollback %s: %w", mig.ID, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(string(script)); err != nil {
		return fmt.Errorf("db.Migrator rollback %s: %w", mig.ID, err)
	}

	del := fmt.Sprintf("DELETE FROM %s WHERE id = %s", migrationsTable, quoteSQLString(mig.ID))
	if _, err := tx.Exec(del); err != nil {
		return fmt.Errorf("db.Migrator unmark applied %s: %w", mig.ID, err)
	}

	delChecksum := fmt.Sprintf("DELETE FROM %s WHERE id = %s", migrationsChecksumsTable, quoteSQLString(mig.ID))
	if _, err := tx.Exec(delChecksum); err != nil {
		return fmt.Errorf("db.Migrator drop checksum %s: %w", mig.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db.Migrator commit rollback %s: %w", mig.ID, err)
	}
	return nil
}

func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func ensureChecksumsTable(db *sql.DB, system string) error {
	if db == nil {
		return fmt.Errorf("db.Migrator ensure checksums table: nil sql DB")
	}
	q := checksumsTableDDL(system)
	if _, err := db.Exec(q); err != nil {
		return fmt.Errorf("db.Migrator ensure checksums table: %w", err)
	}
	return nil
}

// checksumsTableDDL returns the CREATE-TABLE-if-not-exists statement for
// the migration checksums tracking table. Mirrors the dialect handling of
// migrationsTableDDL.
func checksumsTableDDL(system string) string {
	switch system {
	case "mssql":
		return fmt.Sprintf(`IF OBJECT_ID('%s', 'U') IS NULL
	CREATE TABLE %s (
		id NVARCHAR(255) NOT NULL PRIMARY KEY,
		checksum NVARCHAR(64) NOT NULL
	)`, migrationsChecksumsTable, migrationsChecksumsTable)
	case "oracle":
		return fmt.Sprintf(`BEGIN
	EXECUTE IMMEDIATE 'CREATE TABLE %s (
		id VARCHAR2(255) NOT NULL PRIMARY KEY,
		checksum VARCHAR2(64) NOT NULL
	)';
EXCEPTION
	WHEN OTHERS THEN
		IF SQLCODE != -955 THEN RAISE; END IF;
END;`, migrationsChecksumsTable)
	default:
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			checksum TEXT NOT NULL
		)`, migrationsChecksumsTable)
	}
}

func loadChecksums(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT id, checksum FROM %s", migrationsChecksumsTable))
	if err != nil {
		return nil, fmt.Errorf("db.Migrator load checksums: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var id, checksum string
		if err := rows.Scan(&id, &checksum); err != nil {
			return nil, fmt.Errorf("db.Migrator load checksums scan: %w", err)
		}
		out[id] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db.Migrator load checksums rows: %w", err)
	}
	return out, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
