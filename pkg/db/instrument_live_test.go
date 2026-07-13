package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestInstrumentLive_WrapsRealDriver exercises the driver wrapper against a
// real engine from the CI SQL matrix (postgres/mysql required; mssql/oracle
// exploratory). The unit tests in instrument_test.go use sqlite; this proves
// the optional-interface forwarding (QueryerContext/ExecerContext,
// NamedValueChecker, prepared statements) does not break drivers with richer
// interface sets — and that direct statements are observed while
// model-observed ones are skipped.
func TestInstrumentLive_WrapsRealDriver(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("NUCLEUS_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("NUCLEUS_SQL_MATRIX_URL is not set; skipping instrumentation live test")
	}
	// Restrict to the required matrix profiles (postgres/mysql): they carry
	// the richest driver interface sets (pgx implements NamedValueChecker) and
	// use portable placeholders. The exploratory mssql/oracle lanes use
	// engine-specific placeholders (@pN, :1) that would complicate this raw-SQL
	// test without adding wrapper coverage — the wrapper is driver-agnostic.
	lower := strings.ToLower(rawURL)
	if !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://") && !strings.HasPrefix(lower, "mysql://") {
		t.Skipf("NUCLEUS_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}

	c := &collector{}
	sqlDB, err := openConfiguredDB(Config{
		DatabaseURL:       rawURL,
		StatementObserver: c.observe,
	})
	if err != nil {
		t.Fatalf("openConfiguredDB(%q): %v", rawURL, err)
	}
	defer func() { _ = sqlDB.Close() }()

	ctx := context.Background()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// A unique-ish table name; best-effort drop first and after.
	const tbl = "nucleus_instr_live"
	_, _ = sqlDB.ExecContext(ctx, "DROP TABLE "+tbl)
	if _, err := sqlDB.ExecContext(ctx, "CREATE TABLE "+tbl+" (id INTEGER, label VARCHAR(64))"); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _, _ = sqlDB.ExecContext(context.Background(), "DROP TABLE "+tbl) }()

	// Placeholder style differs per engine; the CRUD layer rebinds, but here
	// we issue raw SQL, so use each engine's native placeholder.
	insertSQL := "INSERT INTO " + tbl + " (id, label) VALUES (?, ?)"
	if strings.HasPrefix(lower, "postgres") {
		insertSQL = "INSERT INTO " + tbl + " (id, label) VALUES ($1, $2)"
	}
	if _, err := sqlDB.ExecContext(ctx, insertSQL, 1, "alpha"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// The direct INSERT must have been observed with its args passed raw.
	var insertSeen bool
	for _, s := range c.snapshot() {
		if s.Operation == "insert" && len(s.Args) == 2 {
			insertSeen = true
		}
	}
	if !insertSeen {
		t.Errorf("live INSERT was not observed by the driver wrapper; seen=%+v", c.snapshot())
	}

	// A model-observed context must be skipped even on the real driver.
	before := len(c.snapshot())
	crudCtx := observe.CtxWithModelObserved(ctx)
	if _, err := sqlDB.ExecContext(crudCtx, insertSQL, 2, "beta"); err != nil {
		t.Fatalf("marked insert: %v", err)
	}
	if len(c.snapshot()) != before {
		t.Errorf("model-observed live statement was recorded by the wrapper")
	}
}
