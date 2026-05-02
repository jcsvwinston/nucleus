package quark

import (
	"database/sql"
	"testing"
	"log/slog"
	"os"

	_ "modernc.org/sqlite"
)

func TestSuiteSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client, err := New(db, WithDialect(SQLite()), WithQueryObserver(NewSQLQueryLogger(logger)))
	if err != nil {
		t.Fatal(err)
	}

	SharedSuite(t, client)
}
