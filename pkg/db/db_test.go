package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	cfg := Config{
		Engine:          EngineSQL,
		DatabaseURL:     "sqlite://:memory:",
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}
	d, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestNew_DefaultEngineIsSQL(t *testing.T) {
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

	if d.Engine() != EngineSQL {
		t.Fatalf("expected engine %s, got %s", EngineSQL, d.Engine())
	}
}

func TestNew_UnsupportedEngine(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	_, err := New(Config{
		Engine:      "bun",
		DatabaseURL: "sqlite://:memory:",
	}, logger)
	if !errors.Is(err, ErrUnsupportedEngine) {
		t.Fatalf("expected ErrUnsupportedEngine, got %v", err)
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
	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB failed: %v", err)
	}
	if _, err := sqlDB.Exec("CREATE TABLE tx_test (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	err = d.Tx(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO tx_test (name) VALUES (?)", "ok")
		return err
	})
	if err != nil {
		t.Fatalf("Tx should succeed: %v", err)
	}
}

func TestTx_Rollback(t *testing.T) {
	d := newTestDB(t)
	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB failed: %v", err)
	}
	if _, err := sqlDB.Exec("CREATE TABLE tx_test2 (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	err = d.Tx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO tx_test2 (name) VALUES (?)", "rollback"); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("Tx should return error on rollback")
	}
}

func TestTx_WithNilReceiver(t *testing.T) {
	var d *DB
	err := d.Tx(context.Background(), func(tx *sql.Tx) error { return nil })
	if !errors.Is(err, ErrSQLRequired) {
		t.Fatalf("expected ErrSQLRequired, got %v", err)
	}
}

func TestOpenSQLDB_Unsupported(t *testing.T) {
	_, err := openSQLDB("ftp://something")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}

func TestOpenSQLDB_Sqlite(t *testing.T) {
	d, err := openSQLDB("sqlite://:memory:")
	if err != nil || d == nil {
		t.Fatalf("expected valid SQL DB, got err=%v", err)
	}
	_ = d.Close()
}

func TestOpenSQLDB_DbFile(t *testing.T) {
	d, err := openSQLDB("test.db")
	if err != nil || d == nil {
		t.Fatalf("expected valid SQL DB for .db file, got err=%v", err)
	}
	_ = d.Close()
}

func TestOpenSQLDB_MSSQLAndOracleSchemes(t *testing.T) {
	cases := []string{
		"sqlserver://sa:Password123!@localhost:1433/master",
		"mssql://sa:Password123!@localhost:1433/master",
		"oracle://system:oracle@localhost:1521/FREEPDB1",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			conn, err := openSQLDB(raw)
			if err != nil || conn == nil {
				t.Fatalf("expected valid DB handle for %q, got err=%v", raw, err)
			}
			_ = conn.Close()
		})
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

func TestNormalizeMSSQLURL(t *testing.T) {
	if got := normalizeMSSQLURL("mssql://sa:pass@localhost:1433/master"); got != "sqlserver://sa:pass@localhost:1433/master" {
		t.Fatalf("unexpected mssql alias normalization: %s", got)
	}
	if got := normalizeMSSQLURL("sqlserver://sa:pass@localhost:1433/master"); got != "sqlserver://sa:pass@localhost:1433/master" {
		t.Fatalf("unexpected sqlserver URL normalization: %s", got)
	}
}
