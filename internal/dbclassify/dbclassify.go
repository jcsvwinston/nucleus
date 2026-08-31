// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package dbclassify holds the per-driver error predicates.
//
// It exists to have exactly one copy of each. Three places need them and they
// cannot all reach the same one otherwise: the driver modules under drivers/,
// the `nucleus` CLI (which links every engine, being a tool people point at
// whatever database they have), and the framework's own test binary. Written
// out three times they would drift — and a classifier that drifts does not
// fail, it answers false, which is a wrong answer nothing reports.
//
// Nothing in the framework's runtime imports this package, and that is the
// point: importing it means importing the driver error types, which is the
// weight ADR-031 removed. The drivers-and-exporters CI lane asserts that
// pkg/app cannot reach any of them.
package dbclassify

import (
	"errors"

	gomysql "github.com/go-sql-driver/mysql"
	mssqldb "github.com/microsoft/go-mssqldb"
	goora "github.com/sijms/go-ora/v2/network"
	moderncsqlite "modernc.org/sqlite"
)

// MySQLUniqueViolation matches error 1062 (ER_DUP_ENTRY) on the CODE, not on
// the message: a server running with lc_messages set to another language
// answers the same rejection in that language, and a substring check would
// silently return false there.
func MySQLUniqueViolation(err error) bool {
	var e *gomysql.MySQLError
	return errors.As(err, &e) && e.Number == 1062
}

// MSSQLUniqueViolation matches 2627 (unique/primary-key CONSTRAINT) and 2601
// (duplicate row in a unique INDEX). Both exist because the engine raises a
// different number depending on how uniqueness was declared, and a caller
// cares about neither distinction.
//
// mssqldb.Error has a value receiver on Error(), so errors.As must target the
// VALUE type — a pointer target never matches.
func MSSQLUniqueViolation(err error) bool {
	var e mssqldb.Error
	return errors.As(err, &e) && (e.Number == 2627 || e.Number == 2601)
}

// OracleUniqueViolation matches ORA-00001 ("unique constraint violated").
//
// go-ora/v2 may return *network.OracleError directly or wrapped inside a
// *network.SessionError; errors.As walks the Unwrap chain, so one check
// covers both shapes — do not "simplify" this into a type switch.
func OracleUniqueViolation(err error) bool {
	var e *goora.OracleError
	return errors.As(err, &e) && e.ErrCode == 1
}

// SQLiteUniqueViolation matches SQLite's extended result codes: 2067 is
// SQLITE_CONSTRAINT_UNIQUE and 1555 SQLITE_CONSTRAINT_PRIMARYKEY. Both mean
// "that value is already taken"; the primary-key code is separate and would
// be missed by a check that only looked for the unique one.
func SQLiteUniqueViolation(err error) bool {
	var e *moderncsqlite.Error
	if errors.As(err, &e) {
		code := e.Code()
		return code == 2067 || code == 1555
	}
	return false
}
