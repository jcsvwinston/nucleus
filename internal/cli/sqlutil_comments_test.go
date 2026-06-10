package cli

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// TestExecuteSQLScript_LeadingCommentStatements is the regression guard for
// fleetdesk finding #7: a statement preceded by a `--` line comment (the
// idiomatic way to document seed files) was silently SKIPPED by
// executeSQLStatements — `nucleus seed` reported success while applying only
// the uncommented statements. Comment-only chunks must still be skipped.
func TestExecuteSQLScript_LeadingCommentStatements(t *testing.T) {
	logger := observe.NewLogger("error", "text")
	database, err := db.New(db.Config{
		Engine:      db.EngineSQL,
		DatabaseURL: "sqlite://:memory:",
	}, logger)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlDB, err := database.SqlDB()
	if err != nil {
		t.Fatalf("SqlDB: %v", err)
	}

	script := `
-- Seed: reference table (this comment used to swallow the CREATE)
CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT NOT NULL);

-- Seed rows (and this one swallowed the INSERT)
INSERT INTO widgets (name) VALUES ('alpha'), ('beta');

/* block comment heading a statement */
INSERT INTO widgets (name) VALUES ('gamma');

-- a trailing comment-only chunk must still be skipped, not error
`
	if err := executeSQLScript(sqlDB, script); err != nil {
		t.Fatalf("executeSQLScript: %v", err)
	}

	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 seeded rows, got %d — commented statements were dropped", n)
	}
}

func TestSQLCommentOnly(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"   \n\t", true},
		{"-- just a comment", true},
		{"-- one\n-- two", true},
		{"/* block */", true},
		{"/* a */ -- b", true},
		{"/* unterminated", true},
		{"-- heading\nINSERT INTO t VALUES (1)", false},
		{"/* doc */ SELECT 1", false},
		{"SELECT 1", false},
	}
	for _, tc := range cases {
		if got := sqlCommentOnly(tc.in); got != tc.want {
			t.Errorf("sqlCommentOnly(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
