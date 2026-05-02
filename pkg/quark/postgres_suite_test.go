package quark

import (
	"database/sql"
	"os"
	"testing"

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

	client, err := New(db, WithDialect(PostgreSQL()))
	if err != nil {
		t.Fatal(err)
	}

	SharedSuite(t, client)
}
