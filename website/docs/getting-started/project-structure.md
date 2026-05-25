---
sidebar_position: 3
title: Project structure
covers:
  - pkg/nucleus.New
  - pkg/nucleus.AppBuilder.WithoutDefaults
  - pkg/nucleus.Module
  - pkg/nucleus.Runtime
config_keys:
  - env
  - databases.default
---

# Project structure

`nucleus new myapp` produces a self-contained Go module. The scaffolder
generates one of two templates depending on the `--template` flag you pass.

## `api` template (default for REST-only apps)

```
myapp/
├── main.go                          # Composition root — nucleus.New().FromConfigFile(...).Mount(...).Start()
├── internal/
│   └── <resource>/
│       ├── module.go                # nucleus.Module[C] — OnStart wires rt.DB(); Routes registers r.Resource(...)
│       ├── controller.go            # Handler methods (Index, Show, Create, Update, Destroy)
│       ├── <resource>.go            # Domain model (embeds model.BaseModel)
│       ├── service.go               # Business logic (optional)
│       └── repository.go            # SQL access via *sql.DB (optional)
├── migrations/
│   ├── 001_create_<resource>.up.sql
│   └── 001_create_<resource>.down.sql
├── config/
│   └── nucleus.yaml                 # Runtime configuration (port, databases.default.url, …)
├── go.mod
└── go.sum
```

The module struct in `module.go` is the seam between the framework and your
domain code. It is imported directly in `main.go` — there is no `cmd/server/`
indirection:

```go file=<rootDir>/examples/mvc_api/main.go
```

## `mvc` template (full-stack with admin + RBAC)

The `mvc` template extends `api` with web views, an admin panel, and a
Casbin RBAC policy file:

```
myapp/
├── main.go                          # Same fluent pattern; no WithoutDefaults()
├── internal/
│   ├── web/                         # Home / catch-all web module (MVC views)
│   │   └── module.go
│   └── <resource>/
│       ├── module.go
│       ├── controller.go
│       ├── <resource>.go
│       ├── service.go
│       └── repository.go
├── migrations/
├── seeds/                           # Optional seed data scripts
├── config/
│   └── nucleus.yaml
├── rbac_policy.csv                  # Casbin RBAC policy (scoped to this app)
├── go.mod
└── go.sum
```

## What goes where

| Path | Purpose |
|------|---------|
| `main.go` | Composition root. Calls `nucleus.New()` and mounts modules. The entry point for `go run .`. |
| `internal/<resource>/module.go` | `nucleus.Module[C]` value — `OnStart` captures `rt.DB()`, `Routes` registers `r.Resource(...)`. |
| `internal/<resource>/controller.go` | HTTP handlers. One file per resource keeps the diff surface small. |
| `internal/<resource>/<resource>.go` | Domain model struct (embeds `model.BaseModel`). |
| `internal/<resource>/service.go` | Orchestration above repositories. The place to enforce invariants. |
| `internal/<resource>/repository.go` | SQL access. Uses `*sql.DB` directly — no ORM. |
| `internal/web/` | MVC-mode web module: home routes, template rendering (mvc template only). |
| `migrations/` | SQL files named `001_create_<resource>.up.sql` / `.down.sql`. |
| `seeds/` | Optional seed data (mvc template only). |
| `config/nucleus.yaml` | Single source of truth for runtime configuration. |
| `rbac_policy.csv` | Casbin RBAC CSV policy (mvc template only). |

## Templates

`nucleus new` accepts a `--template` flag:

| Template | Defaults |
|----------|---------|
| `api` | REST only — `nucleus.New().WithoutDefaults()` (no admin, no authz, no mail, no storage). |
| `mvc` | Full stack — controllers, services, repos, views, admin panel, RBAC. |

```bash
nucleus new myapp --template api
nucleus new myapp --template mvc
```

## Why this layout

- **Composition root at `main.go`** — no `cmd/server/` nesting. `go run .`
  is the single start command regardless of template.
- **`internal/<resource>/`** — each resource is a self-contained package.
  Refactors stay private until you decide otherwise.
- **`migrations/`** at the top level means SQL is reviewable as data, not
  embedded in code, and the CLI can manage it without reflection.
- **`config/nucleus.yaml`** keeps configuration out of source files — the
  only Go-level configuration is the call to `nucleus.New()`.
- **`nucleus.Module[C]`** is the single seam: `OnStart` runs before
  `Routes`, so the database handle captured via `rt.DB()` is always
  non-nil when routes are registered.
