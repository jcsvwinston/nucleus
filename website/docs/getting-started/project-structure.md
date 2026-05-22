---
sidebar_position: 3
title: Project structure
covers:
  - pkg/app.New
  - pkg/app.WithoutDefaults
  - pkg/app.WithExtensions
  - pkg/nucleus.New
  - pkg/nucleus.Module
config_keys:
  - env
  - database_default
---

# Project structure

`nucleus new myapp` produces a self-contained Go module with the layout
below. The structure follows the conventional `cmd/` + `internal/` split
and mirrors the Standard Go Project Layout.

```
myapp/
├── cmd/
│   └── server/
│       └── main.go              # HTTP entry point
├── internal/
│   ├── controllers/             # HTTP handlers (one file per resource)
│   ├── models/                  # Domain models with `db:` tags
│   ├── repositories/            # Data access on top of pkg/db
│   ├── services/                # Business logic
│   └── views/                   # html/template views (MVC mode)
├── migrations/                  # SQL migrations (up/down pairs)
├── static/                      # Static assets served by the router
├── templates/                   # Layout templates (MVC mode)
├── nucleus.yml                  # Canonical configuration
├── go.mod
└── go.sum
```

## What goes where

| Path             | Purpose                                                   |
| ---------------- | --------------------------------------------------------- |
| `cmd/server/`    | Composition root. Calls `pkg/nucleus.New()` or `pkg/app.New()`. |
| `internal/models/`     | Structs registered with the model registry. They drive admin, generic CRUD and metadata-aware migrations. |
| `internal/controllers/`| HTTP handlers. One file per resource keeps the diff surface small. |
| `internal/services/`   | Orchestration above repositories. The place to enforce invariants and emit events. |
| `internal/repositories/` | SQL access. Use `pkg/db` directly — no ORM. |
| `internal/views/`      | `html/template` views and partials when running in MVC mode. |
| `migrations/`          | SQL files named `0001_initial.up.sql` / `0001_initial.down.sql`. |
| `nucleus.yml`          | Single source of truth for runtime configuration. |

## Templates

`nucleus new` accepts a template flag:

| Template       | Defaults                                                |
| -------------- | ------------------------------------------------------- |
| `mvc` (default)| Full stack — controllers, services, repos, views, admin |
| `api`          | Core only — `pkg/app.New(app.WithoutDefaults())`        |

```bash
nucleus new myapp --template api
```

The `api` template skips the admin panel, MVC views and a couple of MVC-only
middleware. Use it when you want the smallest viable surface and plan to
attach extensions explicitly via `app.WithExtensions(...)`.

## Why this layout

- **`internal/`** keeps everything except `cmd/server` un-importable from
  outside the module. Refactors stay private until you decide otherwise.
- **`migrations/`** at the top level means the SQL is reviewable as data,
  not embedded in code, and the CLI can manage it without reflection.
- **`nucleus.yml`** at the top level keeps configuration out of source
  files — the only Go-level configuration is the call to
  `pkg/app.New(cfg)` or `pkg/nucleus.New()`.
