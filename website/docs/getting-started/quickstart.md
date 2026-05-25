---
sidebar_position: 2
title: Quickstart
covers:
  - pkg/nucleus.New
  - pkg/nucleus.Run
  - pkg/nucleus.App
  - pkg/nucleus.AppBuilder
  - pkg/nucleus.AppBuilder.FromConfigFile
  - pkg/nucleus.AppBuilder.Mount
  - pkg/nucleus.AppBuilder.Start
  - pkg/nucleus.AppBuilder.Use
  - pkg/nucleus.AppBuilder.WithoutDefaults
  - pkg/nucleus.Module
  - pkg/nucleus.Methods
  - pkg/nucleus.Router
  - pkg/nucleus.Runtime
  - pkg/app.App.AutoMigrate
  - pkg/db.ErrAutoMigrate
config_keys:
  - databases.default
  - port
---

# Quickstart

Five minutes from zero to a running app with a database, a model, and REST
endpoints.

## 1 — Scaffold a project

```bash
nucleus new myapp
cd myapp
go mod tidy
```

`nucleus new` writes a **minimal empty skeleton** — a composition-root `main.go`,
`nucleus.yml`, `.gitignore`, `README.md`, and an empty `migrations/` directory.
There is no `replace` directive; no local clone of Nucleus required. The
skeleton has no feature code yet: it starts the server, serves `/healthz` (and
`/admin` for the `mvc` template), and waits for you to add modules.

## 2 — Run the skeleton

```bash
go run .   # start the server; the skeleton serves /healthz (and /admin for mvc)
```

By default the server listens on the port configured in `nucleus.yml`
(default `8080`). No migrations are needed until you add a feature with a
database model.

## 3 — Add a feature: write a module and Mount it

All application behaviour lives in **modules**. A module is a
`nucleus.Module[C]` value that carries a name, optional models, a lifecycle
hook (`OnStart`), and a route registration function (`Routes`). You write the
module, then tell the framework about it via `.Mount()` in `main.go`.

The code below is imported from the canonical `examples/mvc_api` reference
application. It is a complete worked example of a `notes` REST resource — use
it as the model for your own first module. It is **not** what `nucleus new`
generates; the scaffold is intentionally empty so you own the first module
entirely.

**Entry point (`main.go` — from `examples/mvc_api`)**

```go file=<rootDir>/examples/mvc_api/main.go
```

**Module definition (`internal/notes/module.go` — from `examples/mvc_api`)**

```go file=<rootDir>/examples/mvc_api/internal/notes/module.go
```

**Controller (`internal/notes/controller.go` — from `examples/mvc_api`)**

```go file=<rootDir>/examples/mvc_api/internal/notes/controller.go
```

### What the fluent builder does

`nucleus.New()` returns an `*AppBuilder`. Each method returns the same builder
so calls can be chained:

| Method | Effect |
|--------|--------|
| `.FromConfigFile(path)` | Load `nucleus.yml` (or `nucleus.yaml`); merges left-to-right when called with multiple paths. |
| `.WithoutDefaults()` | Skip optional built-ins (admin, storage, mail, authz). Produces a lean binary. The `api` skeleton includes this; the `mvc` skeleton does not. |
| `.Mount(spec)` | Register a `nucleus.ModuleSpec` — its `OnStart` and `Routes` are called by the framework. |
| `.Start()` | Block until the server exits; returns the first non-nil error. |

### Module lifecycle

A `nucleus.Module[C]` carries four concerns in one value:

- `Models []any` — structs the framework registers with the model registry.
- `OnStart func(ctx, rt nucleus.Runtime, cfg C) error` — called before
  `Routes`; use `rt.DB()` to capture the framework-managed `*sql.DB`.
- `Routes func(r nucleus.Router, cfg C)` — registers HTTP handlers; runs
  after `OnStart`, so `m.db` is already populated.
- No `OnShutdown` needed here: the framework owns the managed connection
  pool and closes it at shutdown.

Call `.Build()` on the `Module[C]` struct to produce the `nucleus.ModuleSpec`
accepted by `.Mount(...)`.

### Direct-struct surface (tests and programmatic embedding)

```go
err := nucleus.Run(nucleus.App{
    Modules: map[string]nucleus.ModuleSpec{
        "notes": notes.Module(),
    },
})
```

### Global middleware

```go
nucleus.New().
    FromConfigFile("nucleus.yml").
    Use(middleware.Logger(), middleware.Recover()).
    Mount(notes.Module()).
    Start()
```

`Use(...)` appends middleware applied to all routes before module routes are
registered. Per-module middleware lives on `Module[C].Middleware`.

:::info AutoMigrate (dev-mode only)

`(*app.App).AutoMigrate(models ...any)` derives idempotent
`CREATE TABLE` statements from struct tags and runs them against the
configured database. Five dialects are supported: **SQLite, PostgreSQL,
MySQL, MSSQL, and Oracle** — each via its own deterministic scaffold
builder in
[`pkg/model`](https://github.com/jcsvwinston/nucleus/blob/main/pkg/model).
On SQLite/Postgres/MySQL the generated SQL uses `CREATE TABLE IF NOT
EXISTS`; on MSSQL it wraps the CREATE in `IF OBJECT_ID(..., 'U') IS
NULL`; on Oracle it wraps it in a PL/SQL block that swallows `ORA-00955`
("name is already used by an existing object"). Either way the operation
is safe to re-run.

`AutoMigrate` returns `db.ErrAutoMigrate` only for unknown drivers.

`AutoMigrate` does **not** alter existing tables — it is
`CREATE IF NOT EXISTS` only. For production schema evolution, prefer
explicit SQL migration files (`migrations/*.up.sql` plus
`nucleus migrate`): they are reversible, reviewable in PR diffs, and the
only path the framework offers compatibility guarantees on.
`nucleus migrate drift` will surface any applied migration that has since
lost its `.up.sql` file on disk.

:::

## 4 — Run a migration

For non-trivial apps, write SQL migrations under `migrations/` and apply
them with the CLI:

```bash
nucleus migrate up      # apply pending migrations
nucleus migrate status  # show plan vs. applied
nucleus migrate down    # roll back the most recent batch
```

## 5 — Create an admin user

```bash
nucleus createuser
```

Prompts for username, email and password. The user goes into the auth
table referenced by your `nucleus.yml`. You can now sign in to the admin
panel at `/admin`.

## Next steps

- **[Project structure](./project-structure.md)** — how a scaffolded
  project is laid out.
- **[Concepts → Application](../concepts/application.md)** — how the
  application container is wired up (`pkg/app` and `pkg/nucleus`).
- **[Concepts → Configuration](../concepts/configuration.md)** — the
  `nucleus.yml` schema and multi-file loader.
