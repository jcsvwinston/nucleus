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
endpoints. Start here if you have the CLI installed and want to see Nucleus
work before reading about how it works.

## 1 — Scaffold a project

```bash
nucleus new myapp
cd myapp
go mod tidy
```

`nucleus new` writes a **minimal empty skeleton**: a composition-root
`main.go`, `nucleus.yml`, `.gitignore`, `README.md`, and an empty
`migrations/` directory. No `replace` directive, no local clone of Nucleus
required.

The skeleton has no feature code yet. It starts the server, serves
`/healthz`, and waits for you to add modules.

## 2 — Run the skeleton

```bash
go run .   # start the server; the skeleton serves /healthz
```

By default the server listens on the port configured in `nucleus.yml`
(default `8080`). No migrations are needed until you add a feature with a
database model.

Any config key can be overridden from the environment without editing
`nucleus.yml`: prefix it with `NUCLEUS_` (nested keys use `__` as the
separator):

```bash
NUCLEUS_PORT=8081 go run .
```

The full precedence rules live in the
[`CONFIG_KEY_REGISTRY`](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CONFIG_KEY_REGISTRY.md).

## 3 — Add a feature: write a module and Mount it

All application behaviour lives in **modules**. A module is a
`nucleus.Module[C]` value carrying four things: a name, optional models, a
startup hook (`OnStart`), and a route registration function (`Routes`). You
write the module, then hand it to the framework with `.Mount()` in `main.go`.

The code below comes from the `examples/mvc_api` reference application — a
complete `notes` REST resource you can copy as the shape of your own first
module. It is **not** what `nucleus new` generates: the scaffold stays empty
so the first module is entirely yours.

**Entry point (`main.go` — from `examples/mvc_api`)**

```go file=<rootDir>/examples/mvc_api/main.go
```

Note the blank import of `drivers/sqlite`. The framework links no database
driver — each engine is a module the application imports — and your
scaffold's `main.go` already carries that line; keep it when you rewrite
the file (or run `nucleus add sqlite` to put it back). Without it the build
still succeeds and startup stops with the import to add.

**Module definition (`internal/notes/module.go` — from `examples/mvc_api`)**

```go file=<rootDir>/examples/mvc_api/internal/notes/module.go
```

**Controller (`internal/notes/controller.go` — from `examples/mvc_api`)**

```go file=<rootDir>/examples/mvc_api/internal/notes/controller.go
```

**Model (`internal/notes/note.go` — from `examples/mvc_api`)**

Both files above use the `Note` model and its `noteRow` scan helper — without
this file the module does not compile:

```go file=<rootDir>/examples/mvc_api/internal/notes/note.go
```

**Migrations (`migrations/` — from `examples/mvc_api`)**

The controller queries a `notes` table, so the project needs its migration
pair before step 4:

```sql file=<rootDir>/examples/mvc_api/migrations/001_create_notes.up.sql
```

```sql file=<rootDir>/examples/mvc_api/migrations/001_create_notes.down.sql
```

Those five files are the complete slice. If you copy the example's
`notes_test.go` too, rewrite its import path first — it imports
`…/examples/mvc_api/internal/notes`, which is an `internal` path of the
framework repository and will not resolve from your module:

```bash
sed -i '' 's#github.com/jcsvwinston/nucleus/examples/mvc_api#github.com/acme/myapp#' internal/notes/notes_test.go
```

### What the fluent builder does

`nucleus.New()` returns an `*AppBuilder`. Each method returns the same builder
so calls can be chained:

| Method | Effect |
|--------|--------|
| `.FromConfigFile(path)` | Load `nucleus.yml` (or `nucleus.yaml`); merges left-to-right when called with multiple paths. |
| `.WithoutDefaults()` | Skip the optional built-ins (storage, mail, authz): nothing is mounted or enforced. It is a runtime flag, not a build flag, so the binary is the same size either way. The `api` skeleton uses it; the `mvc` skeleton does not. |
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

`(*app.App).AutoMigrate(models ...any)` derives idempotent `CREATE TABLE`
statements from your struct tags and runs them against the configured
database. It is a development convenience — **in production, use explicit
SQL migrations instead**.

What it does:

- Supports five dialects — **SQLite, PostgreSQL, MySQL, MSSQL and Oracle** —
  each through its own deterministic scaffold builder in
  [`pkg/model`](https://github.com/jcsvwinston/nucleus/blob/main/pkg/model).
- Is always safe to re-run. On SQLite/Postgres/MySQL it emits `CREATE TABLE
  IF NOT EXISTS`; on MSSQL it wraps the CREATE in `IF OBJECT_ID(..., 'U') IS
  NULL`; on Oracle it wraps it in a PL/SQL block that swallows `ORA-00955`
  ("name is already used by an existing object").
- Returns `db.ErrAutoMigrate` only for unknown drivers.

What it does **not** do: alter existing tables. It is `CREATE IF NOT EXISTS`
only. For production schema evolution, prefer explicit SQL migration files
(`migrations/*.up.sql` plus `nucleus migrate`): they are reversible,
reviewable in PR diffs, and the only path the framework offers compatibility
guarantees on. `nucleus migrate drift` will surface any applied migration
that has since lost its `.up.sql` file on disk.

:::

## 4 — Run a migration

For non-trivial apps, write SQL migrations under `migrations/` and apply
them with the CLI:

```bash
nucleus migrate up      # apply pending migrations
nucleus migrate status  # show plan vs. applied
nucleus migrate down    # roll back the most recent batch
```

## Add the admin panel (optional)

The admin panel is not part of the core — it ships as the separate
[orbit](https://github.com/jcsvwinston/orbit) module and mounts like any
other module:

```bash
go get github.com/jcsvwinston/orbit
```

```go
import "github.com/jcsvwinston/orbit"

nucleus.New().
    FromConfigFile("nucleus.yml").
    Mount(notes.Module()).
    Mount(orbit.Module(orbit.Config{Prefix: "/admin"})).
    Start()
```

Start the app once so orbit creates its `nucleus_admin_users` table, then
create the admin account:

```bash
nucleus createuser
```

Two things `nucleus createuser` is honest about:

- It manages **orbit admin accounts** (the `nucleus_admin_users` table),
  not your application's users. Run before orbit has been mounted and
  started once, it fails with an error saying exactly that.
- Your application's own users are a different story: you implement
  `auth.UserProvider` over your own table. The full worked slice is
  [Auth → Your first login](../features/auth/your-first-login.md).

See the orbit repository for the panel's configuration and its own
quick start.

## A note on CSRF

The mvc scaffold ships with `csrf_enabled: true` in `nucleus.yml`. Browser
routes — the session-cookie ones — are protected by Sec-Fetch-Site origin
verification, with a double-submit token as the fallback. `/api/` is exempt
via `csrf_exempt_paths`, so Bearer-token and curl/SDK clients keep working.

Two practical consequences:

- **HTML forms must embed the token.** Render it with
  `router.CSRFToken(r)` into a hidden `_csrf_token` field (or send it in
  the `X-CSRF-Token` header from JavaScript).
- **Non-browser POSTs outside `/api/` are rejected** with status 419
  unless they carry the token. Put programmatic endpoints under an exempt
  prefix, or extend `csrf_exempt_paths`.

The api scaffold leaves CSRF off — a pure Bearer-token API is not
CSRF-forgeable. See the
[CSRF guide](https://github.com/jcsvwinston/nucleus/blob/main/docs/guides/CSRF_GUIDE.md)
for the full option surface.

## A note on the default-deny gate

The mvc scaffold mounts a **default-deny RBAC gate** over every route.
The first time you add a session-authenticated route and it answers 403,
that is the gate working as designed — read
[Auth → Your first 403](../features/auth/rbac-and-middleware.md#your-first-403)
before fighting it: the fix is two policy rows, not more middleware.

## Next steps

- **[Project structure](./project-structure.md)** — how a scaffolded
  project is laid out.
- **[Auth → Your first login](../features/auth/your-first-login.md)** —
  the complete login slice: users table, `UserProvider`, `POST /login`,
  session, logout.
- **[Features → Using Quark](../features/using-quark.md)** — the suite's
  ORM as your data layer, instead of the raw SQL shown above.
- **[Concepts → Application](../concepts/application.md)** — how the
  application container is wired up (`pkg/app` and `pkg/nucleus`).
- **[Concepts → Configuration](../concepts/configuration.md)** — the
  `nucleus.yml` schema and multi-file loader.
