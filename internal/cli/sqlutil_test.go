package cli

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestExecuteSQLStatements(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	statements := []string{
		`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
		`INSERT INTO items (id, name) VALUES (1, 'one')`,
		`-- comment only statement should be skipped`,
		``,
		`INSERT INTO items (id, name) VALUES (2, 'two')`,
	}

	if err := executeSQLStatements(dbConn, statements); err != nil {
		t.Fatalf("executeSQLStatements failed: %v", err)
	}

	var count int
	if err := dbConn.QueryRow(`SELECT count(*) FROM items`).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("unexpected row count: got %d want 2", count)
	}
}
