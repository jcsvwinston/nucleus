package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// expectedDriftUsersTable is the canonical "what we expect to see"
// table fed into SchemaDrift across the SQLite unit tests. Mirrors the
// shape of the model used in the live-DB AutoMigrate tests (pk
// integer, NOT NULL email, nullable nickname, NOT NULL created_at) so
// the two test surfaces stay in sync.
var expectedDriftUsersTable = ExpectedTable{
	Name: "drift_users",
	Columns: []ExpectedColumn{
		{Name: "id", Nullable: false},
		{Name: "email", Nullable: false},
		{Name: "nickname", Nullable: true},
		{Name: "created_at", Nullable: false},
	},
}

var expectedDriftExtrasTable = ExpectedTable{
	Name: "drift_extras",
	Columns: []ExpectedColumn{
		{Name: "id", Nullable: false},
		{Name: "label", Nullable: false},
	},
}

func TestSchemaDrift_EmptyWhenLiveSchemaMatchesExpected(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	writeMigrationPair(t, dir, "000001_create_drift_users",
		`CREATE TABLE drift_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			nickname TEXT,
			created_at DATETIME NOT NULL
		);`,
		"DROP TABLE IF EXISTS drift_users;",
	)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	drift, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if err != nil {
		t.Fatalf("SchemaDrift: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("expected no drift when live schema matches expected, got %+v", drift)
	}
}

func TestSchemaDrift_FlagsMissingTable(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	// Apply only the drift_users migration; the comparator will be
	// asked about both drift_users and drift_extras and must report
	// the latter as a missing table.
	writeMigrationPair(t, dir, "000001_create_drift_users",
		`CREATE TABLE drift_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			nickname TEXT,
			created_at DATETIME NOT NULL
		);`,
		"DROP TABLE IF EXISTS drift_users;",
	)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	drift, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable, expectedDriftExtrasTable})
	if err != nil {
		t.Fatalf("SchemaDrift: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("expected exactly one missing-table entry, got %d (%+v)", len(drift), drift)
	}
	if drift[0].Kind != DriftKindSchemaMissingTable {
		t.Fatalf("unexpected drift kind: %q", drift[0].Kind)
	}
	if drift[0].Table != "drift_extras" {
		t.Fatalf("unexpected table: %q", drift[0].Table)
	}
	if drift[0].Column != "" {
		t.Fatalf("missing-table entries must not carry a column, got %q", drift[0].Column)
	}
}

func TestSchemaDrift_FlagsMissingColumn(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	// Live table is missing the `nickname` column.
	writeMigrationPair(t, dir, "000001_create_drift_users",
		`CREATE TABLE drift_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		"DROP TABLE IF EXISTS drift_users;",
	)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	drift, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if err != nil {
		t.Fatalf("SchemaDrift: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("expected exactly one missing-column entry, got %d (%+v)", len(drift), drift)
	}
	if drift[0].Kind != DriftKindSchemaMissingColumn || drift[0].Column != "nickname" {
		t.Fatalf("unexpected drift entry: %+v", drift[0])
	}
}

func TestSchemaDrift_FlagsExtraColumn(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	// Live table has an extra `legacy_handle` column the expected
	// schema does not declare. Classic ad-hoc-DDL drift.
	writeMigrationPair(t, dir, "000001_create_drift_users",
		`CREATE TABLE drift_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			nickname TEXT,
			created_at DATETIME NOT NULL,
			legacy_handle TEXT
		);`,
		"DROP TABLE IF EXISTS drift_users;",
	)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	drift, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if err != nil {
		t.Fatalf("SchemaDrift: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("expected exactly one extra-column entry, got %d (%+v)", len(drift), drift)
	}
	if drift[0].Kind != DriftKindSchemaExtraColumn || drift[0].Column != "legacy_handle" {
		t.Fatalf("unexpected drift entry: %+v", drift[0])
	}
}

func TestSchemaDrift_FlagsNullabilityMismatch(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	// `email` is NOT NULL in the expected schema but nullable in the
	// live one — the canonical "someone relaxed a constraint
	// out-of-band" drift.
	writeMigrationPair(t, dir, "000001_create_drift_users",
		`CREATE TABLE drift_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT,
			nickname TEXT,
			created_at DATETIME NOT NULL
		);`,
		"DROP TABLE IF EXISTS drift_users;",
	)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	drift, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if err != nil {
		t.Fatalf("SchemaDrift: %v", err)
	}
	if len(drift) != 1 {
		t.Fatalf("expected exactly one nullability-mismatch entry, got %d (%+v)", len(drift), drift)
	}
	got := drift[0]
	if got.Kind != DriftKindSchemaColumnNullability {
		t.Fatalf("unexpected drift kind: %q", got.Kind)
	}
	if got.Column != "email" {
		t.Fatalf("unexpected column: %q", got.Column)
	}
	if got.Expected != "not_null" || got.Actual != "nullable" {
		t.Fatalf("unexpected polarity report: expected %q vs actual %q", got.Expected, got.Actual)
	}
}

func TestSchemaDrift_ResultsSortedByTableThenKindThenColumn(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	// Two single-statement migrations rather than one multi-statement
	// `.up.sql`: the pure-Go SQLite driver (`modernc.org/sqlite`,
	// occasionally used in CI lanes) executes only the first
	// statement in a multi-statement Exec call. Splitting keeps the
	// test portable across both CGO and pure-Go drivers.
	writeMigrationPair(t, dir, "000001_create_drift_users",
		`CREATE TABLE drift_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT,                   -- nullability drift
			nickname TEXT,
			created_at DATETIME NOT NULL,
			zextra_col TEXT               -- extra-column drift, alphabetically after nickname
		);`,
		"DROP TABLE IF EXISTS drift_users;",
	)
	writeMigrationPair(t, dir, "000002_create_drift_extras",
		`CREATE TABLE drift_extras (
			id INTEGER PRIMARY KEY AUTOINCREMENT
			-- missing the required 'label' column
		);`,
		"DROP TABLE IF EXISTS drift_extras;",
	)
	m := NewMigrator(d, dir, observe.NewLogger("error", "text"))
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}

	drift, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable, expectedDriftExtrasTable})
	if err != nil {
		t.Fatalf("SchemaDrift: %v", err)
	}
	if len(drift) < 3 {
		t.Fatalf("expected at least three drift entries, got %d (%+v)", len(drift), drift)
	}

	// Validate the (table, kind, column) sort.
	for i := 1; i < len(drift); i++ {
		prev, cur := drift[i-1], drift[i]
		if prev.Table > cur.Table {
			t.Fatalf("drift entries not sorted by table at index %d: %+v vs %+v", i, prev, cur)
		}
		if prev.Table == cur.Table && prev.Kind > cur.Kind {
			t.Fatalf("drift entries not sorted by kind within table at index %d: %+v vs %+v", i, prev, cur)
		}
		if prev.Table == cur.Table && prev.Kind == cur.Kind && prev.Column > cur.Column {
			t.Fatalf("drift entries not sorted by column within (table, kind) at index %d: %+v vs %+v", i, prev, cur)
		}
	}
}

func TestSchemaDrift_ReturnsErrSchemaDriftUnsupported_ForMSSQL(t *testing.T) {
	// The system check is meant to short-circuit BEFORE acquiring a
	// sql handle. Construct a Migrator with a forced "mssql" system
	// and confirm the sentinel comes back without the call needing a
	// real DB.
	d := &DB{system: "mssql"}
	m := &Migrator{db: d}
	_, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if err == nil {
		t.Fatal("expected an error for mssql, got nil")
	}
	if !errors.Is(err, ErrSchemaDriftUnsupported) {
		t.Fatalf("expected ErrSchemaDriftUnsupported, got %v", err)
	}
}

func TestSchemaDrift_ReturnsErrSchemaDriftUnsupported_ForOracle(t *testing.T) {
	d := &DB{system: "oracle"}
	m := &Migrator{db: d}
	_, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if !errors.Is(err, ErrSchemaDriftUnsupported) {
		t.Fatalf("expected ErrSchemaDriftUnsupported, got %v", err)
	}
}

func TestSchemaDrift_NilReceiverIsRejected(t *testing.T) {
	var m *Migrator
	_, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if err == nil {
		t.Fatal("expected an error for nil receiver, got nil")
	}
	if !strings.Contains(err.Error(), "nil receiver") {
		t.Fatalf("expected the error message to mention nil receiver, got %v", err)
	}
}

func TestSchemaDrift_NilDatabaseIsRejected(t *testing.T) {
	m := &Migrator{db: nil}
	_, err := m.SchemaDrift(context.Background(), []ExpectedTable{expectedDriftUsersTable})
	if err == nil {
		t.Fatal("expected an error for nil database, got nil")
	}
	if !strings.Contains(err.Error(), "nil database") {
		t.Fatalf("expected the error message to mention nil database, got %v", err)
	}
}

func TestSchemaDrift_EmptyExpectedReturnsEmptyDrift(t *testing.T) {
	d := newTestDB(t)
	m := NewMigrator(d, t.TempDir(), observe.NewLogger("error", "text"))
	drift, err := m.SchemaDrift(context.Background(), nil)
	if err != nil {
		t.Fatalf("SchemaDrift: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("expected empty drift slice for nil expected input, got %+v", drift)
	}

	drift, err = m.SchemaDrift(context.Background(), []ExpectedTable{})
	if err != nil {
		t.Fatalf("SchemaDrift on empty slice: %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("expected empty drift slice for []ExpectedTable{} input, got %+v", drift)
	}
}

func TestSchemaDrift_DuplicateExpectedTableNameIsRejected(t *testing.T) {
	d := newTestDB(t)
	m := NewMigrator(d, t.TempDir(), observe.NewLogger("error", "text"))
	dup := []ExpectedTable{
		{Name: "drift_users", Columns: []ExpectedColumn{{Name: "id", Nullable: false}}},
		{Name: "drift_users", Columns: []ExpectedColumn{{Name: "id", Nullable: false}}},
	}
	_, err := m.SchemaDrift(context.Background(), dup)
	if err == nil {
		t.Fatal("expected an error for duplicate ExpectedTable name, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate ExpectedTable name") {
		t.Fatalf("expected the error to mention duplicate ExpectedTable name, got %v", err)
	}
}

func TestSchemaDrift_EmptyExpectedTableNameIsRejected(t *testing.T) {
	d := newTestDB(t)
	m := NewMigrator(d, t.TempDir(), observe.NewLogger("error", "text"))
	_, err := m.SchemaDrift(context.Background(), []ExpectedTable{{Name: "  ", Columns: nil}})
	if err == nil {
		t.Fatal("expected an error for empty ExpectedTable name, got nil")
	}
	if !strings.Contains(err.Error(), "empty Name") {
		t.Fatalf("expected the error to mention empty Name, got %v", err)
	}
}
