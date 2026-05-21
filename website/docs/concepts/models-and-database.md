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
config_keys:
  - databases.default
  - database_default
---

# Models & database

Nucleus's data layer is **SQL-first** by design. There is no ORM, no
auto-magical query builder. `pkg/db` wraps `database/sql` with
production-quality defaults; `pkg/model` adds metadata, registry and a
generic CRUD operator on top.

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

When `multi_tenant.enabled: true` is set, `App.DatabaseForRequest(r)`
resolves the database — and the tenant scope — for the current request.
The admin panel auto-filters by tenant; for application code, you scope
queries explicitly:

```go
db := a.DatabaseForRequest(r)
tenantID := router.TenantFrom(r.Context())
rows, _ := db.Query(ctx, `SELECT * FROM articles WHERE tenant_id = ?`, tenantID)
```

## Health and telemetry

`pkg/db` registers health checks and emits OpenTelemetry spans for every
query when OTel is enabled. The admin panel surfaces the connection
pool, the migration plan and the latest health status under
`/admin/system`.
