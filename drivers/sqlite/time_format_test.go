// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"database/sql"
	"testing"
	"time"
)

// DEP-2026-026 (NU-110) pins the v1 behaviour: a time.Time bound to a
// statement is stored as the text Go's time.Time.String() produces, which
// SQLite's own date functions do not parse — date() over the column is
// NULL — so a query that filters or groups by date cannot be written in SQL
// against what the framework stored. At v2.0.0 the framework stores the
// format SQLite understands and keeps reading both; the 2.0 PR changes the
// first subtest in the same commit as the default. The second subtest is
// the way out that exists today: modernc's `_time_format=sqlite` DSN
// parameter, which an application adds to its sqlite:// URL.
func TestTimeFormat_GoTextUntilV2(t *testing.T) {
	probe := func(t *testing.T, dsn string) (stored string, date sql.NullString) {
		t.Helper()
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec("CREATE TABLE t (ts DATETIME)"); err != nil {
			t.Fatal(err)
		}
		when := time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)
		if _, err := db.Exec("INSERT INTO t (ts) VALUES (?)", when); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT CAST(ts AS TEXT), date(ts) FROM t").Scan(&stored, &date); err != nil {
			t.Fatal(err)
		}
		var back time.Time
		if err := db.QueryRow("SELECT ts FROM t").Scan(&back); err != nil {
			t.Fatalf("scanning the stored value back into time.Time: %v", err)
		}
		if !back.Equal(when) {
			t.Fatalf("round trip: got %v, want %v", back, when)
		}
		return stored, date
	}

	t.Run("default DSN stores Go's text and date() is NULL", func(t *testing.T) {
		stored, date := probe(t, ":memory:")
		if date.Valid {
			t.Fatalf("date(ts) = %q over %q: in v1 SQLite's date functions do not read what the driver stores", date.String, stored)
		}
		if stored != "2026-10-16 12:30:00 +0000 UTC" {
			t.Fatalf("stored = %q, want Go's time.Time.String() text", stored)
		}
	})

	t.Run("_time_format=sqlite stores what date() reads", func(t *testing.T) {
		stored, date := probe(t, "file::memory:?_time_format=sqlite")
		if !date.Valid || date.String != "2026-10-16" {
			t.Fatalf("date(ts) = %v over %q, want 2026-10-16", date, stored)
		}
	})
}
