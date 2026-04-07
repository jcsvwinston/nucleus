package cli

import (
	"strings"
	"testing"
)

func TestSelectTablesForDump(t *testing.T) {
	all := []string{"posts", "users"}

	selected, err := selectTablesForDump(all, nil, nil)
	if err != nil {
		t.Fatalf("select all tables failed: %v", err)
	}
	if got := strings.Join(selected, ","); got != "posts,users" {
		t.Fatalf("unexpected selected tables: %s", got)
	}

	selected, err = selectTablesForDump(all, []string{"users"}, nil)
	if err != nil {
		t.Fatalf("select included tables failed: %v", err)
	}
	if got := strings.Join(selected, ","); got != "users" {
		t.Fatalf("unexpected included tables: %s", got)
	}

	selected, err = selectTablesForDump(all, nil, []string{"posts"})
	if err != nil {
		t.Fatalf("exclude tables failed: %v", err)
	}
	if got := strings.Join(selected, ","); got != "users" {
		t.Fatalf("unexpected excluded tables: %s", got)
	}
}

func TestSelectTablesForDumpUnknown(t *testing.T) {
	_, err := selectTablesForDump([]string{"users"}, []string{"missing"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown table")
	}
}

func TestDecodeFixtureDocument(t *testing.T) {
	raw := []byte(`{"generated_at":"2026-04-01T12:00:00Z","engine":"sqlite","tables":[{"name":"users","rows":[{"id":1,"name":"alice"}]}]}`)
	doc, err := decodeFixtureDocument(raw)
	if err != nil {
		t.Fatalf("decode fixture document failed: %v", err)
	}
	if len(doc.Tables) != 1 || doc.Tables[0].Name != "users" {
		t.Fatalf("unexpected decoded tables: %+v", doc.Tables)
	}
}

func TestDecodeFixtureDocumentArray(t *testing.T) {
	raw := []byte(`[{"name":"users","rows":[{"id":1}]}]`)
	doc, err := decodeFixtureDocument(raw)
	if err != nil {
		t.Fatalf("decode fixture array failed: %v", err)
	}
	if len(doc.Tables) != 1 || doc.Tables[0].Name != "users" {
		t.Fatalf("unexpected decoded tables: %+v", doc.Tables)
	}
}

func TestBuildInsertStatement(t *testing.T) {
	sqliteStmt := buildInsertStatement(dbFlavorSQLite, "users", []string{"id", "name"})
	if sqliteStmt != `INSERT INTO "users" ("id", "name") VALUES (?, ?)` {
		t.Fatalf("unexpected sqlite insert statement: %s", sqliteStmt)
	}

	pgStmt := buildInsertStatement(dbFlavorPostgres, "users", []string{"id", "name"})
	if pgStmt != `INSERT INTO "users" ("id", "name") VALUES ($1, $2)` {
		t.Fatalf("unexpected postgres insert statement: %s", pgStmt)
	}

	mssqlStmt := buildInsertStatement(dbFlavorMSSQL, "users", []string{"id", "name"})
	if mssqlStmt != `INSERT INTO [users] ([id], [name]) VALUES (@p1, @p2)` {
		t.Fatalf("unexpected mssql insert statement: %s", mssqlStmt)
	}

	oracleStmt := buildInsertStatement(dbFlavorOracle, "users", []string{"id", "name"})
	if oracleStmt != `INSERT INTO "USERS" ("ID", "NAME") VALUES (:1, :2)` {
		t.Fatalf("unexpected oracle insert statement: %s", oracleStmt)
	}
}
