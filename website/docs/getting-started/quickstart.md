---
sidebar_position: 2
title: Quickstart
---

# Quickstart

Five minutes from zero to a running app with a database, a model, and an
embedded admin panel.

## 1 — Scaffold a project

```bash
nucleus new myapp
cd myapp
go mod tidy
```

`nucleus new` writes a self-contained Go module. There is no `replace`
directive, no local clone of Nucleus required.

## 2 — Run the server

```bash
nucleus serve
```

Four endpoints are now live:

| URL                                | Purpose                          |
| ---------------------------------- | -------------------------------- |
| `http://localhost:8080/`           | The web app                      |
| `http://localhost:8080/api/...`    | Auto-mounted REST endpoints      |
| `http://localhost:8080/admin`      | Embedded admin panel             |
| `http://localhost:8080/healthz`    | Liveness/readiness checks        |

The default config (`nucleus.yml`) uses SQLite at `app.db`. Override the
database with environment variables or by editing `nucleus.yml`.

## 3 — A minimal API in code

The canonical entry point is `pkg/nucleus`. The fluent builder pattern
assembles an application from modules:

```go
package main

import (
    "log"

    "github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func main() {
    err := nucleus.New().
        FromConfigFile("nucleus.yml").
        Mount(articlesModule.Build()).
        Start()
    if err != nil {
        log.Fatal(err)
    }
}
```

Where `articlesModule` is a `nucleus.Module[C]` value — the typed
module constructor:

```go
var articlesModule = nucleus.Module[struct{}]{
    Name:   "articles",
    Prefix: "/api/articles",
    Routes: func(r nucleus.Router, _ struct{}) {
        r.Get("/", listArticles)
        r.Post("/", createArticle)
        r.Get("/{id}", showArticle)
        r.Put("/{id}", updateArticle)
        r.Delete("/{id}", deleteArticle)
    },
}
```

For CRUD modules, `Router.Resource` is a concise alternative:

```go
Routes: func(r nucleus.Router, _ struct{}) {
    r.Resource("/", ticketsController{}, nucleus.Methods(
        nucleus.Index,
        nucleus.Show,
        nucleus.Create,
        nucleus.Update,
        nucleus.Destroy,
    ))
},
```

`nucleus.Methods(...)` selects which REST verbs to register. A missing
controller method for a requested verb panics at startup rather than
silently producing a 404.

### Direct-struct surface (tests and programmatic embedding)

```go
err := nucleus.Run(nucleus.App{
    Config: app.Config{Port: 8080},
    Modules: map[string]nucleus.ModuleSpec{
        "articles": articlesModule.Build(),
    },
})
```

### Global middleware

```go
nucleus.New().
    FromConfigFile("nucleus.yml").
    Use(middleware.Logger(), middleware.Recover()).
    Mount(articlesModule.Build()).
    Start()
```

`Use(...)` appends middleware applied to all routes before any module
routes are registered. Per-module middleware lives on `Module[C].Middleware`.

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
nucleus migrate         # apply pending migrations
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
