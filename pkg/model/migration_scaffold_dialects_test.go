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
}
