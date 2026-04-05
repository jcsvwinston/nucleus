package db

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

func TestSQLMatrix_BunConnectAndPing(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("GOFRAME_SQL_MATRIX_URL"))
	if rawURL == "" {
		t.Skip("GOFRAME_SQL_MATRIX_URL is not set; skipping SQL matrix integration test")
	}

	lower := strings.ToLower(rawURL)
	if !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://") && !strings.HasPrefix(lower, "mysql://") {
		t.Skipf("GOFRAME_SQL_MATRIX_URL=%q is not a required SQL matrix profile", rawURL)
	}

	logger := observe.NewLogger("error", "text")
	database, err := New(Config{
		Engine:      EngineBun,
		DatabaseURL: rawURL,
	}, logger)
	if err != nil {
		t.Fatalf("db.New failed for SQL matrix URL %q: %v", rawURL, err)
	}
	defer func() { _ = database.Close() }()

	if err := database.Health(context.Background()); err != nil {
		t.Fatalf("db.Health failed for SQL matrix URL %q: %v", rawURL, err)
	}

	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("db.SqlDB failed for SQL matrix URL %q: %v", rawURL, err)
	}

	var n int
	if err := sqlDB.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("SELECT 1 failed for SQL matrix URL %q: %v", rawURL, err)
	}
	if n != 1 {
		t.Fatalf("expected SELECT 1 to return 1, got %d", n)
	}
}

func TestSQLMatrix_UnsupportedExploratoryURL(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("GOFRAME_SQL_EXPLORATORY_URL"))
	if rawURL == "" {
		t.Skip("GOFRAME_SQL_EXPLORATORY_URL is not set; skipping exploratory compatibility test")
	}

	logger := observe.NewLogger("error", "text")
	_, err := New(Config{
		Engine:      EngineBun,
		DatabaseURL: rawURL,
	}, logger)
	if err == nil {
		t.Fatalf("expected unsupported database scheme for exploratory URL %q", rawURL)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported database url scheme") {
		t.Fatalf("unexpected exploratory URL error for %q: %v", rawURL, err)
	}
}

func TestDialectorFromURL_UnsupportedEnterpriseCandidates(t *testing.T) {
	candidates := []string{
		"sqlserver://sa:Password123!@localhost:1433/master",
		"mssql://sa:Password123!@localhost:1433/master",
		"oracle://system:oracle@localhost:1521/FREEPDB1",
	}

	for _, rawURL := range candidates {
		t.Run(rawURL, func(t *testing.T) {
			_, err := dialectorFromURL(rawURL)
			if err == nil {
				t.Fatalf("expected unsupported dialector URL error for %q", rawURL)
			}
		})
	}
}

func TestOpenSQLDB_UnsupportedEnterpriseCandidates(t *testing.T) {
	candidates := []string{
		"sqlserver://sa:Password123!@localhost:1433/master",
		"mssql://sa:Password123!@localhost:1433/master",
		"oracle://system:oracle@localhost:1521/FREEPDB1",
	}

	for _, rawURL := range candidates {
		t.Run(rawURL, func(t *testing.T) {
			_, err := openSQLDB(rawURL)
			if err == nil {
				t.Fatalf("expected unsupported sql DB URL error for %q", rawURL)
			}
		})
	}
}

func TestBunDialectFromURL_UnsupportedEnterpriseCandidates(t *testing.T) {
	candidates := []string{
		"sqlserver://sa:Password123!@localhost:1433/master",
		"mssql://sa:Password123!@localhost:1433/master",
		"oracle://system:oracle@localhost:1521/FREEPDB1",
	}

	for _, rawURL := range candidates {
		t.Run(rawURL, func(t *testing.T) {
			_, err := bunDialectFromURL(rawURL)
			if err == nil {
				t.Fatalf("expected unsupported bun dialect URL error for %q", rawURL)
			}
		})
	}
}
