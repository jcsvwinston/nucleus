// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// --- lib/pq shape ------------------------------------------------------------

// pqCode mirrors lib/pq's `pq.ErrorCode` — a named string type holding the
// five-character SQLSTATE.
type pqCode string

// libpqError reproduces the shape of `*github.com/lib/pq.Error`: a pointer
// type carrying the SQLSTATE in a named-string field and exposing it through
// a `SQLState() string` method on a POINTER receiver.
//
// lib/pq is not a dependency of nucleus — pkg/db registers pgx/v5 — hence the
// stand-in. It is tested anyway because an application may hand pkg/db a
// *sql.DB it opened itself with lib/pq, and the contract IsUniqueViolation
// documents is "any error in the chain exposing SQLState()", not "pgx".
type libpqError struct {
	Code pqCode
	Msg  string
}

func (e *libpqError) Error() string    { return "pq: " + e.Msg }
func (e *libpqError) SQLState() string { return string(e.Code) }

// TestIsUniqueViolation_AcrossDrivers pins the per-driver code mapping. The
// fixtures are the canonical error type each driver returns, so the classifier
// is exercised without a live server for the engines whose error types can be
// constructed.
func TestIsUniqueViolation_AcrossDrivers(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("connection refused"), false},

		// PostgreSQL: 23505 = unique_violation.
		{"pgx 23505", &pgconn.PgError{Code: "23505"}, true},
		{"lib/pq 23505", &libpqError{Code: "23505"}, true},
		{"pgx 23503 foreign key is not unique", &pgconn.PgError{Code: "23503"}, false},
		{"pgx 23502 not null is not unique", &pgconn.PgError{Code: "23502"}, false},
		{"pgx 40P01 deadlock is not unique", &pgconn.PgError{Code: "40P01"}, false},

		// MySQL / MariaDB: 1062 = ER_DUP_ENTRY.

		// Wrapped — errors.As walks the Unwrap chain, so an error wrapped by
		// a caller (or by pkg/db itself) classifies identically.
		{"wrapped pgx", fmt.Errorf("insert user: %w", &pgconn.PgError{Code: "23505"}), true},
		{"wrapped lib/pq", fmt.Errorf("insert user: %w", &libpqError{Code: "23505"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Errorf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsUniqueViolation_IgnoresMessageLanguage is the regression this
// predicate exists for.
//
// The classifier it replaces matched English substrings of the driver's
// message. PostgreSQL, MySQL, Oracle and SQL Server all translate those
// messages when the server runs in another language, so on such a deployment
// every substring check returned false and the branch it guarded became dead
// code. The two rows below are the same violation reported by a server in
// Spanish and one in English; both must classify, and neither answer may
// depend on the wording.
//
// The third row is the converse, and the reason the substring check was not
// merely incomplete but wrong: a message that CONTAINS the English wording
// while carrying a different SQLSTATE must NOT be classified as unique.
func TestIsUniqueViolation_IgnoresMessageLanguage(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			// Verbatim from a PostgreSQL 16 server with lc_messages='es_ES.utf8'.
			name: "spanish message, code 23505",
			err:  &libpqError{Code: "23505", Msg: "llave duplicada viola restricción de unicidad «users_email_key»"},
			want: true,
		},
		{
			name: "english message, code 23505",
			err:  &libpqError{Code: "23505", Msg: `duplicate key value violates unique constraint "users_email_key"`},
			want: true,
		},
		{
			// A check constraint whose NAME contains the English wording. The
			// substring classifier said "unique"; the code says 23514.
			name: "english wording, code 23514",
			err:  &libpqError{Code: "23514", Msg: `new row violates check constraint "duplicate key guard"`},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Errorf("IsUniqueViolation(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// foreignError stands in for an error from a driver that is NOT PostgreSQL.
// It is deliberately a local type rather than a real driver's error: the
// property under test is that pgSQLState answers only for errors carrying a
// SQLState() method, and importing another driver to prove it would put a
// driver back in the framework's test dependencies — which is the thing this
// arc removed.
type foreignError struct{}

func (foreignError) Error() string { return "duplicate entry" }

func TestPGSQLState_DoesNotCaptureOtherDrivers(t *testing.T) {
	if state, ok := pgSQLState(foreignError{}); ok {
		t.Errorf("pgSQLState captured a mysql error (state %q); the PostgreSQL branch "+
			"now shadows mysql and its violations classify as false", state)
	}

	// The positive, so the assertion above cannot pass vacuously.
	for _, c := range []struct {
		name string
		err  error
	}{
		{"pgx", &pgconn.PgError{Code: "23505"}},
		{"lib/pq", &libpqError{Code: "23505"}},
	} {
		t.Run("captures/"+c.name, func(t *testing.T) {
			if state, ok := pgSQLState(c.err); !ok || state != "23505" {
				t.Errorf("pgSQLState(%s) = (%q, %v), want (\"23505\", true)", c.name, state, ok)
			}
		})
	}
}
