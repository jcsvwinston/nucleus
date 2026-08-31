// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

func TestRegistersTheSQLDriver(t *testing.T) {
	if !slices.Contains(sql.Drivers(), "sqlite") {
		t.Errorf("importing this module must register the \"sqlite\" driver; registered: %v", sql.Drivers())
	}
}

// The classifier is verified against an error the driver itself produced.
// modernc.org/sqlite's Error type has unexported fields and cannot be
// fabricated, and a hand-rolled stand-in would only prove that the test
// agrees with itself — so the test provokes the real thing, which an
// in-memory database makes free.
func TestClassifierConformance(t *testing.T) {
	ctx := context.Background()
	database, err := db.New(db.Config{DatabaseURL: "sqlite://:memory:"}, nil)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer database.Close()
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}
	for _, stmt := range []string{
		"CREATE TABLE u (email TEXT NOT NULL UNIQUE, note TEXT)",
		"INSERT INTO u (email, note) VALUES ('a@b.c', 'first')",
	} {
		if _, err := sqlDB.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}

	dup := execErr(t, ctx, sqlDB, "INSERT INTO u (email, note) VALUES ('a@b.c', 'second')")
	// A NOT NULL failure is a constraint error on the same table, and must
	// NOT be reported as unique or the caller blames the wrong field. This
	// is the assertion that fails if the branch is widened to "any
	// SQLITE_CONSTRAINT_*".
	notNull := execErr(t, ctx, sqlDB, "INSERT INTO u (email, note) VALUES (NULL, 'third')")

	drivertest.VerifyClassifier(t, drivertest.Case{
		Engine:       "sqlite",
		Classify:     dbclassify.SQLiteUniqueViolation,
		Violation:    dup,
		NotViolation: []error{notNull},
	})

	// End to end, through the framework's predicate rather than the
	// module's own function: this is what a caller actually invokes.
	if !db.IsUniqueViolation(fmt.Errorf("insert user: %w", dup)) {
		t.Errorf("db.IsUniqueViolation did not recognise a wrapped real SQLite violation: %v", dup)
	}
}

func execErr(t *testing.T, ctx context.Context, sqlDB *sql.DB, stmt string) error {
	t.Helper()
	_, err := sqlDB.ExecContext(ctx, stmt)
	if err == nil {
		t.Fatalf("%s must fail", stmt)
	}
	return err
}
