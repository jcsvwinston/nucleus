// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package db: driver error classification.
//
// Code that writes to the database needs to tell one failure apart from
// another, and the only portable signal database/sql offers is the error
// value itself. The tempting shortcut — matching substrings of the driver's
// message ("duplicate key", "unique constraint", …) — is wrong in a way that
// is invisible in development and fatal in production: PostgreSQL, MySQL,
// Oracle and SQL Server all TRANSLATE their messages when the server runs in
// another language. A PostgreSQL server started with lc_messages='es_ES.utf8'
// answers a rejected insert with
//
//	llave duplicada viola restricción de unicidad «users_email_key»
//
// in which no English substring appears. Every such check silently returns
// false, and the branch it guards becomes dead code on that deployment.
//
// The predicates here match on the CODE the driver reports, through
// errors.As, so they are unaffected by the server's locale and by wording
// changes between driver releases. They walk the Unwrap chain, so an error
// the caller has wrapped still classifies.
package db

import (
	"errors"

	gomysql "github.com/go-sql-driver/mysql"
	moderncsqlite "modernc.org/sqlite"
)

// pgSQLState extracts the five-character SQLSTATE from a PostgreSQL driver
// error, reporting whether err came from PostgreSQL at all.
//
// It asserts on the `SQLState() string` METHOD rather than on a concrete
// driver type, because the code must classify identically under any
// PostgreSQL driver. Nucleus registers pgx/v5 (db.go), but an application is
// free to hand pkg/db a handle opened with lib/pq, and both expose the code
// through this method — as does any future driver following the convention.
// The assertion also costs no import: matching *pgconn.PgError would drag
// the pgconn package in, and would silently skip every lib/pq error.
func pgSQLState(err error) (string, bool) {
	type sqlStater interface{ SQLState() string }
	var sse sqlStater
	if errors.As(err, &sse) {
		return sse.SQLState(), true
	}
	return "", false
}

// IsUniqueViolation reports whether err was caused by a unique or primary-key
// constraint. It is the signal to treat a rejected insert as "that value is
// already taken" rather than as an internal error:
//
//	if err := insertUser(ctx, sqlDB, u); err != nil {
//	    if db.IsUniqueViolation(err) {
//	        http.Error(w, "email already registered", http.StatusConflict)
//	        return
//	    }
//	    return err
//	}
//
// It does NOT report foreign-key, not-null or check violations: a caller
// acting on "unique" wants to point at one field, and widening the predicate
// later would silently change what that branch catches.
//
// Coverage tracks the drivers pkg/db actually registers, including the build
// tags: PostgreSQL, MySQL/MariaDB and SQLite always; SQL Server and Oracle
// when built with `-tags mssql` / `-tags oracle`, the same tags that register
// those drivers in the first place. An engine whose driver is not linked into
// the binary cannot produce an error to classify.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	// PostgreSQL. SQLSTATE 23505 = unique_violation.
	if state, ok := pgSQLState(err); ok {
		return state == "23505"
	}

	// MySQL / MariaDB. 1062 = ER_DUP_ENTRY.
	var mysqlErr *gomysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}

	// SQL Server (2627 constraint / 2601 unique index) and Oracle (ORA-00001).
	// Both drivers sit behind the build tag that registers them, so these
	// helpers are compiled as constant-false stubs in a default build — see
	// errors_classify_mssql.go / errors_classify_oracle.go and their `no`
	// counterparts.
	if isMSSQLUniqueViolation(err) {
		return true
	}
	if isOracleUniqueViolation(err) {
		return true
	}

	// SQLite (modernc.org/sqlite, the pure-Go driver pkg/db registers).
	var moderncErr *moderncsqlite.Error
	if errors.As(err, &moderncErr) {
		code := moderncErr.Code()
		return code == 2067 /* SQLITE_CONSTRAINT_UNIQUE */ ||
			code == 1555 /* SQLITE_CONSTRAINT_PRIMARYKEY */
	}

	return false
}
