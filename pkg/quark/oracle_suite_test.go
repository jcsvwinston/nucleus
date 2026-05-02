package quark_test

import (
	"github.com/jcsvwinston/GoFrame/pkg/quark"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "github.com/sijms/go-ora/v2"
)

func TestSuiteOracle(t *testing.T) {
	dsn := os.Getenv("QUARK_TEST_ORACLE_DSN")
	if dsn == "" {
		t.Skip("QUARK_TEST_ORACLE_DSN not set")
	}

	db, err := sql.Open("oracle", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	client, err := quark.New(db, quark.WithDialect(quark.Oracle()), quark.WithQueryObserver(NewSQLQueryLogger(logger)))
	if err != nil {
		t.Fatal(err)
	}

	SharedSuite(t, client)
}
