package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jcsvwinston/GoFrame/pkg/observe"
	"github.com/uptrace/bun"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	cfg := Config{
		Engine:          EngineGORM,
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

func newTestBunDB(t *testing.T) *DB {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	cfg := Config{
		Engine:          EngineBun,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}
	d, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create test Bun DB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestNew_DefaultEngineIsBun(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	d, err := New(Config{
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if d.BunDB() == nil {
		t.Fatal("BunDB() should not be nil")
	}
	if d.Engine() != EngineBun {
		t.Fatalf("expected engine %s, got %s", EngineBun, d.Engine())
	}
}

func TestNew_Bun_SQLiteMemory(t *testing.T) {
	d := newTestBunDB(t)
	if d.BunDB() == nil {
		t.Fatal("BunDB() should not be nil")
	}
	if d.GormDB() != nil {
		t.Fatal("GormDB() should be nil for bun engine")
	}
	if d.Engine() != EngineBun {
		t.Fatalf("expected engine %s, got %s", EngineBun, d.Engine())
	}
}

func TestNew_GORM_SQLiteMemory(t *testing.T) {
	d := newTestDB(t)
	if d.GormDB() == nil {
		t.Fatal("GormDB() should not be nil")
	}
	if d.Engine() != EngineGORM {
		t.Fatalf("expected engine %s, got %s", EngineGORM, d.Engine())
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

func TestTx_OnBunEngine_ReturnsErrGORMRequired(t *testing.T) {
	d := newTestBunDB(t)
	err := d.Tx(context.Background(), func(tx *gorm.DB) error { return nil })
	if !errors.Is(err, ErrGORMRequired) {
		t.Fatalf("expected ErrGORMRequired, got %v", err)
	}
}

func TestTxBun_OnGORMEngine_ReturnsErrBunRequired(t *testing.T) {
	d := newTestDB(t)
	err := d.TxBun(context.Background(), func(tx bun.Tx) error { return nil })
	if !errors.Is(err, ErrBunRequired) {
		t.Fatalf("expected ErrBunRequired, got %v", err)
	}
}

func TestTxBun_Success(t *testing.T) {
	d := newTestBunDB(t)
	if err := d.TxBun(context.Background(), func(tx bun.Tx) error { return nil }); err != nil {
		t.Fatalf("expected TxBun success, got %v", err)
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
