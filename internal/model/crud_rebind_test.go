package model

import "testing"

// TestCRUDRebind_PerDialect locks the F-3 (ADR-013) placeholder rewrite: the
// CRUD layer emits `?` markers, and rebind must convert them to the syntax each
// engine requires. PostgreSQL/SQL Server/Oracle break on raw `?`; MySQL/SQLite
// use it natively and must pass through byte-for-byte.
func TestCRUDRebind_PerDialect(t *testing.T) {
	const q = "SELECT a, b FROM t WHERE x = ? AND y = ? ORDER BY z = ?"

	cases := []struct {
		dialect string
		want    string
	}{
		{"postgres", "SELECT a, b FROM t WHERE x = $1 AND y = $2 ORDER BY z = $3"},
		{"sqlserver", "SELECT a, b FROM t WHERE x = @p1 AND y = @p2 ORDER BY z = @p3"},
		{"mssql", "SELECT a, b FROM t WHERE x = @p1 AND y = @p2 ORDER BY z = @p3"},
		{"oracle", "SELECT a, b FROM t WHERE x = :1 AND y = :2 ORDER BY z = :3"},
		{"mysql", q},
		{"sqlite", q},
		{"sqlite3", q},
		{"", q}, // unset dialect falls back to native `?`
	}

	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			c := &CRUD{dialect: tc.dialect}
			if got := c.rebind(q); got != tc.want {
				t.Fatalf("rebind(%q) for dialect %q:\n got  %q\n want %q", q, tc.dialect, got, tc.want)
			}
		})
	}
}

// TestCRUDRebind_NoPlaceholders confirms a query with no `?` is returned
// unchanged for every dialect (the common DDL / literal-only case).
func TestCRUDRebind_NoPlaceholders(t *testing.T) {
	const q = "SELECT COUNT(*) FROM t"
	for _, d := range []string{"postgres", "mssql", "oracle", "mysql", "sqlite"} {
		c := &CRUD{dialect: d}
		if got := c.rebind(q); got != q {
			t.Fatalf("rebind(%q) for dialect %q changed a placeholder-free query: %q", q, d, got)
		}
	}
}

// TestCRUDRebind_OrdinalsAreSequential guards against off-by-one / shared-counter
// regressions: the Nth `?` must become the Nth ordinal regardless of spacing.
func TestCRUDRebind_OrdinalsAreSequential(t *testing.T) {
	c := &CRUD{dialect: "postgres"}
	got := c.rebind("INSERT INTO t (a,b,c,d) VALUES (?,?,?,?)")
	want := "INSERT INTO t (a,b,c,d) VALUES ($1,$2,$3,$4)"
	if got != want {
		t.Fatalf("rebind ordinals:\n got  %q\n want %q", got, want)
	}
}

// TestSetDialect_NormalisesNamingConventions locks the F-3 fix that collapses
// the codebase's two dialect-naming conventions to one canonical token, so a
// caller passing db.DB.System()'s "postgresql"/"mssql" (e.g. data-studio) gets
// the same rebind as one passing app.detectDatabaseDialect's "postgres".
func TestSetDialect_NormalisesNamingConventions(t *testing.T) {
	cases := []struct{ in, wantDialect, wantRebound string }{
		{"postgresql", "postgres", "WHERE a = $1"},
		{"POSTGRESQL", "postgres", "WHERE a = $1"},
		{"postgres", "postgres", "WHERE a = $1"},
		{"sqlserver", "mssql", "WHERE a = @p1"},
		{"mssql", "mssql", "WHERE a = @p1"},
		{"mysql", "mysql", "WHERE a = ?"},
		{"sqlite", "sqlite", "WHERE a = ?"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			c := &CRUD{}
			c.SetDialect(tc.in)
			if c.dialect != tc.wantDialect {
				t.Fatalf("SetDialect(%q) stored %q, want %q", tc.in, c.dialect, tc.wantDialect)
			}
			if got := c.rebind("WHERE a = ?"); got != tc.wantRebound {
				t.Fatalf("after SetDialect(%q), rebind = %q, want %q", tc.in, got, tc.wantRebound)
			}
		})
	}
}
