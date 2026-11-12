// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeUnderDir writes body at path, creating the directories above it.
func writeUnderDir(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeQuarkModel writes a models package with one Quark model (a db tag)
// under root/internal/models and returns the directory.
func writeQuarkModel(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "internal", "models")
	writeUnderDir(t, filepath.Join(dir, "gear.go"), `package models

import "time"

type Gear struct {
	ID        uint      `+"`db:\"id\" pk:\"true\"`"+`
	Name      string    `+"`db:\"name\" quark:\"not_null\"`"+`
	CreatedAt time.Time `+"`db:\"created_at\"`"+`
}

func (Gear) TableName() string { return "gears" }
`)
	return dir
}

// fakeQuarkCLI writes an executable standing in for the quark CLI and
// points NUCLEUS_QUARK_CLI at it. It records its arguments in <dir>/args
// and prints body's stdout, so a test reads both.
func fakeQuarkCLI(t *testing.T, stdoutJSON string) (argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in quark CLI is a shell script")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin := filepath.Join(dir, "quark")
	writePluginExecutable(t, bin, "#!/bin/sh\necho \"$@\" > "+argsFile+"\npwd >> "+argsFile+"\ncat <<'JSON'\n"+stdoutJSON+"\nJSON\n")
	t.Setenv(quarkCLIEnv, bin)
	return argsFile
}

const gearDocument = `{"schema":1,"tool":"quark","version":"v9.9.9","dialect":"postgres","models":[{"name":"Gear","table":"gears"}],"up":["CREATE TABLE IF NOT EXISTS gears (\n\tid BIGSERIAL PRIMARY KEY,\n\tname TEXT NOT NULL\n)","CREATE INDEX IF NOT EXISTS idx_gears_name ON gears (name)"],"down":["DROP TABLE IF EXISTS gears"]}`

func readPair(t *testing.T, migDir, name string) (up, down string) {
	t.Helper()
	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", migDir, err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(migDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(e.Name(), "_"+name+".up.sql"):
			up = string(raw)
		case strings.HasSuffix(e.Name(), "_"+name+".down.sql"):
			down = string(raw)
		}
	}
	if up == "" || down == "" {
		t.Fatalf("expected the %s pair in %s, found %v", name, migDir, entries)
	}
	return up, down
}

func TestMigrateCreateFromModelsRendersThroughTheQuarkCLI(t *testing.T) {
	root := t.TempDir()
	models := writeQuarkModel(t, root)
	argsFile := fakeQuarkCLI(t, gearDocument)
	migDir := filepath.Join(root, "migrations")

	var stdout, stderr bytes.Buffer
	err := runMigrate([]string{"--migrations", migDir, "--dialect", "postgresql", "create", "init_schema", "--from-models", models}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("migrate create --from-models: %v\nstderr: %s", err, stderr.String())
	}

	// The CLI was asked exactly what the contract says: the absolute
	// models directory (QK-100), nucleus's engine name, --emit json.
	recorded, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := string(recorded)
	for _, want := range []string{"migrate create init_schema", "--from-models " + models, "--dialect postgresql", "--emit json"} {
		if !strings.Contains(args, want) {
			t.Errorf("the quark CLI was not asked %q; it got:\n%s", want, args)
		}
	}

	up, down := readPair(t, migDir, "init_schema")
	for _, want := range []string{
		"-- Rendered by nucleus makemigrations from " + filepath.ToSlash(models) + " with quark v9.9.9 for postgresql.",
		"-- Models: Gear -> gears.",
		"CREATE TABLE IF NOT EXISTS gears (\n\tid BIGSERIAL PRIMARY KEY,\n\tname TEXT NOT NULL\n);\n",
		"\nCREATE INDEX IF NOT EXISTS idx_gears_name ON gears (name);\n",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("up file lacks %q:\n%s", want, up)
		}
	}
	if !strings.Contains(down, "DROP TABLE IF EXISTS gears;\n") || !strings.Contains(down, "with quark v9.9.9") {
		t.Errorf("down file:\n%s", down)
	}
	if strings.Contains(up, "Write your SQL here") {
		t.Errorf("the rendered pair still carries the placeholder:\n%s", up)
	}
	for _, want := range []string{`Migration files created for "init_schema" from ` + models + " (1 model(s), quark v9.9.9, dialect postgresql):", "  up:   ", "  down: ", "Review the SQL before nucleus migrate up."} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
}

func TestMigrateCreateFromModelsTerminatesOracleStatementsWithASlashLine(t *testing.T) {
	root := t.TempDir()
	models := writeQuarkModel(t, root)
	fakeQuarkCLI(t, `{"schema":1,"tool":"quark","version":"v9.9.9","dialect":"oracle","models":[{"name":"Gear","table":"gears"}],"up":["CREATE TABLE gears (\n\tid NUMBER\n)","CREATE INDEX idx ON gears (id)"],"down":["DROP TABLE gears"]}`)
	migDir := filepath.Join(root, "migrations")

	var stdout, stderr bytes.Buffer
	if err := runMigrate([]string{"--migrations", migDir, "--dialect", "oracle", "--from-models", models, "create", "ora"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("migrate create: %v\nstderr: %s", err, stderr.String())
	}
	up, down := readPair(t, migDir, "ora")
	// pkg/db.ExecScript splits Oracle scripts on a `/` line, never on `;`.
	if !strings.Contains(up, "CREATE TABLE gears (\n\tid NUMBER\n)\n/\n\nCREATE INDEX idx ON gears (id)\n/\n") || strings.Contains(up, ";") {
		t.Errorf("oracle up file:\n%s", up)
	}
	if !strings.Contains(down, "DROP TABLE gears\n/\n") {
		t.Errorf("oracle down file:\n%s", down)
	}
}

func TestMigrateCreateFromModelsReportsAQuarkCLIWithoutEmitJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in quark CLI is a shell script")
	}
	root := t.TempDir()
	models := writeQuarkModel(t, root)
	bin := filepath.Join(t.TempDir(), "quark")
	writePluginExecutable(t, bin, "#!/bin/sh\necho 'Error: unknown flag: --emit' >&2\nexit 1\n")
	t.Setenv(quarkCLIEnv, bin)

	var stdout, stderr bytes.Buffer
	err := runMigrate([]string{"--migrations", filepath.Join(root, "migrations"), "--dialect", "sqlite", "create", "x", "--from-models", models}, strings.NewReader(""), &stdout, &stderr)
	if err == nil {
		t.Fatal("an old quark CLI must be reported, not worked around")
	}
	var old *errQuarkCLITooOld
	if !errors.As(err, &old) {
		t.Fatalf("expected errQuarkCLITooOld, got %T: %v", err, err)
	}
	for _, want := range []string{bin, "cmd/quark v1.3.0", "go install github.com/jcsvwinston/quark/cmd/quark@latest", quarkCLIEnv, "unknown flag: --emit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "migrations")); len(entries) != 0 {
		t.Errorf("no pair may be written when the CLI is too old, found %v", entries)
	}
}

func TestMigrateCreateFromModelsRefusesAnUnknownDocumentSchema(t *testing.T) {
	root := t.TempDir()
	models := writeQuarkModel(t, root)
	fakeQuarkCLI(t, `{"schema":2,"tool":"quark","version":"v12.0.0","dialect":"sqlite","models":[],"up":[],"down":[]}`)

	var stdout, stderr bytes.Buffer
	err := runMigrate([]string{"--migrations", filepath.Join(root, "migrations"), "--dialect", "sqlite", "create", "x", "--from-models", models}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "schema 2") || !strings.Contains(err.Error(), "reads schema 1") || !strings.Contains(err.Error(), "v12.0.0") {
		t.Fatalf("expected the schema mismatch to be reported, got: %v", err)
	}
}

func TestMigrateCreateFromModelsRefusesSomethingThatIsNotTheDocument(t *testing.T) {
	root := t.TempDir()
	models := writeQuarkModel(t, root)
	fakeQuarkCLI(t, `Rendered DDL for 1 model(s)`)

	var stdout, stderr bytes.Buffer
	err := runMigrate([]string{"--migrations", filepath.Join(root, "migrations"), "--dialect", "sqlite", "create", "x", "--from-models", models}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "did not print the DDL document") || !strings.Contains(err.Error(), "Rendered DDL for 1 model(s)") {
		t.Fatalf("expected the non-JSON stdout to be reported, got: %v", err)
	}
}

func TestMigrateCreateFromModelsWithoutQuarkModelsWritesTheEmptyPair(t *testing.T) {
	root := t.TempDir()
	models := filepath.Join(root, "internal", "models")
	writeUnderDir(t, filepath.Join(models, "plain.go"), "package models\n\n// Plain is a struct, not a model: no db tag.\ntype Plain struct{ Name string `json:\"name\"` }\n\ntype hidden struct{ ID int `db:\"id\"` }\n")
	writeUnderDir(t, filepath.Join(models, "plain_test.go"), "package models\n\ntype Tagged struct{ ID int `db:\"id\"` }\n")
	// A CLI that fails if run: the decision is taken without it.
	if runtime.GOOS != "windows" {
		bin := filepath.Join(t.TempDir(), "quark")
		writePluginExecutable(t, bin, "#!/bin/sh\necho 'must not run' >&2\nexit 7\n")
		t.Setenv(quarkCLIEnv, bin)
	}
	migDir := filepath.Join(root, "migrations")

	var stdout, stderr bytes.Buffer
	if err := runMigrate([]string{"--migrations", migDir, "--dialect", "sqlite", "create", "nothing", "--from-models", models}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("migrate create: %v", err)
	}
	up, down := readPair(t, migDir, "nothing")
	if up != "-- Write your SQL here\n" || down != "-- Write your SQL here\n" {
		t.Errorf("expected the empty pair, got up=%q down=%q", up, down)
	}
	if !strings.Contains(stdout.String(), "No Quark models in "+models+" (no exported struct carries a db tag): wrote the empty pair.") || !strings.Contains(stdout.String(), `Migration files created for "nothing"`) {
		t.Errorf("stdout:\n%s", stdout.String())
	}
}

func TestMigrateCreateFromModelsNamesAMissingDirectory(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := runMigrate([]string{"--migrations", filepath.Join(root, "migrations"), "--dialect", "sqlite", "create", "x", "--from-models", filepath.Join(root, "nowhere")}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "no such directory") || !strings.Contains(err.Error(), "internal/models") {
		t.Fatalf("expected the missing directory to be named, got: %v", err)
	}
}

func TestMakeMigrationsAliasRendersFromTheProjectModels(t *testing.T) {
	root := t.TempDir()
	writeQuarkModel(t, root)
	writeUnderDir(t, filepath.Join(root, "nucleus.yml"), "database_default: default\ndatabases:\n  default:\n    url: postgres://user:pw@localhost/app\nlog_level: error\n")
	argsFile := fakeQuarkCLI(t, gearDocument)
	migDir := filepath.Join(root, "migrations")

	// --config names the project, so the alias finds internal/models next
	// to it from any working directory.
	cmd, args, err := canonicalizeCommand("makemigrations", []string{"--config", filepath.Join(root, "nucleus.yml"), "--migrations", migDir, "init"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "migrate" {
		t.Fatalf("command %q", cmd)
	}
	want := "--config " + filepath.Join(root, "nucleus.yml") + " --migrations " + migDir + " --from-models " + filepath.Join(root, "internal", "models") + " create init"
	if got := strings.Join(args, " "); got != want {
		t.Fatalf("alias args:\n got %q\nwant %q", got, want)
	}
	var stdout, stderr bytes.Buffer
	if err := runMigrate(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("makemigrations: %v\nstderr: %s", err, stderr.String())
	}
	// The dialect came from the configured database URL.
	if recorded, _ := os.ReadFile(argsFile); !strings.Contains(string(recorded), "--dialect postgresql") {
		t.Errorf("the dialect must come from the configuration; the CLI got:\n%s", recorded)
	}
	up, _ := readPair(t, migDir, "init")
	if !strings.Contains(up, "for postgresql.") || !strings.Contains(up, "CREATE TABLE IF NOT EXISTS gears") {
		t.Errorf("up file:\n%s", up)
	}
}

func TestMakeMigrationsAliasWithoutModelsPackageIsTheEmptyPair(t *testing.T) {
	root := t.TempDir()
	writeUnderDir(t, filepath.Join(root, "nucleus.yml"), "database_default: default\ndatabases:\n  default:\n    url: sqlite://app.db\n")
	_, args, err := canonicalizeCommand("makemigrations", []string{"--config", filepath.Join(root, "nucleus.yml"), "--dialect", "mysql", "--database", "default", "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); strings.Contains(got, "--from-models") || !strings.HasSuffix(got, "--database default --dialect mysql create plain") {
		t.Fatalf("alias args without internal/models: %q", got)
	}
}

func TestResolveQuarkCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH lookups of shell scripts")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "quark")
	writePluginExecutable(t, bin, "#!/bin/sh\nexit 0\n")

	t.Run("the environment pins a path", func(t *testing.T) {
		cli, err := resolveQuarkCLI(func(string) string { return bin })
		if err != nil || cli.bin != bin || cli.how != quarkCLIEnv || len(cli.args) != 0 {
			t.Fatalf("got %+v, %v", cli, err)
		}
	})
	t.Run("the environment names a missing path", func(t *testing.T) {
		_, err := resolveQuarkCLI(func(string) string { return filepath.Join(dir, "missing") })
		if err == nil || !strings.Contains(err.Error(), quarkCLIEnv) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("the environment names a command on PATH", func(t *testing.T) {
		t.Setenv("PATH", dir)
		cli, err := resolveQuarkCLI(func(string) string { return "quark" })
		if err != nil || cli.bin != bin {
			t.Fatalf("got %+v, %v", cli, err)
		}
	})
	t.Run("quark on PATH", func(t *testing.T) {
		t.Setenv("PATH", dir)
		cli, err := resolveQuarkCLI(func(string) string { return "" })
		if err != nil || cli.bin != bin || cli.how != "PATH" {
			t.Fatalf("got %+v, %v", cli, err)
		}
	})
	t.Run("go run when there is no quark", func(t *testing.T) {
		goDir := t.TempDir()
		writePluginExecutable(t, filepath.Join(goDir, "go"), "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", goDir)
		cli, err := resolveQuarkCLI(func(string) string { return "" })
		if err != nil || cli.how != "go run" || strings.Join(cli.args, " ") != "run "+quarkCLIModule+"@latest" {
			t.Fatalf("got %+v, %v", cli, err)
		}
	})
	t.Run("neither quark nor go", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := resolveQuarkCLI(func(string) string { return "" })
		if err == nil || !strings.Contains(err.Error(), "go install "+quarkCLIModule+"@latest") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestMakeMigrationsLeavesOutTablesEarlierMigrationsCreate(t *testing.T) {
	root := t.TempDir()
	models := writeQuarkModel(t, root)
	fakeQuarkCLI(t, `{"schema":1,"tool":"quark","version":"v9.9.9","dialect":"mssql","models":[{"name":"Cog","table":"cogs"},{"name":"Gear","table":"gears"}],"up":["IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'gears') CREATE TABLE [gears] (\n  [id] BIGINT\n)","CREATE INDEX [idx_gears_name] ON [gears] ([name])","CREATE TABLE [cogs] (\n  [id] BIGINT\n)"],"down":["DROP TABLE IF EXISTS [cogs]","DROP TABLE IF EXISTS [gears]"]}`)
	migDir := filepath.Join(root, "migrations")
	// An earlier migration — hand-written, quoted the SQL Server way —
	// already creates gears.
	writeUnderDir(t, filepath.Join(migDir, "20260101000000_init.up.sql"), "-- first\nCREATE TABLE [Gears] (\n  [id] BIGINT\n);\n")
	writeUnderDir(t, filepath.Join(migDir, "20260101000000_init.down.sql"), "DROP TABLE [Gears];\n")

	var stdout, stderr bytes.Buffer
	if err := runMigrate([]string{"--migrations", migDir, "--dialect", "mssql", "create", "add_cogs", "--from-models", models}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("migrate create: %v\nstderr: %s", err, stderr.String())
	}
	up, down := readPair(t, migDir, "add_cogs")
	if strings.Contains(up, "gears") || !strings.Contains(up, "CREATE TABLE [cogs]") || !strings.Contains(up, "-- Models: Cog -> cogs.") {
		t.Errorf("up must carry cogs only:\n%s", up)
	}
	if strings.Contains(down, "gears") || !strings.Contains(down, "DROP TABLE IF EXISTS [cogs];") {
		t.Errorf("down must carry cogs only:\n%s", down)
	}
	for _, want := range []string{"Already created by migrations in " + migDir + ", left out: Gear (gears, created by 20260101000000_init.up.sql).", "(1 model(s), quark v9.9.9, dialect mssql)"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
}

func TestMakeMigrationsWithNothingNewWritesNothing(t *testing.T) {
	root := t.TempDir()
	models := writeQuarkModel(t, root)
	fakeQuarkCLI(t, gearDocument)
	migDir := filepath.Join(root, "migrations")
	writeUnderDir(t, filepath.Join(migDir, "20260101000000_init.up.sql"), "CREATE TABLE IF NOT EXISTS gears (\n\tid BIGSERIAL PRIMARY KEY\n);\n")
	writeUnderDir(t, filepath.Join(migDir, "20260101000000_init.down.sql"), "DROP TABLE IF EXISTS gears;\n")

	var stdout, stderr bytes.Buffer
	if err := runMigrate([]string{"--migrations", migDir, "--dialect", "postgresql", "create", "again", "--from-models", models}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("migrate create: %v\nstderr: %s", err, stderr.String())
	}
	entries, err := os.ReadDir(migDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("nothing new must write nothing; the directory holds %v", entries)
	}
	for _, want := range []string{"left out: Gear (gears, created by 20260101000000_init.up.sql).", "No changes detected: every model in " + models + " has its table. A column you add to a struct is an ALTER TABLE you write by hand."} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
}

func TestStatementTable(t *testing.T) {
	for stmt, want := range map[string]string{
		"CREATE TABLE IF NOT EXISTS gears (\n\tid INT\n)":                                                  "gears",
		"create table \"Users\" (id int)":                                                                  "users",
		"IF NOT EXISTS (SELECT * FROM sys.tables WHERE name = 'gears') CREATE TABLE [gears] ([id] BIGINT)": "gears",
		"CREATE INDEX IF NOT EXISTS idx_gears_name ON gears (name)":                                        "gears",
		"CREATE UNIQUE INDEX \"ux\" ON \"cogs\" (\"slug\")":                                                "cogs",
		"CREATE INDEX [idx] ON [gears] ([name])":                                                           "gears",
		"DROP TABLE IF EXISTS `gears`":                                                                     "gears",
		"DROP TABLE \"cogs\"":                                                                              "cogs",
		"ALTER TABLE gears ADD COLUMN teeth INT":                                                           "",
		"-- a comment":                                                                                     "",
	} {
		if got := statementTable(stmt); got != want {
			t.Errorf("statementTable(%q) = %q, want %q", stmt, got, want)
		}
	}
}
