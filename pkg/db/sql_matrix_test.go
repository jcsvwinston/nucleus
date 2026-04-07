package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

func TestSQLMatrix_ConnectAndPing(t *testing.T) {
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
		Engine:      EngineSQL,
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

func TestSQLMatrix_ExploratoryURLCompatibility(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("GOFRAME_SQL_EXPLORATORY_URL"))
	if rawURL == "" {
		t.Skip("GOFRAME_SQL_EXPLORATORY_URL is not set; skipping exploratory compatibility test")
	}

	logger := observe.NewLogger("error", "text")
	database, err := New(Config{
		Engine:      EngineSQL,
		DatabaseURL: rawURL,
	}, logger)
	if err == nil && database != nil {
		_ = database.Close()
	}

	lower := strings.ToLower(rawURL)
	isEnterpriseProfile := strings.HasPrefix(lower, "sqlserver://") || strings.HasPrefix(lower, "mssql://") || strings.HasPrefix(lower, "oracle://")

	if isEnterpriseProfile {
		// For enterprise profiles, the scheme is recognized by the runtime.
		// In exploratory CI lanes, connection may still fail (no live server), but
		// it must not fail due to unsupported URL parsing.
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "unsupported database url scheme") {
			t.Fatalf("expected recognized exploratory scheme for %q, got: %v", rawURL, err)
		}
		return
	}

	if err == nil {
		t.Fatalf("expected unsupported database scheme for exploratory URL %q", rawURL)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported database url scheme") {
		t.Fatalf("unexpected exploratory URL error for unsupported profile %q: %v", rawURL, err)
	}
}

func TestSQLMatrix_ExploratoryLiveConnectAndPing(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("GOFRAME_SQL_EXPLORATORY_URL"))
	if rawURL == "" {
		t.Skip("GOFRAME_SQL_EXPLORATORY_URL is not set; skipping exploratory live connectivity test")
	}

	lower := strings.ToLower(rawURL)
	isEnterpriseProfile := strings.HasPrefix(lower, "sqlserver://") || strings.HasPrefix(lower, "mssql://") || strings.HasPrefix(lower, "oracle://")
	if !isEnterpriseProfile {
		t.Skipf("GOFRAME_SQL_EXPLORATORY_URL=%q is not an enterprise exploratory profile", rawURL)
	}

	database, err := waitForExploratorySQLReady(rawURL, 4*time.Minute)
	if err != nil {
		t.Fatalf("live connectivity failed for exploratory URL %q: %v", rawURL, err)
	}
	defer func() { _ = database.Close() }()

	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("db.SqlDB failed for exploratory URL %q: %v", rawURL, err)
	}

	query := "SELECT 1"
	if strings.HasPrefix(lower, "oracle://") {
		query = "SELECT 1 FROM dual"
	}

	var n int
	if err := sqlDB.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s failed for exploratory URL %q: %v", query, rawURL, err)
	}
	if n != 1 {
		t.Fatalf("expected %s to return 1, got %d", query, n)
	}
}

func waitForExploratorySQLReady(rawURL string, timeout time.Duration) (*DB, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for {
		logger := observe.NewLogger("error", "text")
		database, err := New(Config{
			Engine:      EngineSQL,
			DatabaseURL: rawURL,
		}, logger)
		if err == nil {
			if healthErr := database.Health(context.Background()); healthErr == nil {
				return database, nil
			} else {
				lastErr = healthErr
			}
			_ = database.Close()
		} else {
			lastErr = err
			if strings.Contains(strings.ToLower(err.Error()), "unsupported database url scheme") {
				return nil, err
			}
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("database not ready before timeout (%s): %w", timeout, lastErr)
		}
		time.Sleep(2 * time.Second)
	}
}

func TestOpenSQLDB_EnterpriseCandidatesSupported(t *testing.T) {
	candidates := []string{
		"sqlserver://sa:Password123!@localhost:1433/master",
		"mssql://sa:Password123!@localhost:1433/master",
		"oracle://system:oracle@localhost:1521/FREEPDB1",
	}

	for _, rawURL := range candidates {
		t.Run(rawURL, func(t *testing.T) {
			conn, err := openSQLDB(rawURL)
			if err != nil {
				t.Fatalf("expected supported sql DB URL for %q, got err=%v", rawURL, err)
			}
			_ = conn.Close()
		})
	}
}
