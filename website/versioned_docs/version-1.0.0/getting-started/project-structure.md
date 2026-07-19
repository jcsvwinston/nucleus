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
(`/healthz`) with no modules mounted. You add features by writing modules and
calling `.Mount()`.

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

The `api` skeleton calls `.WithoutDefaults()`: no Casbin enforcer, no
storage, no mail. Routes are unauthenticated until you add access control.

## Skeleton layout — `mvc` template (full-stack with RBAC)

```
myapp/
├── main.go          # Composition root — nucleus.New().FromConfigFile("nucleus.yml").Start()
├── nucleus.yml      # Runtime configuration (includes rbac_policy_file)
├── rbac_policy.csv  # Casbin policy; grants anonymous access to built-in endpoints
├── migrations/      # Empty
├── go.mod
├── go.sum           # (after go mod tidy)
└── .gitignore
```

The `mvc` skeleton omits `.WithoutDefaults()`: a default-deny Casbin
enforcer is active. `rbac_policy.csv` grants public access to the built-in
health endpoint; widen it as you add your own routes. The admin panel
(orbit) is not included in the scaffold — mount it explicitly with
`.Mount(orbit.Module(...))` when you need it.

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

> **Running the example.** `examples/mvc_api` resolves its SQLite database and
> config via paths relative to the working directory, so it is meant to be run
> **from the repository root** (`go run ./examples/mvc_api`). Running it from
> another directory breaks the relative database/config paths. Apply the same
> care in your own app: relative `databases.default.url` and `--config` paths
> resolve from the process working directory.

## Two layouts, and when to use each

Nucleus supports two project layouts. Both compile and run identically — the
choice is organisational, and the framework does not push you toward either
one.

### Feature-folder (module) layout

The layout shown above — and used by `examples/mvc_api` — groups code by
feature, one package per module under `internal/<feature>/`. Each feature owns
its routes, controller, model, and service behind a single `Module` you
`.Mount(...)`. Use it when features are cohesive units you want to add, move,
or remove as a whole, and when you want a feature's routes, model, and service
to live together.

### Layered layout (generate resource)

`nucleus generate resource` instead emits a layout grouped by architectural
role:

```
internal/
├── models/        # data structures and persistence
├── controllers/   # HTTP handlers
├── services/      # business logic
├── repositories/  # SQL access
└── contracts/     # request/response types
```

Use it when you prefer role-based folders and want the generator to scaffold
each resource for you. You can mix the two — start layered and extract a
feature folder when a feature grows its own surface.

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
| `api` | REST only — `nucleus.New().WithoutDefaults()` (no authz, no mail, no storage). |
| `mvc` | Full stack — RBAC enforcer, built-in endpoints. Mount orbit for the admin panel. |

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
