// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package dbclassify_test

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	mssqldb "github.com/microsoft/go-mssqldb"
	goora "github.com/sijms/go-ora/v2/network"
	moderncsqlite "modernc.org/sqlite"

	"github.com/jcsvwinston/nucleus/internal/dbclassify"
	"github.com/jcsvwinston/nucleus/pkg/db/driver"
	"github.com/jcsvwinston/nucleus/pkg/db/driver/drivertest"
)

// These tests hold internal/dbclassify — the framework's engine-free copy of
// the classifiers — to the engines' real error types, so they import four
// engines. They live in this module and not next to the package because a
// module's go.mod lists what its own tests import, and the framework's go.mod
// is the one every application inherits (NU-106). This module is never
// published; it reaches the framework's internal packages because its path
// sits under the framework's.
//
// The conformance kit checks the classifier is registered under its engine,
// so the predicates are registered here the way the driver modules published
// up to v0.1.7 register them. PostgreSQL is absent on purpose: pkg/db reads
// its SQLSTATE through the method every PostgreSQL driver exposes.
func init() {
	driver.MustRegisterUniqueViolation("mysql", dbclassify.MySQLUniqueViolation)
	driver.MustRegisterUniqueViolation("sqlite", dbclassify.SQLiteUniqueViolation)
	driver.MustRegisterUniqueViolation("sqlserver", dbclassify.MSSQLUniqueViolation)
	driver.MustRegisterUniqueViolation("oracle", dbclassify.OracleUniqueViolation)
}

// The predicates as they were before NU-8 — typed, importing each engine —
// kept verbatim as the reference. The package now answers without the
// imports, and the driver modules published up to v0.1.7 still register it,
// so "it answers what it answered" is the property to hold, error by error.
var typed = map[string]func(error) bool{
	"mysql": func(err error) bool {
		var e *gomysql.MySQLError
		return errors.As(err, &e) && e.Number == 1062
	},
	"sqlserver": func(err error) bool {
		var e mssqldb.Error
		return errors.As(err, &e) && (e.Number == 2627 || e.Number == 2601)
	},
	"oracle": func(err error) bool {
		var e *goora.OracleError
		return errors.As(err, &e) && e.ErrCode == 1
	},
	"sqlite": func(err error) bool {
		var e *moderncsqlite.Error
		if errors.As(err, &e) {
			code := e.Code()
			return code == 2067 || code == 1555
		}
		return false
	},
}

var untyped = map[string]func(error) bool{
	"mysql":     dbclassify.MySQLUniqueViolation,
	"sqlserver": dbclassify.MSSQLUniqueViolation,
	"oracle":    dbclassify.OracleUniqueViolation,
	"sqlite":    dbclassify.SQLiteUniqueViolation,
}

// sqliteErrors provokes real SQLite errors: modernc.org/sqlite's Error has
// unexported fields and cannot be fabricated honestly.
func sqliteErrors(t *testing.T) (unique, primaryKey, notNull error) {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1)
	for _, stmt := range []string{
		"CREATE TABLE u (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE)",
		"INSERT INTO u (id, email) VALUES (1, 'a@b.c')",
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	fail := func(stmt string) error {
		_, err := conn.Exec(stmt)
		if err == nil {
			t.Fatalf("%s must fail", stmt)
		}
		return err
	}
	return fail("INSERT INTO u (id, email) VALUES (2, 'a@b.c')"),
		fail("INSERT INTO u (id, email) VALUES (1, 'x@y.z')"),
		fail("INSERT INTO u (id, email) VALUES (3, NULL)")
}

type sqlStater struct{ code string }

func (e *sqlStater) Error() string    { return "SQLSTATE " + e.code }
func (e *sqlStater) SQLState() string { return e.code }

// corpus is every error the predicates are asked about: each engine's
// violation and its neighbours, bare, wrapped, wrapped twice and joined, and
// the errors of the engines that are not theirs.
func corpus(t *testing.T) []error {
	sqliteUnique, sqlitePK, sqliteNotNull := sqliteErrors(t)
	base := []error{
		&gomysql.MySQLError{Number: 1062},
		&gomysql.MySQLError{Number: 1452}, // foreign key
		&gomysql.MySQLError{Number: 1048}, // not null
		&gomysql.MySQLError{Number: 1213}, // deadlock
		mssqldb.Error{Number: 2627},
		mssqldb.Error{Number: 2601},
		mssqldb.Error{Number: 2602},
		mssqldb.Error{Number: 547}, // foreign key / check
		mssqldb.Error{Number: 515}, // not null
		&mssqldb.Error{Number: 2627},
		&goora.OracleError{ErrCode: 1},
		&goora.OracleError{ErrCode: 2291},
		&goora.OracleError{ErrCode: 1400},
		sqliteUnique,
		sqlitePK,
		sqliteNotNull,
		&sqlStater{code: "23505"},
		errors.New("duplicate key value violates unique constraint"),
		sql.ErrNoRows,
	}
	out := []error{nil}
	for _, err := range base {
		out = append(out,
			err,
			fmt.Errorf("insert user: %w", err),
			fmt.Errorf("handler: %w", fmt.Errorf("insert user: %w", err)),
			errors.Join(errors.New("rollback failed"), err),
			fmt.Errorf("two: %w, %w", sql.ErrConnDone, err),
		)
	}
	return out
}

func TestAnswersWhatTheTypedPredicatesAnswered(t *testing.T) {
	errs := corpus(t)
	for engine, want := range typed {
		got := untyped[engine]
		matched := 0
		for _, err := range errs {
			w := want(err)
			if w {
				matched++
			}
			if g := got(err); g != w {
				t.Errorf("%s: %v answers %v, the typed predicate answered %v", engine, err, g, w)
			}
		}
		if matched == 0 {
			t.Errorf("%s: the corpus holds no violation of this engine, so the comparison proves nothing", engine)
		}
	}
}

// A nil pointer of the driver's type, inside a non-nil error, is no
// violation. The typed predicates dereferenced it.
func TestATypedNilIsNotAViolation(t *testing.T) {
	for name, err := range map[string]error{
		"mysql":  (*gomysql.MySQLError)(nil),
		"oracle": (*goora.OracleError)(nil),
		"sqlite": (*moderncsqlite.Error)(nil),
	} {
		for engine, classify := range untyped {
			if classify(err) {
				t.Errorf("%s: a nil *%s error classified as a violation", engine, name)
			}
		}
	}
}

// The conformance kit every driver module runs, against the root's copy.
func TestConformance(t *testing.T) {
	sqliteUnique, sqlitePK, sqliteNotNull := sqliteErrors(t)
	for _, c := range []drivertest.Case{
		{
			Engine: "mysql", Classify: dbclassify.MySQLUniqueViolation,
			Violation:    &gomysql.MySQLError{Number: 1062},
			NotViolation: []error{&gomysql.MySQLError{Number: 1452}, &gomysql.MySQLError{Number: 1048}, &gomysql.MySQLError{Number: 1213}},
		},
		{
			Engine: "sqlserver", Classify: dbclassify.MSSQLUniqueViolation,
			Violation:    mssqldb.Error{Number: 2601},
			NotViolation: []error{mssqldb.Error{Number: 2602}, mssqldb.Error{Number: 547}, mssqldb.Error{Number: 515}},
		},
		{
			Engine: "oracle", Classify: dbclassify.OracleUniqueViolation,
			Violation:    &goora.OracleError{ErrCode: 1},
			NotViolation: []error{&goora.OracleError{ErrCode: 2291}, &goora.OracleError{ErrCode: 1400}},
		},
		{
			Engine: "sqlite", Classify: dbclassify.SQLiteUniqueViolation,
			Violation:    sqliteUnique,
			NotViolation: []error{sqliteNotNull},
		},
		{
			Engine: "sqlite", Classify: dbclassify.SQLiteUniqueViolation,
			Violation: sqlitePK,
		},
	} {
		t.Run(c.Engine, func(t *testing.T) { drivertest.VerifyClassifier(t, c) })
	}
}
