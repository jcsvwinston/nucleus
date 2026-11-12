---
sidebar_position: 5
title: Models and the database
covers:
  - pkg/db.NewMigrator
  - pkg/db.NewModuleMigrator
  - pkg/db.NewModuleFSMigrator
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
  - pkg/app.App.AutoMigrate
  - pkg/db.ErrAutoMigrate
config_keys:
  - databases.default
  - database_default
---

# Models and the database

Nucleus owns the connection and the schema; **Quark**, the suite's ORM, is
the data layer you write features on. The framework opens the pool
`nucleus.yml` declares and closes it at shutdown, applies the SQL migrations
you write, and hands every module the pool through `rt.DB()`. A module
derives a Quark client over that pool and reads and writes typed models
through it. Nothing on this page that you are told to write imports
`pkg/model`: that package is the substrate underneath, deprecated, and it
leaves the public API at v2.0.0 — its own section closes the page.

This is what the CLI writes: `nucleus generate module <name>`, `generate
resource <name>` and `startapp <name>` all produce a Quark model, a storage
or repository on `quark.For[T]` over the managed pool, and the SQL migration
pair for the configured engine. `--data sql` keeps the same model and
renders `database/sql` statements instead of Quark calls, for a feature that
wants to hand-write its SQL.

**Supported engines:** SQLite, PostgreSQL, MySQL, SQL Server and Oracle.
Each driver is a module your application imports — `nucleus add postgres`
(or `mysql`, `sqlite`, `sqlserver`, `oracle`) runs the `go get` and writes
the blank import — so a binary carries one engine, not five. See
[Getting started → Installation](../getting-started/installation.md#database-drivers-are-modules).

## What a model is

A model is a plain Go struct with Quark's tags. `db:"<column>"` names the
column (an option after a comma, such as `size=255`, is not part of the
name), `pk:"true"` marks the key, `quark:"…"` carries the constraints the
migration also declares, and `TableName()` pins the struct to its table —
without it Quark pluralises the type name in snake case (`UserProfile` →
`user_profiles`). The columns `created_at`, `updated_at` and `deleted_at`
are the ones the CLI's migration creates; a `deleted_at` column makes
`Delete` a soft delete, and every read leaves soft-deleted rows out on its
own.

```go
package notes

import "time"

type Note struct {
	ID        uint       `db:"id" pk:"true" json:"id"`
	Title     string     `db:"title" quark:"not_null" json:"title"`
	Body      string     `db:"body" json:"body"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at" json:"-"`
}

func (Note) TableName() string { return "notes" }
```

A Quark model is not registered anywhere: no `Models` entry on the module,
no registry call. The struct and its table are the whole declaration.
Quark's own hooks — `BeforeCreate`, `AfterCreate`, `BeforeUpdate`,
`AfterUpdate`, `BeforeDelete`, `AfterDelete`, `BeforeFind`, `AfterFind`,
each a method `(ctx context.Context) error` on the model type — run around
the operation that names them. Relations, the query builder, transactions
and the rest of the ORM are Quark's to document; [Features → Using
Quark](../features/using-quark.md) is where Nucleus hands over.

## A client over the framework's pool

The framework opens the database from `databases.default.url` (or
`database_default`) in `nucleus.yml` and owns its lifecycle. A module does
not open a second connection: in `OnStart` it derives a Quark client over
the managed `*sql.DB` with `quark.NewWithDB`, and keeps it for its handlers.
The first argument is the `database/sql` driver name, which tells Quark
which dialect to speak — `sqlite`, `pgx`, `mysql`, `sqlserver` or `oracle` —
and the blank import of Quark's driver module for that engine
(`github.com/jcsvwinston/quark/drivers/sqlite`, `postgres`, `mysql`, `mssql`,
`oracle`) teaches it the engine's errors, duplicate key and deadlock among
them.

```go
package notes

import (
	"context"
	"fmt"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/quark"

	_ "github.com/jcsvwinston/quark/drivers/sqlite"
)

type module struct {
	client *quark.Client
}

func Module() nucleus.ModuleSpec {
	m := &module{}
	return nucleus.Module[struct{}]{
		Name: "notes",
		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
			db := rt.DB()
			if db == nil {
				return fmt.Errorf("notes: no managed database configured")
			}
			client, err := quark.NewWithDB("sqlite", db)
			if err != nil {
				return fmt.Errorf("notes: quark client over the managed pool: %w", err)
			}
			m.client = client
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) { /* handlers use m.client */ },
	}.Build()
}
```

There is no `OnShutdown` for the client: the pool is the framework's, and
a module closing it would be a double close. A client built with
`quark.New(driver, dsn)` in `main` and handed to the module is the other
shape, for an application whose Quark client predates Nucleus; the
framework-pool shape is what the generators write.

## Reading and writing with `quark.For[T]`

`quark.For[T](ctx, client)` is a typed query over the model's table. The
five calls a CRUD resource needs are `Find`, `Create`, `UpdateFields`,
`Delete` and `List`; `Where`, `OrderBy` and `Limit` shape a list. Quark
warns when `List()` runs without a `Limit`, and falls back to 100 rows — so
write the cap where it can be seen.

```go
package notes

import (
	"context"
	"errors"
	"time"

	"github.com/jcsvwinston/quark"
)

var ErrNotFound = errors.New("note not found")

type Storage struct {
	client *quark.Client
}

func (s *Storage) List(ctx context.Context) ([]Note, error) {
	return quark.For[Note](ctx, s.client).OrderBy("id", "DESC").Limit(100).List()
}

func (s *Storage) Get(ctx context.Context, id uint) (Note, error) {
	note, err := quark.For[Note](ctx, s.client).Find(id)
	if errors.Is(err, quark.ErrNotFound) {
		return Note{}, ErrNotFound
	}
	return note, err
}

func (s *Storage) Create(ctx context.Context, title string) (Note, error) {
	now := time.Now().UTC()
	note := Note{Title: title, CreatedAt: now, UpdatedAt: now}
	err := quark.For[Note](ctx, s.client).Create(&note) // the key comes back on the struct
	return note, err
}

func (s *Storage) Rename(ctx context.Context, id uint, title string) error {
	note, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	note.Title, note.UpdatedAt = title, time.Now().UTC()
	affected, err := quark.For[Note](ctx, s.client).UpdateFields(&note, "title", "updated_at")
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return err
}
```

`Find` on a missing or soft-deleted row returns `quark.ErrNotFound`;
`UpdateFields` and `Delete` report the rows they touched, so a zero is a
row that is gone. A handler decodes its request body into an input struct
and copies the fields it accepts onto the model — never straight into the
model, which would let the client choose the key and every other column.
The [quickstart](../getting-started/quickstart.md) has the complete slice,
controller included.

## Plain SQL when you want it

`--data sql` on the generators, or your own code, writes `database/sql`
statements against the same pool and the same model. `pkg/db` exposes the
underlying `*sql.DB` plus thin helpers — `Scan`/`ScanAll` for
repository-style code, and a `Tx` wrapper that propagates context and
panics safely. There is no query builder in the framework.

```go
rows, err := a.DB.Query(ctx,
    `SELECT id, title FROM notes WHERE deleted_at IS NULL ORDER BY id DESC`)
if err != nil {
    return err
}
defer rows.Close()
```

## SQL-first migrations

The schema is yours, in SQL. Quark reads and writes the tables; it does not
create them for you in a Nucleus application. Migrations are plain SQL
files under `migrations/`, applied in lexicographic order by the CLI:

```
migrations/
├── 000001_create_notes.up.sql
├── 000001_create_notes.down.sql
├── 000002_add_notes_index.up.sql
└── 000002_add_notes_index.down.sql
```

```bash
nucleus migrate up
nucleus migrate status
nucleus migrate steps 1
nucleus migrate down
```

Where the pair comes from:

- `nucleus generate module`, `generate resource` and `startapp` **render the
  pair for the configured engine** — the `CREATE TABLE` with the model's
  columns, and the `DROP TABLE` — beside the model they write. A `generate
  module` slice embeds its pair in the package and applies it on start
  through `rt.ApplyModuleMigrations()` (below); `generate resource` writes it
  to `migrations/` for `nucleus migrate up`.
- `nucleus makemigrations <name>` **renders the pair from your models**:
  when the project has `internal/models` (where the generators write
  Quark models), it runs the quark CLI — `quark migrate create
  --from-models internal/models --emit json`, for the configured
  database's dialect — and writes what it printed as a timestamped
  up/down pair whose header names the quark that rendered it. The quark
  CLI is `quark` on PATH, else `go run
  github.com/jcsvwinston/quark/cmd/quark@latest` (a download the command
  announces), and `NUCLEUS_QUARK_CLI` pins one; it needs cmd/quark
  v1.3.0 or later, and an older one is reported with that recipe. The
  same is `nucleus migrate create <name> --from-models <dir>` with any
  directory, and `--dialect` picks the engine without a configuration.
  The pair is a snapshot of the models as they are: a column you add
  later is a migration you write, and the `CREATE TABLE` it renders is
  the whole table, so render once per model and hand-write the `ALTER`s.
- `nucleus migrate create <name>` without `--from-models`, and `nucleus
  generate migration <name>`, write an **empty** up/down pair with a
  timestamp prefix for you to fill in. So does `makemigrations` in a
  project with no `internal/models`, or one whose structs carry no `db`
  tag — it says so on stdout.
- `nucleus migrate drift` reports an applied migration whose `.up.sql` file
  has since left the disk.

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
embedded one, say), and returns an error for a module name holding `/` or
NUL. `db.NewModuleMigrator` and `db.NewModuleFSMigrator`, which panic on
such a name, are deprecated in its favour.

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
disk ones. Nothing bypasses the ledger. The
boot-time WARN about declared-but-unapplied embedded migrations is
suppressed once the module applies them this way, and `nucleus migrate
status` lists those rows under their `articles/` namespace after the
directory plan — `db.Migrator.Applied` is the call behind that view, the
whole ledger with each row's namespace.

Application boot itself still never mutates the schema on its own: the
call above is module code you choose to write, and the alternative —
shipping the SQL as disk files for `nucleus migrate up` — remains fully
supported. Under the hood this is `db.NewModuleFSMigrator`, usable
directly for custom flows.

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

Even with disciplined SQL migrations, the live database and your models can
fall out of sync: an ad-hoc `ALTER TABLE` in production, a column rename
that landed in code but not in a migration, or a brand-new model nobody
migrated. The check is independent of the model layer: it compares the
schema with the tables and columns you say you expect.

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

## Test data: `nucleustest.Make`

A test that boots the application with `pkg/nucleustest` persists one
record of a model with `nucleustest.Make[T](srv)`: strings become
`<field>-<n>`, numbers `n`, times now, from a per-process sequence, and the
key comes back filled. A Quark model is written through Quark itself, by
the factory of the `pkg/nucleustest/quarkmake` module — a module of its
own, so the framework's `go.mod` does not depend on Quark — installed once
per test server; the table has to exist (Quark's `Migrate`, or
`srv.MigrateDir`). `MakeN` makes several and an override function sets what
the test cares about. [Getting started → Testing](../getting-started/testing.md#test-data-with-one-call)
has the details and the registered models it still reads from the registry.

```go
client, err := quark.NewWithDB("sqlite", srv.DB())
if err != nil { t.Fatal(err) }
srv.UseFactory(quarkmake.New(client, quarkmake.Model[Note]()))
note := nucleustest.Make[Note](srv, func(n *Note) { n.Title = "first" })
```

## `pkg/model`, the substrate that leaves in 2.0

:::caution `pkg/model` leaves the public API at v2.0.0 (DEP-2026-024)
The package, `BaseModel`, `NewRegistry` and `NewCRUD` are marked
`Deprecated`. Quark is the data layer of the suite, and keeping two means
two tag vocabularies and two ways to write a model. Nothing is removed in
v1.x: an application built on `pkg/model` keeps working, its registry keeps
feeding the admin panel, and the contract freeze still covers every symbol
until the major. New code is written on Quark. The migration assistant is
`docs/migration_assistants/MA-2026-024-pkg-model-to-quark.md` in the
repository, and `docs/pkg-model-vs-quark.md` is the symbol-by-symbol
inventory of what each layer does.
:::

What the package still is, for the code that has it:

- **The registry.** `a.Models.Register(&Article{})` extracts metadata by
  reflection and drives a generic CRUD operator over models the caller
  cannot name at compile time — which is what the admin panel needs to
  operate on any registered model, and why the operator works over
  `interface{}` at all. A module's `Models []any` field registers its
  structs there. A Quark model is not registered: with Quark's tags the
  registry would read every `db:"<column>"` as an unknown directive, and
  the panel reads Quark models through `quarkdatasource` instead.
- **A different tag grammar.** `pkg/model` reads `db:` as directives
  separated by `;` — `db:"column:title;required"`, `db:"pk"`,
  `db:"fk:users.id;index"`, `db:"readonly"`, `db:"tenant"`, `db:"-"` to
  exclude a field — and, without a `column:` directive, derives the column
  from the field name (`AuthorID` → `author_id`). Quark reads `db:` as the
  column name with comma-separated options. The two contradict each other
  on the same struct, so a model belongs to one layer.
- **The `WARN` for an unknown directive.** A `db:` directive the parser
  does not recognise is applied as nothing, and the framework says so: at
  startup every registered field carrying one produces a `WARN` naming the
  model, the field and the ignored text, and names Quark's grammar when the
  tokens look like it. A typo never silently becomes a missing constraint;
  a Quark model registered by mistake is announced the same way.
- **`AutoMigrate` (dev-mode only).** `(*app.App).AutoMigrate(models ...any)`
  derives idempotent `CREATE TABLE` statements from `pkg/model` tags for the
  five engines and runs them, returning `db.ErrAutoMigrate` only for an
  unknown driver. It never alters an existing table, it bypasses the
  migration ledger, and it reads the registry's grammar — so it is for a
  `pkg/model` struct in development, not for a Quark model and not for
  production. Explicit SQL migrations are the path with compatibility
  guarantees.

```go
import "github.com/jcsvwinston/nucleus/pkg/model"

a.Models.Register(&Article{}, model.ModelConfig{RejectClientPK: true})
```

### How `Create` treats the primary key

This is the registry's `Create`, the operator the admin panel drives. A
zero-value key stays out of its `INSERT` and the database
generates it (back-filled onto the entity where the engine supports that;
Oracle does not back-fill, and SQL Server rejects the back-fill mechanism on
tables with triggers). A **non-zero key travels in the `INSERT`** — so a
handler that decoded a request body straight into the entity let the HTTP
client choose the row's key. Registering with
`model.ModelConfig{RejectClientPK: true}` makes `Create` refuse an entity
that arrives carrying one, with `model.ErrClientAssignedPK` (check with
`errors.Is`); the check runs before hooks, so a `BeforeCreate` hook that
assigns a server-generated key keeps working. The Quark shape avoids the
problem by construction: the handler decodes into an input struct and
copies the accepted fields onto the model.
