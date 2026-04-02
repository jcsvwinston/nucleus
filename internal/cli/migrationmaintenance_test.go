package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptimizeMigrationSQL(t *testing.T) {
	raw := `
-- comment
CREATE TABLE users (id INTEGER PRIMARY KEY);

CREATE TABLE users (id INTEGER PRIMARY KEY);
INSERT INTO users (id) VALUES (1);
`
	optimized, stats := optimizeMigrationSQL(raw)
	if stats.OriginalStatements != 3 {
		t.Fatalf("unexpected original statement count: %d", stats.OriginalStatements)
	}
	if stats.OptimizedStatements != 2 {
		t.Fatalf("unexpected optimized statement count: %d", stats.OptimizedStatements)
	}
	if stats.RemovedStatements != 1 {
		t.Fatalf("unexpected removed statement count: %d", stats.RemovedStatements)
	}
	if strings.Count(optimized, "CREATE TABLE users") != 1 {
		t.Fatalf("expected deduplicated CREATE TABLE statement, got: %s", optimized)
	}
}

func TestFindMigrationRangeIndices(t *testing.T) {
	pairs := []migrationPair{
		{ID: "20260401120000_init"},
		{ID: "20260401121000_add_users"},
		{ID: "20260401122000_add_posts"},
	}

	start, end, err := findMigrationRangeIndices(pairs, "20260401120000_init", "20260401122000_add_posts")
	if err != nil {
		t.Fatalf("findMigrationRangeIndices failed: %v", err)
	}
	if start != 0 || end != 2 {
		t.Fatalf("unexpected range indices: start=%d end=%d", start, end)
	}
}

func TestFindMigrationRangeIndicesErrors(t *testing.T) {
	pairs := []migrationPair{
		{ID: "20260401120000_init"},
		{ID: "20260401121000_add_users"},
	}
	if _, _, err := findMigrationRangeIndices(pairs, "missing", "20260401121000_add_users"); err == nil {
		t.Fatal("expected missing migration error")
	}
	if _, _, err := findMigrationRangeIndices(pairs, "20260401121000_add_users", "20260401120000_init"); err == nil {
		t.Fatal("expected reversed range error")
	}
}

func TestBuildSquashedSQL(t *testing.T) {
	dir := t.TempDir()
	firstUp := filepath.Join(dir, "20260401120000_init.up.sql")
	firstDown := filepath.Join(dir, "20260401120000_init.down.sql")
	secondUp := filepath.Join(dir, "20260401121000_add_users.up.sql")
	secondDown := filepath.Join(dir, "20260401121000_add_users.down.sql")

	writeMigrationFile(t, firstUp, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
	writeMigrationFile(t, firstDown, "DROP TABLE IF EXISTS users;")
	writeMigrationFile(t, secondUp, "ALTER TABLE users ADD COLUMN name TEXT;")
	writeMigrationFile(t, secondDown, "ALTER TABLE users DROP COLUMN name;")

	upSQL, downSQL, err := buildSquashedSQL([]migrationPair{
		{ID: "20260401120000_init", UpPath: firstUp, DownPath: firstDown},
		{ID: "20260401121000_add_users", UpPath: secondUp, DownPath: secondDown},
	})
	if err != nil {
		t.Fatalf("buildSquashedSQL failed: %v", err)
	}

	if !strings.Contains(upSQL, "CREATE TABLE users") || !strings.Contains(upSQL, "ALTER TABLE users ADD COLUMN name TEXT") {
		t.Fatalf("unexpected squashed up SQL: %s", upSQL)
	}
	if strings.Index(downSQL, "ALTER TABLE users DROP COLUMN name") > strings.Index(downSQL, "DROP TABLE IF EXISTS users") {
		t.Fatalf("expected down SQL in reverse order, got: %s", downSQL)
	}
}

func writeMigrationFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write migration file failed: %v", err)
	}
}
