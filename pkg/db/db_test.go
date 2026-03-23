package db

import (
	"context"
	"errors"
	"testing"

	"github.com/goframe/goframe/pkg/app"
	"github.com/goframe/goframe/pkg/observe"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	cfg := &app.Config{
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}
	d, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestNew_SQLiteMemory(t *testing.T) {
	d := newTestDB(t)
	if d.GormDB() == nil {
		t.Fatal("GormDB() should not be nil")
	}
}

func TestHealth(t *testing.T) {
	d := newTestDB(t)
	if err := d.Health(context.Background()); err != nil {
		t.Fatalf("Health failed: %v", err)
	}
}

func TestTx_Commit(t *testing.T) {
	d := newTestDB(t)

	// Create a test table
	d.GormDB().Exec("CREATE TABLE tx_test (id INTEGER PRIMARY KEY, name TEXT)")

	err := d.Tx(context.Background(), func(tx *gorm.DB) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Tx should succeed: %v", err)
	}
}

func TestTx_Rollback(t *testing.T) {
	d := newTestDB(t)
	d.GormDB().Exec("CREATE TABLE tx_test2 (id INTEGER PRIMARY KEY, name TEXT)")

	err := d.Tx(context.Background(), func(tx *gorm.DB) error {
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("Tx should return error on rollback")
	}
}

func TestDialectorFromURL_Unsupported(t *testing.T) {
	_, err := dialectorFromURL("ftp://something")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestDialectorFromURL_Sqlite(t *testing.T) {
	d, err := dialectorFromURL("sqlite://:memory:")
	if err != nil || d == nil {
		t.Fatalf("expected valid dialector, got err=%v", err)
	}
}

func TestDialectorFromURL_DbFile(t *testing.T) {
	d, err := dialectorFromURL("test.db")
	if err != nil || d == nil {
		t.Fatalf("expected valid dialector for .db file, got err=%v", err)
	}
}

func TestMySQLURLToDSN(t *testing.T) {
	dsn, err := mysqlURLToDSN("mysql://user:pass@localhost:3306/mydb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "user:pass@tcp(localhost:3306)/mydb?parseTime=true&charset=utf8mb4"
	if dsn != expected {
		t.Errorf("expected %s, got %s", expected, dsn)
	}
}
