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

This page shows what `nucleus new myapp` writes to disk, where your own code
goes, and how to choose between the two supported layouts.

The scaffold is a **minimal empty skeleton**: a composition root, config, a
`.gitignore`, a `README.md`, and an empty `migrations/` directory. It
generates no feature code — there is no `internal/<resource>/` tree. The
skeleton runs immediately and serves the framework's built-in `/healthz`
endpoint with no modules mounted. You add features by writing modules and
calling `.Mount()`.

## Skeleton layout — `api` template (lightweight, core-only)

```
myapp/
├── main.go          # Composition root — nucleus.New().FromConfigFile("nucleus.yml").WithoutDefaults().Start()
├── nucleus.yml      # Runtime configuration (port, databases.default.url, …)
├── migrations/      # Empty — add *.up.sql / *.down.sql here as you build features
├── go.mod
├── go.sum           # written by nucleus new (absent with --offline until go mod tidy)
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
├── go.sum           # written by nucleus new (absent with --offline until go mod tidy)
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

```go
// Command mvc_api is the Nucleus mvc_api reference application.
//
// It demonstrates the canonical three-surface fluent builder pattern
// with a single REST resource: notes.
//
// # Quick start
//
//	cd examples/mvc_api
//
//	# 1. Run migrations (creates the notes table in examples_mvc_api.db)
//	nucleus migrate --config config/nucleus.yaml --migrations migrations up
//
//	# 2. Start the server
//	go run .
//
//	# 3. Try the API
//	curl -s http://localhost:8090/notes | jq .
//	curl -s -X POST http://localhost:8090/notes \
//	    -H 'Content-Type: application/json' \
//	    -d '{"title":"hello","body":"world"}' | jq .
package main

import (
	"log"

	"github.com/jcsvwinston/nucleus/examples/mvc_api/internal/notes"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	// The framework links no database driver: each ships as its own module
	// and the application imports the one it uses, the way
	// database/sql drivers have always been wired. Drop this line and the
	// build still succeeds — startup then stops with the line to add back.
	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)

func main() {
	err := nucleus.New().
		FromConfigFile("config/nucleus.yaml").
		// No WithoutDefaults() here (DX-11): this example is the model the
		// quickstart tells you to copy onto the mvc scaffold, so it runs
		// with the same barriers the scaffold turns on — default-deny authz
		// and CSRF. Its config supplies the policy rows and the CSRF
		// exemption; copy those too if you write your own config.
		Mount(notes.Module()).
		Start()
	if err != nil {
		log.Fatalf("mvc_api: %v", err)
	}
}
```

> **Running the example.** `examples/mvc_api` is its own Go module — the
> shape of a real application, down to the driver import. Run it **from
> its directory** (`cd examples/mvc_api && go run .`): it resolves its
> SQLite database and config through paths relative to the working
> directory, so running it from anywhere else breaks those paths. The same
> rule applies to your own app: relative `databases.default.url` and
> `--config` paths resolve from the process working directory.

## Two layouts, and when to use each

Nucleus supports two project layouts. Both compile and run identically, so
the choice is purely organisational — the framework does not push you toward
either one.

### Feature-folder (module) layout

Groups code by feature: one package per module under `internal/<feature>/`.
This is the layout shown above, and the one `examples/mvc_api` uses. Each
feature owns its routes, controller, model, and service behind a single
`Module` you `.Mount(...)`.

Use it when features are cohesive units you want to add, move, or remove as
a whole.

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
each resource for you.

You can mix the two: start layered, then extract a feature folder when a
feature grows its own surface.

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
