package quark

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSuiteSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	client, err := New(db, WithDialect(SQLite()))
	if err != nil {
		t.Fatal(err)
	}

	SharedSuite(t, client)
}
