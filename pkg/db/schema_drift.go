package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrSchemaDriftUnsupported is returned by Migrator.SchemaDrift when
// the underlying database dialect does not yet have an introspection
// implementation. Today this covers MSSQL and Oracle; SQLite,
// PostgreSQL, and MySQL are supported. Callers can `errors.Is` against
// this sentinel to distinguish "no drift detected" from "drift could
// not be checked on this engine".
var ErrSchemaDriftUnsupported = errors.New("db.Migrator.SchemaDrift: schema-level introspection is not yet implemented for this database engine")

// Drift kinds reported by Migrator.SchemaDrift. These complement the
// file-level kinds in migrate.go (DriftKindMissingUpFile,
// DriftKindChecksumMismatch).
const (
	// DriftKindSchemaMissingTable is reported when the caller declares
	// a table that does not exist in the live database. Typical
	// cause: a migration was forgotten or the deployment never ran
	// AutoMigrate after a model was added.
	DriftKindSchemaMissingTable = "schema_missing_table"

	// DriftKindSchemaMissingColumn is reported when the caller's
	// expected schema includes a column the live table is missing.
	// Typical cause: a column was added to the model after the initial
	// migration; AutoMigrate does not ALTER existing tables, so the
	// new column never made it into the DB.
	DriftKindSchemaMissingColumn = "schema_missing_column"

	// DriftKindSchemaExtraColumn is reported when the live table has a
	// column the caller's expected schema does not declare. Typical
	// cause: a column was added by an ad-hoc DDL (psql, a sidecar
	// migration, a manual fix) but never reflected back into the model
	// definition. Not always a bug — but always worth surfacing.
	DriftKindSchemaExtraColumn = "schema_extra_column"

	// DriftKindSchemaColumnNullability is reported when a column
	// exists on both sides but the expected schema says NOT NULL and
	// the live table says nullable (or vice versa). The detected
	// polarity is recorded in the entry's Expected/Actual fields so
	// the operator can read the row and know which side is wrong.
	DriftKindSchemaColumnNullability = "schema_column_nullability"
)

// ExpectedTable is the caller's declaration of what a single table
// should look like, fed into Migrator.SchemaDrift for comparison
// against the live database. ExpectedTable is intentionally
// model-agnostic — `pkg/db` does not import `pkg/model` (that would
// cycle through model's tests, which use pkg/db). Callers wrap their
// own metadata source (the typical one is `model.ExtractMeta`, but
// anything that can produce a table name + column list works).
type ExpectedTable struct {
	// Name is the SQL table name as it appears in the database. Case
	// matters for engines that fold identifiers (Oracle, MSSQL with
	// case-sensitive collation); for the supported engines (SQLite,
	// PostgreSQL, MySQL) lower-snake-case is the convention.
	Name string
	// Columns are the columns the caller expects on the table.
	// Indexes, constraints, and foreign keys are intentionally out of
	// scope for the initial SchemaDrift cut.
	Columns []ExpectedColumn
}

// ExpectedColumn is one row of an ExpectedTable. SchemaDrift compares
// existence and nullability today; column types are explicitly out of
// scope because cross-dialect type families (BIGINT vs INT vs
// BIGSERIAL vs NUMBER vs NVARCHAR vs VARCHAR vs TEXT) require a
// per-dialect compatibility table that is its own rabbit hole.
//
// ExpectedColumn may grow additively (e.g. a future Type, Default, or
// CheckConstraint field) — never via positional replacement. Callers
// should always use field-named struct literals so the public surface
// stays forward-compatible.
type ExpectedColumn struct {
	Name     string
	Nullable bool
}

// SchemaDriftEntry describes a single divergence between an
// ExpectedTable set and the live database shape, reported by
// SchemaDrift.
type SchemaDriftEntry struct {
	Kind     string `json:"kind"`
	Table    string `json:"table"`
	Column   string `json:"column,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// liveColumn is the per-engine projection of a column row from the
// dialect-specific introspection query.
type liveColumn struct {
	Name     string
	Nullable bool
}

// SchemaDrift compares a caller-provided expected schema against the
// live database shape and returns entries describing the divergences.
//
// The comparator is deliberately conservative:
//
//   - It checks table existence (DriftKindSchemaMissingTable) and the
//     full set of column names (DriftKindSchemaMissingColumn,
//     DriftKindSchemaExtraColumn).
//   - It checks nullability per column
//     (DriftKindSchemaColumnNullability) because nullability is
//     deterministic across dialects.
//   - It does NOT compare column types — see ExpectedColumn's
//     comment for the reasoning.
//
// MSSQL and Oracle return ErrSchemaDriftUnsupported until per-dialect
// introspection lands in a follow-up iteration.
func (m *Migrator) SchemaDrift(ctx context.Context, expected []ExpectedTable) ([]SchemaDriftEntry, error) {
	if m == nil {
		return nil, fmt.Errorf("db.Migrator.SchemaDrift: nil receiver")
	}
	if m.db == nil {
		return nil, fmt.Errorf("db.Migrator.SchemaDrift: nil database")
	}
	// System check is BEFORE sqlDB() so the unsupported-engine path
	// returns ErrSchemaDriftUnsupported without first trying (and
	// failing) to acquire a sql handle.
	system := m.db.system
	switch system {
	case "sqlite", "postgresql", "mysql":
		// Supported; fall through.
	case "mssql", "oracle":
		return nil, fmt.Errorf("%w (system=%q)", ErrSchemaDriftUnsupported, system)
	default:
		return nil, fmt.Errorf("%w (unknown system=%q)", ErrSchemaDriftUnsupported, system)
	}

	sqlDB, err := m.sqlDB()
	if err != nil {
		return nil, err
	}

	// Normalise the expected schema into a {tableName: {colName:
	// nullable}} map so the comparator can lookup in O(1).
	expectedMap, err := normaliseExpected(expected)
	if err != nil {
		return nil, err
	}

	drift := make([]SchemaDriftEntry, 0)
	for _, tableName := range sortedKeys(expectedMap) {
		expCols := expectedMap[tableName]
		liveCols, err := introspectTableColumns(ctx, sqlDB, system, tableName)
		if err != nil {
			return nil, fmt.Errorf("introspect %s.%s: %w", system, tableName, err)
		}
		if liveCols == nil {
			drift = append(drift, SchemaDriftEntry{
				Kind:  DriftKindSchemaMissingTable,
				Table: tableName,
			})
			continue
		}
		drift = append(drift, diffColumns(tableName, expCols, liveCols)...)
	}

	sort.SliceStable(drift, func(i, j int) bool {
		if drift[i].Table != drift[j].Table {
			return drift[i].Table < drift[j].Table
		}
		if drift[i].Kind != drift[j].Kind {
			return drift[i].Kind < drift[j].Kind
		}
		return drift[i].Column < drift[j].Column
	})
	return drift, nil
}

// normaliseExpected builds the {tableName: {colName: nullable}} map
// the comparator wants from the caller's ExpectedTable slice.
// Identifiers are lower-cased so cross-dialect comparison (where
// engines fold case) works.
func normaliseExpected(tables []ExpectedTable) (map[string]map[string]bool, error) {
	out := make(map[string]map[string]bool, len(tables))
	for _, tbl := range tables {
		name := strings.TrimSpace(tbl.Name)
		if name == "" {
			return nil, fmt.Errorf("ExpectedTable with empty Name")
		}
		lower := strings.ToLower(name)
		if _, exists := out[lower]; exists {
			// Silently overwriting a duplicate table would mask the
			// drift findings of the first one — fail loudly instead.
			return nil, fmt.Errorf("duplicate ExpectedTable name %q", name)
		}
		cols := make(map[string]bool, len(tbl.Columns))
		for _, col := range tbl.Columns {
			c := strings.TrimSpace(col.Name)
			if c == "" {
				return nil, fmt.Errorf("ExpectedColumn with empty Name in table %q", name)
			}
			cols[strings.ToLower(c)] = col.Nullable
		}
		out[lower] = cols
	}
	return out, nil
}

// diffColumns compares an expected column set against a live
// (introspected) column set and emits the drift entries that
// describe the divergence.
func diffColumns(table string, expected map[string]bool, live []liveColumn) []SchemaDriftEntry {
	entries := make([]SchemaDriftEntry, 0)
	liveByName := make(map[string]liveColumn, len(live))
	for _, c := range live {
		liveByName[strings.ToLower(c.Name)] = c
	}

	for _, col := range sortedKeys(expected) {
		expNullable := expected[col]
		lc, ok := liveByName[col]
		if !ok {
			entries = append(entries, SchemaDriftEntry{
				Kind:   DriftKindSchemaMissingColumn,
				Table:  table,
				Column: col,
			})
			continue
		}
		if expNullable != lc.Nullable {
			entries = append(entries, SchemaDriftEntry{
				Kind:     DriftKindSchemaColumnNullability,
				Table:    table,
				Column:   col,
				Expected: nullabilityLabel(expNullable),
				Actual:   nullabilityLabel(lc.Nullable),
			})
		}
	}

	for _, lc := range live {
		col := strings.ToLower(lc.Name)
		if _, ok := expected[col]; !ok {
			entries = append(entries, SchemaDriftEntry{
				Kind:   DriftKindSchemaExtraColumn,
				Table:  table,
				Column: col,
			})
		}
	}
	return entries
}

// introspectTableColumns runs a dialect-specific query and returns the
// columns of the given table. A nil slice means "the table does not
// exist" — distinct from an empty slice (which would mean "table
// exists but has no columns", an unreachable state in practice).
func introspectTableColumns(ctx context.Context, db *sql.DB, system, table string) ([]liveColumn, error) {
	var (
		query string
		args  []any
	)
	switch system {
	case "sqlite":
		// SQLite has no information_schema; PRAGMA table_info is the
		// canonical path. It returns rows for an existing table and an
		// empty result set for a missing one — we cannot distinguish
		// "missing" from "no columns" from PRAGMA alone, so we
		// pre-check the master table.
		var name string
		err := db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		// SQLite quirk: INTEGER PRIMARY KEY columns report notnull=0
		// in pragma_table_info because they are an alias for ROWID, a
		// dialect-specific "not really nullable but PRAGMA says so"
		// state. The expected-side of the comparator treats PK columns
		// as NOT NULL (PKs always are), so we mirror that here by
		// forcing nullable=false when the PRAGMA pk column is non-zero.
		// Without this fix every SQLite test reports a spurious
		// nullability mismatch on the id column.
		query = `SELECT name,
		                CASE WHEN "notnull" = 0 AND pk = 0 THEN 1 ELSE 0 END AS nullable
		         FROM pragma_table_info(?)`
		args = []any{table}
	case "postgresql":
		// PG: a missing table returns zero rows from
		// information_schema.tables. Pre-check, then list columns.
		var exists int
		err := db.QueryRowContext(ctx,
			`SELECT 1 FROM information_schema.tables
			 WHERE table_schema = current_schema() AND table_name = $1`, table).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		query = `SELECT column_name,
		                CASE WHEN is_nullable = 'YES' THEN 1 ELSE 0 END AS nullable
		         FROM information_schema.columns
		         WHERE table_schema = current_schema() AND table_name = $1
		         ORDER BY ordinal_position`
		args = []any{table}
	case "mysql":
		var exists int
		err := db.QueryRowContext(ctx,
			`SELECT 1 FROM information_schema.tables
			 WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		query = `SELECT COLUMN_NAME,
		                CASE WHEN IS_NULLABLE = 'YES' THEN 1 ELSE 0 END AS nullable
		         FROM information_schema.COLUMNS
		         WHERE table_schema = DATABASE() AND table_name = ?
		         ORDER BY ORDINAL_POSITION`
		args = []any{table}
	default:
		return nil, fmt.Errorf("introspectTableColumns: unsupported system %q", system)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make([]liveColumn, 0, 8)
	for rows.Next() {
		var (
			name     string
			nullable int
		)
		if err := rows.Scan(&name, &nullable); err != nil {
			return nil, err
		}
		cols = append(cols, liveColumn{Name: name, Nullable: nullable == 1})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func nullabilityLabel(nullable bool) string {
	if nullable {
		return "nullable"
	}
	return "not_null"
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
