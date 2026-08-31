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

	"github.com/jcsvwinston/nucleus/pkg/db/driver"
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
// Coverage follows the driver modules linked into the binary. PostgreSQL is
// the exception and is classified here: any PostgreSQL driver exposes the
// SQLSTATE through a `SQLState() string` method, so the check costs no import
// and works for pgx and lib/pq alike. Every other engine is classified by the
// module that registers its driver, because recognising its error requires
// its error TYPE.
//
// An engine whose driver is not linked in cannot produce an error to
// classify, so a build that omits a driver module loses nothing. The case
// that does lose something is a caller who registers a driver directly —
// importing github.com/go-sql-driver/mysql itself instead of the nucleus
// module — and never registers a classifier: this returns false for errors it
// has no way to recognise. Config.Open says so at startup rather than letting
// it surface as a wrong answer under load.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}

	// PostgreSQL. SQLSTATE 23505 = unique_violation.
	if state, ok := pgSQLState(err); ok {
		return state == "23505"
	}

	// Every other engine: the module that registers the driver also
	// registers how that driver reports the violation. A classifier answers
	// only for its own driver's error type, so consulting them in turn is
	// safe — the first true is the engine the error came from.
	for _, classify := range driver.UniqueViolationFuncs() {
		if classify(err) {
			return true
		}
	}

	return false
}
