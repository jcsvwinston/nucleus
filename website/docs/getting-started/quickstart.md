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
  - pkg/nucleus.AppBuilder.WithStorage
  - pkg/nucleus.AppBuilder.WithRateLimit
  - pkg/nucleus.AppBuilder.WithAuthz
  - pkg/nucleus.Module
  - pkg/nucleus.Methods
  - pkg/nucleus.Router
  - pkg/nucleus.Runtime
config_keys:
  - databases.default
  - port
---

# Quickstart

Five minutes from zero to a running app with a database, a model on Quark —
the suite's ORM and the data layer every generated feature gets — and REST
endpoints. Start here if you have the CLI installed and want to see Nucleus
work before reading about how it works.

## 1 — Scaffold a project

```bash
nucleus new myapp
cd myapp
```

`nucleus new` writes a **minimal empty skeleton**: a composition-root
`main.go`, `nucleus.yml`, `rbac_policy.csv`, `.gitignore`, `README.md`,
a `Dockerfile` with its `.dockerignore`, and an empty `migrations/`
directory. It also resolves the database driver
the skeleton imports (`go get` + `go mod tidy`, so `go.sum` is already
there); pass `--offline` to skip that and run the two commands yourself.
`--db postgres` (or `mysql`, `sqlserver`, `oracle`) starts the project on
another engine. No `replace` directive, no local clone of Nucleus required.

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

## 3 — Add a feature: a module, mounted

All application behaviour lives in **modules**. A module is a
`nucleus.Module[C]` value carrying a name, a startup hook (`OnStart`), a
route registration function (`Routes`) and the policy rows its routes need. You
write the module, then hand it to the framework with `.Mount()` in `main.go`.

The shortest path is to let the CLI write the first one and mount it:

```bash
nucleus generate module notes --mount
go run .
```

That creates `internal/notes/` — a Quark model with its storage over the
framework's pool, a REST controller, the module with its own policy rows,
CSRF exemption and embedded migrations (applied on start), a page template
and a test — and adds the import plus
`Mount(notes.Module())` to the `nucleus.New()` chain in `main.go`. `GET
/notes` answers `200` right away; the default policy keeps writes for
authenticated subjects (`POST /notes` is `403` until you add a role row in
`module.go`, or generate with `--with-policy` for a development spike).
`go test ./...` runs the module's test against an in-process application.
On the `api` starter, whose `main.go` builds the application
`WithoutDefaults()` without `WithAuthz()` — no enforcer to load rows into —
the generator writes the rows commented out, says so, and `POST /notes`
answers `201` until you add `.WithAuthz()` and uncomment them.

The rest of this step shows the same shape written by hand: a complete
`notes` REST resource you can copy as the model for your own module. It is
**not** what `nucleus new` generates: the scaffold stays empty so the first
module is entirely yours, and the module carries the policy rows and CSRF
exemption its routes need, so the skeleton's `rbac_policy.csv` and
`nucleus.yml` stay untouched.

**Entry point (`main.go`)**

```go
package main

import (
	"log"

	"github.com/acme/myapp/internal/notes"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"

	// The framework links no database driver: each ships as its own module
	// and the application imports the one it uses, the way
	// database/sql drivers have always been wired. Drop this line and the
	// build still succeeds — startup then stops with the line to add back.
	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)

func main() {
	err := nucleus.New().
		FromConfigFile("nucleus.yml").
		// No WithoutDefaults() here (DX-11): the slice runs under the same
		// barriers the scaffold turns on — default-deny authz and CSRF —
		// and the module carries the policy rows and the CSRF exemption its
		// routes need, so nucleus.yml stays as `nucleus new` wrote it.
		Mount(notes.Module()).
		Start()
	if err != nil {
		log.Fatalf("myapp: %v", err)
	}
}
```

Note the blank import of `drivers/sqlite`. The framework links no database
driver — each engine is a module the application imports — and your
scaffold's `main.go` already carries that line; keep it when you rewrite
the file (or run `nucleus add sqlite` to put it back). Without it the build
still succeeds and startup stops with the import to add.

**Module definition (`internal/notes/module.go`)**

```go
package notes

import (
	"context"
	"fmt"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// module holds the storage so the Routes closure can capture it directly.
// It is nil until OnStart derives it from the framework-managed pool via
// rt.DB(); because OnStart is guaranteed to run before Routes, eager
// capture inside Routes is correct.
type module struct {
	storage *Storage
}

// Module returns the nucleus.ModuleSpec for the notes feature.
// It is registered via nucleus.New().Mount(notes.Module()) in main.go.
//
// Lifecycle:
//   - OnStart: receives a nucleus.Runtime and derives the Quark client
//     over rt.DB(). If no database is configured, OnStart returns an error
//     immediately. There is no OnShutdown: the framework owns the managed
//     connection pool and closes it at shutdown; a module closing it would
//     be a bug.
//   - Routes: runs after OnStart, so it can build the controller eagerly
//     from the already-populated m.storage rather than deferring to
//     request time.
//
// The schema is managed by explicit SQL migrations in migrations/; run
// `nucleus migrate up` before starting the server. Note is a Quark model,
// so it is not listed in Models: that field feeds the pkg/model registry,
// which leaves at v2.0.0, and a Quark model needs no registration.
func Module() nucleus.ModuleSpec {
	m := &module{}

	return nucleus.Module[struct{}]{
		Name: "notes",

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

		// OnStart derives the storage from the framework-managed *sql.DB.
		// The framework opens the connection from databases.default.url in
		// nucleus.yml, owns its lifecycle, and closes it at shutdown.
		// Modules must NOT open or close the connection themselves.
		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
			db := rt.DB()
			if db == nil {
				return fmt.Errorf("notes: no managed database configured (set databases.default.url in nucleus.yml)")
			}
			storage, err := NewStorage(db)
			if err != nil {
				return fmt.Errorf("notes: quark client over the managed pool: %w", err)
			}
			m.storage = storage
			rt.Logger().Info("notes: storage ready")
			return nil
		},

		// No OnShutdown: the framework owns the managed pool and closes it.
		// A module closing rt.DB() would be a double-close bug.

		Routes: func(r nucleus.Router, _ struct{}) {
			// OnStart has already run, so m.storage is non-nil here. Build
			// the controller eagerly — no lazy accessor needed.
			ctl := NewController(m.storage)
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

**Controller (`internal/notes/controller.go`)**

```go
package notes

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// NewController creates a Controller over the given storage. The storage
// is the one the module's OnStart hook derived from the framework-managed
// pool; tests wire one over their own in-memory database here.
func NewController(storage *Storage) *Controller {
	return &Controller{storage: storage}
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
// The controller never touches SQL: every read and write goes through the
// Storage, which runs Quark queries over the framework-managed pool.
type Controller struct {
	storage *Storage
}

// noteInput is the request body for POST /notes and PUT /notes/{id}.
type noteInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Index handles GET /notes — returns all non-deleted notes, newest first.
func (ctl *Controller) Index(c *nucleus.Context) error {
	notes, err := ctl.storage.List(c.Request.Context())
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to list notes", err)
	}
	return c.JSON(http.StatusOK, map[string]any{"notes": notes, "count": len(notes)})
}

// Show handles GET /notes/{id} — returns a single note by id.
func (ctl *Controller) Show(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}
	note, err := ctl.storage.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to fetch note", err)
	}
	return c.JSON(http.StatusOK, note)
}

// Create handles POST /notes — creates a new note and returns 201 with the created row.
func (ctl *Controller) Create(c *nucleus.Context) error {
	var input noteInput
	if err := c.BindJSON(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if input.Title == "" {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "title is required"})
	}
	note, err := ctl.storage.Create(c.Request.Context(), input.Title, input.Body)
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to create note", err)
	}
	return c.JSON(http.StatusCreated, note)
}

// Update handles PUT /notes/{id} — replaces a note's title and body.
func (ctl *Controller) Update(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}
	var input noteInput
	if err := c.BindJSON(&input); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
	}
	if input.Title == "" {
		return c.JSON(http.StatusUnprocessableEntity, map[string]string{"error": "title is required"})
	}
	note, err := ctl.storage.Update(c.Request.Context(), id, input.Title, input.Body)
	if errors.Is(err, ErrNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to update note", err)
	}
	return c.JSON(http.StatusOK, note)
}

// Destroy handles DELETE /notes/{id} — soft-deletes a note.
func (ctl *Controller) Destroy(c *nucleus.Context) error {
	id, err := parseID(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id must be a positive integer"})
	}
	err = ctl.storage.Delete(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "note not found"})
	}
	if err != nil {
		return respondError(c, http.StatusInternalServerError, "failed to delete note", err)
	}
	return c.NoContent()
}

// parseID extracts the {id} URL parameter and returns a positive integer.
func parseID(c *nucleus.Context) (uint, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("invalid id")
	}
	return uint(id), nil
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

**Model and storage (`internal/notes/note.go`)**

Both files above use the `Note` model and its `Storage` — without this file
the module does not compile. The model is a **Quark** struct: `db:"<column>"`
names the column, `pk:"true"` marks the key, `quark:"…"` carries the
constraints the migration also declares, and `TableName()` pins it to the
table the migration creates. The storage is the one new idea on this page:
`quark.NewWithDB` borrows the framework's pool instead of opening a second
connection, and `quark.For[Note](ctx, client)` is a typed query over that
table — `Find`, `Create`, `UpdateFields`, `Delete`, `List`. Because the model
has a `deleted_at` column, `Delete` is a soft delete and every read leaves
soft-deleted rows out on its own.

```go
// Package notes manages short notes.
// It demonstrates a Nucleus Module[C] with a REST Resource controller whose
// data layer is Quark, the suite's ORM, over the framework-managed pool.
package notes

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jcsvwinston/quark"

	// Quark's driver module for the engine teaches it the engine's errors
	// (duplicate key, deadlock); without this import `go mod tidy` drops
	// the module and those errors stay generic.
	_ "github.com/jcsvwinston/quark/drivers/sqlite"
)

// ErrNotFound reports a note that does not exist (or is soft-deleted).
var ErrNotFound = errors.New("note not found")

// Note is the Quark model behind the /notes resource. The deleted_at column
// makes quark.Delete a soft delete, and every read excludes soft-deleted
// rows.
type Note struct {
	ID        uint       `db:"id" pk:"true" json:"id"`
	Title     string     `db:"title" quark:"not_null" json:"title"`
	Body      string     `db:"body" json:"body"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt time.Time  `db:"updated_at" json:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at" json:"-"`
}

// TableName pins the model to the table the migration creates.
func (Note) TableName() string { return "notes" }

// Storage runs Quark queries against the framework-managed *sql.DB —
// injected by the module's OnStart hook via rt.DB(). quark.NewWithDB
// borrows the pool: the framework owns its lifecycle, and Storage never
// opens or closes connections itself.
type Storage struct {
	client *quark.Client
}

// NewStorage derives a Quark client over the managed pool. The driver name
// tells Quark which dialect to speak; it matches the engine the migration
// was written for.
func NewStorage(db *sql.DB) (*Storage, error) {
	client, err := quark.NewWithDB("sqlite", db)
	if err != nil {
		return nil, err
	}
	return &Storage{client: client}, nil
}

// listPageSize caps what List returns. Quark warns when List() runs without
// an explicit Limit and falls back to 100, so the cap is written where it
// can be seen and changed.
const listPageSize = 100

func (s *Storage) List(ctx context.Context) ([]Note, error) {
	notes, err := quark.For[Note](ctx, s.client).OrderBy("id", "DESC").Limit(listPageSize).List()
	if err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, nil
}

func (s *Storage) Get(ctx context.Context, id uint) (Note, error) {
	note, err := quark.For[Note](ctx, s.client).Find(id)
	if errors.Is(err, quark.ErrNotFound) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, err
	}
	return note, nil
}

func (s *Storage) Create(ctx context.Context, title, body string) (Note, error) {
	now := time.Now().UTC()
	note := Note{Title: title, Body: body, CreatedAt: now, UpdatedAt: now}
	if err := quark.For[Note](ctx, s.client).Create(&note); err != nil {
		return Note{}, err
	}
	return note, nil
}

func (s *Storage) Update(ctx context.Context, id uint, title, body string) (Note, error) {
	note, err := s.Get(ctx, id)
	if err != nil {
		return Note{}, err
	}
	note.Title, note.Body, note.UpdatedAt = title, body, time.Now().UTC()
	affected, err := quark.For[Note](ctx, s.client).UpdateFields(&note, "title", "body", "updated_at")
	if err != nil {
		return Note{}, err
	}
	if affected == 0 {
		return Note{}, ErrNotFound
	}
	return note, nil
}

func (s *Storage) Delete(ctx context.Context, id uint) error {
	note, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	affected, err := quark.For[Note](ctx, s.client).Delete(&note)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
```

**Migrations (`migrations/`)**

The storage reads and writes a `notes` table, so the project needs its
migration pair before step 4. Quark only reads and writes the table; the
schema is yours, in SQL:

```sql
CREATE TABLE IF NOT EXISTS notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

-- recommend: CREATE INDEX idx_notes_deleted_at ON notes(deleted_at);
-- Soft-delete queries filter on deleted_at IS NULL; an index on that column
-- significantly speeds up Index/Show/Update/Destroy at scale.
```

```sql
DROP TABLE IF EXISTS notes;
```

Those five files are the complete slice. Replace `github.com/acme/myapp`
with your own module path as you copy them, then run `go mod tidy`: it
fetches Quark and its SQLite driver module — the two imports `note.go`
adds — at the tag the module proxy calls latest, the same resolution
`nucleus generate module` runs for you. The repository's test suite does
exactly that on every change: it pastes these listings onto what `nucleus
new` writes, tidies, runs `nucleus migrate up`, boots the result and calls
the five verbs — so what you copy is what compiles and serves today.

### What the fluent builder does

`nucleus.New()` returns an `*AppBuilder`. Each method returns the same builder
so calls can be chained:

| Method | Effect |
|--------|--------|
| `.FromConfigFile(path)` | Load `nucleus.yml` (or `nucleus.yaml`); merges left-to-right when called with multiple paths. |
| `.WithoutDefaults()` | Skip the optional built-ins (storage, mail, the rate limiter, authz): nothing is mounted or enforced, and a key in `nucleus.yml` that asks for one of them is reported at boot in one ERROR line. It is a runtime flag, not a build flag, so the binary is the same size either way. The `api` skeleton uses it; the `mvc` skeleton does not. |
| `.WithStorage()` | Beside `.WithoutDefaults()`, build the storage `nucleus.yml` declares (a `storage:` block, or `NUCLEUS_STORAGE__*` variables), and none while it declares none. Without it, a declared storage block on a `WithoutDefaults()` application is ignored, and the boot log says so in one ERROR line naming this option; from v2.0.0 that configuration refuses to start (DEP-2026-013). The `api` skeleton carries it. |
| `.WithRateLimit()` | Beside `.WithoutDefaults()`, mount the rate limiter `nucleus.yml` declares (`rate_limit_requests` above 0, with the other `rate_limit_*` keys), and none while it declares none. Without it, the limit is not enforced on a `WithoutDefaults()` application, and the boot log says so in one ERROR line naming this option; from v2.0.0 that configuration refuses to start (DEP-2026-016). The `api` skeleton carries it. |
| `.WithAuthz()` | Beside `.WithoutDefaults()`, mount the default stack's authorization: a default-deny enforcer that loads `rbac_policy_file` and the rows modules declare, the bearer decoded ahead of the rate limiter and the interceptors, API key scopes as subjects. Every route outside the bootstrap allow-list (`/healthz`, `/livez`, `/readyz`, `/login`, …) then answers 403 until a policy row allows it. The `api` skeleton does not carry it — its contract is "no authz" — so this is how to add authorization to it. |
| `.Mount(spec)` | Register a `nucleus.ModuleSpec` — its `OnStart` and `Routes` are called by the framework. |
| `.Start()` | Block until the server exits; returns the first non-nil error. |

### Module lifecycle

A `nucleus.Module[C]` carries its concerns in one value:

- `OnStart func(ctx, rt nucleus.Runtime, cfg C) error` — called before
  `Routes`; `rt.DB()` is the framework-managed `*sql.DB`, and
  `quark.NewWithDB(driver, rt.DB())` derives the Quark client over it.
- `Routes func(r nucleus.Router, cfg C)` — registers HTTP handlers; runs
  after `OnStart`, so `m.storage` is already populated.
- `Policies` and `CSRFExempt` — the access rows and the CSRF exemption the
  module's own routes need, so mounting it edits nothing in the host.
- No `OnShutdown` needed here: the framework owns the managed connection
  pool and closes it at shutdown.
- `Models []any` exists too, and the slice does not use it: it registers
  structs with the `pkg/model` registry, which leaves the public API at
  v2.0.0 (DEP-2026-024). A Quark model needs no registration.

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
quick start. To start with the panel already mounted — or with the whole
suite, Quark and both bridges included — scaffold with `--with orbit` or
`--template suite`: [Start a suite app](suite-app.md).

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
before fighting it: the fix is two policy rows, not more middleware. The
gate only judges routes that exist: a path nothing serves answers a plain
404 (`curl localhost:8080/nope`), so a 403 always points at a policy row
and a 404 at the URL.

## Next steps

- **[Project structure](./project-structure.md)** — how a scaffolded
  project is laid out.
- **[Auth → Your first login](../features/auth/your-first-login.md)** —
  the complete login slice: users table, `UserProvider`, `POST /login`,
  session, logout.
- **[Concepts → Models and the database](../concepts/models-and-database.md)**
  — what a Quark model is, migrations, test data, and where `pkg/model`
  still stands until v2.0.0.
- **[Features → Using Quark](../features/using-quark.md)** — the rest of
  the ORM the slice above uses: relations, the query builder, hooks, the
  Orbit bridges.
- **[Concepts → Application](../concepts/application.md)** — how the
  application container is wired up (`pkg/app` and `pkg/nucleus`).
- **[Concepts → Configuration](../concepts/configuration.md)** — the
  `nucleus.yml` schema and multi-file loader.
