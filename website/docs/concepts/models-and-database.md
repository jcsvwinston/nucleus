---
sidebar_position: 4
title: Models & database
covers:
  - pkg/db.NewMigrator
  - pkg/db.NewModuleMigrator
  - pkg/db.Migrator.Up
  - pkg/db.Migrator.Down
  - pkg/db.Migrator.Steps
  - pkg/db.Migrator.Status
  - pkg/db.Migrator.Drift
  - pkg/db.Migrator.Create
  - pkg/db.MigrationStatus
  - pkg/db.DriftEntry
  - pkg/db.ExecScript
  - pkg/db.SchemaDriftEntry
  - pkg/db.ExpectedTable
config_keys:
  - databases.default
  - database_default
---

# Models & database

Nucleus's data layer is **SQL-first** by design. There is no ORM, no
auto-magical query builder. `pkg/db` wraps `database/sql` with
production-quality defaults; `pkg/model` adds metadata, registry and a
generic CRUD operator on top.

**Supported engines:** SQLite, PostgreSQL and MySQL are built into the
default binary. **MSSQL and Oracle are opt-in via Go build tags**
(`-tags mssql`, `-tags oracle`) — they keep the default binary small
and free of additional CGO requirements. See
[Getting started → Installation](../getting-started/installation.md#build-tagged-enterprise-drivers)
for the install commands.

## Defining a model

```go
type Article struct {
    ID        int64     `db:"id,primary"     json:"id"`
    Title     string    `db:"title"          json:"title"     validate:"required,min=3"`
    Body      string    `db:"body"           json:"body"`
    AuthorID  int64     `db:"author_id,fk=users.id" json:"author_id"`
    CreatedAt time.Time `db:"created_at"     json:"created_at"`
}
```

Tag conventions:

| Tag                          | Meaning                                       |
| ---------------------------- | --------------------------------------------- |
| `db:"name"`                  | Column name.                                  |
| `db:"name,primary"`          | Primary key.                                  |
| `db:"name,fk=table.column"`  | Foreign key.                                  |
| `db:"name,index"`            | Single-column index.                          |
| `db:"name,index=composite"`  | Composite index.                              |
| `db:"-"`                     | Ignore.                                       |
| `validate:"…"`               | go-playground/validator rules.                |
| `admin:"…"`                  | Admin panel hints (label, ordering, …).       |

## Registering a model

```go
import "github.com/jcsvwinston/nucleus/pkg/model"

a.Models.Register(&Article{})
```

The registry drives:

- the embedded admin panel (`pkg/admin`),
- the generic CRUD operator,
- metadata-aware migration scaffolding (`nucleus generate migration`),
- the JSON schema endpoint that the admin UI consumes at boot.

## SQL-first migrations

Migrations are plain SQL files under `migrations/`:

```
migrations/
├── 0001_initial.up.sql
├── 0001_initial.down.sql
├── 0002_add_articles_index.up.sql
└── 0002_add_articles_index.down.sql
```

The CLI applies them in lexicographic order:

```bash
nucleus migrate
nucleus migrate status
nucleus migrate steps 1
```

`nucleus generate migration <name>` writes a stub for you. `nucleus
generate migration --from-model Article` produces a `CREATE TABLE`
based on the registered model metadata — useful as a starting point,
not as the final word.

### Module-scoped migrations

When two modules share a database alias they may each ship a file named
`001_init.up.sql`. `db.NewMigrator` stores rows under the bare file ID, so
two such files would collide on insert. Use `db.NewModuleMigrator` instead:

```go
import "github.com/jcsvwinston/nucleus/pkg/db"

// Each module gets its own namespace in nucleus_schema_migrations.
articlesMigrator := db.NewModuleMigrator(database, "modules/articles/migrations", "articles", logger)
commentsMigrator := db.NewModuleMigrator(database, "modules/comments/migrations", "comments", logger)
```

Rows are stored as `articles/001_init`, `comments/001_init`, etc. — no
collision. `Migrator.Drift` is ownership-aware: it only reports rows that
belong to the Migrator that calls it. On-disk filenames are unchanged;
the namespace is a storage concern only.

### Multi-block scripts (Oracle PL/SQL)

Oracle's PL/SQL slash-terminator (`/`) is a SQL\*Plus directive, not
syntax that `database/sql` can pass to the driver. Multi-block scripts
that mix DDL and `BEGIN…END;` blocks would otherwise fail with cryptic
parser errors. `db.ExecScript` splits scripts on `/` at the start of a
line and executes each block individually:

```go
err := db.ExecScript(ctx, conn, dialect, scriptSQL)
```

For the default engines (SQLite, PostgreSQL, MySQL) the script is
passed through unchanged. The splitter is a no-op outside Oracle. See
ADR-011 for the Oracle identifier-casing decisions that go alongside
this.

## Schema drift detection

Even with disciplined SQL migrations, the live database and the model
registry can fall out of sync — an ad-hoc `ALTER TABLE` in production,
a column rename that landed in code but not in a migration, or a
brand-new model that nobody migrated. `Migrator.SchemaDrift` is the
opt-in check that catches these:

```go
drift, err := migrator.SchemaDrift(ctx, []db.ExpectedTable{
    {
        Name: "articles",
        Columns: []db.ExpectedColumn{
            {Name: "id",         Nullable: false},
            {Name: "title",      Nullable: false},
            {Name: "body",       Nullable: false},
            {Name: "author_id",  Nullable: false},
            {Name: "created_at", Nullable: false},
        },
    },
})
```

It reports four classes of drift:

| Class | Meaning |
|---|---|
| `DriftKindSchemaMissingTable` | The model declares a table the database does not have (migration forgotten?). |
| `DriftKindSchemaMissingColumn` | A column exists in the model but not in the database. |
| `DriftKindSchemaExtraColumn` | A column exists in the database but not in any registered model. |
| `DriftKindSchemaColumnNullability` | The column exists but `NULL`/`NOT NULL` does not match. |

Supported on **SQLite, PostgreSQL, MySQL, MSSQL and Oracle** (the
MSSQL/Oracle implementations landed alongside the build-tagged drivers;
see ADR-009 and its addendum). On any engine without a backend
implementation the call returns `ErrSchemaDriftUnsupported`.

`nucleus migrate drift` exposes this check from the CLI; pipe to
`--json` for machine-readable output suitable for CI.

## Querying

`pkg/db` exposes the underlying `*sql.DB` plus thin helpers. There is no
custom query builder:

```go
rows, err := a.DB.Query(ctx,
    `SELECT id, title FROM articles WHERE author_id = ? ORDER BY id DESC`,
    authorID)
if err != nil {
    return err
}
defer rows.Close()
```

For repository-style code, `pkg/db` ships `Scan`/`ScanAll` helpers and a
`Tx` wrapper that propagates context and panics safely.

## Multi-tenant queries

When `multitenant.enabled: true` is set, `App.DatabaseForRequest(r)`
resolves the database — and the tenant scope — for the current request.
The admin panel auto-filters by tenant; for application code, you scope
queries explicitly:

```go
tdb := a.DatabaseForRequest(r)
tenantID := app.TenantFromContext(r.Context())
sqlDB, _ := tdb.SqlDB()
rows, _ := sqlDB.QueryContext(ctx, `SELECT * FROM articles WHERE tenant_id = ?`, tenantID)
```

## Health and telemetry

`pkg/db` registers health checks and emits OpenTelemetry spans for every
query when OTel is enabled. The admin panel surfaces the connection
pool, the migration plan and the latest health status under
`/admin/system`.
