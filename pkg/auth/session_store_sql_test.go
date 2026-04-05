package auth

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/glebarez/sqlite"
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
		TableName:   "goframe_sessions",
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
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM "goframe_sessions" WHERE token = ?`, "token-expired").Scan(&count); err != nil {
		t.Fatalf("count expired rows failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected expired session row deleted, got count=%d", count)
	}
}
