package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/openapi"
	_ "modernc.org/sqlite"
)

func TestRun_MigrateCreate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	migDir := filepath.Join(dir, "migrations")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "create", "init_users"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%s)", code, errOut.String())
	}

	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 migration files, got %d", len(entries))
	}

	var up, down bool
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".up.sql") {
			up = true
		}
		if strings.HasSuffix(name, ".down.sql") {
			down = true
		}
	}
	if !up || !down {
		t.Fatalf("expected both .up.sql and .down.sql files; got %v", entries)
	}
}

func TestRun_MigrateLifecycle(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	writeFile(t, filepath.Join(migDir, "000001_create_items.up.sql"), "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
	writeFile(t, filepath.Join(migDir, "000001_create_items.down.sql"), "DROP TABLE IF EXISTS items;")

	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "up"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("up failed: code=%d stderr=%s", code, errOut.String())
	}
	if !tableExists(t, dbPath, "items") {
		t.Fatal("items table should exist after migrate up")
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "status"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("status failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "applied") {
		t.Fatalf("expected status output to contain 'applied', got: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "down"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("down failed: code=%d stderr=%s", code, errOut.String())
	}
	if tableExists(t, dbPath, "items") {
		t.Fatal("items table should not exist after migrate down")
	}
}

func TestRun_MigrateUnknownAction(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"migrate", "wat"}, &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero code for unknown action")
	}
	if !strings.Contains(errOut.String(), "unknown migrate action") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestRun_OptimizeMigration(t *testing.T) {
	dir := t.TempDir()
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	upPath := filepath.Join(migDir, "20260401120000_create_users.up.sql")
	downPath := filepath.Join(migDir, "20260401120000_create_users.down.sql")
	writeFile(t, upPath, `
-- duplicate create
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE users (id INTEGER PRIMARY KEY);
`)
	writeFile(t, downPath, `DROP TABLE IF EXISTS users;`)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"optimizemigration", "--migrations", migDir, "--write", "create_users"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("optimizemigration failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Migration optimized:") {
		t.Fatalf("unexpected optimizemigration output: %s", out.String())
	}

	optimized, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read optimized migration failed: %v", err)
	}
	if strings.Count(string(optimized), "CREATE TABLE users") != 1 {
		t.Fatalf("expected duplicate statements removed, got: %s", string(optimized))
	}
}

func TestRun_MakeMigrationsAliasAndShowMigrationsAlias(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	migDir := filepath.Join(dir, "migrations")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"makemigrations", "--config", cfgPath, "--migrations", migDir, "init_books"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("makemigrations alias failed: code=%d stderr=%s", code, errOut.String())
	}

	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 migration files, got %d", len(entries))
	}

	var upPath, downPath string
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			upPath = filepath.Join(migDir, name)
		case strings.HasSuffix(name, ".down.sql"):
			downPath = filepath.Join(migDir, name)
		}
	}
	if upPath == "" || downPath == "" {
		t.Fatalf("expected generated up/down migration files, got %v", entries)
	}
	writeFile(t, upPath, "CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT NOT NULL);")
	writeFile(t, downPath, "DROP TABLE IF EXISTS books;")

	out.Reset()
	errOut.Reset()
	code = run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "up"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate up failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"showmigrations", "--config", cfgPath, "--migrations", migDir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("showmigrations alias failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "applied") {
		t.Fatalf("expected showmigrations output to contain 'applied', got: %s", out.String())
	}
}

func TestRun_SquashMigrations(t *testing.T) {
	dir := t.TempDir()
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	firstUp := filepath.Join(migDir, "20260401120000_init.up.sql")
	firstDown := filepath.Join(migDir, "20260401120000_init.down.sql")
	secondUp := filepath.Join(migDir, "20260401121000_add_users.up.sql")
	secondDown := filepath.Join(migDir, "20260401121000_add_users.down.sql")

	writeFile(t, firstUp, "CREATE TABLE users (id INTEGER PRIMARY KEY);")
	writeFile(t, firstDown, "DROP TABLE IF EXISTS users;")
	writeFile(t, secondUp, "ALTER TABLE users ADD COLUMN name TEXT;")
	writeFile(t, secondDown, "ALTER TABLE users DROP COLUMN name;")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"squashmigrations",
		"--migrations", migDir,
		"--from", "init",
		"--to", "add_users",
		"--name", "baseline",
		"--write",
		"--archive-old",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("squashmigrations failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Squashed migrations written:") {
		t.Fatalf("unexpected squashmigrations output: %s", out.String())
	}

	matchesUp, err := filepath.Glob(filepath.Join(migDir, "*_baseline.up.sql"))
	if err != nil {
		t.Fatalf("glob squashed up file failed: %v", err)
	}
	matchesDown, err := filepath.Glob(filepath.Join(migDir, "*_baseline.down.sql"))
	if err != nil {
		t.Fatalf("glob squashed down file failed: %v", err)
	}
	if len(matchesUp) != 1 || len(matchesDown) != 1 {
		t.Fatalf("expected one squashed migration pair, got up=%v down=%v", matchesUp, matchesDown)
	}

	if _, err := os.Stat(firstUp); !os.IsNotExist(err) {
		t.Fatalf("expected original first up migration archived, stat err=%v", err)
	}
	if _, err := os.Stat(secondUp); !os.IsNotExist(err) {
		t.Fatalf("expected original second up migration archived, stat err=%v", err)
	}

	archiveEntries, err := os.ReadDir(filepath.Join(migDir, ".squashed"))
	if err != nil {
		t.Fatalf("read archive root failed: %v", err)
	}
	if len(archiveEntries) == 0 {
		t.Fatal("expected at least one archive folder under migrations/.squashed")
	}
}

func TestRun_SQLMigrate(t *testing.T) {
	dir := t.TempDir()
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	upPath := filepath.Join(migDir, "20260401120000_create_books.up.sql")
	downPath := filepath.Join(migDir, "20260401120000_create_books.down.sql")
	writeFile(t, upPath, "CREATE TABLE books (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL);")
	writeFile(t, downPath, "DROP TABLE IF EXISTS books;")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"sqlmigrate", "--migrations", migDir, "create_books"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("sqlmigrate up failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "CREATE TABLE books") {
		t.Fatalf("unexpected sqlmigrate up output: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"sqlmigrate", "--migrations", migDir, "--down", "create_books"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("sqlmigrate down failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "DROP TABLE IF EXISTS books") {
		t.Fatalf("unexpected sqlmigrate down output: %s", out.String())
	}
}

func TestRun_SQLFlushAndFlush(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	_, _ = dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")
	_, _ = dbConn.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL);")
	_, _ = dbConn.Exec("CREATE TABLE nucleus_schema_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('alice'), ('bob');")
	_, _ = dbConn.Exec("INSERT INTO posts (title) VALUES ('hello');")
	_, _ = dbConn.Exec("INSERT INTO nucleus_schema_migrations (id, applied_at) VALUES ('20260401120000_init', '2026-04-01T12:00:00Z');")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"sqlflush", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("sqlflush failed: code=%d stderr=%s", code, errOut.String())
	}
	sqlText := out.String()
	if !strings.Contains(sqlText, "DELETE FROM \"users\";") || !strings.Contains(sqlText, "DELETE FROM \"posts\";") {
		t.Fatalf("unexpected sqlflush output: %s", sqlText)
	}
	if strings.Contains(sqlText, "nucleus_schema_migrations") {
		t.Fatalf("sqlflush should skip schema migrations table: %s", sqlText)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"flush", "--config", cfgPath, "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("flush failed: code=%d stderr=%s", code, errOut.String())
	}

	var usersCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM users").Scan(&usersCount); err != nil {
		t.Fatalf("count users failed: %v", err)
	}
	if usersCount != 0 {
		t.Fatalf("expected users to be flushed, got %d rows", usersCount)
	}

	var postsCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM posts").Scan(&postsCount); err != nil {
		t.Fatalf("count posts failed: %v", err)
	}
	if postsCount != 0 {
		t.Fatalf("expected posts to be flushed, got %d rows", postsCount)
	}

	var migrationsCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM nucleus_schema_migrations").Scan(&migrationsCount); err != nil {
		t.Fatalf("count migrations failed: %v", err)
	}
	if migrationsCount != 1 {
		t.Fatalf("expected schema migrations to be preserved, got %d rows", migrationsCount)
	}
}

func TestRun_FlushProductionGuardrailNonInteractive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	_, _ = dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('alice');")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := runWithInput([]string{"flush", "--config", cfgPath}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected flush in production without force/yes to fail; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected flush guardrail error: %s", errOut.String())
	}

	var usersCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM users").Scan(&usersCount); err != nil {
		t.Fatalf("count users before confirmed flush failed: %v", err)
	}
	if usersCount != 1 {
		t.Fatalf("expected users to remain unchanged after guardrail failure, got %d rows", usersCount)
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"flush", "--config", cfgPath, "--yes"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("flush with --yes failed: code=%d stderr=%s", code, errOut.String())
	}

	if err := dbConn.QueryRow("SELECT count(*) FROM users").Scan(&usersCount); err != nil {
		t.Fatalf("count users after confirmed flush failed: %v", err)
	}
	if usersCount != 0 {
		t.Fatalf("expected users to be flushed after --yes, got %d rows", usersCount)
	}
}

func TestRun_SQLSequenceReset(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()
	_, _ = dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('alice');")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"sqlsequencereset", "--config", cfgPath, "users"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("sqlsequencereset failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "DELETE FROM sqlite_sequence WHERE name = 'users';") {
		t.Fatalf("unexpected sqlsequencereset output: %s", out.String())
	}
}

func TestRun_DumpDataAndLoadData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	fixturePath := filepath.Join(dir, "fixtures.json")

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	_, _ = dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")
	_, _ = dbConn.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL);")
	_, _ = dbConn.Exec("CREATE TABLE nucleus_schema_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('alice'), ('bob');")
	_, _ = dbConn.Exec("INSERT INTO posts (title) VALUES ('hello');")
	_, _ = dbConn.Exec("INSERT INTO nucleus_schema_migrations (id, applied_at) VALUES ('20260401120000_init', '2026-04-01T12:00:00Z');")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"dumpdata", "--config", cfgPath, "--output", fixturePath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("dumpdata failed: code=%d stderr=%s", code, errOut.String())
	}

	rawFixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture file failed: %v", err)
	}
	fixtureText := string(rawFixture)
	if !strings.Contains(fixtureText, "\"name\": \"users\"") || !strings.Contains(fixtureText, "\"name\": \"posts\"") {
		t.Fatalf("fixture output missing expected tables: %s", fixtureText)
	}
	if strings.Contains(fixtureText, "nucleus_schema_migrations") {
		t.Fatalf("fixture output should skip migration table: %s", fixtureText)
	}

	_, _ = dbConn.Exec("DELETE FROM users;")
	_, _ = dbConn.Exec("DELETE FROM posts;")

	out.Reset()
	errOut.Reset()
	code = run([]string{"loaddata", "--config", cfgPath, fixturePath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("loaddata failed: code=%d stderr=%s", code, errOut.String())
	}

	var usersCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM users").Scan(&usersCount); err != nil {
		t.Fatalf("count users failed: %v", err)
	}
	if usersCount != 2 {
		t.Fatalf("expected 2 users after loaddata, got %d", usersCount)
	}

	var postsCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM posts").Scan(&postsCount); err != nil {
		t.Fatalf("count posts failed: %v", err)
	}
	if postsCount != 1 {
		t.Fatalf("expected 1 post after loaddata, got %d", postsCount)
	}

	var migrationsCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM nucleus_schema_migrations").Scan(&migrationsCount); err != nil {
		t.Fatalf("count migrations failed: %v", err)
	}
	if migrationsCount != 1 {
		t.Fatalf("expected migrations table untouched, got %d", migrationsCount)
	}
}

func TestRun_LoadDataTruncate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	fixturePath := filepath.Join(dir, "fixture_users.json")

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	_, _ = dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('legacy');")

	writeFile(t, fixturePath, `{"tables":[{"name":"users","rows":[{"id":7,"name":"fresh"}]}]}`)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"loaddata", "--config", cfgPath, "--truncate", "--yes", fixturePath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("loaddata truncate failed: code=%d stderr=%s", code, errOut.String())
	}

	var (
		id   int
		name string
	)
	if err := dbConn.QueryRow("SELECT id, name FROM users LIMIT 1").Scan(&id, &name); err != nil {
		t.Fatalf("query loaded user failed: %v", err)
	}
	if id != 7 || name != "fresh" {
		t.Fatalf("unexpected loaded user row: id=%d name=%s", id, name)
	}
}

func TestRun_LoadDataTruncateProductionGuardrailNonInteractive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")
	fixturePath := filepath.Join(dir, "fixture_users.json")

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	_, _ = dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('legacy');")

	writeFile(t, fixturePath, `{"tables":[{"name":"users","rows":[{"id":7,"name":"fresh"}]}]}`)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := runWithInput(
		[]string{"loaddata", "--config", cfgPath, "--truncate", fixturePath},
		strings.NewReader(""),
		&out,
		&errOut,
	)
	if code == 0 {
		t.Fatalf("expected loaddata --truncate in production without force/yes to fail; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected loaddata guardrail error: %s", errOut.String())
	}

	var count int
	if err := dbConn.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("count users after blocked loaddata failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected legacy users row to remain after blocked loaddata, got %d rows", count)
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput(
		[]string{"loaddata", "--config", cfgPath, "--truncate", "--yes", fixturePath},
		strings.NewReader(""),
		&out,
		&errOut,
	)
	if code != 0 {
		t.Fatalf("loaddata --truncate with --yes failed: code=%d stderr=%s", code, errOut.String())
	}

	var (
		id   int
		name string
	)
	if err := dbConn.QueryRow("SELECT id, name FROM users LIMIT 1").Scan(&id, &name); err != nil {
		t.Fatalf("query loaded user after confirmed truncate failed: %v", err)
	}
	if id != 7 || name != "fresh" {
		t.Fatalf("unexpected user row after confirmed truncate: id=%d name=%s", id, name)
	}
}

func TestRun_InspectDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	_, _ = dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL, created_at DATETIME);")
	_, _ = dbConn.Exec("CREATE TABLE nucleus_schema_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL);")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"inspectdb", "--config", cfgPath, "--tables", "users"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("inspectdb failed: code=%d stderr=%s", code, errOut.String())
	}

	got := out.String()
	if !strings.Contains(got, "package models") {
		t.Fatalf("inspectdb output missing package declaration: %s", got)
	}
	if !strings.Contains(got, "type User struct") {
		t.Fatalf("inspectdb output missing User struct: %s", got)
	}
	if !regexp.MustCompile(`\bID\s+int64\b`).MatchString(got) || !regexp.MustCompile(`\bEmail\s+string\b`).MatchString(got) {
		t.Fatalf("inspectdb output missing expected fields: %s", got)
	}
	if !strings.Contains(got, "func (User) TableName() string") || !strings.Contains(got, `return "users"`) {
		t.Fatalf("inspectdb output missing TableName method: %s", got)
	}
	if strings.Contains(got, "nucleus_schema_migrations") {
		t.Fatalf("inspectdb should skip migration table: %s", got)
	}
}

func TestRun_OGRInspect(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	outputPath := filepath.Join(dir, "internal", "models", "geospatial.go")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create users table failed: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE places (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		geom GEOMETRY
	)`); err != nil {
		t.Fatalf("create places table failed: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"ogrinspect",
		"--config", cfgPath,
		"--output", outputPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("ogrinspect failed: code=%d stderr=%s", code, errOut.String())
	}

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read ogrinspect output failed: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "Code generated by nucleus ogrinspect; DO NOT EDIT.") {
		t.Fatalf("missing ogrinspect header: %s", got)
	}
	if !strings.Contains(got, "type Place struct") {
		t.Fatalf("expected geospatial table struct in output: %s", got)
	}
	if strings.Contains(got, "type User struct") {
		t.Fatalf("expected non-geospatial table excluded by default: %s", got)
	}
}

func TestRun_OGRInspectNoGeospatialTables(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create users table failed: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"ogrinspect",
		"--config", cfgPath,
	}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected ogrinspect to fail when no geospatial tables exist; output=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "no geospatial tables selected") {
		t.Fatalf("unexpected ogrinspect error: %s", errOut.String())
	}
}

func TestRun_InspectDBOutputFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	outFile := filepath.Join(dir, "internal", "models", "introspected.go")

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()
	_, _ = dbConn.Exec("CREATE TABLE projects (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"inspectdb",
		"--config", cfgPath,
		"--package", "models",
		"--output", outFile,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("inspectdb output file failed: code=%d stderr=%s", code, errOut.String())
	}

	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read inspectdb output file failed: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "type Project struct") {
		t.Fatalf("inspectdb file output missing Project struct: %s", text)
	}
}

func TestRun_GenerateModelAndHandler(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"generate", "--out", dir, "model", "UserProfile"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate model failed: code=%d stderr=%s", code, errOut.String())
	}
	modelPath := filepath.Join(dir, "internal", "models", "user_profile.go")
	if _, err := os.Stat(modelPath); err != nil {
		t.Fatalf("expected model scaffold file %s: %v", modelPath, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "models", "user_profile.go")); err == nil {
		t.Fatalf("unexpected legacy model path generated")
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"generate", "--out", dir, "handler", "UserProfile"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate handler failed: code=%d stderr=%s", code, errOut.String())
	}
	handlerPath := filepath.Join(dir, "internal", "controllers", "user_profile_handler.go")
	if _, err := os.Stat(handlerPath); err != nil {
		t.Fatalf("expected handler scaffold file %s: %v", handlerPath, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "handlers", "user_profile_handler.go")); err == nil {
		t.Fatalf("unexpected legacy handler path generated")
	}
	handlerRaw, err := os.ReadFile(handlerPath)
	if err != nil {
		t.Fatalf("read generated handler failed: %v", err)
	}
	handlerText := string(handlerRaw)
	if strings.Contains(handlerText, "internal/services") {
		t.Fatalf("standalone handler scaffold should remain module-agnostic without go.mod: %s", handlerText)
	}

	moduleDir := t.TempDir()
	writeFile(t, filepath.Join(moduleDir, "go.mod"), fmt.Sprintf(`module example.com/generated

go 1.25.0

require github.com/jcsvwinston/nucleus v0.0.0

replace github.com/jcsvwinston/nucleus => %s
`, filepath.ToSlash(repoRoot(t))))

	out.Reset()
	errOut.Reset()
	code = run([]string{"generate", "--out", moduleDir, "handler", "UserProfile"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate module-aware handler failed: code=%d stderr=%s", code, errOut.String())
	}
	moduleHandlerPath := filepath.Join(moduleDir, "internal", "controllers", "user_profile_handler.go")
	moduleServicePath := filepath.Join(moduleDir, "internal", "services", "user_profile_service.go")
	if _, err := os.Stat(moduleHandlerPath); err != nil {
		t.Fatalf("expected module-aware handler scaffold file %s: %v", moduleHandlerPath, err)
	}
	if _, err := os.Stat(moduleServicePath); err != nil {
		t.Fatalf("expected companion service scaffold file %s: %v", moduleServicePath, err)
	}
	moduleHandlerRaw, err := os.ReadFile(moduleHandlerPath)
	if err != nil {
		t.Fatalf("read module-aware handler failed: %v", err)
	}
	moduleHandlerText := string(moduleHandlerRaw)
	if !strings.Contains(moduleHandlerText, `"example.com/generated/internal/services"`) {
		t.Fatalf("expected module-aware handler to import services: %s", moduleHandlerText)
	}
	if !strings.Contains(moduleHandlerText, "services.UserProfileHealthInput{}") {
		t.Fatalf("expected module-aware handler to call the generated service contract: %s", moduleHandlerText)
	}
	runGoMod(t, moduleDir, "mod", "tidy")
	runGoTest(t, moduleDir)

	out.Reset()
	errOut.Reset()
	code = run([]string{"generate", "--out", dir, "service", "UserProfile"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate service failed: code=%d stderr=%s", code, errOut.String())
	}
	servicePath := filepath.Join(dir, "internal", "services", "user_profile_service.go")
	if _, err := os.Stat(servicePath); err != nil {
		t.Fatalf("expected service scaffold file %s: %v", servicePath, err)
	}
	serviceRaw, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("read generated service failed: %v", err)
	}
	serviceText := string(serviceRaw)
	if !strings.Contains(serviceText, "type UserProfileResult struct") {
		t.Fatalf("expected generated service result contract: %s", serviceText)
	}
	if !strings.Contains(serviceText, "type UserProfileHealthInput struct{}") {
		t.Fatalf("expected generated service input contract: %s", serviceText)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"generate", "--out", dir, "repository", "UserProfile"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate repository failed: code=%d stderr=%s", code, errOut.String())
	}
	repositoryPath := filepath.Join(dir, "internal", "repositories", "user_profile_repository.go")
	if _, err := os.Stat(repositoryPath); err != nil {
		t.Fatalf("expected repository scaffold file %s: %v", repositoryPath, err)
	}
}

func TestRun_GenerateResource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), fmt.Sprintf(`module example.com/generated

go 1.25.0

require github.com/jcsvwinston/nucleus v0.0.0

replace github.com/jcsvwinston/nucleus => %s
`, filepath.ToSlash(repoRoot(t))))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"generate", "--out", dir, "resource", "Category"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate resource failed: code=%d stderr=%s", code, errOut.String())
	}

	modelPath := filepath.Join(dir, "internal", "models", "category.go")
	handlerPath := filepath.Join(dir, "internal", "controllers", "category_handler.go")
	servicePath := filepath.Join(dir, "internal", "services", "category_service.go")
	repositoryPath := filepath.Join(dir, "internal", "repositories", "category_repository.go")
	contractPath := filepath.Join(dir, "internal", "contracts", "category_contract.go")
	testPath := filepath.Join(dir, "internal", "controllers", "category_handler_test.go")

	for _, p := range []string{modelPath, handlerPath, servicePath, repositoryPath, contractPath, testPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected generated file %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "models", "category.go")); err == nil {
		t.Fatalf("unexpected legacy resource model path generated")
	}
	if _, err := os.Stat(filepath.Join(dir, "handlers", "category_handler.go")); err == nil {
		t.Fatalf("unexpected legacy resource handler path generated")
	}

	migrationsDir := filepath.Join(dir, "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 migration files, got %d", len(entries))
	}
	var upPath string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			upPath = filepath.Join(migrationsDir, entry.Name())
			break
		}
	}
	if upPath == "" {
		t.Fatalf("expected generated up migration file, got %v", entries)
	}
	upRaw, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read generated up migration failed: %v", err)
	}
	upText := string(upRaw)
	if !strings.Contains(upText, `CREATE TABLE IF NOT EXISTS "categories"`) {
		t.Fatalf("unexpected generated up migration table DDL: %s", upText)
	}
	if !strings.Contains(upText, `CREATE INDEX IF NOT EXISTS "idx_categories_name" ON "categories" ("name");`) {
		t.Fatalf("expected deterministic name index in generated migration: %s", upText)
	}

	handlerRaw, err := os.ReadFile(handlerPath)
	if err != nil {
		t.Fatalf("read generated handler failed: %v", err)
	}
	handlerText := string(handlerRaw)
	if strings.Contains(handlerText, "StatusNotImplemented") {
		t.Fatalf("resource handler should not contain placeholder 501 responses: %s", handlerText)
	}
	if !strings.Contains(handlerText, `"example.com/generated/internal/services"`) {
		t.Fatalf("expected generated resource handler to depend on services: %s", handlerText)
	}
	if strings.Contains(handlerText, "sync.RWMutex") {
		t.Fatalf("module-aware resource handler should not keep in-memory state directly: %s", handlerText)
	}
	if !strings.Contains(handlerText, `r.Resource("/categories", router.ResourceHandlers{`) {
		t.Fatalf("expected resource helper wiring in generated handler: %s", handlerText)
	}
	if !strings.Contains(handlerText, `h.service.Create(`) {
		t.Fatalf("expected generated handler to delegate create to service: %s", handlerText)
	}
	if !strings.Contains(handlerText, `services.ListCategoryInput{`) || !strings.Contains(handlerText, `c.Query("q")`) {
		t.Fatalf("expected generated handler to pass list query input into service: %s", handlerText)
	}

	serviceRaw, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("read generated service failed: %v", err)
	}
	serviceText := string(serviceRaw)
	if strings.Contains(serviceText, "sync.RWMutex") {
		t.Fatalf("resource service should not hold repository state directly: %s", serviceText)
	}
	if !strings.Contains(serviceText, "type CreateCategoryInput struct") || !strings.Contains(serviceText, "type UpdateCategoryInput struct") {
		t.Fatalf("expected explicit service contracts for resource scaffold: %s", serviceText)
	}
	if !strings.Contains(serviceText, "type ListCategoryInput struct") || !strings.Contains(serviceText, "repositories.ListCategoryParams") {
		t.Fatalf("expected generated resource service to formalize list input/params conventions: %s", serviceText)
	}
	if strings.Contains(serviceText, "repositories.CategoryRecord") && !strings.Contains(serviceText, "mapCategoryRecord(") {
		t.Fatalf("resource service should map repository records into service records: %s", serviceText)
	}
	if !strings.Contains(serviceText, "type CategoryRepository interface") {
		t.Fatalf("expected resource service to depend on repository interface: %s", serviceText)
	}

	repositoryRaw, err := os.ReadFile(repositoryPath)
	if err != nil {
		t.Fatalf("read generated repository failed: %v", err)
	}
	repositoryText := string(repositoryRaw)
	if !strings.Contains(repositoryText, "type CategoryRepository struct") || !strings.Contains(repositoryText, "func (r *CategoryRepository) Create") {
		t.Fatalf("expected repository-backed resource scaffold: %s", repositoryText)
	}
	if !strings.Contains(repositoryText, "type ListCategoryParams struct") || !strings.Contains(repositoryText, `strings.TrimSpace(params.Query)`) {
		t.Fatalf("expected generated resource repository to formalize list params and filtering: %s", repositoryText)
	}

	contractRaw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read generated contract failed: %v", err)
	}
	contractText := string(contractRaw)
	if !strings.Contains(contractText, `"github.com/jcsvwinston/nucleus/pkg/openapi"`) {
		t.Fatalf("expected openapi import in generated contract scaffold: %s", contractText)
	}
	if !strings.Contains(contractText, "RegisterContract(RegisterCategoryContract)") {
		t.Fatalf("expected generated resource contract scaffold to auto-register into contracts aggregator: %s", contractText)
	}
	if !strings.Contains(contractText, `func RegisterCategoryContract`) || !strings.Contains(contractText, `doc.Paths["/categories"]`) {
		t.Fatalf("expected generated openapi contract scaffold for resource: %s", contractText)
	}
	if !strings.Contains(contractText, `openapi.PathParameter("id", openapi.IDSchema(), "Category identifier")`) {
		t.Fatalf("expected generated resource contract scaffold to declare id path parameter helper: %s", contractText)
	}
	if !strings.Contains(contractText, "openapi.JSONRequestBody(") || !strings.Contains(contractText, "openapi.JSONResponse(") {
		t.Fatalf("expected generated resource contract scaffold to use shared openapi helpers: %s", contractText)
	}
	if !strings.Contains(contractText, "openapi.CollectionEnvelopeSchema(") || !strings.Contains(contractText, "openapi.DataEnvelopeSchema(") {
		t.Fatalf("expected generated resource contract scaffold to use shared envelope helpers: %s", contractText)
	}
	if !strings.Contains(contractText, "openapi.SearchQueryParameter(") {
		t.Fatalf("expected generated resource contract scaffold to declare shared search query parameter: %s", contractText)
	}
	if !strings.Contains(contractText, "openapi.ErrorResponse(") || !strings.Contains(contractText, "openapi.EmptyResponse(") {
		t.Fatalf("expected generated resource contract scaffold to use shared openapi error/empty helpers: %s", contractText)
	}

	testRaw, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("read generated handler test failed: %v", err)
	}
	testText := string(testRaw)
	if !strings.Contains(testText, "CRUDLifecycle") {
		t.Fatalf("expected CRUD lifecycle test in generated test scaffold: %s", testText)
	}
	if !strings.Contains(testText, "services.NewCategoryService") {
		t.Fatalf("expected resource test scaffold to wire repository and service: %s", testText)
	}
	// Per-entity helper name (assertCategoryErrorResponse): generated files
	// are self-contained so multi-entity projects never collide on a shared
	// package-level symbol.
	if !strings.Contains(testText, "assertCategoryErrorResponse") {
		t.Fatalf("expected resource test scaffold to validate structured error responses: %s", testText)
	}

	runGoMod(t, dir, "mod", "tidy")
	runGoTest(t, dir)
}

func TestRun_NewProjectScaffold(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"new",
		"BlogApp",
		"--out", dir,
		"--module", "example.com/blogapp",
		"--port", "9095",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("new project failed: code=%d stderr=%s", code, errOut.String())
	}

	projectDir := filepath.Join(dir, "BlogApp")
	// Since the 2026-05-25 skeleton rework `nucleus new` emits an EMPTY
	// skeleton — a fluent root main.go, config, an empty migrations/ dir and
	// the RBAC policy — with NO demo feature code (the worked demo lives only
	// in examples/mvc_api). This is the CLI-integration smoke; the deep
	// skeleton assertions live in internal/cli/new_scaffold_test.go.
	expectedFiles := []string{
		filepath.Join(projectDir, "go.mod"),
		filepath.Join(projectDir, "nucleus.yml"),
		filepath.Join(projectDir, "README.md"),
		filepath.Join(projectDir, ".gitignore"),
		filepath.Join(projectDir, "main.go"),
		filepath.Join(projectDir, "rbac_policy.csv"),
		filepath.Join(projectDir, "migrations", ".gitkeep"),
	}
	for _, p := range expectedFiles {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected generated file %s: %v", p, err)
		}
	}

	// The removed demo layout must not reappear.
	for _, gone := range []string{
		filepath.Join(projectDir, "cmd", "server", "main.go"),
		filepath.Join(projectDir, "internal", "models", "article.go"),
		filepath.Join(projectDir, "internal", "contracts"),
		filepath.Join(projectDir, "seeds"),
	} {
		if _, err := os.Stat(gone); err == nil {
			t.Fatalf("skeleton must not generate the removed demo path %s", gone)
		}
	}

	if info, err := os.Stat(filepath.Join(projectDir, "migrations")); err != nil || !info.IsDir() {
		t.Fatalf("expected an empty migrations/ directory: %v", err)
	}

	goModRaw, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod failed: %v", err)
	}
	goMod := string(goModRaw)
	if !strings.Contains(goMod, "module example.com/blogapp") {
		t.Fatalf("go.mod missing module path: %s", goMod)
	}
	if !strings.Contains(goMod, "go 1.26") {
		t.Fatalf("go.mod missing expected go version: %s", goMod)
	}
	// The framework go.mod pins no `toolchain` line (its `go` directive is a
	// full patch version), so the scaffold must not emit one either
	// (audit CLI-V2-1). The exact values are enforced by the internal/cli
	// freshness test; here we just pin the no-stale-toolchain behavior.
	if strings.Contains(goMod, "toolchain ") {
		t.Fatalf("go.mod should not contain a toolchain directive: %s", goMod)
	}

	mainRaw, err := os.ReadFile(filepath.Join(projectDir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go failed: %v", err)
	}
	if !strings.Contains(string(mainRaw), "nucleus.New(") {
		t.Fatalf("expected skeleton main.go to compose on the fluent surface: %s", string(mainRaw))
	}

	cfgRaw, err := os.ReadFile(filepath.Join(projectDir, "nucleus.yml"))
	if err != nil {
		t.Fatalf("read nucleus.yml failed: %v", err)
	}
	cfg := string(cfgRaw)
	if !strings.Contains(cfg, "port: 9095") {
		t.Fatalf("nucleus.yml missing configured port: %s", cfg)
	}
	if !strings.Contains(cfg, "rate_limit_requests: 0") || !strings.Contains(cfg, "rate_limit_window: 1m") {
		t.Fatalf("nucleus.yml missing rate limit defaults: %s", cfg)
	}
	if !strings.Contains(cfg, "rate_limit_burst: 0") || !strings.Contains(cfg, "rate_limit_by_route: false") || !strings.Contains(cfg, "rate_limit_by_role: false") {
		t.Fatalf("nucleus.yml missing advanced rate limit defaults: %s", cfg)
	}
	if !strings.Contains(cfg, "otlp_endpoint: \"\"") {
		t.Fatalf("nucleus.yml missing otlp_endpoint default: %s", cfg)
	}
}

func TestRun_NewProjectFailsWithoutForceWhenExists(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	first := run([]string{"new", "Demo", "--out", dir}, &out, &errOut)
	if first != 0 {
		t.Fatalf("first scaffold should pass: code=%d stderr=%s", first, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	second := run([]string{"new", "Demo", "--out", dir}, &out, &errOut)
	if second == 0 {
		t.Fatalf("expected second scaffold without --force to fail")
	}
	if !strings.Contains(errOut.String(), "already exists") {
		t.Fatalf("unexpected error output: %s", errOut.String())
	}
}

func TestRun_NewProjectSupportsFlagsBeforeName(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"new",
		"--out", dir,
		"--module", "example.com/flagsbefore",
		"--port", "9111",
		"FlagsBefore",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("new project (flags before name) failed: code=%d stderr=%s", code, errOut.String())
	}

	projectDir := filepath.Join(dir, "FlagsBefore")
	if _, err := os.Stat(filepath.Join(projectDir, "main.go")); err != nil {
		t.Fatalf("expected scaffolded main.go: %v", err)
	}
}

func TestRun_NewProjectSupportsTemplateFlag(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"new",
		"TemplateApp",
		"--out", dir,
		"--module", "example.com/templateapp",
		"--template", "mvc",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("new project with --template failed: code=%d stderr=%s", code, errOut.String())
	}

	projectDir := filepath.Join(dir, "TemplateApp")
	if _, err := os.Stat(filepath.Join(projectDir, "main.go")); err != nil {
		t.Fatalf("expected scaffolded main.go: %v", err)
	}
}

func TestRun_NewProjectRejectsUnknownTemplate(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"new",
		"BadTemplateApp",
		"--out", dir,
		"--template", "graphql",
	}, &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero code for unsupported template")
	}
	if !strings.Contains(errOut.String(), "unsupported template") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestRun_StartAppScaffold(t *testing.T) {
	dir := t.TempDir()
	migDir := filepath.Join(dir, "migrations")
	writeFile(t, filepath.Join(dir, "go.mod"), fmt.Sprintf(`module example.com/scaffold

go 1.25.0

require github.com/jcsvwinston/nucleus v0.0.0

replace github.com/jcsvwinston/nucleus => %s
`, filepath.ToSlash(repoRoot(t))))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"startapp",
		"Billing",
		"--out", dir,
		"--migrations", migDir,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("startapp failed: code=%d stderr=%s", code, errOut.String())
	}

	expectedFiles := []string{
		filepath.Join(dir, "internal", "models", "billing.go"),
		filepath.Join(dir, "internal", "controllers", "billing_api.go"),
		filepath.Join(dir, "internal", "controllers", "billing_page.go"),
		filepath.Join(dir, "internal", "contracts", "billing_contract.go"),
		filepath.Join(dir, "internal", "services", "billing_service.go"),
		filepath.Join(dir, "internal", "repositories", "billing_repository.go"),
		filepath.Join(dir, "internal", "tasks", "billing_tasks.go"),
		filepath.Join(dir, "internal", "web", "templates", "billing", "index.html"),
	}
	for _, p := range expectedFiles {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected generated file %s: %v", p, err)
		}
	}

	expectedDirs := []string{
		filepath.Join(dir, "internal", "contracts"),
		filepath.Join(dir, "internal", "services"),
		filepath.Join(dir, "internal", "repositories"),
		filepath.Join(dir, "internal", "web", "static", "billing"),
	}
	for _, p := range expectedDirs {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected generated directory %s: %v", p, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", p)
		}
	}

	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("read migrations dir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 migration files, got %d", len(entries))
	}

	var up, down bool
	var upPath string
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, "create_billings_table") && strings.HasSuffix(name, ".up.sql") {
			up = true
			upPath = filepath.Join(migDir, name)
		}
		if strings.Contains(name, "create_billings_table") && strings.HasSuffix(name, ".down.sql") {
			down = true
		}
	}
	if !up || !down {
		t.Fatalf("expected up/down billing migration files, got %v", entries)
	}
	upRaw, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatalf("read billing up migration failed: %v", err)
	}
	upText := string(upRaw)
	if !strings.Contains(upText, `CREATE INDEX IF NOT EXISTS "idx_billings_name" ON "billings" ("name");`) {
		t.Fatalf("expected deterministic billing name index in scaffold migration: %s", upText)
	}

	controllerRaw, err := os.ReadFile(filepath.Join(dir, "internal", "controllers", "billing_api.go"))
	if err != nil {
		t.Fatalf("read billing controller failed: %v", err)
	}
	controllerText := string(controllerRaw)
	if !strings.Contains(controllerText, `"example.com/scaffold/internal/services"`) {
		t.Fatalf("expected service import in startapp controller scaffold: %s", controllerText)
	}
	if strings.Contains(controllerText, "database/sql") {
		t.Fatalf("did not expect direct sql dependency in module-aware controller scaffold: %s", controllerText)
	}
	if !strings.Contains(controllerText, `"data":  items`) || !strings.Contains(controllerText, `"count": len(items)`) {
		t.Fatalf("expected startapp controller list scaffold to use shared collection envelope: %s", controllerText)
	}
	if !strings.Contains(controllerText, `"data": item`) {
		t.Fatalf("expected startapp controller create scaffold to use shared data envelope: %s", controllerText)
	}
	if !strings.Contains(controllerText, `services.ListBillingInput{`) || !strings.Contains(controllerText, `c.Query("q")`) {
		t.Fatalf("expected startapp controller list scaffold to pass query input into service: %s", controllerText)
	}
	if strings.Contains(controllerText, `"resource":`) || strings.Contains(controllerText, `"items":    items`) || strings.Contains(controllerText, `"item":     item`) {
		t.Fatalf("did not expect legacy startapp envelope fields: %s", controllerText)
	}

	serviceRaw, err := os.ReadFile(filepath.Join(dir, "internal", "services", "billing_service.go"))
	if err != nil {
		t.Fatalf("read billing service failed: %v", err)
	}
	serviceText := string(serviceRaw)
	if !strings.Contains(serviceText, `"example.com/scaffold/internal/repositories"`) {
		t.Fatalf("expected repository import in module-aware service scaffold: %s", serviceText)
	}
	if !strings.Contains(serviceText, "type BillingRecord struct") {
		t.Fatalf("expected explicit service output contract in module-aware scaffold: %s", serviceText)
	}
	if !strings.Contains(serviceText, "type CreateBillingInput struct") || !strings.Contains(serviceText, "type ListBillingInput struct") {
		t.Fatalf("expected explicit service input contract in module-aware scaffold: %s", serviceText)
	}
	if !strings.Contains(serviceText, "func (s *BillingService) RecordCreated") {
		t.Fatalf("expected module-aware service scaffold to expose task-facing hook: %s", serviceText)
	}
	if !strings.Contains(serviceText, "type BillingRepository interface") {
		t.Fatalf("expected module-aware service scaffold to depend on repository interface: %s", serviceText)
	}
	if strings.Contains(serviceText, "repository *repositories.BillingRepository") {
		t.Fatalf("module-aware service scaffold should not depend on concrete repository struct: %s", serviceText)
	}
	if !strings.Contains(serviceText, "repository BillingRepository") {
		t.Fatalf("expected module-aware service scaffold to store repository interface: %s", serviceText)
	}
	if !strings.Contains(serviceText, "repositories.ListBillingParams") || !strings.Contains(serviceText, "repositories.CreateBillingParams") {
		t.Fatalf("expected module-aware service scaffold to use repository params conventions: %s", serviceText)
	}

	repositoryRaw, err := os.ReadFile(filepath.Join(dir, "internal", "repositories", "billing_repository.go"))
	if err != nil {
		t.Fatalf("read billing repository failed: %v", err)
	}
	repositoryText := string(repositoryRaw)
	if !strings.Contains(repositoryText, "type ListBillingParams struct") || !strings.Contains(repositoryText, "type CreateBillingParams struct") {
		t.Fatalf("expected module-aware repository scaffold to expose list/create params: %s", repositoryText)
	}
	if !strings.Contains(repositoryText, `strings.TrimSpace(params.Query)`) {
		t.Fatalf("expected module-aware repository scaffold to honor query filtering: %s", repositoryText)
	}

	taskRaw, err := os.ReadFile(filepath.Join(dir, "internal", "tasks", "billing_tasks.go"))
	if err != nil {
		t.Fatalf("read billing task scaffold failed: %v", err)
	}
	taskText := string(taskRaw)
	if !strings.Contains(taskText, `"example.com/scaffold/internal/services"`) {
		t.Fatalf("expected module-aware task scaffold to import services: %s", taskText)
	}
	if !strings.Contains(taskText, "func RegisterBillingTasks(manager gftasks.Manager, service *services.BillingService) error") {
		t.Fatalf("expected module-aware task registration to accept service dependency: %s", taskText)
	}
	if !strings.Contains(taskText, "gftasks.DecodeJSONPayload(task, &payload)") {
		t.Fatalf("expected module-aware task scaffold to use shared payload decoder helper: %s", taskText)
	}
	if !strings.Contains(taskText, "service.RecordCreated") {
		t.Fatalf("expected module-aware task handler to delegate into service: %s", taskText)
	}

	contractRaw, err := os.ReadFile(filepath.Join(dir, "internal", "contracts", "billing_contract.go"))
	if err != nil {
		t.Fatalf("read billing contract failed: %v", err)
	}
	contractText := string(contractRaw)
	if !strings.Contains(contractText, `"github.com/jcsvwinston/nucleus/pkg/openapi"`) {
		t.Fatalf("expected startapp contract to import openapi: %s", contractText)
	}
	if !strings.Contains(contractText, "RegisterContract(RegisterBillingContract)") {
		t.Fatalf("expected startapp contract scaffold to auto-register into contracts aggregator: %s", contractText)
	}
	if !strings.Contains(contractText, `func RegisterBillingContract`) || !strings.Contains(contractText, `doc.Paths["/billings"]`) {
		t.Fatalf("expected startapp openapi contract scaffold: %s", contractText)
	}
	if !strings.Contains(contractText, "openapi.JSONRequestBody(") || !strings.Contains(contractText, "openapi.CollectionEnvelopeSchema(") || !strings.Contains(contractText, "openapi.DataEnvelopeSchema(") {
		t.Fatalf("expected startapp contract scaffold to use shared openapi envelope helpers: %s", contractText)
	}
	if !strings.Contains(contractText, "openapi.SearchQueryParameter(") {
		t.Fatalf("expected startapp contract scaffold to declare shared search query parameter: %s", contractText)
	}
	if !strings.Contains(contractText, "openapi.ErrorResponse(") {
		t.Fatalf("expected startapp contract scaffold to use shared openapi error response helper: %s", contractText)
	}

	runGoMod(t, dir, "mod", "tidy")
	runGoTest(t, dir)
}

func TestRun_OpenAPIExport(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"new",
		"ContractApp",
		"--out", dir,
		"--module", "example.com/contractapp",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("new project failed: code=%d stderr=%s", code, errOut.String())
	}

	projectDir := filepath.Join(dir, "ContractApp")
	wireGeneratedModuleToRepo(t, projectDir)

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"startapp",
		"Billing",
		"--out", projectDir,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("startapp failed: code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"generate",
		"--out", projectDir,
		"resource",
		"Category",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate resource failed: code=%d stderr=%s", code, errOut.String())
	}

	runGoMod(t, projectDir, "mod", "tidy")
	runGoTest(t, projectDir)

	openAPIPath := filepath.Join(projectDir, "openapi.json")
	out.Reset()
	errOut.Reset()
	code = run([]string{
		"openapi",
		"--project", projectDir,
		"--out", openAPIPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("openapi export failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "OpenAPI document exported:") {
		t.Fatalf("unexpected openapi export output: %s", out.String())
	}

	raw, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Fatalf("read exported openapi file failed: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected valid exported openapi JSON: %s", string(raw))
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode exported openapi document failed: %v", err)
	}
	if got := doc["openapi"]; got != "3.1.0" {
		t.Fatalf("unexpected openapi version: %v", got)
	}

	info, ok := doc["info"].(map[string]any)
	if !ok {
		t.Fatalf("expected info object in exported document: %#v", doc["info"])
	}
	// Skeleton rework (2026-05-25) removed the baked-in contracts aggregator;
	// the title is now derived from the module path via defaultOpenAPITitle
	// (toPascalCase of the module base) rather than the project-name casing.
	if got := info["title"]; got != "Contractapp API" {
		t.Fatalf("unexpected openapi title: %v", got)
	}
	if got := info["description"]; got != "Experimental OpenAPI contract generated by Nucleus from internal/contracts." {
		t.Fatalf("unexpected openapi description: %v", got)
	}
	if servers, ok := doc["servers"].([]any); !ok || len(servers) != 1 {
		t.Fatalf("expected default servers metadata in exported document: %#v", doc["servers"])
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths object in exported document: %#v", doc["paths"])
	}
	for _, path := range []string{"/billings", "/categories", "/categories/{id}"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("expected exported path %s in document: %#v", path, paths)
		}
	}

	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("expected components object in exported document: %#v", doc["components"])
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("expected schemas object in exported document: %#v", components["schemas"])
	}
	for _, schema := range []string{
		"BillingRecord",
		"CreateBillingInput",
		"CategoryRecord",
		"CreateCategoryInput",
		"UpdateCategoryInput",
	} {
		if _, ok := schemas[schema]; !ok {
			t.Fatalf("expected exported schema %s in document: %#v", schema, schemas)
		}
	}

	var typedDoc openapi.Document
	if err := json.Unmarshal(raw, &typedDoc); err != nil {
		t.Fatalf("decode exported openapi document into typed struct failed: %v", err)
	}
	assertOperationMetadata(t, typedDoc.Paths["/billings"].Get, "listBillings", "billings")
	assertCollectionEnvelopeResponse(t, typedDoc.Paths["/billings"].Get, "200")
	assertSearchQueryParameter(t, typedDoc.Paths["/billings"].Get, "Filter billings by name.")
	assertOperationErrorResponse(t, typedDoc.Paths["/billings"].Get, "500")
	assertOperationMetadata(t, typedDoc.Paths["/billings"].Post, "createBilling", "billings")
	assertOperationJSONRequestBody(t, typedDoc.Paths["/billings"].Post)
	assertDataEnvelopeResponse(t, typedDoc.Paths["/billings"].Post, "201")
	assertOperationErrorResponse(t, typedDoc.Paths["/billings"].Post, "400")
	assertOperationMetadata(t, typedDoc.Paths["/categories"].Get, "listCategories", "categories")
	assertCollectionEnvelopeResponse(t, typedDoc.Paths["/categories"].Get, "200")
	assertSearchQueryParameter(t, typedDoc.Paths["/categories"].Get, "Filter categories by name.")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories"].Get, "500")
	assertOperationMetadata(t, typedDoc.Paths["/categories"].Post, "createCategory", "categories")
	assertOperationJSONRequestBody(t, typedDoc.Paths["/categories"].Post)
	assertDataEnvelopeResponse(t, typedDoc.Paths["/categories"].Post, "201")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories"].Post, "400")
	assertPathIDOperation(t, typedDoc.Paths["/categories/{id}"].Get, "getCategory")
	assertDataEnvelopeResponse(t, typedDoc.Paths["/categories/{id}"].Get, "200")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories/{id}"].Get, "400")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories/{id}"].Get, "404")
	assertPathIDOperation(t, typedDoc.Paths["/categories/{id}"].Put, "updateCategory")
	assertDataEnvelopeResponse(t, typedDoc.Paths["/categories/{id}"].Put, "200")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories/{id}"].Put, "400")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories/{id}"].Put, "404")
	assertPathIDOperation(t, typedDoc.Paths["/categories/{id}"].Delete, "deleteCategory")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories/{id}"].Delete, "400")
	assertOperationErrorResponse(t, typedDoc.Paths["/categories/{id}"].Delete, "404")
	assertEmptyResponse(t, typedDoc.Paths["/categories/{id}"].Delete, "204", "Resource deleted")

	runtimeTestPath := filepath.Join(projectDir, "openapi_runtime_test.go")
	writeFile(t, runtimeTestPath, `package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/contractapp/internal/contracts"
	"github.com/jcsvwinston/nucleus/pkg/app"
)

	func TestRuntimeOpenAPIEndpointMatchesContractsDocument(t *testing.T) {
		cfg, err := app.LoadConfig("nucleus.yml")
		if err != nil {
			t.Fatalf("load config: %v", err)
		}

		a, err := app.New(cfg)
		if err != nil {
			t.Fatalf("new app: %v", err)
		}
		defer a.Shutdown(nil)

		// ADR-004: framework mounts a default-deny middleware. Seed an
		// anonymous allow for the path under test so this harness can
		// exercise OpenAPI runtime serving without modelling auth.
		if err := a.Authorizer.AddPolicy("anonymous", "/openapi.json", "*"); err != nil {
			t.Fatalf("seed allow: %v", err)
		}
		if err := a.MountOpenAPI("/openapi.json", contracts.NewDocument); err != nil {
			t.Fatalf("mount openapi: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		var runtimeDoc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &runtimeDoc); err != nil {
			t.Fatalf("decode runtime doc: %v", err)
		}

		expectedRaw, err := json.MarshalIndent(contracts.NewDocument(), "", "  ")
		if err != nil {
			t.Fatalf("marshal expected doc: %v", err)
		}

		var expectedDoc map[string]any
		if err := json.Unmarshal(expectedRaw, &expectedDoc); err != nil {
			t.Fatalf("decode expected doc: %v", err)
		}

		runtimeNormalized, err := json.Marshal(runtimeDoc)
		if err != nil {
			t.Fatalf("marshal runtime doc: %v", err)
		}
		expectedNormalized, err := json.Marshal(expectedDoc)
		if err != nil {
			t.Fatalf("marshal expected normalized doc: %v", err)
		}

		if string(runtimeNormalized) != string(expectedNormalized) {
			t.Fatalf("runtime document mismatch\nruntime=%s\nexpected=%s", string(runtimeNormalized), string(expectedNormalized))
		}
	}
`)

	runGoTest(t, projectDir)

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"openapi",
		"--project", projectDir,
		"--out", "-",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("openapi stdout export failed: code=%d stderr=%s", code, errOut.String())
	}

	var cliStdoutDoc map[string]any
	if err := json.Unmarshal(out.Bytes(), &cliStdoutDoc); err != nil {
		t.Fatalf("decode openapi stdout export failed: %v", err)
	}
	if !reflect.DeepEqual(cliStdoutDoc, doc) {
		t.Fatalf("expected CLI file/stdout export equivalence")
	}
}

func TestRun_StartAppFailsWithoutForceWhenExists(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	first := run([]string{"startapp", "Inventory", "--out", dir}, &out, &errOut)
	if first != 0 {
		t.Fatalf("first startapp should pass: code=%d stderr=%s", first, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	second := run([]string{"startapp", "Inventory", "--out", dir}, &out, &errOut)
	if second == 0 {
		t.Fatal("expected second startapp without --force to fail")
	}
	if !strings.Contains(errOut.String(), "already exists") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func assertOperationJSONRequestBody(t *testing.T, op *openapi.Operation) {
	t.Helper()
	if op == nil || op.RequestBody == nil {
		t.Fatalf("expected operation with request body, got %#v", op)
	}
	if !op.RequestBody.Required {
		t.Fatalf("expected request body to be required, got %#v", op.RequestBody)
	}
	if _, ok := op.RequestBody.Content["application/json"]; !ok {
		t.Fatalf("expected application/json request body content, got %#v", op.RequestBody.Content)
	}
}

func assertOperationJSONResponse(t *testing.T, op *openapi.Operation, status string) {
	t.Helper()
	if op == nil {
		t.Fatal("expected operation, got nil")
	}
	response, ok := op.Responses[status]
	if !ok {
		t.Fatalf("expected %s response, got %#v", status, op.Responses)
	}
	if _, ok := response.Content["application/json"]; !ok {
		t.Fatalf("expected application/json response content, got %#v", response.Content)
	}
}

func assertCollectionEnvelopeResponse(t *testing.T, op *openapi.Operation, status string) {
	t.Helper()
	assertOperationJSONResponse(t, op, status)

	schema := op.Responses[status].Content["application/json"].Schema
	if schema.Type != "object" {
		t.Fatalf("expected collection envelope object schema, got %#v", schema)
	}
	data, ok := schema.Properties["data"]
	if !ok || data.Type != "array" {
		t.Fatalf("expected collection envelope data array, got %#v", schema.Properties)
	}
	count, ok := schema.Properties["count"]
	if !ok || count.Type != "integer" {
		t.Fatalf("expected collection envelope count integer, got %#v", schema.Properties)
	}
	assertRequiredFields(t, schema.Required, "data", "count")
	assertNoLegacyEnvelopeFields(t, schema.Properties)
}

func assertDataEnvelopeResponse(t *testing.T, op *openapi.Operation, status string) {
	t.Helper()
	assertOperationJSONResponse(t, op, status)

	schema := op.Responses[status].Content["application/json"].Schema
	if schema.Type != "object" {
		t.Fatalf("expected data envelope object schema, got %#v", schema)
	}
	if _, ok := schema.Properties["data"]; !ok {
		t.Fatalf("expected data envelope field, got %#v", schema.Properties)
	}
	assertRequiredFields(t, schema.Required, "data")
	assertNoLegacyEnvelopeFields(t, schema.Properties)
}

func assertOperationMetadata(t *testing.T, op *openapi.Operation, operationID string, tag string) {
	t.Helper()
	if op == nil {
		t.Fatal("expected operation, got nil")
	}
	if op.OperationID != operationID {
		t.Fatalf("expected operationId %q, got %q", operationID, op.OperationID)
	}
	if strings.TrimSpace(op.Summary) == "" || strings.TrimSpace(op.Description) == "" {
		t.Fatalf("expected non-empty operation summary/description, got %#v", op)
	}
	if len(op.Tags) != 1 || op.Tags[0] != tag {
		t.Fatalf("expected single tag %q, got %#v", tag, op.Tags)
	}
}

func assertOperationErrorResponse(t *testing.T, op *openapi.Operation, status string) {
	t.Helper()
	if op == nil {
		t.Fatal("expected operation, got nil")
	}
	response, ok := op.Responses[status]
	if !ok {
		t.Fatalf("expected %s error response, got %#v", status, op.Responses)
	}
	content, ok := response.Content["application/json"]
	if !ok {
		t.Fatalf("expected application/json error response content, got %#v", response.Content)
	}
	errorField, ok := content.Schema.Properties["error"]
	if !ok {
		t.Fatalf("expected structured error schema, got %#v", content.Schema.Properties)
	}
	if _, ok := errorField.Properties["code"]; !ok {
		t.Fatalf("expected error code field, got %#v", errorField.Properties)
	}
	if _, ok := errorField.Properties["message"]; !ok {
		t.Fatalf("expected error message field, got %#v", errorField.Properties)
	}
}

func assertSearchQueryParameter(t *testing.T, op *openapi.Operation, description string) {
	t.Helper()
	if op == nil {
		t.Fatal("expected operation, got nil")
	}
	if len(op.Parameters) != 1 {
		t.Fatalf("expected single query parameter, got %#v", op.Parameters)
	}
	param := op.Parameters[0]
	if param.Name != "q" || param.In != "query" || param.Required {
		t.Fatalf("expected optional query parameter q, got %#v", param)
	}
	if param.Schema.Type != "string" {
		t.Fatalf("expected string query schema, got %#v", param.Schema)
	}
	if param.Description != description {
		t.Fatalf("expected query parameter description %q, got %q", description, param.Description)
	}
}

func assertRequiredFields(t *testing.T, required []string, fields ...string) {
	t.Helper()
	got := append([]string(nil), required...)
	sort.Strings(got)
	want := append([]string(nil), fields...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected required fields %v, got %v", want, got)
	}
}

func assertNoLegacyEnvelopeFields(t *testing.T, properties map[string]openapi.Schema) {
	t.Helper()
	for _, legacy := range []string{"items", "item", "resource", "total"} {
		if _, ok := properties[legacy]; ok {
			t.Fatalf("did not expect legacy envelope field %q in %#v", legacy, properties)
		}
	}
}

func assertPathIDOperation(t *testing.T, op *openapi.Operation, operationID string) {
	t.Helper()
	if op == nil {
		t.Fatal("expected operation, got nil")
	}
	if op.OperationID != operationID {
		t.Fatalf("expected operationId %q, got %q", operationID, op.OperationID)
	}
	if len(op.Parameters) != 1 {
		t.Fatalf("expected single path parameter, got %#v", op.Parameters)
	}
	param := op.Parameters[0]
	if param.Name != "id" || param.In != "path" || !param.Required {
		t.Fatalf("expected required path id parameter, got %#v", param)
	}
	if param.Schema.Type != "integer" || param.Schema.Format != "int64" {
		t.Fatalf("expected int64 id parameter schema, got %#v", param.Schema)
	}
}

func assertEmptyResponse(t *testing.T, op *openapi.Operation, status string, description string) {
	t.Helper()
	if op == nil {
		t.Fatal("expected operation, got nil")
	}
	response, ok := op.Responses[status]
	if !ok {
		t.Fatalf("expected %s response, got %#v", status, op.Responses)
	}
	if response.Description != description {
		t.Fatalf("expected %s response description %q, got %#v", status, description, response)
	}
	if response.Content != nil {
		t.Fatalf("expected empty response content for %s, got %#v", status, response.Content)
	}
}

func TestRun_TestCommandDryRun(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"test",
		"--dry-run",
		"--run", "TestRun_MigrateLifecycle",
		"./cmd/nucleus",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("test --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "go test -run TestRun_MigrateLifecycle ./cmd/nucleus") {
		t.Fatalf("unexpected dry-run output: %s", out.String())
	}
}

func TestRun_Seed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	seedsDir := filepath.Join(dir, "seeds")
	if err := os.MkdirAll(seedsDir, 0755); err != nil {
		t.Fatalf("mkdir seeds failed: %v", err)
	}

	writeFile(t, filepath.Join(seedsDir, "001_schema.sql"), "CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT NOT NULL);")
	writeFile(t, filepath.Join(seedsDir, "002_data.sql"), "INSERT INTO books (id, title) VALUES (1, 'Go in Action');")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := runWithInput([]string{"seed", "--config", cfgPath, "--seeds", seedsDir}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("seed failed: code=%d stderr=%s", code, errOut.String())
	}

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	var count int
	if err := dbConn.QueryRow("SELECT count(*) FROM books").Scan(&count); err != nil {
		t.Fatalf("query seeded rows failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 seeded row, got %d", count)
	}
}

func TestRun_SeedProductionRequiresForce(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")
	seedsDir := filepath.Join(dir, "seeds")
	if err := os.MkdirAll(seedsDir, 0755); err != nil {
		t.Fatalf("mkdir seeds failed: %v", err)
	}
	writeFile(t, filepath.Join(seedsDir, "001_seed.sql"), "CREATE TABLE IF NOT EXISTS prod_test (id INTEGER PRIMARY KEY);")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := runWithInput([]string{"seed", "--config", cfgPath, "--seeds", seedsDir}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected seed in production without force to fail; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected seed error: %s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"seed", "--config", cfgPath, "--seeds", seedsDir, "--force"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("seed with --force should pass: code=%d stderr=%s", code, errOut.String())
	}
}

// seedOrbitAdminUsersTable pre-creates the nucleus_admin_users table that the
// orbit module owns (ADR-019). createuser/changepassword now require it to
// pre-exist (they no longer auto-create it), so CLI tests exercising those
// commands seed it — simulating an app where orbit initialised the schema.
func seedOrbitAdminUsersTable(t *testing.T, dbPath string) {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("seed admin schema: open sqlite: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS nucleus_admin_users (
	id VARCHAR(64) PRIMARY KEY,
	username VARCHAR(191) NOT NULL UNIQUE,
	email VARCHAR(191) NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_superuser INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`); err != nil {
		t.Fatalf("seed admin schema: create table: %v", err)
	}
}

func TestRun_CreateUserNoInput(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	seedOrbitAdminUsersTable(t, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"createuser",
		"--config", cfgPath,
		"--no-input",
		"--username", "admin",
		"--email", "admin@example.com",
		"--password", "supersecret123",
		"--superuser=true",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createuser failed: code=%d stderr=%s", code, errOut.String())
	}

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	var (
		username string
		email    string
		super    int
	)
	if err := dbConn.QueryRow("SELECT username, email, is_superuser FROM nucleus_admin_users LIMIT 1").Scan(&username, &email, &super); err != nil {
		t.Fatalf("query admin user failed: %v", err)
	}
	if username != "admin" || email != "admin@example.com" || super != 1 {
		t.Fatalf("unexpected admin user row: username=%s email=%s super=%d", username, email, super)
	}
}

func TestRun_CreateSuperUserAliasNoInput(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	seedOrbitAdminUsersTable(t, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"createsuperuser",
		"--config", cfgPath,
		"--no-input",
		"--username", "admin_alias",
		"--email", "admin_alias@example.com",
		"--password", "supersecret123",
		"--superuser=true",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createsuperuser alias failed: code=%d stderr=%s", code, errOut.String())
	}

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	var (
		username string
		email    string
		super    int
	)
	if err := dbConn.QueryRow("SELECT username, email, is_superuser FROM nucleus_admin_users WHERE username = 'admin_alias' LIMIT 1").Scan(&username, &email, &super); err != nil {
		t.Fatalf("query admin user failed: %v", err)
	}
	if username != "admin_alias" || email != "admin_alias@example.com" || super != 1 {
		t.Fatalf("unexpected admin user row: username=%s email=%s super=%d", username, email, super)
	}
}

func TestRun_ChangePasswordNoInput(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	seedOrbitAdminUsersTable(t, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{
		"createuser",
		"--config", cfgPath,
		"--no-input",
		"--username", "admin",
		"--email", "admin@example.com",
		"--password", "supersecret123",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createuser failed: code=%d stderr=%s", code, errOut.String())
	}

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	var beforeHash string
	if err := dbConn.QueryRow("SELECT password_hash FROM nucleus_admin_users WHERE username = 'admin' LIMIT 1").Scan(&beforeHash); err != nil {
		t.Fatalf("query password hash before changepassword failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"changepassword",
		"--config", cfgPath,
		"--no-input",
		"--password", "newsecret456",
		"admin",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("changepassword failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Password updated: admin") {
		t.Fatalf("unexpected changepassword output: %s", out.String())
	}

	var afterHash string
	if err := dbConn.QueryRow("SELECT password_hash FROM nucleus_admin_users WHERE username = 'admin' LIMIT 1").Scan(&afterHash); err != nil {
		t.Fatalf("query password hash after changepassword failed: %v", err)
	}
	if beforeHash == afterHash {
		t.Fatal("expected password hash to change after changepassword")
	}
	if !auth.CheckPassword("newsecret456", afterHash) {
		t.Fatal("expected updated hash to match new password")
	}
	if auth.CheckPassword("supersecret123", afterHash) {
		t.Fatal("expected old password to stop matching updated hash")
	}
}

func TestRun_CreateCacheTable(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"createcachetable", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createcachetable failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Cache table ready: nucleus_cache_entries") {
		t.Fatalf("unexpected createcachetable output: %s", out.String())
	}
	if !tableExists(t, dbPath, "nucleus_cache_entries") {
		t.Fatal("expected nucleus_cache_entries table to exist")
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"createcachetable", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createcachetable should be idempotent: code=%d stderr=%s", code, errOut.String())
	}
}

func TestRun_CreateCacheTable_UsesPrimaryAlias(t *testing.T) {
	dir := t.TempDir()
	defaultDBPath := filepath.Join(dir, "default.db")
	primaryDBPath := filepath.Join(dir, "primary.db")
	cfgPath := writeCLIConfigWithAliases(t, dir, "primary", map[string]string{
		"default": primarySQLiteURL(defaultDBPath),
		"primary": primarySQLiteURL(primaryDBPath),
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"createcachetable", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createcachetable failed: code=%d stderr=%s", code, errOut.String())
	}
	if !tableExists(t, primaryDBPath, "nucleus_cache_entries") {
		t.Fatal("expected nucleus_cache_entries table in primary alias database")
	}
	if tableExists(t, defaultDBPath, "nucleus_cache_entries") {
		t.Fatal("expected default alias database to remain untouched")
	}
}

func TestRun_ClearSessions(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	_, _ = dbConn.Exec("CREATE TABLE nucleus_sessions (id TEXT PRIMARY KEY, payload TEXT NOT NULL, expires_at TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO nucleus_sessions (id, payload, expires_at) VALUES ('old', '{}', datetime('now','-1 day'));")
	_, _ = dbConn.Exec("INSERT INTO nucleus_sessions (id, payload, expires_at) VALUES ('new', '{}', datetime('now','+1 day'));")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"clearsessions", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("clearsessions failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Sessions cleared (expired)") {
		t.Fatalf("unexpected clearsessions output: %s", out.String())
	}

	var count int
	if err := dbConn.QueryRow("SELECT count(*) FROM nucleus_sessions").Scan(&count); err != nil {
		t.Fatalf("count sessions failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 non-expired session remaining, got %d", count)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"clearsessions", "--config", cfgPath, "--all"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("clearsessions --all failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Sessions cleared (all)") {
		t.Fatalf("unexpected clearsessions --all output: %s", out.String())
	}

	if err := dbConn.QueryRow("SELECT count(*) FROM nucleus_sessions").Scan(&count); err != nil {
		t.Fatalf("count sessions after --all failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected all sessions removed, got %d", count)
	}
}

func TestRun_MakeMessagesAndCompileMessages(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	localesPath := filepath.Join(dir, "locales")
	inputPath := filepath.Join(dir, "src")
	writeFile(t, cfgPath, "default_locale: en\nlocales_path: "+localesPath+"\n")

	if err := os.MkdirAll(inputPath, 0755); err != nil {
		t.Fatalf("mkdir input path failed: %v", err)
	}
	writeFile(t, filepath.Join(inputPath, "handler.go"), `package sample
func f() {
	_ = T("Welcome")
	_ = _("Goodbye")
}`)
	writeFile(t, filepath.Join(inputPath, "view.html"), `{{ trans "Welcome" }}`)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"makemessages",
		"--config", cfgPath,
		"--locale", "es",
		"--domain", "messages",
		"--input", inputPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("makemessages failed: code=%d stderr=%s", code, errOut.String())
	}

	poPath := filepath.Join(localesPath, "es", "LC_MESSAGES", "messages.po")
	poRaw, err := os.ReadFile(poPath)
	if err != nil {
		t.Fatalf("read generated po file failed: %v", err)
	}
	poText := string(poRaw)
	if !strings.Contains(poText, `msgid "Welcome"`) || !strings.Contains(poText, `msgid "Goodbye"`) {
		t.Fatalf("generated po missing expected message ids: %s", poText)
	}

	updatedPO := strings.Replace(poText, "msgid \"Welcome\"\nmsgstr \"\"", "msgid \"Welcome\"\nmsgstr \"Bienvenido\"", 1)
	if updatedPO == poText {
		t.Fatalf("unable to update Welcome translation in PO file: %s", poText)
	}
	writeFile(t, poPath, updatedPO)

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"compilemessages",
		"--config", cfgPath,
		"--locale", "es",
		"--domain", "messages",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("compilemessages failed: code=%d stderr=%s", code, errOut.String())
	}

	jsonPath := filepath.Join(localesPath, "es", "LC_MESSAGES", "messages.json")
	jsonRaw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read compiled catalog failed: %v", err)
	}

	var compiled struct {
		Entries map[string]string `json:"entries"`
	}
	if err := json.Unmarshal(jsonRaw, &compiled); err != nil {
		t.Fatalf("decode compiled catalog failed: %v", err)
	}
	if compiled.Entries["Welcome"] != "Bienvenido" {
		t.Fatalf("unexpected compiled Welcome translation: %q", compiled.Entries["Welcome"])
	}
	if compiled.Entries["Goodbye"] != "Goodbye" {
		t.Fatalf("expected fallback for untranslated message, got %q", compiled.Entries["Goodbye"])
	}
}

func TestRun_SendTestEmailDryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, "mail_driver: sendgrid\nmail_from: noreply@example.com\nsendgrid_endpoint: https://api.sendgrid.test/v3/mail/send\n")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"sendtestemail",
		"--config", cfgPath,
		"--to", "dev@example.com",
		"--subject", "Hello from Nucleus",
		"--dry-run",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("sendtestemail --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "DRY-RUN\tSENDTESTEMAIL") {
		t.Fatalf("unexpected dry-run output: %s", output)
	}
	if !strings.Contains(output, "driver=sendgrid") {
		t.Fatalf("dry-run output missing driver: %s", output)
	}
	if !strings.Contains(output, "dev@example.com") || !strings.Contains(output, "Hello from Nucleus") {
		t.Fatalf("dry-run output missing expected fields: %s", output)
	}
}

func TestRun_SendTestEmailDryRunDriverOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, "mail_driver: smtp\nmail_from: noreply@example.com\nsendgrid_endpoint: https://api.sendgrid.test/v3/mail/send\n")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"sendtestemail",
		"--config", cfgPath,
		"--driver", "sendgrid",
		"--to", "dev@example.com",
		"--dry-run",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("sendtestemail --dry-run with driver override failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "driver=sendgrid") {
		t.Fatalf("expected sendgrid override driver in output: %s", output)
	}
	if strings.Contains(output, "driver=smtp") {
		t.Fatalf("expected smtp driver to be overridden: %s", output)
	}
}

func TestRun_MailProviders(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, "mail_driver: sendgrid\n")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"mailproviders",
		"--config", cfgPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("mailproviders failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "Active driver: sendgrid") {
		t.Fatalf("expected active driver output, got: %s", output)
	}
	if !strings.Contains(output, "sendgrid") {
		t.Fatalf("expected sendgrid in output, got: %s", output)
	}
}

func TestRun_CollectStaticAndFindStatic(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, "static_root: collected_static\n")

	if err := os.MkdirAll(filepath.Join(dir, "internal", "web", "static", "js"), 0o755); err != nil {
		t.Fatalf("mkdir static source failed: %v", err)
	}
	writeFile(t, filepath.Join(dir, "internal", "web", "static", "app.css"), "body{}")
	writeFile(t, filepath.Join(dir, "internal", "web", "static", "js", "app.js"), "console.log('ok')")

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(prevWD)
	}()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"collectstatic",
		"--config", cfgPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("collectstatic failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Collected static files") {
		t.Fatalf("unexpected collectstatic output: %s", out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "collected_static", "app.css")); err != nil {
		t.Fatalf("expected collected app.css: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "collected_static", "js", "app.js")); err != nil {
		t.Fatalf("expected collected app.js: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"findstatic",
		"--config", cfgPath,
		"js/app.js",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("findstatic failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), filepath.Join("internal", "web", "static", "js", "app.js")) {
		t.Fatalf("unexpected findstatic output: %s", out.String())
	}
}

func TestRun_RemoveStaleContentTypes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"); err != nil {
		t.Fatalf("create users failed: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE nucleus_content_types (id INTEGER PRIMARY KEY, model TEXT NOT NULL);"); err != nil {
		t.Fatalf("create content types table failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO nucleus_content_types(model) VALUES ('users'), ('ghost_model');"); err != nil {
		t.Fatalf("seed content types failed: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"remove_stale_contenttypes",
		"--config", cfgPath,
		"--dry-run",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("remove_stale_contenttypes --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ghost_model") {
		t.Fatalf("expected stale model in dry-run output, got: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"remove_stale_contenttypes",
		"--config", cfgPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("remove_stale_contenttypes failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Removed stale content types") {
		t.Fatalf("unexpected command output: %s", out.String())
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM nucleus_content_types WHERE model = 'ghost_model'").Scan(&count); err != nil {
		t.Fatalf("count stale model failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected stale content type removed, count=%d", count)
	}
}

func TestRun_AdminMaintenanceCommands_JSONOutputContract(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	seedOrbitAdminUsersTable(t, dbPath)

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()

	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{
		"--output", "json",
		"createuser",
		"--config", cfgPath,
		"--no-input",
		"--username", "admin_json",
		"--email", "admin_json@example.com",
		"--password", "supersecret123",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createuser json failed: code=%d stderr=%s", code, errOut.String())
	}
	createPayload := decodeCommandStatusJSON(t, out.Bytes())
	if createPayload["command"] != "createuser" || createPayload["status"] != "ok" {
		t.Fatalf("unexpected createuser json payload: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{
		"--output", "json",
		"changepassword",
		"--config", cfgPath,
		"--no-input",
		"--password", "newsecret456",
		"admin_json",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("changepassword json failed: code=%d stderr=%s", code, errOut.String())
	}
	passwordPayload := decodeCommandStatusJSON(t, out.Bytes())
	if passwordPayload["command"] != "changepassword" || passwordPayload["status"] != "ok" {
		t.Fatalf("unexpected changepassword json payload: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"--output", "json", "createcachetable", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("createcachetable json failed: code=%d stderr=%s", code, errOut.String())
	}
	cachePayload := decodeCommandStatusJSON(t, out.Bytes())
	if cachePayload["command"] != "createcachetable" || cachePayload["status"] != "ok" {
		t.Fatalf("unexpected createcachetable json payload: %s", out.String())
	}

	if _, err := dbConn.Exec("CREATE TABLE nucleus_sessions (id TEXT PRIMARY KEY, payload TEXT NOT NULL, expires_at TEXT NOT NULL);"); err != nil {
		t.Fatalf("create sessions table failed: %v", err)
	}
	if _, err := dbConn.Exec("INSERT INTO nucleus_sessions (id, payload, expires_at) VALUES ('old', '{}', datetime('now','-1 day'));"); err != nil {
		t.Fatalf("seed expired session failed: %v", err)
	}
	if _, err := dbConn.Exec("INSERT INTO nucleus_sessions (id, payload, expires_at) VALUES ('new', '{}', datetime('now','+1 day'));"); err != nil {
		t.Fatalf("seed active session failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"--output", "json", "clearsessions", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("clearsessions json failed: code=%d stderr=%s", code, errOut.String())
	}
	sessionsPayload := decodeCommandStatusJSON(t, out.Bytes())
	if sessionsPayload["command"] != "clearsessions" || sessionsPayload["status"] != "ok" {
		t.Fatalf("unexpected clearsessions json payload: %s", out.String())
	}

	if _, err := dbConn.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);"); err != nil {
		t.Fatalf("create users table failed: %v", err)
	}
	if _, err := dbConn.Exec("CREATE TABLE nucleus_content_types (id INTEGER PRIMARY KEY, model TEXT NOT NULL);"); err != nil {
		t.Fatalf("create content types table failed: %v", err)
	}
	if _, err := dbConn.Exec("INSERT INTO nucleus_content_types(model) VALUES ('users'), ('ghost_model');"); err != nil {
		t.Fatalf("seed content types failed: %v", err)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"--output", "json", "remove_stale_contenttypes", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("remove_stale_contenttypes json failed: code=%d stderr=%s", code, errOut.String())
	}
	contentTypesPayload := decodeCommandStatusJSON(t, out.Bytes())
	if contentTypesPayload["command"] != "remove_stale_contenttypes" || contentTypesPayload["status"] != "ok" {
		t.Fatalf("unexpected remove_stale_contenttypes json payload: %s", out.String())
	}
}

func TestRun_RemoveStaleContentTypesProductionGuardrail(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("CREATE TABLE nucleus_content_types (id INTEGER PRIMARY KEY, model TEXT NOT NULL);"); err != nil {
		t.Fatalf("create content types table failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO nucleus_content_types(model) VALUES ('ghost_model');"); err != nil {
		t.Fatalf("seed content types failed: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := runWithInput([]string{
		"remove_stale_contenttypes",
		"--config", cfgPath,
	}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected production guardrail failure without --force/--yes")
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected guardrail error: %s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{
		"remove_stale_contenttypes",
		"--config", cfgPath,
		"--force",
	}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("expected remove_stale_contenttypes with --force to pass: code=%d stderr=%s", code, errOut.String())
	}
}

func TestRun_SendTestEmailRejectsNoopWithoutDryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, "mail_driver: noop\nmail_from: noreply@example.com\n")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"sendtestemail",
		"--config", cfgPath,
		"--to", "dev@example.com",
	}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected sendtestemail with noop driver to fail; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "mail_driver is noop") {
		t.Fatalf("unexpected sendtestemail error: %s", errOut.String())
	}
}

func TestRun_TestServerDryRun(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)
	fixturePath := filepath.Join(dir, "fixtures.json")

	writeFile(t, fixturePath, `{
  "tables": [
    {
      "name": "users",
      "rows": [
        {"id": 1, "name": "alice"}
      ]
    }
  ]
}`)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"testserver",
		"--config", cfgPath,
		"--dry-run",
		fixturePath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("testserver --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "DRY-RUN\tLOAD\tusers\trows=1") {
		t.Fatalf("unexpected testserver dry-run output: %s", out.String())
	}
	if !strings.Contains(out.String(), "Dry-run completed; server startup skipped") {
		t.Fatalf("expected dry-run completion message, got: %s", out.String())
	}
}

func TestRun_ShellCommand(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"shell", "--config", cfgPath, "-c", "SELECT 1 AS n;"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("shell -c failed: code=%d stderr=%s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, "n") || !strings.Contains(output, "1") {
		t.Fatalf("unexpected shell output: %s", output)
	}
}

func TestRun_ShellFromStdin(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := runWithInput(
		[]string{"shell", "--config", cfgPath},
		strings.NewReader("SELECT 2 AS n;"),
		&out,
		&errOut,
	)
	if code != 0 {
		t.Fatalf("shell stdin failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "2") {
		t.Fatalf("unexpected shell stdin output: %s", out.String())
	}
}

func TestRun_ShellSandboxAllowsSelect(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"shell", "--config", cfgPath, "--sandbox", "-c", "SELECT 3 AS n;"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("shell sandbox select failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "3") {
		t.Fatalf("unexpected shell sandbox output: %s", out.String())
	}
}

func TestRun_ShellSandboxBlocksWrite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"shell", "--config", cfgPath, "--sandbox", "-c", "CREATE TABLE sandbox_test (id INTEGER PRIMARY KEY);"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected shell sandbox write to fail; stdout=%s", out.String())
	}
	if !strings.Contains(errOut.String(), "sandbox mode only allows read-only") {
		t.Fatalf("unexpected shell sandbox error: %s", errOut.String())
	}
}

func TestRun_MigrateResetProductionRequiresForce(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := runWithInput([]string{"migrate", "--config", cfgPath, "reset"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected migrate reset in production without force to fail")
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected error: %s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"migrate", "--config", cfgPath, "--force", "reset"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate reset with --force failed: code=%d stderr=%s", code, errOut.String())
	}
}

func TestRun_MigrateDownProductionGuardrailNonInteractive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	writeFile(t, filepath.Join(migDir, "000001_create_items.up.sql"), "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
	writeFile(t, filepath.Join(migDir, "000001_create_items.down.sql"), "DROP TABLE IF EXISTS items;")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "up"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate up failed: code=%d stderr=%s", code, errOut.String())
	}
	if !tableExists(t, dbPath, "items") {
		t.Fatal("items table should exist after migrate up")
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "down"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected migrate down in production without force/yes to fail")
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected migrate down guardrail error: %s", errOut.String())
	}
	if !tableExists(t, dbPath, "items") {
		t.Fatal("items table should remain after blocked migrate down")
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "--yes", "down"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate down with --yes failed: code=%d stderr=%s", code, errOut.String())
	}
	if tableExists(t, dbPath, "items") {
		t.Fatal("items table should be removed after confirmed migrate down")
	}
}

func TestRun_MigrateStepsRollbackProductionGuardrailNonInteractive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	writeFile(t, filepath.Join(migDir, "000001_create_users.up.sql"), "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);")
	writeFile(t, filepath.Join(migDir, "000001_create_users.down.sql"), "DROP TABLE IF EXISTS users;")
	writeFile(t, filepath.Join(migDir, "000002_create_posts.up.sql"), "CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL);")
	writeFile(t, filepath.Join(migDir, "000002_create_posts.down.sql"), "DROP TABLE IF EXISTS posts;")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "up"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate up failed: code=%d stderr=%s", code, errOut.String())
	}
	if !tableExists(t, dbPath, "posts") {
		t.Fatal("posts table should exist after migrate up")
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "steps", "-1"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected migrate steps -1 in production without force/yes to fail")
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected migrate steps guardrail error: %s", errOut.String())
	}
	if !tableExists(t, dbPath, "posts") {
		t.Fatal("posts table should remain after blocked migrate steps rollback")
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "--yes", "steps", "-1"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate steps -1 with --yes failed: code=%d stderr=%s", code, errOut.String())
	}
	if tableExists(t, dbPath, "posts") {
		t.Fatal("posts table should be removed after confirmed rollback step")
	}
	if !tableExists(t, dbPath, "users") {
		t.Fatal("users table should remain after rolling back one step")
	}
}

func TestRun_MigrateRefreshProductionGuardrailNonInteractive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfigWithEnv(t, dir, dbPath, "production")
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0755); err != nil {
		t.Fatalf("mkdir migrations failed: %v", err)
	}

	writeFile(t, filepath.Join(migDir, "000001_create_users.up.sql"), "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")
	writeFile(t, filepath.Join(migDir, "000001_create_users.down.sql"), "DROP TABLE IF EXISTS users;")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "up"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate up failed: code=%d stderr=%s", code, errOut.String())
	}

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer dbConn.Close()
	_, _ = dbConn.Exec("INSERT INTO users (id, name) VALUES (1, 'legacy');")

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "refresh"}, strings.NewReader(""), &out, &errOut)
	if code == 0 {
		t.Fatalf("expected migrate refresh in production without force/yes to fail")
	}
	if !strings.Contains(errOut.String(), "requires --force or --yes") {
		t.Fatalf("unexpected migrate refresh guardrail error: %s", errOut.String())
	}

	var beforeCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM users").Scan(&beforeCount); err != nil {
		t.Fatalf("count users after blocked refresh failed: %v", err)
	}
	if beforeCount != 1 {
		t.Fatalf("expected users data to remain after blocked refresh, got %d rows", beforeCount)
	}

	out.Reset()
	errOut.Reset()
	code = runWithInput([]string{"migrate", "--config", cfgPath, "--migrations", migDir, "--yes", "refresh"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("migrate refresh with --yes failed: code=%d stderr=%s", code, errOut.String())
	}
	if !tableExists(t, dbPath, "users") {
		t.Fatal("users table should exist after confirmed migrate refresh")
	}

	var afterCount int
	if err := dbConn.QueryRow("SELECT count(*) FROM users").Scan(&afterCount); err != nil {
		t.Fatalf("count users after confirmed refresh failed: %v", err)
	}
	if afterCount != 0 {
		t.Fatalf("expected users data reset after confirmed refresh, got %d rows", afterCount)
	}
}

func TestRun_HealthJSON(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"health", "--config", cfgPath, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("health failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "\"status\": \"ok\"") {
		t.Fatalf("unexpected health output: %s", out.String())
	}
}

func TestRun_HealthJSON_ReportsPrimaryAlias(t *testing.T) {
	dir := t.TempDir()
	defaultDBPath := filepath.Join(dir, "default.db")
	primaryDBPath := filepath.Join(dir, "primary.db")
	cfgPath := writeCLIConfigWithAliases(t, dir, "primary", map[string]string{
		"default": primarySQLiteURL(defaultDBPath),
		"primary": primarySQLiteURL(primaryDBPath),
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"health", "--config", cfgPath, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("health failed: code=%d stderr=%s", code, errOut.String())
	}
	body := out.String()
	if !strings.Contains(body, "alias=primary") || !strings.Contains(body, "flavor=sqlite") {
		t.Fatalf("unexpected health details for primary alias: %s", body)
	}
}

func TestRun_GlobalOutputJSON_Health(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"--output", "json", "health", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("health with global json output failed: code=%d stderr=%s", code, errOut.String())
	}
	body := out.String()
	if !strings.Contains(body, "\"status\": \"ok\"") || !strings.Contains(body, "\"components\"") {
		t.Fatalf("unexpected global json health output: %s", body)
	}
}

func TestRun_GlobalOutputPretty_Health(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"--output", "pretty", "--no-symbols", "health", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("health with global pretty output failed: code=%d stderr=%s", code, errOut.String())
	}
	body := out.String()
	if !strings.Contains(body, "Overall: ") || !strings.Contains(body, "database") {
		t.Fatalf("unexpected global pretty health output: %s", body)
	}
}

func TestRun_GlobalColorAlways_Health(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"--output", "pretty", "--color", "always", "health", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("health with forced color output failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("expected ANSI colors in output, got: %q", out.String())
	}
}

func TestRun_GlobalOutputInvalidValue(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"--output", "fancy", "health"}, &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit for invalid global --output value")
	}
	if !strings.Contains(errOut.String(), "invalid --output value") {
		t.Fatalf("unexpected stderr: %s", errOut.String())
	}
}

func TestRun_GlobalJSONShorthand(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"--json", "routes", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("routes with global --json failed: code=%d stderr=%s", code, errOut.String())
	}
	body := out.String()
	if !strings.Contains(body, "\"method\"") || !strings.Contains(body, "\"pattern\"") {
		t.Fatalf("unexpected routes json output: %s", body)
	}
}

func TestRun_InspectDB_UsesPrimaryAlias(t *testing.T) {
	dir := t.TempDir()
	defaultDBPath := filepath.Join(dir, "default.db")
	primaryDBPath := filepath.Join(dir, "primary.db")
	cfgPath := writeCLIConfigWithAliases(t, dir, "primary", map[string]string{
		"default": primarySQLiteURL(defaultDBPath),
		"primary": primarySQLiteURL(primaryDBPath),
	})

	primaryConn, err := sql.Open("sqlite", primaryDBPath)
	if err != nil {
		t.Fatalf("open primary sqlite failed: %v", err)
	}
	defer primaryConn.Close()
	if _, err := primaryConn.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL)`); err != nil {
		t.Fatalf("create customers table in primary failed: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"inspectdb", "--config", cfgPath, "--tables", "customers"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("inspectdb failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "type Customer struct") {
		t.Fatalf("inspectdb output does not include Customer struct: %s", out.String())
	}
}

func TestRun_DiffSettings(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, fmt.Sprintf(
		"database_default: default\n"+
			"databases:\n"+
			"  default:\n"+
			"    url: sqlite://%s\n"+
			"port: 9090\n"+
			"log_format: text\n"+
			"debug: true\n",
		dbPath,
	))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"diffsettings", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("diffsettings failed: code=%d stderr=%s", code, errOut.String())
	}

	result := out.String()
	if !strings.Contains(result, "port") || !strings.Contains(result, "9090") {
		t.Fatalf("diffsettings output missing port diff: %s", result)
	}
	if !strings.Contains(result, "log_format") || !strings.Contains(result, "text") {
		t.Fatalf("diffsettings output missing log_format diff: %s", result)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"diffsettings", "--config", cfgPath, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("diffsettings --json failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"changed"`) {
		t.Fatalf("diffsettings --json output missing changed section: %s", out.String())
	}
}

func TestRun_CheckDeploy(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, fmt.Sprintf(
		"database_default: default\n"+
			"databases:\n"+
			"  default:\n"+
			"    url: sqlite://%s\n"+
			"env: production\n"+
			"debug: true\n"+
			"log_format: text\n"+
			"rate_limit_requests: 0\n",
		dbPath,
	))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"check", "--config", cfgPath, "--deploy", "--json"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected check --deploy with weak settings to fail; output=%s", out.String())
	}
	if !strings.Contains(out.String(), "deploy.jwt_secret") && !strings.Contains(out.String(), "deploy.debug") {
		t.Fatalf("expected deploy check failures in output, got: %s", out.String())
	}

	secureCfgPath := filepath.Join(dir, "nucleus_secure.yaml")
	writeFile(t, secureCfgPath, fmt.Sprintf(
		"database_default: default\n"+
			"databases:\n"+
			"  default:\n"+
			"    url: sqlite://%s\n"+
			"env: production\n"+
			"debug: false\n"+
			"log_format: json\n"+
			"jwt_secret: 12345678901234567890123456789012\n"+
			"rate_limit_requests: 100\n"+
			"session_store: sql\n"+
			"session_table: nucleus_sessions\n"+
			"session_cookie_secure: true\n"+
			"session_cookie_samesite: strict\n"+
			"storage_driver: local\n"+
			"mail_driver: smtp\n"+
			"mail_from: noreply@example.com\n"+
			"smtp_host: smtp.example.com\n"+
			"smtp_port: 587\n",
		filepath.Join(dir, "secure.db"),
	))

	out.Reset()
	errOut.Reset()
	code = run([]string{"check", "--config", secureCfgPath, "--deploy", "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected secure check --deploy to pass: code=%d stderr=%s output=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), `"status": "ok"`) {
		t.Fatalf("expected ok deploy report, got: %s", out.String())
	}
}

func TestRun_CheckDeployWarnsOnNoopMailDriver(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, fmt.Sprintf(
		"database_default: default\n"+
			"databases:\n"+
			"  default:\n"+
			"    url: sqlite://%s\n"+
			"env: production\n"+
			"debug: false\n"+
			"log_format: json\n"+
			"jwt_secret: 12345678901234567890123456789012\n"+
			"rate_limit_requests: 100\n"+
			"storage_driver: local\n",
		dbPath,
	))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"check", "--config", cfgPath, "--deploy", "--json"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected deploy check with noop mail driver to fail; output=%s", out.String())
	}
	if !strings.Contains(out.String(), "deploy.mail_driver") {
		t.Fatalf("expected deploy.mail_driver finding, got: %s", out.String())
	}
}

func TestRun_CheckDeployFlagsSessionHardeningGaps(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, fmt.Sprintf(
		"database_default: default\n"+
			"databases:\n"+
			"  default:\n"+
			"    url: sqlite://%s\n"+
			"env: production\n"+
			"debug: false\n"+
			"log_format: json\n"+
			"jwt_secret: 12345678901234567890123456789012\n"+
			"rate_limit_requests: 100\n"+
			"storage_driver: local\n"+
			"mail_driver: smtp\n"+
			"mail_from: noreply@example.com\n"+
			"smtp_host: smtp.example.com\n"+
			"smtp_port: 587\n"+
			"session_store: memory\n"+
			"session_cookie_secure: false\n",
		dbPath,
	))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"check", "--config", cfgPath, "--deploy", "--json"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected deploy check with weak session hardening to fail; output=%s", out.String())
	}
	if !strings.Contains(out.String(), "deploy.session_store") && !strings.Contains(out.String(), "deploy.session_cookie_secure") {
		t.Fatalf("expected deploy session findings, got: %s", out.String())
	}
}

func TestRun_CheckDeployFlagsRedisSessionStoreWithoutRedisURL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := filepath.Join(dir, "nucleus.yml")
	writeFile(t, cfgPath, fmt.Sprintf(
		"database_default: default\n"+
			"databases:\n"+
			"  default:\n"+
			"    url: sqlite://%s\n"+
			"env: production\n"+
			"debug: false\n"+
			"log_format: json\n"+
			"jwt_secret: 12345678901234567890123456789012\n"+
			"rate_limit_requests: 100\n"+
			"storage_driver: local\n"+
			"mail_driver: smtp\n"+
			"mail_from: noreply@example.com\n"+
			"smtp_host: smtp.example.com\n"+
			"smtp_port: 587\n"+
			"session_store: redis\n"+
			"session_cookie_secure: true\n"+
			"session_cookie_samesite: strict\n",
		dbPath,
	))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"check", "--config", cfgPath, "--deploy", "--json"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected deploy check with redis session store and no redis URL to fail; output=%s", out.String())
	}
	if !strings.Contains(out.String(), "deploy.session_redis_url") {
		t.Fatalf("expected deploy.session_redis_url finding, got: %s", out.String())
	}
}

func TestRun_Routes(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"routes", "--config", cfgPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("routes failed: code=%d stderr=%s", code, errOut.String())
	}
	// The admin panel was extracted to the orbit module, so /admin is no longer
	// a framework route. The framework-owned /healthz probe always registers.
	if !strings.Contains(out.String(), "/healthz") {
		t.Fatalf("expected routes output to include /healthz, got: %s", out.String())
	}
}

func TestRun_ExternalCommandPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "nucleus-hello")
	plugin := "#!/bin/sh\necho plugin:$1\n"
	writeFile(t, pluginPath, plugin)
	if err := os.Chmod(pluginPath, 0755); err != nil {
		t.Fatalf("chmod plugin failed: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"hello", "world"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("external plugin command failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "plugin:world") {
		t.Fatalf("unexpected plugin output: %s", out.String())
	}
}

func writeCLIConfig(t *testing.T, dir, dbPath string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf("database_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\n", dbPath)
	writeFile(t, cfgPath, cfg)
	return cfgPath
}

func writeCLIConfigWithEnv(t *testing.T, dir, dbPath, env string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "nucleus.yml")
	cfg := fmt.Sprintf("database_default: default\ndatabases:\n  default:\n    url: sqlite://%s\nlog_level: error\nlog_format: text\nenv: %s\n", dbPath, env)
	writeFile(t, cfgPath, cfg)
	return cfgPath
}

func writeCLIConfigWithAliases(t *testing.T, dir, defaultAlias string, aliases map[string]string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "nucleus.yml")

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("database_default: %s\n", defaultAlias))
	builder.WriteString("databases:\n")
	keys := make([]string, 0, len(aliases))
	for alias := range aliases {
		keys = append(keys, alias)
	}
	sort.Strings(keys)
	for _, alias := range keys {
		builder.WriteString(fmt.Sprintf("  %s:\n", alias))
		builder.WriteString(fmt.Sprintf("    url: %s\n", aliases[alias]))
	}
	builder.WriteString("log_level: error\n")
	builder.WriteString("log_format: text\n")

	writeFile(t, cfgPath, builder.String())
	return cfgPath
}

func primarySQLiteURL(path string) string {
	return "sqlite://" + path
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write file %s failed: %v", path, err)
	}
}

func tableExists(t *testing.T, dbPath, table string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	defer db.Close()

	var cnt int
	row := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table)
	if err := row.Scan(&cnt); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	return cnt > 0
}

func decodeCommandStatusJSON(t *testing.T, raw []byte) map[string]interface{} {
	t.Helper()

	payload := make(map[string]interface{})
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode command status json failed: %v raw=%s", err, string(raw))
	}
	return payload
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve repo root: runtime.Caller failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func runGoTest(t *testing.T, dir string) {
	t.Helper()

	runGoMod(t, dir, "test", "./...")
}

func runGoMod(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	// CI runners have GOPROXY=https://proxy.golang.org,direct. When the
	// proxy lacks an unreleased version of the framework, Go falls back
	// to a direct git clone of github.com/jcsvwinston/nucleus, which
	// fails on hosted runners because there is no credential helper.
	// Tests use a `replace` directive pointing at the local repo, so
	// they do not need network access at all — declare nucleus as
	// GOPRIVATE to skip the sumdb / proxy round trip and keep
	// GOFLAGS=-mod=mod so the local source path is honoured.
	cmd.Env = append(os.Environ(),
		"GOPRIVATE=github.com/jcsvwinston/nucleus",
		"GOFLAGS=-mod=mod",
		"GONOSUMCHECK=*",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, string(output))
	}
}

func wireGeneratedModuleToRepo(t *testing.T, dir string) {
	t.Helper()

	goModPath := filepath.Join(dir, "go.mod")
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read go.mod failed: %v", err)
	}

	body := string(raw)
	if strings.Contains(body, "replace github.com/jcsvwinston/nucleus =>") {
		return
	}

	body = strings.TrimSpace(body) + fmt.Sprintf(`

require github.com/jcsvwinston/nucleus v0.0.0

replace github.com/jcsvwinston/nucleus => %s
`, filepath.ToSlash(repoRoot(t)))
	writeFile(t, goModPath, body+"\n")
}
