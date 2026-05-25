# mvc_api — Nucleus Reference Application

Minimal MVC + REST API demonstrating ADR-010 Phase 4, Slice 1.

One resource: **notes**. Full CRUD via the Nucleus fluent builder and
REST Resource controller pattern.

## What this demonstrates

- The fluent builder: `nucleus.New().FromConfigFile(...).WithoutDefaults().Mount(...).Start()`
- `Module[C any]` generic constructor with an `OnStart` lifecycle hook that receives `nucleus.Runtime`
- `rt.DB()` as the idiomatic module→DB access pattern: the framework hands each module its managed `*sql.DB` in `OnStart`; the module captures it and the `Routes` closure uses it directly
- `OnStart` runs before `Routes`, so eager capture of `rt.DB()` in the closure is correct — no lazy accessor needed
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

## Idiomatic module→DB pattern (ADR-010 Phase 4, Gap 1 + Gap 2)

Both framework gaps surfaced during the initial Slice 1 authoring are now
fixed. This example reflects the current idiomatic pattern:

- **`rt.DB()` replaces manual connection open.** `OnStart` receives a
  `nucleus.Runtime` handle. Calling `rt.DB()` returns the framework-managed
  `*sql.DB` for the module's declared database alias (the app default when
  unset). The framework opens the pool from `databases.default.url`, owns
  its lifecycle, and closes it at shutdown. Modules must NOT open or close
  their own connection.
- **`OnStart` runs before `Routes`.** The ordering is now guaranteed:
  `OnStart` populates `m.db = rt.DB()` before the `Routes` closure runs, so
  the closure can construct the controller eagerly (`NewController(m.db)`)
  without any lazy accessor. There is no `OnShutdown` in the notes module —
  the framework closes the managed pool; a module closing it would be a
  double-close bug.

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
    ├── controller.go                # REST Resource controller (eager *sql.DB field)
    ├── module.go                    # nucleus.Module[struct{}] definition (rt.DB() pattern)
    └── notes_test.go                # hermetic smoke test
```
