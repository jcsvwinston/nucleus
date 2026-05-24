package model

import (
	"strings"
	"testing"
	"time"
)

// fixtureModelMeta returns a populated meta with int PK, scalar
// columns of every supported Go type, a unique index and a foreign
// key — enough to exercise every code path in the dialect scaffolds.
func fixtureModelMeta() *ModelMeta {
	return &ModelMeta{
		Name:  "Article",
		Table: "articles",
		Fields: []FieldMeta{
			{Name: "ID", Column: "id", GoType: "int64", IsPK: true},
			{Name: "Title", Column: "title", GoType: "string", IsRequired: true},
			{Name: "AuthorID", Column: "author_id", GoType: "int64", IsRequired: true},
			{Name: "Published", Column: "published", GoType: "bool"},
			{Name: "Score", Column: "score", GoType: "float64"},
			{Name: "Payload", Column: "payload", GoType: "[]byte"},
			{Name: "CreatedAt", Column: "created_at", GoType: "time.Time", IsRequired: true},
		},
		Indexes: []IndexMeta{
			{Name: "uq_articles_title", Columns: []string{"title"}, Unique: true},
		},
		ForeignKeys: []ForeignKey{
			{Column: "author_id", ForeignTable: "users", ForeignColumn: "id"},
		},
	}
}

// Use time.Time in an unused var so the import lands consistently.
var _ = time.Time{}

// ---------- Postgres ----------

func TestBuildPostgresMigrationScaffold_ShapeAndTypes(t *testing.T) {
	up, down, err := BuildPostgresMigrationScaffold(fixtureModelMeta())
	if err != nil {
		t.Fatalf("BuildPostgresMigrationScaffold: %v", err)
	}

	for _, want := range []string{
		`CREATE TABLE IF NOT EXISTS "articles"`,
		`"id" BIGSERIAL PRIMARY KEY`,
		`"title" TEXT NOT NULL`,
		`"author_id" BIGINT NOT NULL`,
		`"published" BOOLEAN`,
		`"score" DOUBLE PRECISION`,
		`"payload" BYTEA`,
		`"created_at" TIMESTAMPTZ NOT NULL`,
		`CONSTRAINT "fk_articles_author_id__users_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id")`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "uq_articles_title" ON "articles" ("title");`,
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("postgres UP missing %q\n--- got ---\n%s", want, up)
		}
	}

	for _, want := range []string{
		`DROP INDEX IF EXISTS "uq_articles_title";`,
		`DROP TABLE IF EXISTS "articles" CASCADE;`,
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("postgres DOWN missing %q\n--- got ---\n%s", want, down)
		}
	}
}

func TestBuildPostgresMigrationScaffold_StringPK(t *testing.T) {
	meta := &ModelMeta{
		Name:  "Token",
		Table: "tokens",
		Fields: []FieldMeta{
			{Name: "Token", Column: "token", GoType: "string", IsPK: true},
			{Name: "ExpiresAt", Column: "expires_at", GoType: "time.Time", IsRequired: true},
		},
	}
	up, _, err := BuildPostgresMigrationScaffold(meta)
	if err != nil {
		t.Fatalf("BuildPostgresMigrationScaffold: %v", err)
	}
	if !strings.Contains(up, `"token" TEXT PRIMARY KEY`) {
		t.Fatalf("string PK should not be BIGSERIAL:\n%s", up)
	}
}

func TestBuildPostgresMigrationScaffold_RejectsNilAndEmpty(t *testing.T) {
	if _, _, err := BuildPostgresMigrationScaffold(nil); err == nil {
		t.Fatal("nil meta should error")
	}
	empty := &ModelMeta{Name: "X", Table: "x"}
	if _, _, err := BuildPostgresMigrationScaffold(empty); err == nil {
		t.Fatal("empty fields should error")
	}
	bad := &ModelMeta{Name: "X", Table: "bad name", Fields: []FieldMeta{{Name: "ID", Column: "id", GoType: "int64", IsPK: true}}}
	if _, _, err := BuildPostgresMigrationScaffold(bad); err == nil {
		t.Fatal("invalid table name should error")
	}
}

// ---------- MySQL ----------

func TestBuildMySQLMigrationScaffold_ShapeAndTypes(t *testing.T) {
	up, down, err := BuildMySQLMigrationScaffold(fixtureModelMeta())
	if err != nil {
		t.Fatalf("BuildMySQLMigrationScaffold: %v", err)
	}

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS `articles`",
		"`id` BIGINT AUTO_INCREMENT PRIMARY KEY",
		"`title` TEXT NOT NULL",
		"`author_id` BIGINT NOT NULL",
		"`published` TINYINT(1)",
		"`score` DOUBLE",
		"`payload` LONGBLOB",
		"`created_at` DATETIME(6) NOT NULL",
		"CONSTRAINT `fk_articles_author_id__users_id` FOREIGN KEY (`author_id`) REFERENCES `users` (`id`)",
		"CREATE UNIQUE INDEX `uq_articles_title` ON `articles` (`title`);",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("mysql UP missing %q\n--- got ---\n%s", want, up)
		}
	}

	for _, want := range []string{
		"DROP INDEX `uq_articles_title` ON `articles`;",
		"DROP TABLE IF EXISTS `articles`;",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("mysql DOWN missing %q\n--- got ---\n%s", want, down)
		}
	}
}

func TestBuildMySQLMigrationScaffold_StringPK(t *testing.T) {
	meta := &ModelMeta{
		Name:  "Token",
		Table: "tokens",
		Fields: []FieldMeta{
			{Name: "Token", Column: "token", GoType: "string", IsPK: true},
		},
	}
	up, _, err := BuildMySQLMigrationScaffold(meta)
	if err != nil {
		t.Fatalf("BuildMySQLMigrationScaffold: %v", err)
	}
	if !strings.Contains(up, "`token` TEXT PRIMARY KEY") {
		t.Fatalf("string PK should not be AUTO_INCREMENT:\n%s", up)
	}
}

// ---------- MSSQL ----------

func TestBuildMSSQLMigrationScaffold_ShapeAndTypes(t *testing.T) {
	up, down, err := BuildMSSQLMigrationScaffold(fixtureModelMeta())
	if err != nil {
		t.Fatalf("BuildMSSQLMigrationScaffold: %v", err)
	}

	for _, want := range []string{
		"IF OBJECT_ID('articles', 'U') IS NULL",
		"CREATE TABLE [articles]",
		"[id] BIGINT IDENTITY(1,1) PRIMARY KEY",
		"[title] NVARCHAR(MAX) NOT NULL",
		"[author_id] BIGINT NOT NULL",
		"[published] BIT",
		"[score] FLOAT(53)",
		"[payload] VARBINARY(MAX)",
		"[created_at] DATETIME2 NOT NULL",
		"CONSTRAINT [fk_articles_author_id__users_id] FOREIGN KEY ([author_id]) REFERENCES [users] ([id])",
		"IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'uq_articles_title' AND object_id = OBJECT_ID('articles'))",
		"CREATE UNIQUE INDEX [uq_articles_title] ON [articles] ([title]);",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("mssql UP missing %q\n--- got ---\n%s", want, up)
		}
	}

	for _, want := range []string{
		"DROP INDEX [uq_articles_title] ON [articles];",
		"DROP TABLE IF EXISTS [articles];",
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("mssql DOWN missing %q\n--- got ---\n%s", want, down)
		}
	}
}

func TestBuildMSSQLMigrationScaffold_StringPK(t *testing.T) {
	meta := &ModelMeta{
		Name:  "Token",
		Table: "tokens",
		Fields: []FieldMeta{
			{Name: "Token", Column: "token", GoType: "string", IsPK: true},
		},
	}
	up, _, err := BuildMSSQLMigrationScaffold(meta)
	if err != nil {
		t.Fatalf("BuildMSSQLMigrationScaffold: %v", err)
	}
	if !strings.Contains(up, "[token] NVARCHAR(MAX) PRIMARY KEY") {
		t.Fatalf("string PK should not be IDENTITY:\n%s", up)
	}
}

func TestBuildMSSQLMigrationScaffold_RejectsNilAndEmpty(t *testing.T) {
	if _, _, err := BuildMSSQLMigrationScaffold(nil); err == nil {
		t.Fatal("nil meta should error")
	}
	empty := &ModelMeta{Name: "X", Table: "x"}
	if _, _, err := BuildMSSQLMigrationScaffold(empty); err == nil {
		t.Fatal("empty fields should error")
	}
	bad := &ModelMeta{Name: "X", Table: "bad name", Fields: []FieldMeta{{Name: "ID", Column: "id", GoType: "int64", IsPK: true}}}
	if _, _, err := BuildMSSQLMigrationScaffold(bad); err == nil {
		t.Fatal("invalid table name should error")
	}
}

// ---------- Oracle ----------

func TestBuildOracleMigrationScaffold_ShapeAndTypes(t *testing.T) {
	up, down, err := BuildOracleMigrationScaffold(fixtureModelMeta())
	if err != nil {
		t.Fatalf("BuildOracleMigrationScaffold: %v", err)
	}

	for _, want := range []string{
		// Identifiers are UNQUOTED (ADR-011): Oracle folds them to upper
		// case, matching the CRUD / migrations / introspection layers.
		// Table create wrapped in PL/SQL block swallowing ORA-00955.
		`EXECUTE IMMEDIATE 'CREATE TABLE articles (`,
		`id NUMBER GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY`,
		`title VARCHAR2(4000) NOT NULL`,
		`author_id NUMBER(19) NOT NULL`,
		`published NUMBER(1)`,
		`score BINARY_DOUBLE`,
		`payload BLOB`,
		`created_at TIMESTAMP(6) WITH TIME ZONE NOT NULL`,
		`CONSTRAINT fk_articles_author_id__users_id FOREIGN KEY (author_id) REFERENCES users (id)`,
		`IF SQLCODE != -955 THEN RAISE; END IF; -- table already exists`,
		`EXECUTE IMMEDIATE 'CREATE UNIQUE INDEX uq_articles_title ON articles (title)'`,
		`IF SQLCODE != -955 THEN RAISE; END IF; -- index already exists`,
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("oracle UP missing %q\n--- got ---\n%s", want, up)
		}
	}

	// Regression guard: no double-quoted identifier must survive in EITHER
	// script (ADR-011 — quoting is what made tables invisible to the rest of
	// the Oracle path).
	for _, s := range []struct{ label, body string }{{"UP", up}, {"DOWN", down}} {
		if strings.Contains(s.body, `"`) {
			t.Errorf("oracle %s must not contain double-quoted identifiers (ADR-011)\n--- got ---\n%s", s.label, s.body)
		}
	}

	for _, want := range []string{
		`EXECUTE IMMEDIATE 'DROP INDEX uq_articles_title'`,
		`IF SQLCODE != -1418 THEN RAISE; END IF; -- specified index does not exist`,
		`EXECUTE IMMEDIATE 'DROP TABLE articles'`,
		`IF SQLCODE != -942 THEN RAISE; END IF; -- table or view does not exist`,
	} {
		if !strings.Contains(down, want) {
			t.Fatalf("oracle DOWN missing %q\n--- got ---\n%s", want, down)
		}
	}

	// Each PL/SQL block is terminated by a `/` on its own line — the split
	// marker `db.ExecScript` uses to run one block per go-ora Exec (it strips
	// the `/`, which the driver rejects with ORA-06550). The articles fixture
	// has a secondary index, so both UP (CREATE TABLE + CREATE INDEX) and DOWN
	// (DROP INDEX + DROP TABLE) emit two blocks, hence two `/` separators.
	for _, sql := range []struct {
		label string
		body  string
	}{{"UP", up}, {"DOWN", down}} {
		nBegin := strings.Count(sql.body, "BEGIN\n")
		nEnd := strings.Count(sql.body, "END;")
		var nSlash int
		for _, line := range strings.Split(sql.body, "\n") {
			if strings.TrimSpace(line) == "/" {
				nSlash++
			}
		}
		if nBegin < 2 {
			t.Errorf("oracle %s expected ≥2 PL/SQL blocks (table + index); got %d\n--- got ---\n%s", sql.label, nBegin, sql.body)
		}
		if nBegin != nEnd {
			t.Errorf("oracle %s has unbalanced PL/SQL blocks: %d BEGIN vs %d END;\n--- got ---\n%s", sql.label, nBegin, nEnd, sql.body)
		}
		if nSlash != nBegin {
			t.Errorf("oracle %s expected one `/` separator per block (%d); got %d\n--- got ---\n%s", sql.label, nBegin, nSlash, sql.body)
		}
	}
}

// TestOracleIdentifier_PassThrough pins ADR-011: the Oracle identifier
// emitter must NOT quote. It also guards the helper from being inlined or
// pruned as "dead" — it is the deliberate single choke point where the
// reserved-word follow-up would add selective quoting.
func TestOracleIdentifier_PassThrough(t *testing.T) {
	for _, in := range []string{"users", "author_id", "uq_articles_title", "MixedCase"} {
		if got := oracleIdentifier(in); got != in {
			t.Errorf("oracleIdentifier(%q) = %q; want unquoted pass-through %q (ADR-011)", in, got, in)
		}
	}
}

func TestBuildOracleMigrationScaffold_StringPK(t *testing.T) {
	meta := &ModelMeta{
		Name:  "Token",
		Table: "tokens",
		Fields: []FieldMeta{
			{Name: "Token", Column: "token", GoType: "string", IsPK: true},
		},
	}
	up, _, err := BuildOracleMigrationScaffold(meta)
	if err != nil {
		t.Fatalf("BuildOracleMigrationScaffold: %v", err)
	}
	if !strings.Contains(up, `token VARCHAR2(4000) PRIMARY KEY`) {
		t.Fatalf("string PK should not be IDENTITY:\n%s", up)
	}
}

func TestBuildOracleMigrationScaffold_RejectsNilAndEmpty(t *testing.T) {
	if _, _, err := BuildOracleMigrationScaffold(nil); err == nil {
		t.Fatal("nil meta should error")
	}
	empty := &ModelMeta{Name: "X", Table: "x"}
	if _, _, err := BuildOracleMigrationScaffold(empty); err == nil {
		t.Fatal("empty fields should error")
	}
}

// ---------- Cross-dialect determinism ----------

func TestScaffoldBuilders_AllDialectsProduceDeterministicOutput(t *testing.T) {
	meta := fixtureModelMeta()

	build := func(t *testing.T, name string, fn func(*ModelMeta) (string, string, error)) {
		t.Helper()
		first, _, err := fn(meta)
		if err != nil {
			t.Fatalf("%s first build: %v", name, err)
		}
		second, _, err := fn(meta)
		if err != nil {
			t.Fatalf("%s second build: %v", name, err)
		}
		if first != second {
			t.Fatalf("%s output is non-deterministic across builds", name)
		}
	}

	build(t, "sqlite", BuildSQLiteMigrationScaffold)
	build(t, "postgres", BuildPostgresMigrationScaffold)
	build(t, "mysql", BuildMySQLMigrationScaffold)
	build(t, "mssql", BuildMSSQLMigrationScaffold)
	build(t, "oracle", BuildOracleMigrationScaffold)
}
