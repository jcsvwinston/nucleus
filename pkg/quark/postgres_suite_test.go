package quark_test

import (
	"github.com/jcsvwinston/GoFrame/pkg/quark"
	"database/sql"
	"os"
	"testing"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSuitePostgres(t *testing.T) {
	dsn := os.Getenv("QUARK_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("QUARK_TEST_POSTGRES_DSN not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client, err := quark.New(db, quark.WithDialect(quark.PostgreSQL()), quark.WithQueryObserver(NewSQLQueryLogger(logger)))
	if err != nil {
		t.Fatal(err)
	}

	SharedSuite(t, client)
}
