package quark

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestSuiteMySQL(t *testing.T) {
	dsn := os.Getenv("QUARK_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("QUARK_TEST_MYSQL_DSN not set")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	client, err := New(db, WithDialect(MySQL()))
	if err != nil {
		t.Fatal(err)
	}

	SharedSuite(t, client)
}
