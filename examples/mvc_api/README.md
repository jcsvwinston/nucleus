# mvc_api — Nucleus Reference Application

Minimal MVC + REST API demonstrating ADR-010 Phase 4, Slice 1.

One resource: **notes**. Full CRUD via the Nucleus fluent builder and
REST Resource controller pattern.

## What this demonstrates

- The fluent builder: `nucleus.New().FromConfigFile(...).WithoutDefaults().Mount(...).Start()`
- `Module[C any]` generic constructor with `OnStart` / `OnShutdown` lifecycle hooks
- `Router.Resource(path, controller, nucleus.Methods(...))` with five verbs
- The five REST sub-interfaces: `Indexer`, `Shower`, `Creator`, `Updater`, `Destroyer`
- Explicit SQL migrations via `nucleus migrate up` (no auto-migrate)
- A hermetic smoke test using SQLite `:memory:`

## Prerequisites

Go 1.26+ (matches the `go` directive in `go.mod`).

## Setup

All commands are run from the repository root.

**1. Apply migrations (creates `examples_mvc_api.db`):**

```
go run ./cmd/nucleus migrate \
    --config     examples/mvc_api/config/nucleus.yaml \
    --migrations examples/mvc_api/migrations \
    up
```

Or, if you have the `nucleus` binary on `PATH`:

```
nucleus migrate \
    --config     examples/mvc_api/config/nucleus.yaml \
    --migrations examples/mvc_api/migrations \
    up
```

**2. Start the server:**

```
go run ./examples/mvc_api
```

The server listens on port 8090 (set in `config/nucleus.yaml`).

## Trying the API

```
# List notes
curl -s http://localhost:8090/notes | jq .

# Create a note
curl -s -X POST http://localhost:8090/notes \
     -H 'Content-Type: application/json' \
     -d '{"title":"hello","body":"world"}' | jq .

# Show a note
curl -s http://localhost:8090/notes/1 | jq .

# Update a note
curl -s -X PUT http://localhost:8090/notes/1 \
     -H 'Content-Type: application/json' \
     -d '{"title":"updated","body":"new body"}' | jq .

# Delete a note (soft delete)
curl -s -X DELETE http://localhost:8090/notes/1
```

## Running the tests

The smoke test is hermetic — it opens a SQLite `:memory:` database and
exercises the full CRUD path without touching the filesystem.

```
go test ./examples/mvc_api/...
```

## Known limitations (surfaced by this example — tracked follow-ups)

This is the first reference app authored on the post-Phase-1 fluent
surface, and writing it surfaced two framework gaps. The example works
around them correctly, but the workarounds are **temporary** — do not
treat them as the recommended long-term pattern:

1. **Module DB access — the `openSQLite` workaround is temporary.**
   `Module.OnStart` receives `*nucleus.App` (the config struct), not the
   runtime `*app.App` that owns the managed connection pool (`.DB`/`.DBs`).
   So `internal/notes/module.go` opens its **own** `*sql.DB` from the
   config URL, bypassing the framework's pool. The planned fix (next
   ADR-010 Phase 4 slice) passes a `nucleus.Runtime` handle into
   `OnStart`/`OnShutdown` so modules use `rt.DB()` instead of opening their
   own connection. Until then, this example demonstrates the verb/routing
   surface, not the final DB-access pattern.

2. **`Routes` runs before `OnStart`.** `nucleus.Run` calls `Routes`
   (route registration) before module `OnStart`. Capturing a not-yet-
   initialised handle in the `Routes` closure (`&Controller{db: m.db}`)
   silently captures `nil`. The controller therefore reads the DB
   **lazily** at request time (`func() *sql.DB`); see
   `lifecycle_regression_test.go`.

## Migration note — fluent AutoMigrate gap

The fluent `Run`/`Start` path mounts routes and fires lifecycle hooks,
but does NOT auto-migrate models. `Module.Models` is captured at build
time but not consumed by `nucleus.Run`. The idiomatic path is explicit
SQL migrations via `nucleus migrate up`, as shown above. (This is
consistent with SPEC.md's SQL-first stance; auto-migrate is a convenience
for dev, not the production path.)

## File layout

```
examples/mvc_api/
├── main.go                          # fluent entry point
├── config/
│   └── nucleus.yaml                 # port 8090, sqlite database
├── migrations/
│   ├── 001_create_notes.up.sql
│   └── 001_create_notes.down.sql
└── internal/notes/
    ├── note.go                      # Note model (embeds model.BaseModel)
    ├── controller.go                # REST Resource controller
    ├── module.go                    # nucleus.Module[struct{}] definition
    └── notes_test.go                # hermetic smoke test
```
