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
    ID        int64     `db:"pk"                    json:"id"`
    Title     string    `db:"column:title;required" json:"title"     validate:"required,min=3"`
    Body      string    `db:"column:body"           json:"body"`
    AuthorID  int64     `db:"fk:users.id;index"     json:"author_id"`
    CreatedAt time.Time `db:"readonly"              json:"created_at"`
    Draft     string    `db:"-"                     json:"-"`
}
```

The `db:` tag holds directives separated by `;`. Without a `column:`
directive the column name is the snake_case of the field name
(`AuthorID` → `author_id`).

| Directive               | Meaning                                                  |
| ----------------------- | -------------------------------------------------------- |
| `column:<name>`         | Column name override.                                    |
| `pk`                    | Primary key.                                             |
| `fk:<table.column>`     | Foreign key (also `fk:model=…`/`table=…`/`column=…`).    |
| `index` / `index:<name>`| Single-column index / named (composite) index.           |
| `unique` / `unique:<name>` | Unique index / named unique index.                    |
| `not null` / `required` | Required field.                                          |
| `readonly`              | Read-only in admin forms (aliases: `ro`, `autocreatetime`, `autoupdatetime`). |
| `tenant`                | Tenant-ID column for multi-tenant isolation.             |
| `db:"-"`                | Exclude the field from the persistence layer entirely: no column, no CRUD, no scaffolded DDL, no admin. |
| `validate:"…"`          | go-playground/validator rules.                           |
| `admin:"…"`             | Hints for admin tooling (label, ordering, …). Used by the orbit module. |

A directive the parser does not recognize is applied as **nothing** — and
the framework tells you so: at startup, every registered model field with
an unrecognized `db:` directive produces a `WARN` log naming the model,
the field and the ignored text, so a typo never silently becomes a
missing constraint.

## Registering a model

```go
import "github.com/jcsvwinston/nucleus/pkg/model"

a.Models.Register(&Article{})
```

The registry drives:

- the generic CRUD operator,
- metadata-aware migration scaffolding (`nucleus generate migration`),
- extension modules that introspect model metadata (such as orbit).

### How `Create` treats the primary key

- **Zero-value PK** (the common case): the key stays out of the `INSERT`; the
  database generates it and CRUD back-fills it onto the entity where the
  engine supports that.
- **Pre-assigned PK** (client-generated UUIDs, natural keys): a non-zero key
  travels **in** the `INSERT`, and neither the read-back nor the back-fill
  runs — the entity keeps exactly the key you set. This works for string and
  integer keys alike; note that SQL Server rejects explicit values on
  `IDENTITY` columns with its own clear error.

**Security note — do not decode request bodies straight into the entity.**
Because a non-zero key travels in the `INSERT`, a handler that does
`BindJSON(&entity)` followed by `Create` lets the HTTP client choose the
row's primary key. Either decode into a DTO that has no key field (or zero
the key before `Create`), or register the model with `RejectClientPK` so
`Create` refuses entities that arrive carrying a key:

```go
a.Models.Register(&Article{}, model.ModelConfig{RejectClientPK: true})
```

With that option set, `Create` returns `model.ErrClientAssignedPK`
(check with `errors.Is`) instead of inserting the client's key. The check
runs before hooks, so a `BeforeCreate` hook that assigns a server-generated
key keeps working. The default is off: pre-assigned keys are accepted, as
described above.

Two engine-specific limits of `Create` are worth knowing before you rely on
the generated key: on **Oracle** the primary key is not back-filled onto the
entity, and on **MSSQL** tables with triggers the back-fill mechanism
(`OUTPUT INSERTED`) is rejected by the engine. Details in
[Support & compatibility](../architecture/compatibility.md#databases).

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
passed through unchanged. The splitter is a no-op outside Oracle.

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

Supported on **SQLite, PostgreSQL, MySQL, MSSQL and Oracle** — the MSSQL and
Oracle implementations ship with the build-tagged drivers for those engines.
On any engine without a backend implementation the call returns
`ErrSchemaDriftUnsupported`.

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
For application code, you scope queries explicitly:

```go
tdb := a.DatabaseForRequest(r)
tenantID := app.TenantFromContext(r.Context())
sqlDB, _ := tdb.SqlDB()
rows, _ := sqlDB.QueryContext(ctx, `SELECT * FROM articles WHERE tenant_id = ?`, tenantID)
```

## Health and telemetry

`pkg/db` registers health checks and emits OpenTelemetry spans for every
query when OTel is enabled. Connection-pool stats and migration-plan
status are exported via the framework's OpenTelemetry metrics surface.
