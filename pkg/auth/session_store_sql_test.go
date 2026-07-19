package auth

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestSQLSessionStore(t *testing.T) (*SQLSessionStore, *sql.DB) {
	t.Helper()

	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	store, err := NewSQLSessionStore(dbConn, SQLSessionStoreConfig{
		DatabaseURL: "sqlite://:memory:",
		TableName:   "nucleus_sessions",
	})
	if err != nil {
		t.Fatalf("new sql session store: %v", err)
	}

	return store, dbConn
}

func TestNewSQLSessionStore_InvalidTableName(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer dbConn.Close()

	_, err = NewSQLSessionStore(dbConn, SQLSessionStoreConfig{
		DatabaseURL: "sqlite://:memory:",
		TableName:   "bad-name",
	})
	if err == nil {
		t.Fatal("expected invalid table name error")
	}
}

func TestSQLSessionStore_CommitFindDelete(t *testing.T) {
	store, _ := newTestSQLSessionStore(t)

	expiry := time.Now().UTC().Add(30 * time.Minute)
	if err := store.Commit("token-1", []byte("payload"), expiry); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	got, found, err := store.Find("token-1")
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if string(got) != "payload" {
		t.Fatalf("unexpected payload %q", string(got))
	}

	if err := store.Delete("token-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, found, err = store.Find("token-1")
	if err != nil {
		t.Fatalf("find after delete failed: %v", err)
	}
	if found {
		t.Fatal("expected found=false after delete")
	}
}

func TestSQLSessionStore_ExpiredTokenReturnsMissingAndIsRemoved(t *testing.T) {
	store, dbConn := newTestSQLSessionStore(t)

	expiry := time.Now().UTC().Add(-10 * time.Minute)
	if err := store.Commit("token-expired", []byte("payload"), expiry); err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	_, found, err := store.Find("token-expired")
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found {
		t.Fatal("expected expired session to be treated as missing")
	}

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM "nucleus_sessions" WHERE token = ?`, "token-expired").Scan(&count); err != nil {
		t.Fatalf("count expired rows failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected expired session row deleted, got count=%d", count)
	}
}

func TestSQLSessionStore_AllReturnsOnlyActiveSessions(t *testing.T) {
	store, _ := newTestSQLSessionStore(t)

	if err := store.Commit("token-active", []byte("active"), time.Now().UTC().Add(20*time.Minute)); err != nil {
		t.Fatalf("commit active failed: %v", err)
	}
	if err := store.Commit("token-expired", []byte("expired"), time.Now().UTC().Add(-20*time.Minute)); err != nil {
		t.Fatalf("commit expired failed: %v", err)
	}

	all, err := store.All()
	if err != nil {
		t.Fatalf("all failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 active session, got %d", len(all))
	}
	if string(all["token-active"]) != "active" {
		t.Fatalf("unexpected payload for active token: %q", string(all["token-active"]))
	}
	if _, ok := all["token-expired"]; ok {
		t.Fatal("expired token should not be included in All()")
	}
}

func TestParseSessionExpiryValue(t *testing.T) {
	t.Run("time.Time", func(t *testing.T) {
		input := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
		result, err := parseSessionExpiryValue(input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !result.Equal(input) {
			t.Error("expected same time")
		}
	})

	t.Run("RFC3339Nano string", func(t *testing.T) {
		input := "2024-01-15T10:30:45.123456789Z"
		result, err := parseSessionExpiryValue(input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result.IsZero() {
			t.Error("expected non-zero time")
		}
	})

	t.Run("byte slice", func(t *testing.T) {
		input := []byte("2024-01-15 10:30:45")
		result, err := parseSessionExpiryValue(input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result.IsZero() {
			t.Error("expected non-zero time")
		}
	})

	t.Run("int64 unix timestamp", func(t *testing.T) {
		input := int64(1705327845)
		result, err := parseSessionExpiryValue(input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result.IsZero() {
			t.Error("expected non-zero time")
		}
	})

	t.Run("float64 unix timestamp", func(t *testing.T) {
		input := float64(1705327845)
		result, err := parseSessionExpiryValue(input)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result.IsZero() {
			t.Error("expected non-zero time")
		}
	})

	t.Run("unsupported type", func(t *testing.T) {
		_, err := parseSessionExpiryValue(true)
		if err == nil {
			t.Error("expected error for unsupported type")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := parseSessionExpiryValue("")
		if err == nil {
			t.Error("expected error for empty string")
		}
	})
}

// A database URL for a dialect the SQL session store cannot speak must
// fail at construction with an explicit error — not fall back to the
// sqlite grammar and emit invalid SQL (LIMIT, ON CONFLICT, BLOB DDL) at
// runtime (NU6-3).
func TestNewSQLSessionStore_UnsupportedDialectFailsFast(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })

	cases := map[string]string{
		"mssql://sa:pass@localhost:1433/master":     "mssql",
		"sqlserver://sa:pass@localhost:1433/master": "mssql",
		"oracle://system:pass@localhost:1521/xe":    "oracle",
	}
	for url, dialect := range cases {
		_, err := NewSQLSessionStore(dbConn, SQLSessionStoreConfig{DatabaseURL: url})
		if err == nil {
			t.Fatalf("%s: expected a construction error for an unsupported dialect", url)
		}
		for _, want := range []string{"supports sqlite/postgres/mysql", "got " + dialect} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: error %q missing %q", url, err.Error(), want)
			}
		}
	}

	// The constructor must fail before touching the schema.
	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table'`).Scan(&count); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no tables created by failed constructors, got %d", count)
	}
}
