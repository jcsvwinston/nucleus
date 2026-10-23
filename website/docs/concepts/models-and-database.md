---
sidebar_position: 5
title: Models & database
covers:
  - pkg/db.NewMigrator
  - pkg/db.NewMigratorFromConfig
  - pkg/db.MigratorConfig
  - pkg/db.Migrator.Up
  - pkg/db.Migrator.Down
  - pkg/db.Migrator.Steps
  - pkg/db.Migrator.Status
  - pkg/db.Migrator.Drift
  - pkg/db.Migrator.Create
  - pkg/db.Migrator.Applied
  - pkg/db.MigrationStatus
  - pkg/db.AppliedMigration
  - pkg/db.DriftEntry
  - pkg/db.ExecScript
  - pkg/db.SchemaDriftEntry
  - pkg/db.ExpectedTable
config_keys:
  - databases.default
  - database_default
---

# Models & database

Nucleus's data layer is **SQL-first** by design. There is no ORM and no
query builder *in the framework core*. `pkg/db` wraps `database/sql` with
production-quality defaults; Quark, the data layer of the suite, is what an
application's models are written with.

:::info `pkg/model` left the public API at v2.0.0 (DEP-2026-024)
Quark is the data layer of the suite, and keeping two meant two tag
vocabularies and two ways to write a model. The framework keeps no model
registry: `App.Models`, `Runtime.Models()` and `Module.Models` are gone with
the package, and the admin panel reads Quark models through
`quarkdatasource`. The migration assistant is
`docs/migration_assistants/MA-2026-024-pkg-model-to-quark.md` in the
repository.
:::

:::tip The data layer a feature gets
**Quark is the data layer a generated feature gets.** `nucleus generate module
<name>` builds a typed model, a repository and its migration on the Quark ORM;
`--data sql` opts out to plain `database/sql` when a slice needs it.
:::

If you do want an ORM, the suite ships one: **Quark** runs inside a
Nucleus module, with optional bridges into orbit's live SQL feed and Data
Studio — see [Features → Using Quark with Nucleus](../features/using-quark.md).
Note that Quark models use their own tag dialect and migration system, not
the `db:` tags documented on this page.

This page covers how you describe a model, how migrations work, and how to
detect when the live schema and your models have drifted apart.

**Supported engines:** SQLite, PostgreSQL, MySQL, SQL Server and Oracle.
Each driver is a module your application imports — `nucleus add postgres`
(or `mysql`, `sqlite`, `sqlserver`, `oracle`) runs the `go get` and writes
the blank import — so a binary carries one engine, not five. See
[Getting started → Installation](../getting-started/installation.md#database-drivers-are-modules).

On SQLite, a `time.Time` is stored as text SQLite's own date functions read
(`2026-10-16 12:30:00+00:00`, so `date(ts)` and `strftime()` work in SQL):
the framework opens every SQLite URL with `_time_format=sqlite`, and a busy
timeout, unless the URL names them itself. A database written before
v2.0.0 holds the text Go produced (`2026-10-16 12:30:00 +0000 UTC`); the
driver reads both forms, so those rows stay readable, but `date()` over
them is `NULL`. If your SQL queries old rows by date, rewrite them once —
one `UPDATE` per time column, after a backup, adapted to the zone the rows
were written in:

```sql
UPDATE t SET ts = strftime('%Y-%m-%d %H:%M:%f', substr(ts, 1, 19)) || '+00:00'
 WHERE ts LIKE '% +0000 UTC';
```

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
| `admin:"…"`             | Accepted and ignored. The hints (`list`, `search`, `filter`, `readonly`, `exclude`, `label:…`, `choices:…`) were read by the admin panel's adapter over the model registry, which left with it: since v2.0.0 the framework keeps no registry (DEP-2026-024) and the panel is mounted over Quark models, so nothing reads this tag. A struct that carries one keeps compiling. |

A directive the parser does not recognise is applied as **nothing**, and
`App.AutoMigrate` says so in a WARN line naming the model, the field and the
directive when it reads the struct — a tag written in Quark's grammar
(`db:"email,size=255"`) is named as such. These tags are what
`App.AutoMigrate` and the test kit's `nucleustest.Make` read from a struct;
there is no registry to register a model with.

### Listing: filters, operators and the total

`FindAll` takes a `QueryOpts`. `Filters` is column → value (exact match) and
`Where` carries the comparisons that need an operator; the two are ANDed, and
a query that sets neither behaves exactly as it always did.

```go
res, err := crud.FindAll(ctx, model.QueryOpts{
    Page: 1, PageSize: 50,
    Filters: map[string]string{"status": "published"},
    Where: []model.Filter{
        {Column: "views", Op: model.OpGreaterEqual, Value: "100"},
        {Column: "title", Op: model.OpContains, Value: "release"},
        {Column: "tag", Op: model.OpIn, Values: []string{"go", "sql"}},
    },
    ExactTotal: true,
})
```

| Operator | Means |
|---|---|
| `OpEqual`, `OpNotEqual` | `=`, `<>` |
| `OpGreater`, `OpGreaterEqual`, `OpLess`, `OpLessEqual` | `>`, `>=`, `<`, `<=` |
| `OpContains`, `OpStartsWith`, `OpEndsWith` | case-insensitive `LIKE`, with `%` and `_` in the value treated as characters |
| `OpIn`, `OpNotIn` | a set (an **empty** set matches nothing, rather than being dropped) |
| `OpIsNull` | `IS NULL`, or `IS NOT NULL` for a false value |

The column is resolved against the model's own columns and the operator comes
from a closed set, so the SQL is built from allow-listed tokens and every
value is bound — the same discipline `OrderBy` follows. A column that does not
resolve is dropped, as an unknown key in `Filters` already was.

**`ExactTotal`** asks for a real `COUNT` of the matching rows. Without it a
filtered list answers `Total: -1` with `IsEstimated: true` — it never counted,
so a pager had nothing to divide — and an unfiltered one answers the engine's
cheap estimate. Counting is a second query, so it is the caller's choice: a
screen with a pager asks for it, a background sweep does not.

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

Two engine-specific limits apply before you rely on a generated key:

- **Oracle** does not back-fill the primary key onto the entity.
- **MSSQL** rejects the back-fill mechanism (`OUTPUT INSERTED`) on tables
  that have triggers.

Details in
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
nucleus migrate up
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
two such files would collide on insert. Give each module its own namespace
with `db.NewMigratorFromConfig` and a `Module` name:

```go
import "github.com/jcsvwinston/nucleus/pkg/db"

// Each module gets its own namespace in nucleus_schema_migrations.
articlesMigrator, err := db.NewMigratorFromConfig(db.MigratorConfig{
    DB: database, Dir: "modules/articles/migrations", Module: "articles",
}, logger)
if err != nil {
    return err
}
commentsMigrator, err := db.NewMigratorFromConfig(db.MigratorConfig{
    DB: database, Dir: "modules/comments/migrations", Module: "comments",
}, logger)
if err != nil {
    return err
}
```

`MigratorConfig` takes `Dir` for a directory or `FS` for an `fs.FS` (an
embedded one, say), and the constructor returns an error for a module name
holding `/` or NUL, a nil `FS`, or neither/both sources. It is the one way
to build a module-scoped migrator: `db.NewModuleMigrator` and
`db.NewModuleFSMigrator`, which panicked on such input, were removed in 2.0
(DEP-2026-012).

Rows are stored as `articles/001_init`, `comments/001_init`, etc. — no
collision. `Migrator.Drift` is ownership-aware: it only reports rows that
belong to the Migrator that calls it. On-disk filenames are unchanged;
the namespace is a storage concern only.

### Embedded module migrations (`Module.Migrations`)

A module can carry its migrations inside the binary and apply them with
one deliberate call — the portable variant of the section above, for
modules meant to be mounted into other applications:

```go
//go:embed migrations/*.sql
var migrationsDir embed.FS

func Module() nucleus.ModuleSpec {
    migrations, _ := fs.Sub(migrationsDir, "migrations")
    return nucleus.Module[struct{}]{
        Name:       "articles",
        Migrations: migrations, // .up.sql/.down.sql at the FS root
        OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
            // Deliberate: the framework NEVER applies these on its own.
            // Remove this call to keep schema changes operator-driven.
            return rt.ApplyModuleMigrations()
        },
    }.Build()
}
```

`rt.ApplyModuleMigrations` goes through the real pipeline — the
module-scoped ledger (`articles/000001_…`) with checksum tracking, so
re-runs are idempotent and `Drift` sees embedded scripts exactly like
disk ones. Unlike `AutoMigrate`, nothing bypasses the ledger. The
boot-time WARN about declared-but-unapplied embedded migrations is
suppressed once the module applies them this way, and `nucleus migrate
status` lists those rows under their `articles/` namespace after the
directory plan — `db.Migrator.Applied` is the call behind that view, the
whole ledger with each row's namespace.

Application boot itself still never mutates the schema on its own: the
call above is module code you choose to write, and the alternative —
shipping the SQL as disk files for `nucleus migrate up` — remains fully
supported. Under the hood this is `db.NewMigratorFromConfig` with `FS` and
`Module` set, usable directly for custom flows.

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

Even with disciplined SQL migrations, the live database and the model registry
can fall out of sync: an ad-hoc `ALTER TABLE` in production, a column rename
that landed in code but not in a migration, or a brand-new model nobody
migrated.

`Migrator.SchemaDrift` is the opt-in check that catches these:

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

Supported on **SQLite, PostgreSQL, MySQL, MSSQL and Oracle**. On any engine
without a backend implementation the call returns
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
