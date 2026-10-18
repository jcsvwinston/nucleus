// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// DEP-2026-026 (NU-110): every SQLite DSN the framework opens carries
// `_time_format=sqlite` unless the URL names a format itself.
func TestResolveDriverSetsTheSQLiteTimeFormat(t *testing.T) {
	cases := map[string]string{
		"sqlite://app.db":   "app.db?_pragma=busy_timeout(5000)&_time_format=sqlite",
		"sqlite://":         ":memory:?_time_format=sqlite",
		"sqlite://:memory:": ":memory:?_time_format=sqlite",
		"sqlite://app.db?_pragma=journal_mode(WAL)": "app.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_time_format=sqlite",
		"app.db":      "app.db?_time_format=sqlite",
		":memory:":    ":memory:?_time_format=sqlite",
		"data.sqlite": "data.sqlite?_time_format=sqlite",
		// A format the URL names is kept, whichever it is.
		"sqlite://app.db?_time_format=datetime": "app.db?_time_format=datetime&_pragma=busy_timeout(5000)",
		"sqlite://app.db?_time_format=sqlite":   "app.db?_time_format=sqlite&_pragma=busy_timeout(5000)",
	}
	for rawURL, want := range cases {
		driverName, dsn, err := resolveDriver(rawURL)
		if err != nil {
			t.Fatalf("resolveDriver(%q): %v", rawURL, err)
		}
		if driverName != "sqlite" {
			t.Fatalf("resolveDriver(%q) driver = %q, want sqlite", rawURL, driverName)
		}
		if dsn != want {
			t.Errorf("resolveDriver(%q) dsn = %q, want %q", rawURL, dsn, want)
		}
	}
}

// A time.Time stored through the framework on SQLite is text SQLite's own
// date functions read; a row written before v2.0.0, in the text Go's
// time.Time.String() produces, still reads back as the same instant, and
// the documented data migration turns it into the SQLite form for date
// queries. Verified against the driver, not against the DSN string.
func TestSQLiteStoresTimeInTheFormatDateFunctionsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "times.db")
	oldRow := time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)
	newRow := oldRow.Add(time.Hour)

	// A v1 database: opened outside the framework with the driver's own
	// default, which is how the framework opened it up to v1.
	v1, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1.Exec("CREATE TABLE t (ts DATETIME)"); err != nil {
		t.Fatal(err)
	}
	if _, err := v1.Exec("INSERT INTO t (ts) VALUES (?)", oldRow); err != nil {
		t.Fatal(err)
	}
	_ = v1.Close()

	database, err := New(Config{DatabaseURL: "sqlite://" + path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec("INSERT INTO t (ts) VALUES (?)", newRow); err != nil {
		t.Fatal(err)
	}

	type row struct {
		stored string
		date   sql.NullString
		back   time.Time
	}
	read := func() []row {
		t.Helper()
		rows, err := sqlDB.Query("SELECT CAST(ts AS TEXT), date(ts), ts FROM t ORDER BY rowid")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.stored, &r.date, &r.back); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, r)
		}
		return out
	}

	got := read()
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
	// Both forms are read: the v1 row is the same instant it was …
	if got[0].stored != "2026-10-16 12:30:00 +0000 UTC" || !got[0].back.Equal(oldRow) {
		t.Errorf("v1 row: stored %q, read back %v; want Go's text read back as %v", got[0].stored, got[0].back, oldRow)
	}
	// … and still invisible to date() until migrated.
	if got[0].date.Valid {
		t.Errorf("date() over the v1 row = %q; SQLite does not read Go's text, which is what the migration is for", got[0].date.String)
	}
	// The row the framework wrote is in the SQLite form and date() reads it.
	if got[1].stored != "2026-10-16 13:30:00+00:00" || !got[1].back.Equal(newRow) {
		t.Errorf("new row: stored %q, read back %v; want the SQLite form read back as %v", got[1].stored, got[1].back, newRow)
	}
	if !got[1].date.Valid || got[1].date.String != "2026-10-16" {
		t.Errorf("date() over the new row = %v, want 2026-10-16", got[1].date)
	}

	// The optional data migration the notice documents, for rows written in
	// UTC: afterwards date() reads every row and each still round-trips.
	if _, err := sqlDB.Exec(`UPDATE t SET ts = strftime('%Y-%m-%d %H:%M:%f', substr(ts, 1, 19)) || '+00:00' WHERE ts LIKE '% +0000 UTC'`); err != nil {
		t.Fatalf("documented migration: %v", err)
	}
	for i, r := range read() {
		want := []time.Time{oldRow, newRow}[i]
		if !r.date.Valid || r.date.String != "2026-10-16" {
			t.Errorf("after the migration, date() over row %d = %v, want 2026-10-16 (stored %q)", i, r.date, r.stored)
		}
		if !r.back.Equal(want) {
			t.Errorf("after the migration, row %d reads back %v, want %v (stored %q)", i, r.back, want, r.stored)
		}
	}
}
