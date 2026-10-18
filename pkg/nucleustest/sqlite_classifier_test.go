// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// NU-117: a test binary with SQLite's driver and no classifier for it got
// false from db.IsUniqueViolation on a duplicate key — the kit, which linked
// the driver itself up to v1, registered no classifier. The kit links no
// driver since v2.0.0 (DEP-2026-019) and still supplies the classifier when
// whoever registered the driver did not. Tested the way a consumer meets it:
// TempSQLite, StartApp, srv.DB(), and the predicate on the error it returns.
package nucleustest_test

import (
	"context"
	"fmt"
	"go/build"
	"slices"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// registersSQLite lists the packages that register SQLite's classifier on
// import. If this package's tests imported one, the test below would pass
// whether or not the kit registers anything — the silent answer it exists to
// catch. The driver this package's tests link is the bare one
// (sqlite_driver_for_test.go): a driver and no classifier.
var registersSQLite = map[string]bool{
	"github.com/jcsvwinston/nucleus/drivers/sqlite":      true,
	"github.com/jcsvwinston/nucleus/internal/alldrivers": true,
	"github.com/jcsvwinston/nucleus/internal/testsqlite": true,
}

const bareSQLiteDriver = "modernc.org/sqlite"

func TestKitSQLiteClassifiesUniqueViolations(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(pkg.Imports, bareSQLiteDriver) {
		t.Fatalf("the kit itself imports %s; it links no driver (DEP-2026-019)", bareSQLiteDriver)
	}
	testImports := append(pkg.TestImports, pkg.XTestImports...)
	for _, imp := range testImports {
		if registersSQLite[imp] {
			t.Fatalf("this package's tests import %s, which can register SQLite's classifier without the kit; the test below would measure nothing", imp)
		}
	}
	if !slices.Contains(testImports, bareSQLiteDriver) {
		t.Fatalf("this package's tests do not import %s; the kit links no driver, so the test below cannot open a database", bareSQLiteDriver)
	}

	cfg := app.DefaultConfig()
	cfg.Databases = nucleustest.TempSQLite(t)
	srv := nucleustest.StartApp(t, nucleus.App{Config: cfg})
	sqlDB := srv.DB()

	ctx := context.Background()
	// One connection, so the foreign-key pragma holds for the statements
	// that need it; SQLite enforces foreign keys per connection.
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, stmt := range []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE, age INTEGER CHECK (age >= 0))`,
		`CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id))`,
		`INSERT INTO users (id, email, age) VALUES (1, 'a@example.com', 30)`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	for _, c := range []struct {
		name   string
		stmt   string
		unique bool
	}{
		{"unique column", `INSERT INTO users (id, email, age) VALUES (2, 'a@example.com', 30)`, true},
		{"primary key", `INSERT INTO users (id, email, age) VALUES (1, 'b@example.com', 30)`, true},
		// The predicate reports unique and primary-key violations only: a
		// caller acting on "unique" points at one field, and these are other
		// fields' failures.
		{"not null", `INSERT INTO users (id, email, age) VALUES (3, NULL, 30)`, false},
		{"check", `INSERT INTO users (id, email, age) VALUES (4, 'c@example.com', -1)`, false},
		{"foreign key", `INSERT INTO posts (id, user_id) VALUES (1, 99)`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := conn.ExecContext(ctx, c.stmt)
			if err == nil {
				t.Fatalf("%s: SQLite accepted it; the case provokes nothing", c.stmt)
			}
			if got := db.IsUniqueViolation(err); got != c.unique {
				t.Errorf("db.IsUniqueViolation(%v) = %v, want %v", err, got, c.unique)
			}
			wrapped := fmt.Errorf("insert: %w", err)
			if got := db.IsUniqueViolation(wrapped); got != c.unique {
				t.Errorf("db.IsUniqueViolation(%v) = %v, want %v", wrapped, got, c.unique)
			}
		})
	}
}
