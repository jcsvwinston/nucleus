package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	_ "github.com/glebarez/sqlite"
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
	_, _ = dbConn.Exec("CREATE TABLE goframe_schema_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('alice'), ('bob');")
	_, _ = dbConn.Exec("INSERT INTO posts (title) VALUES ('hello');")
	_, _ = dbConn.Exec("INSERT INTO goframe_schema_migrations (id, applied_at) VALUES ('20260401120000_init', '2026-04-01T12:00:00Z');")

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
	if strings.Contains(sqlText, "goframe_schema_migrations") {
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
	if err := dbConn.QueryRow("SELECT count(*) FROM goframe_schema_migrations").Scan(&migrationsCount); err != nil {
		t.Fatalf("count migrations failed: %v", err)
	}
	if migrationsCount != 1 {
		t.Fatalf("expected schema migrations to be preserved, got %d rows", migrationsCount)
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
	_, _ = dbConn.Exec("CREATE TABLE goframe_schema_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL);")
	_, _ = dbConn.Exec("INSERT INTO users (name) VALUES ('alice'), ('bob');")
	_, _ = dbConn.Exec("INSERT INTO posts (title) VALUES ('hello');")
	_, _ = dbConn.Exec("INSERT INTO goframe_schema_migrations (id, applied_at) VALUES ('20260401120000_init', '2026-04-01T12:00:00Z');")

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
	if strings.Contains(fixtureText, "goframe_schema_migrations") {
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
	if err := dbConn.QueryRow("SELECT count(*) FROM goframe_schema_migrations").Scan(&migrationsCount); err != nil {
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
	_, _ = dbConn.Exec("CREATE TABLE goframe_schema_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL);")

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
	if strings.Contains(got, "goframe_schema_migrations") {
		t.Fatalf("inspectdb should skip migration table: %s", got)
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
	modelPath := filepath.Join(dir, "models", "user_profile.go")
	if _, err := os.Stat(modelPath); err != nil {
		t.Fatalf("expected model scaffold file %s: %v", modelPath, err)
	}

	out.Reset()
	errOut.Reset()
	code = run([]string{"generate", "--out", dir, "handler", "UserProfile"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate handler failed: code=%d stderr=%s", code, errOut.String())
	}
	handlerPath := filepath.Join(dir, "handlers", "user_profile_handler.go")
	if _, err := os.Stat(handlerPath); err != nil {
		t.Fatalf("expected handler scaffold file %s: %v", handlerPath, err)
	}
}

func TestRun_GenerateResource(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{"generate", "--out", dir, "resource", "Category"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("generate resource failed: code=%d stderr=%s", code, errOut.String())
	}

	modelPath := filepath.Join(dir, "models", "category.go")
	handlerPath := filepath.Join(dir, "handlers", "category_handler.go")
	testPath := filepath.Join(dir, "handlers", "category_handler_test.go")

	for _, p := range []string{modelPath, handlerPath, testPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected generated file %s: %v", p, err)
		}
	}

	migrationsDir := filepath.Join(dir, "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 migration files, got %d", len(entries))
	}
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
	expectedFiles := []string{
		filepath.Join(projectDir, "go.mod"),
		filepath.Join(projectDir, "goframe.yaml"),
		filepath.Join(projectDir, "README.md"),
		filepath.Join(projectDir, ".gitignore"),
		filepath.Join(projectDir, "cmd", "server", "main.go"),
		filepath.Join(projectDir, "cmd", "worker", "main.go"),
		filepath.Join(projectDir, "internal", "models", "article.go"),
		filepath.Join(projectDir, "internal", "controllers", "article_api.go"),
		filepath.Join(projectDir, "internal", "controllers", "home_page.go"),
		filepath.Join(projectDir, "internal", "tasks", "article_events.go"),
		filepath.Join(projectDir, "internal", "web", "templates", "home.html"),
		filepath.Join(projectDir, "migrations", "000001_create_articles.up.sql"),
		filepath.Join(projectDir, "migrations", "000001_create_articles.down.sql"),
		filepath.Join(projectDir, "seeds", "001_articles.sql"),
	}
	for _, p := range expectedFiles {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected generated file %s: %v", p, err)
		}
	}

	goModRaw, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod failed: %v", err)
	}
	goMod := string(goModRaw)
	if !strings.Contains(goMod, "module example.com/blogapp") {
		t.Fatalf("go.mod missing module path: %s", goMod)
	}

	cfgRaw, err := os.ReadFile(filepath.Join(projectDir, "goframe.yaml"))
	if err != nil {
		t.Fatalf("read goframe.yaml failed: %v", err)
	}
	cfg := string(cfgRaw)
	if !strings.Contains(cfg, "port: 9095") {
		t.Fatalf("goframe.yaml missing configured port: %s", cfg)
	}
	if !strings.Contains(cfg, "redis_url: redis://127.0.0.1:6379/0") {
		t.Fatalf("goframe.yaml missing redis_url default: %s", cfg)
	}
	if !strings.Contains(cfg, "rate_limit_requests: 0") || !strings.Contains(cfg, "rate_limit_window: 1m") {
		t.Fatalf("goframe.yaml missing rate limit defaults: %s", cfg)
	}
	if !strings.Contains(cfg, "otlp_endpoint: \"\"") {
		t.Fatalf("goframe.yaml missing otlp_endpoint default: %s", cfg)
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
	if _, err := os.Stat(filepath.Join(projectDir, "cmd", "server", "main.go")); err != nil {
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
	if _, err := os.Stat(filepath.Join(projectDir, "cmd", "server", "main.go")); err != nil {
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
		"--template", "api",
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
		filepath.Join(dir, "internal", "tasks", "billing_tasks.go"),
		filepath.Join(dir, "internal", "web", "templates", "billing", "index.html"),
	}
	for _, p := range expectedFiles {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected generated file %s: %v", p, err)
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
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, "create_billings_table") && strings.HasSuffix(name, ".up.sql") {
			up = true
		}
		if strings.Contains(name, "create_billings_table") && strings.HasSuffix(name, ".down.sql") {
			down = true
		}
	}
	if !up || !down {
		t.Fatalf("expected up/down billing migration files, got %v", entries)
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

func TestRun_TestCommandDryRun(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := run([]string{
		"test",
		"--dry-run",
		"--run", "TestRun_MigrateLifecycle",
		"./cmd/goframe",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("test --dry-run failed: code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "go test -run TestRun_MigrateLifecycle ./cmd/goframe") {
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

func TestRun_CreateUserNoInput(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	cfgPath := writeCLIConfig(t, dir, dbPath)

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
	if err := dbConn.QueryRow("SELECT username, email, is_superuser FROM goframe_admin_users LIMIT 1").Scan(&username, &email, &super); err != nil {
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
	if err := dbConn.QueryRow("SELECT username, email, is_superuser FROM goframe_admin_users WHERE username = 'admin_alias' LIMIT 1").Scan(&username, &email, &super); err != nil {
		t.Fatalf("query admin user failed: %v", err)
	}
	if username != "admin_alias" || email != "admin_alias@example.com" || super != 1 {
		t.Fatalf("unexpected admin user row: username=%s email=%s super=%d", username, email, super)
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
	if !strings.Contains(out.String(), "/admin") {
		t.Fatalf("expected routes output to include /admin, got: %s", out.String())
	}
}

func TestRun_ExternalCommandPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-hello")
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
	cfgPath := filepath.Join(dir, "goframe.yaml")
	cfg := fmt.Sprintf("database_engine: bun\ndatabase_url: sqlite://%s\nlog_level: error\nlog_format: text\n", dbPath)
	writeFile(t, cfgPath, cfg)
	return cfgPath
}

func writeCLIConfigWithEnv(t *testing.T, dir, dbPath, env string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "goframe.yaml")
	cfg := fmt.Sprintf("database_engine: bun\ndatabase_url: sqlite://%s\nlog_level: error\nlog_format: text\nenv: %s\n", dbPath, env)
	writeFile(t, cfgPath, cfg)
	return cfgPath
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
