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
skeleton has no feature code yet: it starts the server, serves `/healthz`,
and waits for you to add modules.

## 2 — Run the skeleton

```bash
go run .   # start the server; the skeleton serves /healthz
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

**Module definition (`internal/notes/module.go` — from `examples/mvc_api`)**

```go
package notes

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// module holds the framework-managed database handle so the Routes closure
// can capture it directly. The handle is nil until OnStart wires it in via
// rt.DB(); because OnStart is guaranteed to run before Routes,
// eager capture inside Routes is correct and the lazy-accessor workaround is
// no longer needed.
type module struct {
	db *sql.DB
}

// Module returns the nucleus.ModuleSpec for the notes feature.
// It is registered via nucleus.New().Mount(notes.Module()) in main.go.
//
// Lifecycle:
//   - OnStart: receives a nucleus.Runtime and captures rt.DB() into m.db.
//     If no database is configured, OnStart returns an error immediately.
//     There is no OnShutdown: the framework owns the managed connection pool
//     and closes it at shutdown; a module closing it would be a bug.
//   - Routes: runs after OnStart, so it can build the controller eagerly from
//     the already-populated m.db rather than deferring to request time.
//
// Database schema is managed by explicit SQL migrations in
// examples/mvc_api/migrations/; run `nucleus migrate up` before starting
// the server (see README.md for the exact command with flags).
//
// Route registration note: routes are registered with their full paths in
// Routes for readability; Module Prefix + Resource("") also works (the old
// empty-pattern panic was fixed in pkg/nucleus/router.go).
func Module() nucleus.ModuleSpec {
	m := &module{}

	return nucleus.Module[struct{}]{
		Name:   "notes",
		Models: []any{Note{}},

		// The module carries the access it needs, so mounting it is the
		// whole integration: no rbac_policy.csv rows, no csrf_exempt_paths
		// edit in the host. These rows open the whole resource to anonymous
		// callers — a development default for a reference application; an
		// operator deny in the host policy file always overrides them.
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/notes", Action: "read"},
			{Subject: "anonymous", Object: "/notes", Action: "create"},
			{Subject: "anonymous", Object: "/notes/*", Action: "read"},
			{Subject: "anonymous", Object: "/notes/*", Action: "update"},
			{Subject: "anonymous", Object: "/notes/*", Action: "delete"},
		},
		// The JSON API takes cookie-less POST/PUT/DELETE from curl and SDK
		// clients; a header-token API is not CSRF-forgeable.
		CSRFExempt: []string{"/notes"},

		// OnStart wires the framework-managed *sql.DB into the module. The
		// framework opens the connection from databases.default.url in
		// nucleus.yaml, owns its lifecycle, and closes it at shutdown.
		// Modules must NOT open or close the connection themselves.
		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
			m.db = rt.DB()
			if m.db == nil {
				return fmt.Errorf("notes: no managed database configured (set databases.default.url in nucleus.yaml)")
			}
			rt.Logger().Info("notes: database connection ready")
			return nil
		},

		// No OnShutdown: the framework owns the managed pool and closes it.
		// A module closing rt.DB() would be a double-close bug.

		Routes: func(r nucleus.Router, _ struct{}) {
			// OnStart has already run, so m.db is non-nil here. Build the
			// controller eagerly — no lazy accessor needed.
			ctl := NewController(m.db)
			r.Resource("/notes", ctl, nucleus.Methods(
				nucleus.Index,
				nucleus.Show,
				nucleus.Create,
				nucleus.Update,
				nucleus.Destroy,
			))
		},
	}.Build()
}
```

**Controller (`internal/notes/controller.go` — from `examples/mvc_api`)**

```go
package notes

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// NewController creates a Controller backed by the given database handle.
// The handle must be the framework-managed *sql.DB injected via rt.DB() in
// the module's OnStart hook; the framework owns the connection pool's
// lifecycle (open and close). Tests wire their own in-memory *sql.DB here.
func NewController(db *sql.DB) *Controller {
	return &Controller{db: db}
}

// Controller implements the Nucleus REST Resource sub-interfaces for the
// five CRUD verbs: Index, Show, Create, Update, Destroy.
//
// It satisfies:
//
//	nucleus.Indexer   — GET  /notes
//	nucleus.Shower    — GET  /notes/{id}
//	nucleus.Creator   — POST /notes
//	nucleus.Updater   — PUT  /notes/{id}
//	nucleus.Destroyer — DELETE /notes/{id}
//
// The database handle is the framework-managed *sql.DB injected by the
// module's OnStart hook (via rt.DB()). Because OnStart now runs before
// Routes, the handle is always non-nil by the time Routes constructs the
// controller.
type Controller struct {
	db *sql.DB
}

// createInput is the request body for POST /notes.
type createInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// updateInput is the request body for PUT /notes/{id}.
type updateInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Index handles GET /notes — returns all non-deleted notes ordered by id desc.
func (ctl *Controller) Index(c *nucleus.Context) error {
	rows, err := ctl.db.QueryContext(c.Request.Context(),
		`SELECT id, title, body, created_at, updated_at FROM notes WHERE deleted_at IS NULL ORDER BY id DESC`)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to list notes", err)
	}
	defer rows.Close()

	notes := make([]noteRow, 0, 16)
	for rows.Next() {
		var n noteRow
		if err := rows.Scan(&n.ID, &n.Title, &n.Body, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return respondError(c, http.StatusInternalServerError, "failed to scan note", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return respondError(c, http.StatusInternalServerError, "row iteration error", err)
	}

	return c.JSON(http.StatusOK, map[string]any{"notes": notes, "count": len(notes)})
}

// Show handles GET /notes/{id} — returns a single note by id.
func (ctl *Controller) Show(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}

	var n noteRow
	err = ctl.db.QueryRowContext(c.Request.Context(),
		`SELECT id, title, body, created_at, updated_at FROM notes WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&n.ID, &n.Title, &n.Body, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to fetch note", err)
	}

	return c.JSON(http.StatusOK, n)
}

// Create handles POST /notes — creates a new note and returns 201 with the created row.
func (ctl *Controller) Create(c *nucleus.Context) error {
	var input createInput
	if err := c.BindJSON(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if input.Title == "" {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "title is required"})
	}

	now := time.Now().UTC()
	res, err := ctl.db.ExecContext(c.Request.Context(),
		`INSERT INTO notes (title, body, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		input.Title, input.Body, now, now,
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to create note", err)
	}

	lastID, err := res.LastInsertId()
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to retrieve new note id", err)
	}

	return c.JSON(http.StatusCreated, noteRow{
		ID:        uint(lastID),
		Title:     input.Title,
		Body:      input.Body,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

// Update handles PUT /notes/{id} — replaces a note's title and body.
func (ctl *Controller) Update(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}

	var input updateInput
	if err := c.BindJSON(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if input.Title == "" {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "title is required"})
	}

	now := time.Now().UTC()
	res, err := ctl.db.ExecContext(c.Request.Context(),
		`UPDATE notes SET title = ?, body = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		input.Title, input.Body, now, id,
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to update note", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to check rows affected", err)
	}
	if n == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}

	// Re-fetch the row so the response shape is consistent with Show
	// (includes created_at which is not available from the UPDATE statement).
	var updated noteRow
	err = ctl.db.QueryRowContext(c.Request.Context(),
		`SELECT id, title, body, created_at, updated_at FROM notes WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&updated.ID, &updated.Title, &updated.Body, &updated.CreatedAt, &updated.UpdatedAt)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to fetch updated note", err)
	}
	return c.JSON(http.StatusOK, updated)
}

// Destroy handles DELETE /notes/{id} — soft-deletes a note.
func (ctl *Controller) Destroy(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}

	now := time.Now().UTC()
	res, err := ctl.db.ExecContext(c.Request.Context(),
		`UPDATE notes SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now, id,
	)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to delete note", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to check rows affected", err)
	}
	if n == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}

	return c.NoContent()
}

// parseID extracts the {id} URL parameter and returns a positive integer.
func parseID(c *nucleus.Context) (int64, error) {
	raw := c.Param("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

// respondError logs the underlying error server-side and returns an opaque
// JSON response to the client. Teaching note: always log internal errors —
// never silently swallow them. The client receives only a generic message so
// implementation details are not leaked.
func respondError(c *nucleus.Context, code int, msg string, err error) error {
	slog.ErrorContext(c.Request.Context(), msg, "err", err, "status", code)
	return c.JSON(code, map[string]string{"error": msg})
}
```

### What the fluent builder does

`nucleus.New()` returns an `*AppBuilder`. Each method returns the same builder
so calls can be chained:

| Method | Effect |
|--------|--------|
| `.FromConfigFile(path)` | Load `nucleus.yml` (or `nucleus.yaml`); merges left-to-right when called with multiple paths. |
| `.WithoutDefaults()` | Skip optional built-ins (storage, mail, authz). Produces a lean binary. The `api` skeleton includes this; the `mvc` skeleton does not. |
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

## 5 — Create a user

```bash
nucleus createuser
```

Prompts for username, email and password. The user goes into the auth
table referenced by your `nucleus.yml`.

## Next steps

- **[Project structure](./project-structure.md)** — how a scaffolded
  project is laid out.
- **[Concepts → Application](../concepts/application.md)** — how the
  application container is wired up (`pkg/app` and `pkg/nucleus`).
- **[Concepts → Configuration](../concepts/configuration.md)** — the
  `nucleus.yml` schema and multi-file loader.
