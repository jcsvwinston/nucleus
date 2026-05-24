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
//
// Module-scoped Migrators (constructed via `NewModuleMigrator`) namespace
// their applied-migrations and checksum rows under a `<moduleName>/`
// prefix in the framework's tracking tables. This prevents two modules
// that ship `001_init.up.sql` from colliding on a primary-key insert
// when they share a database alias. ADR-010 §16 / Phase 2d. The bare
// constructor `NewMigrator` keeps the legacy unscoped behaviour so
// host applications that pre-date the module pattern continue to work
// without re-applying their migration history.
type Migrator struct {
	db             *DB
	migrationsPath string
	moduleName     string // optional namespace for storage IDs; empty = unscoped (legacy)
	logger         *slog.Logger
}

// NewMigrator creates an unscoped Migrator that reads migration files
// from the given directory. Applied migrations and checksums are
// stored under the migration file's bare ID — the legacy behaviour
// preserved for host applications that pre-date the module pattern.
func NewMigrator(db *DB, migrationsPath string, logger *slog.Logger) *Migrator {
	return &Migrator{
		db:             db,
		migrationsPath: migrationsPath,
		logger:         logger,
	}
}

// NewModuleMigrator creates a Migrator scoped to a named module. The
// migration files are still read from `migrationsPath`, but the IDs
// recorded in the framework's tracking tables (`nucleus_schema_migrations`
// and `nucleus_schema_migration_checksums`) are prefixed
// `<moduleName>/`. ADR-010 §16: this prevents cross-module filename
// collisions when multiple modules share a database alias, while
// keeping the on-disk migration filenames module-author-friendly
// (`001_init.up.sql` rather than `articles_001_init.up.sql`).
//
// `moduleName` must be non-empty and must not contain `/` (the
// namespace separator). The function reports an empty-name input as
// a `panic` because constructor-time misuse is a programming error
// the framework cannot recover from; non-`/` validation is enforced
// at storage time.
func NewModuleMigrator(db *DB, migrationsPath, moduleName string, logger *slog.Logger) *Migrator {
	if moduleName == "" {
		panic("db.NewModuleMigrator: moduleName must be non-empty (use NewMigrator for the unscoped case)")
	}
	// Reject characters that would break the storage-ID round-trip:
	// '/' is the namespace separator; '\x00' is the universal C-string
	// terminator that confuses scanning tools downstream even though
	// it would not escape `quoteSQLString`. Both are programming
	// errors at construction time, not user-input vectors.
	if strings.ContainsAny(moduleName, "/\x00") {
		panic(fmt.Sprintf("db.NewModuleMigrator: moduleName %q must not contain '/' or NUL", moduleName))
	}
	return &Migrator{
		db:             db,
		migrationsPath: migrationsPath,
		moduleName:     moduleName,
		logger:         logger,
	}
}

// namespacedID returns the storage key for a migration ID. When the
// Migrator is module-scoped (constructed via `NewModuleMigrator`), the
// storage key is `<moduleName>/<id>`; for the bare unscoped Migrator
// it is the migration's raw file-derived ID — the legacy backward-
// compatible form.
func (m *Migrator) namespacedID(id string) string {
	if m.moduleName == "" {
		return id
	}
	return m.moduleName + "/" + id
}

// ownsStorageID reports whether the given storage-table ID belongs
// to this Migrator. Module-scoped migrators own IDs prefixed
// `<moduleName>/`; the bare unscoped Migrator owns IDs without any
// `/` separator (legacy form). The distinction matters in `Drift`:
// when several modules share a database alias, each Migrator should
// only report drift for migrations it actually owns — IDs from
// foreign modules are not drift, they are someone else's
// responsibility.
//
// Nested storage keys (`<module>/<sub>/<id>`) are intentionally NOT
// claimed by a `<module>`-scoped Migrator: sub-module hierarchies
// are not part of the ADR-010 §16 design today and supporting them
// would require a separate ADR (sub-module discovery, naming
// conflicts, and Drift attribution all need decisions).
func (m *Migrator) ownsStorageID(storageID string) bool {
	if m.moduleName == "" {
		return !strings.Contains(storageID, "/")
	}
	prefix := m.moduleName + "/"
	if !strings.HasPrefix(storageID, prefix) {
		return false
	}
	// The slice is safe because HasPrefix above guarantees
	// len(storageID) >= len(prefix).
	return !strings.Contains(storageID[len(prefix):], "/")
}

// fileIDFromStorageID strips the namespace prefix from a storage ID
// so it can be compared against the on-disk migration file ID set.
// The caller is expected to have established ownership via
// `ownsStorageID` before calling.
func (m *Migrator) fileIDFromStorageID(storageID string) string {
	if m.moduleName == "" {
		return storageID
	}
	return strings.TrimPrefix(storageID, m.moduleName+"/")
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
			if _, ok := applied[m.namespacedID(mig.ID)]; ok {
				continue
			}
			if err := m.applyMigration(sqlDB, mig); err != nil {
				return err
			}
			appliedCount++
			if m.logger != nil {
				attrs := []any{"id", mig.ID}
				if m.moduleName != "" {
					attrs = append(attrs, "module", m.moduleName)
				}
				m.logger.Info("migration applied", attrs...)
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
		if _, ok := applied[m.namespacedID(mig.ID)]; ok {
			appliedMigs = append(appliedMigs, mig)
		}
	}
	rolledBack := 0
	for i := len(appliedMigs) - 1; i >= 0; i-- {
		mig := appliedMigs[i]
		if err := m.rollbackMigration(sqlDB, mig); err != nil {
			return err
		}
		rolledBack++
		if m.logger != nil {
			attrs := []any{"id", mig.ID}
			if m.moduleName != "" {
				attrs = append(attrs, "module", m.moduleName)
			}
			m.logger.Info("migration rolled back", attrs...)
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
		if at, ok := applied[m.namespacedID(mig.ID)]; ok {
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
	for storageID, at := range applied {
		// Only report drift for migrations this Migrator owns; rows
		// belonging to another module's Migrator (or, conversely,
		// module-namespaced rows when this Migrator is unscoped) are
		// out of scope here.
		if !m.ownsStorageID(storageID) {
			continue
		}
		fileID := m.fileIDFromStorageID(storageID)
		mig, ok := onDisk[fileID]
		if !ok || mig.UpPath == "" {
			drift = append(drift, DriftEntry{
				ID:        fileID,
				Kind:      DriftKindMissingUpFile,
				AppliedAt: at,
			})
			continue
		}
		expected, hasChecksum := checksums[storageID]
		if !hasChecksum {
			// Pre-checksum-tracking migration; nothing to compare. Not
			// reported as drift — the absence is informational only.
			continue
		}
		actual, err := fileSHA256(mig.UpPath)
		if err != nil {
			return nil, fmt.Errorf("db.Migrator.Drift checksum %s: %w", fileID, err)
		}
		if actual != expected {
			drift = append(drift, DriftEntry{
				ID:               fileID,
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
		if !ok || id == "" {
			// Reject files whose entire name is the .up.sql / .down.sql
			// suffix (id == ""). A storage row keyed `<module>/`
			// would be ambiguous and corrupt Drift attribution.
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

// applyMigration is a method on *Migrator so it can resolve the
// storage ID for both the applied-migrations row and the checksum
// row through `namespacedID`. Module-scoped Migrators write rows
// keyed `<moduleName>/<mig.ID>`; the unscoped Migrator writes the
// raw `mig.ID` (legacy form).
func (m *Migrator) applyMigration(db *sql.DB, mig migrationFile) error {
	script, err := os.ReadFile(mig.UpPath)
	if err != nil {
		return fmt.Errorf("db.Migrator read up migration %s: %w", mig.ID, err)
	}
	sum := sha256.Sum256(script)
	checksum := hex.EncodeToString(sum[:])
	storageID := m.namespacedID(mig.ID)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db.Migrator begin apply %s: %w", mig.ID, err)
	}
	defer tx.Rollback()

	// ExecScript splits multi-statement scripts per dialect (Oracle: one
	// `/`-separated PL/SQL block per Exec). Caveat (pre-existing, not specific
	// to the split): Oracle DDL auto-commits, so the surrounding tx does not
	// make the DDL and the tracking-row inserts atomic on Oracle — a failure
	// after a committed DDL block leaves it applied. Tightening that is a
	// tracked follow-up; non-Oracle dialects remain fully transactional.
	if err := ExecScript(tx, m.db.system, string(script)); err != nil {
		return fmt.Errorf("db.Migrator apply %s: %w", mig.ID, err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	insert := fmt.Sprintf(
		"INSERT INTO %s (id, applied_at) VALUES (%s, %s)",
		migrationsTable,
		quoteSQLString(storageID),
		quoteSQLString(now),
	)
	if _, err := tx.Exec(insert); err != nil {
		return fmt.Errorf("db.Migrator mark applied %s: %w", mig.ID, err)
	}

	insertChecksum := fmt.Sprintf(
		"INSERT INTO %s (id, checksum) VALUES (%s, %s)",
		migrationsChecksumsTable,
		quoteSQLString(storageID),
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

// rollbackMigration mirrors `applyMigration` — rolls back the file
// and deletes the applied-migration + checksum rows under the
// `namespacedID(mig.ID)` storage key.
func (m *Migrator) rollbackMigration(db *sql.DB, mig migrationFile) error {
	if mig.DownPath == "" {
		return fmt.Errorf("db.Migrator rollback %s: missing .down.sql file", mig.ID)
	}

	script, err := os.ReadFile(mig.DownPath)
	if err != nil {
		return fmt.Errorf("db.Migrator read down migration %s: %w", mig.ID, err)
	}
	storageID := m.namespacedID(mig.ID)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("db.Migrator begin rollback %s: %w", mig.ID, err)
	}
	defer tx.Rollback()

	if err := ExecScript(tx, m.db.system, string(script)); err != nil {
		return fmt.Errorf("db.Migrator rollback %s: %w", mig.ID, err)
	}

	del := fmt.Sprintf("DELETE FROM %s WHERE id = %s", migrationsTable, quoteSQLString(storageID))
	if _, err := tx.Exec(del); err != nil {
		return fmt.Errorf("db.Migrator unmark applied %s: %w", mig.ID, err)
	}

	delChecksum := fmt.Sprintf("DELETE FROM %s WHERE id = %s", migrationsChecksumsTable, quoteSQLString(storageID))
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
