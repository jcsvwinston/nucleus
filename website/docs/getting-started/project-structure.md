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

`nucleus new myapp` produces a **minimal empty skeleton** — a composition root,
config, a `.gitignore`, a `README.md`, and an empty `migrations/` directory.
It does **not** generate any feature code (no `internal/<resource>/` tree).
The skeleton runs immediately and serves the framework's built-in endpoints
(`/healthz`, plus `/admin` for the full `mvc` template) with no modules
mounted. You add features by writing modules and calling `.Mount()`.

## Skeleton layout — `api` template (lightweight, core-only)

```
myapp/
├── main.go          # Composition root — nucleus.New().FromConfigFile("nucleus.yml").WithoutDefaults().Start()
├── nucleus.yml      # Runtime configuration (port, databases.default.url, …)
├── migrations/      # Empty — add *.up.sql / *.down.sql here as you build features
├── go.mod
├── go.sum           # (after go mod tidy)
└── .gitignore
```

The `api` skeleton calls `.WithoutDefaults()`: no admin panel, no Casbin
enforcer, no storage, no mail. Routes are unauthenticated until you add
access control.

## Skeleton layout — `mvc` template (full-stack with admin + RBAC)

```
myapp/
├── main.go          # Composition root — nucleus.New().FromConfigFile("nucleus.yml").Start()
├── nucleus.yml      # Runtime configuration (includes admin_rbac_policy_file)
├── rbac_policy.csv  # Casbin policy; grants anonymous access to built-in endpoints
├── migrations/      # Empty
├── go.mod
├── go.sum           # (after go mod tidy)
└── .gitignore
```

The `mvc` skeleton omits `.WithoutDefaults()`: the admin panel mounts at
`/admin` and a default-deny Casbin enforcer is active. `rbac_policy.csv`
grants public access to the built-in health endpoint; widen it as you add
your own routes.

## Adding your first feature: the module layout

Once you have a skeleton running, add a feature by creating a module package
under `internal/`. Below is the layout from the
[`examples/mvc_api`](https://github.com/jcsvwinston/nucleus/tree/main/examples/mvc_api)
reference app — a single `notes` REST resource — which you can use as a
concrete model:

```
myapp/
└── internal/
    └── notes/
        ├── module.go       # nucleus.Module[C] — OnStart wires rt.DB(); Routes registers r.Resource(...)
        ├── controller.go   # Handler methods (Index, Show, Create, Update, Destroy)
        └── note.go         # Domain model struct (optional; embed model.BaseModel)
```

The module struct in `module.go` is the seam between the framework and your
domain code. Import it in `main.go` and pass it to `.Mount(...)`:

```go file=<rootDir>/examples/mvc_api/main.go
```

## What goes where

| Path | Purpose |
|------|---------|
| `main.go` | Composition root. Calls `nucleus.New()` and mounts modules. The entry point for `go run .`. |
| `nucleus.yml` | Single source of truth for runtime configuration (`port`, `databases.default.url`, …). |
| `migrations/` | SQL files named `001_create_<resource>.up.sql` / `.down.sql`. Managed by `nucleus migrate`. |
| `rbac_policy.csv` | Casbin RBAC CSV policy (`mvc` template only). |
| `internal/<resource>/module.go` | `nucleus.Module[C]` value — `OnStart` captures `rt.DB()`, `Routes` registers `r.Resource(...)`. |
| `internal/<resource>/controller.go` | HTTP handlers. One file per resource keeps the diff surface small. |
| `internal/<resource>/<resource>.go` | Domain model struct (embeds `model.BaseModel`). |
| `internal/<resource>/service.go` | Orchestration above repositories (optional). |
| `internal/<resource>/repository.go` | SQL access via `*sql.DB` (optional). |

## Templates

`nucleus new` accepts a `--template` flag:

| Template | Defaults |
|----------|---------|
| `api` | REST only — `nucleus.New().WithoutDefaults()` (no admin, no authz, no mail, no storage). |
| `mvc` | Full stack — admin panel, RBAC, built-in endpoints. Add modules to grow the app. |

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
- **`nucleus.yml`** keeps configuration out of source files — the
  only Go-level configuration is the call to `nucleus.New()`.
- **`nucleus.Module[C]`** is the single seam: `OnStart` runs before
  `Routes`, so the database handle captured via `rt.DB()` is always
  non-nil when routes are registered.
