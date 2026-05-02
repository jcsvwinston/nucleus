package quark_test

import (
	"github.com/jcsvwinston/GoFrame/pkg/quark"
	"database/sql"
	"testing"
	"log/slog"
	"os"

	_ "modernc.org/sqlite"
)

func TestSuiteSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", "file:suitesqlite?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client, err := quark.New(db, quark.WithDialect(quark.SQLite()), quark.WithQueryObserver(NewSQLQueryLogger(logger)))
	if err != nil {
		t.Fatal(err)
	}

	SharedSuite(t, client)
}
