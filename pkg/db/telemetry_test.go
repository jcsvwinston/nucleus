package db

import (
	"database/sql"
	"testing"
)

func TestDBSystemFromURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "postgres", raw: "postgres://user:pass@localhost:5432/app?sslmode=disable", want: "postgresql"},
		{name: "postgresql", raw: "postgresql://user:pass@localhost:5432/app?sslmode=disable", want: "postgresql"},
		{name: "mysql", raw: "mysql://root:root@localhost:3306/app", want: "mysql"},
		{name: "mssql", raw: "sqlserver://sa:pass@localhost:1433/master", want: "mssql"},
		{name: "mssql alias", raw: "mssql://sa:pass@localhost:1433/master", want: "mssql"},
		{name: "oracle", raw: "oracle://system:oracle@localhost:1521/FREEPDB1", want: "oracle"},
		{name: "sqlite scheme", raw: "sqlite://app.db", want: "sqlite"},
		{name: "sqlite file", raw: "app.sqlite", want: "sqlite"},
		{name: "unknown", raw: "db2://db2inst1:pass@localhost:50000/sample", want: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := dbSystemFromURL(tc.raw); got != tc.want {
				t.Fatalf("dbSystemFromURL(%q)=%q; want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestClassifySQLOperation(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{sql: "SELECT * FROM users", want: "select"},
		{sql: " insert into users(id) values(1)", want: "insert"},
		{sql: "UPDATE users SET name='x' WHERE id=1", want: "update"},
		{sql: "DELETE FROM users WHERE id=1", want: "delete"},
		{sql: "WITH cte AS (SELECT 1) SELECT * FROM cte", want: "with"},
		{sql: "PRAGMA table_info(users)", want: "pragma"},
		{sql: "", want: "unknown"},
		{sql: "COMMIT", want: "other"},
	}

	for _, tc := range tests {
		t.Run(tc.sql, func(t *testing.T) {
			if got := classifySQLOperation(tc.sql); got != tc.want {
				t.Fatalf("classifySQLOperation(%q)=%q; want %q", tc.sql, got, tc.want)
			}
		})
	}
}

func TestRegisterDBPoolTelemetry(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer sqlDB.Close()

	before := len(dbPools)
	cleanup := registerDBPoolTelemetry(sqlDB, "sqlite", "sql")
	if cleanup == nil {
		t.Fatal("expected telemetry cleanup function")
	}
	afterRegister := len(dbPools)
	if afterRegister != before+1 {
		t.Fatalf("expected dbPools size %d after register, got %d", before+1, afterRegister)
	}

	cleanup()
	afterCleanup := len(dbPools)
	if afterCleanup != before {
		t.Fatalf("expected dbPools size %d after cleanup, got %d", before, afterCleanup)
	}
}
