// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"database/sql"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

// DEP-2026-026 (NU-110): a time.Time bound to a statement through this
// driver, opened the way the framework opens it, is stored as text SQLite's
// own date functions read — date() over the column answers — and a value
// stored up to v1, in the text Go's time.Time.String() produces, still reads
// back as the same instant. The framework sets `_time_format=sqlite` on
// every SQLite URL it opens (pkg/db), so the probe resolves the URL through
// pkg/db instead of handing the driver a DSN of its own; a URL that names a
// format keeps it.
func TestTimeFormat_SQLiteByDefault(t *testing.T) {
	when := time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)
	probe := func(t *testing.T, url string) (stored string, date sql.NullString) {
		t.Helper()
		name, dsn, err := db.ResolveDriver(url)
		if err != nil {
			t.Fatal(err)
		}
		database, err := sql.Open(name, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		if _, err := database.Exec("CREATE TABLE t (ts DATETIME)"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("INSERT INTO t (ts) VALUES (?)", when); err != nil {
			t.Fatal(err)
		}
		if err := database.QueryRow("SELECT CAST(ts AS TEXT), date(ts) FROM t").Scan(&stored, &date); err != nil {
			t.Fatal(err)
		}
		var back time.Time
		if err := database.QueryRow("SELECT ts FROM t").Scan(&back); err != nil {
			t.Fatalf("scanning the stored value back into time.Time: %v", err)
		}
		if !back.Equal(when) {
			t.Fatalf("round trip: got %v, want %v", back, when)
		}
		return stored, date
	}

	t.Run("the default URL stores what date() reads", func(t *testing.T) {
		stored, date := probe(t, "sqlite://:memory:")
		if stored != "2026-10-16 12:30:00+00:00" {
			t.Fatalf("stored = %q, want the SQLite form", stored)
		}
		if !date.Valid || date.String != "2026-10-16" {
			t.Fatalf("date(ts) = %v over %q, want 2026-10-16", date, stored)
		}
	})

	t.Run("a format the URL names is kept", func(t *testing.T) {
		stored, date := probe(t, "sqlite://:memory:?_time_format=datetime")
		if stored != "2026-10-16 12:30:00" {
			t.Fatalf("stored = %q, want modernc's datetime form", stored)
		}
		if !date.Valid || date.String != "2026-10-16" {
			t.Fatalf("date(ts) = %v over %q, want 2026-10-16", date, stored)
		}
	})

	t.Run("text written up to v1 reads back", func(t *testing.T) {
		name, dsn, err := db.ResolveDriver("sqlite://:memory:")
		if err != nil {
			t.Fatal(err)
		}
		database, err := sql.Open(name, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		if _, err := database.Exec("CREATE TABLE t (ts DATETIME)"); err != nil {
			t.Fatal(err)
		}
		// The text the driver's own default wrote, as a v1 database holds it.
		if _, err := database.Exec("INSERT INTO t (ts) VALUES (?)", when.String()); err != nil {
			t.Fatal(err)
		}
		var back time.Time
		if err := database.QueryRow("SELECT ts FROM t").Scan(&back); err != nil {
			t.Fatalf("scanning Go's text back into time.Time: %v", err)
		}
		if !back.Equal(when) {
			t.Fatalf("round trip of Go's text: got %v, want %v", back, when)
		}
	})
}
