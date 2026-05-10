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

If you prefer a single-file app to a scaffolded project, use the fluent
entry point:

```go
package main

import (
    "github.com/jcsvwinston/nucleus/pkg/nucleus"
)

type Article struct {
    ID    int64  `json:"id"    db:"id,primary"`
    Title string `json:"title" db:"title" validate:"required"`
}

func main() {
    nucleus.New().
        Port(8080).
        SQLite("app.db").
        Model(&Article{}).
        AutoMigrate().
        Get("/api/articles", func(c *nucleus.Context) error {
            return c.JSON(200, []Article{{ID: 1, Title: "Hello"}})
        }).
        Run()
}
```

`nucleus.New()` returns a builder. The terminal call (`Run`) constructs
the application container, opens the database, applies the migration plan
and starts the HTTP server. Every step is explicit; nothing happens at
import time.

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

- **[Project structure](./project-structure)** — how a scaffolded project
  is laid out.
- **[Concepts → Application](../concepts/application)** — what
  `pkg/nucleus.New()` actually wires up.
- **[Concepts → Configuration](../concepts/configuration)** — the
  `nucleus.yml` schema.
