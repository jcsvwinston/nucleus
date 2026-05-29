# Nucleus Developer Manual

Reference date: 2026-05-29.
Status: Current.

This is the main guide to build, operate, and deploy applications with Nucleus.

## 1. Objective

Nucleus is a Go web framework built for long-lived production systems, focused on:

- MVC + REST API applications
- integrated admin panel
- operational lifecycle CLI (scaffold, migrations, seed, inspection)
- SQL-native foundation with a stable model contract

## 2. Current Scope

Current Nucleus scope includes:

- `pkg/app`: application container (config, logger, router, DB, admin, lifecycle); registers `/healthz` and (when `metrics_path` is set) `/metrics` by default; builds `App.JWT *auth.JWTManager` from `jwt_keys[]` (multi-key/RS256) or `jwt_secret` (legacy HS256 fallback) and auto-mounts `/.well-known/jwks.json` when ≥1 RS256 key is configured; autowraps `mail.Sender.Send` (unless driver is `noop` or empty) and remote `storage.Store` operations (unless provider is `local`) with `pkg/circuit.Breaker` when `circuit_breaker.enabled` is `true` (default)
- `pkg/nucleus`: fluent façade (`nucleus.New()` / `nucleus.Run()`); mounts the auth-gated `GET /_/config` endpoint when the admin subsystem is active (ADR-010 Phase 3b) — returns the effective merged config with per-key provenance and canonical redaction; a `WithoutDefaults()` app never exposes this endpoint
- `pkg/auth`: password hashing, server-side sessions, JWT — single-secret HS256 (legacy) plus multi-key rotation with `kid` header, RS256 + JWKS endpoint; consumed by `pkg/app` to wire `App.JWT` from config
- `pkg/authz`: Casbin enforcer with default-deny + deny-override semantics, `Enforcer.Deny` for explicit overrides
- `pkg/db`: SQL connectivity (`database/sql` runtime), health checks, file-based SQL migrations, `Migrator.Drift` for missing-file / checksum drift detection, `Migrator.SchemaDrift` for live schema introspection (SQLite, PostgreSQL, MySQL; MSSQL/Oracle return `ErrSchemaDriftUnsupported`)
- `pkg/model`: model registry, reflection-based metadata, generic CRUD, hooks; dialect-aware migration scaffolds (SQLite / Postgres / MySQL)
- `pkg/admin`: embedded admin panel (SPA + CRUD API + operational runtime surfaces)
- `pkg/tasks`: async task base layer with Asynq
- `pkg/outbox`: SQL-backed transactional outbox runtime
- `pkg/observe`: structured logging + OpenTelemetry bootstrap (OTLP traces/metrics, optional Prometheus reader for `/metrics`)
- `pkg/health`: dependency probes (DB / Redis / storage / mail) consumed by the `/healthz` handler
- `pkg/circuit`: standalone circuit-breaker primitive; `pkg/app` wires it automatically for `mail.Sender.Send` and remote `storage.Store` operations — set `circuit_breaker.enabled=false` (or tune thresholds) in `nucleus.yml` to opt out or adjust behavior
- `pkg/router`: HTTP guardrails (`CSRF`, security headers, configurable rate limiting — keyed per-tenant when a tenant context is resolved)
- `cmd/nucleus`: modular CLI
- official runnable examples: returning in v0.9.X (the previous `examples/*` tree was removed in the ADR-010 Phase 1 iteration on 2026-05-16; see [`docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md`](../adrs/ADR-010-fluent-api-v2-pkg-nucleus.md))

Related documents:

- `docs/QUICKSTART.md`
- `docs/guides/DETAILED_TUTORIAL.md`
- `docs/reference/PROJECT_LAYOUT.md`
- `docs/guides/MODELING_MULTI_DATABASE.md`
- `docs/governance/RELEASE_CHECKLIST.md`

## 3. Requirements

## 3.1 Runtime and toolchain

- Minimum supported Go version: `1.25`
- Recommended Go for development/release: `1.26.x`
- Node.js: required for admin UI syntax checks in CI/rehearsal

Current policy reference:

- `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`

## 3.2 Database

Supported SQL engines by URL:

- SQLite: `sqlite://app.db` (or `:memory:`)
- PostgreSQL: `postgres://...` or `postgresql://...`
- MySQL: `mysql://...`
- MS SQL Server (exploratory): `sqlserver://...` or `mssql://...`
- Oracle (exploratory): `oracle://...`

Current exploratory note:

- runtime connectivity and exploratory CLI smoke are covered in CI live lanes for `mssql` and `oracle`
- full operational CLI coverage is still in exploratory maturation before promotion to required lanes

CI matrix profiles and local reproduction:

- `docs/governance/CI_MATRIX.md`
- repeated exploratory stability drill helper: `scripts/ci/run_exploratory_stability.sh`

## 4. CLI Installation

There are two recommended paths.

## 4.1 From release binaries (recommended)

Download from official releases:

- `https://github.com/jcsvwinston/nucleus/releases`

Assets per release:

- `nucleus_<version>_linux_amd64.tar.gz`
- `nucleus_<version>_linux_arm64.tar.gz`
- `nucleus_<version>_darwin_amd64.tar.gz`
- `nucleus_<version>_darwin_arm64.tar.gz`
- `nucleus_<version>_windows_amd64.zip`
- `nucleus_<version>_windows_arm64.zip`
- `checksums.txt`

Recommended validation:

1. Verify checksum.
2. Run `nucleus version`.

## 4.2 From source code

```bash
git clone https://github.com/jcsvwinston/nucleus.git
cd Nucleus
go build -o nucleus ./cmd/nucleus
./nucleus version
```

## 4.3 Canonical import path note

Currently, `go.mod` declares:

- `module github.com/jcsvwinston/nucleus`

If you consume Nucleus as a dependency in an external project, keep imports/scripts aligned with that module path until canonical module migration is fully closed.

## 5. First Project in 5 Minutes

## 5.1 Generate skeleton

```bash
nucleus new myapp --module example.com/myapp --out . --port 8080 --template mvc
cd myapp
go mod tidy
```

`nucleus new` produces a **minimal skeleton** — no demo content, no
pre-built modules. The mvc template generates:

- `main.go` (project root — composition root; run with `go run .`)
- `nucleus.yml`
- `rbac_policy.csv` (grants anonymous access to `/healthz`; default-deny Casbin)
- `.gitignore`
- `migrations/` (empty)
- `go.mod` / `README.md`

The api template generates the same minus `rbac_policy.csv` and passes
`WithoutDefaults()` (no admin panel, no authz; serves only `/healthz`).

Add features as `nucleus.Module` values. See `examples/mvc_api` for a
complete worked example: notes model, REST Resource controller, and
migrations on the fluent `nucleus.Module` surface.

## 5.2 Run the app

```bash
go run .
```

Default endpoints after `go run .`:

- `http://localhost:8080/healthz` — always present
- `http://localhost:8080/admin` — mvc template only

## 5.3 Export and serve the experimental OpenAPI contract

Generated projects now include an experimental but real contract lane based on `pkg/openapi` and `internal/contracts`.

Export the current project contract:

```bash
nucleus openapi --out openapi.json
```

Serve the same document at runtime:

```go
if err := a.MountOpenAPI("/openapi.json", contracts.NewDocument); err != nil {
	log.Fatal(err)
}
```

Current convention:

- `internal/contracts/contracts.go` is the package aggregator
- `internal/contracts/contracts.go` provides `DefaultConfig()`, `NewDocument()`, and `NewDocumentWithConfig(cfg Config)` for one shared bootstrap path
- each generated contract file exposes `RegisterXContract(doc *openapi.Document)`
- generated contract files auto-register into the package aggregator
- scaffolded server apps mount `GET /openapi.json` explicitly through `app.MountOpenAPI`

Current scope:

- scaffolded CRUD-style paths and schemas generated by `new`, `startapp`, and `generate resource`
- JSON export of the aggregated project document
- runtime serving of that same aggregated JSON document
- explicit JSON request/response metadata for scaffolded operations
- one shared scaffolded response-envelope convention: collections use `{data, count}` and singular payloads use `{data}`
- explicit path parameters for scaffolded `/{id}` routes
- explicit query parameters where contracts choose to declare them; scaffolded list operations now use an optional `q` search parameter that generated handlers honor end-to-end
- small reusable helpers in `pkg/openapi` for repeated schema/response/parameter patterns (`ObjectSchema`, `ArraySchema`, `DataEnvelopeSchema`, `CollectionEnvelopeSchema`, `IDSchema`, `JSONRequestBody`, `JSONResponse`, `ErrorResponse`, `EmptyResponse`, `PathParameter`, `QueryParameter`, `SearchQueryParameter`)
- more homogeneous scaffold metadata for `operationId`, `tags`, `summary`, and `description`
- a shared structured JSON error-envelope convention in scaffolded contracts and generated resource handlers

Current extension guidance:

- keep contract files explicit and readable; helpers should reduce repetition, not hide the final document shape
- prefer shared helpers for common `data`/`count` JSON envelopes, error envelopes, empty responses, and `id`/query parameters before adding new ad hoc schema literals
- CLI export and runtime serving must continue to use the same `contracts.NewDocument()` source of truth
- runtime serving stays explicit through `app.MountOpenAPI(...)` for now; Nucleus does not auto-mount OpenAPI documents behind the application's back

Not in scope yet:

- perfect or complete OpenAPI coverage
- automatic runtime reflection of all handlers
- Swagger UI or other interactive documentation UIs
- generated API clients
- a large contract DSL or hidden mini-framework around OpenAPI documents
- multiple scaffolded response-envelope conventions for the same CRUD lane

## 6. Application Architecture

## 6.1 `app.App` container

Creation:

```go
cfg, _ := app.LoadConfig("nucleus.yml")
a, _ := app.New(cfg)
```

`App` exposes:

- `Config`
- `Logger`
- `Router`
- `DB`
- `JWT` (`*auth.JWTManager`, nil when no signing material is configured)
- `Mailer`
- `Session`
- `Models`
- `Admin`

Key methods:

- `RegisterModel(...)`
- `MountAdmin()`
- `Run(ctx)`
- `Shutdown(ctx)`
- `OnShutdown(fn)`

## 6.2 Router and lifecycle

- native Nucleus router stack over `net/http`
- `Run` starts HTTP server
- clean shutdown by context cancel or `SIGINT/SIGTERM`

## 6.3 Auto-mounted admin

`app.New` mounts admin automatically at `admin_prefix` (default `/admin`).

## 7. Configuration

Configuration is managed via `nucleus.yml` with `NUCLEUS_`-prefixed environment
variable overrides. The loader applies layers in this order (lowest to highest):

```
struct defaults < config files < NUCLEUS_* env vars < CLI flags < programmatic
```

Both `app.LoadConfig` and the fluent builder (`nucleus.New().FromConfigFile(...)`)
honour this full precedence chain. Previously the fluent builder ignored env vars;
as of Phase 3.1 (ADR-010) both paths apply the env layer.

Key mapping convention: flat keys use single underscores (`port` → `NUCLEUS_PORT`);
nested keys use **double underscores** as the segment separator
(`databases.default.url` → `NUCLEUS_DATABASES__DEFAULT__URL`).

Unknown `NUCLEUS_`-prefixed env vars are silently ignored. Unknown keys in config
files are rejected at boot (or demoted to a `WARN` with
`AppBuilder.WithUnknownFields("warn")`). `NUCLEUS_ENV=production` forces strict
mode regardless of the code-level setting.

For the complete key reference and precedence details, see `docs/reference/CONFIG_KEY_REGISTRY.md`.

## 8. Models

## 8.1 BaseModel

Recommended embed:

```go
type Project struct {
    model.BaseModel
    Name string
}
```

## 8.2 Supported tags

`db` (storage metadata):

- `column:<name>`
- `primaryKey` / `pk`
- `required`
- `readonly`
- `fk` / `fk:<model|table.column|model=...,table=...,column=...>`
- `index` / `index:<name>`
- `unique` / `unique:<name>`

`validate`:

- `required` (required field metadata detection)

`admin`:

- `list`
- `search`
- `filter`
- `readonly`
- `exclude`
- `label:<text>`
- `choices:value|Label;value2|Label2`

Full example:

```go
type User struct {
    model.BaseModel
    Email string `db:"column:email;required;unique" validate:"required,email" admin:"list,search"`
    Role  string `db:"column:role" admin:"list,filter,choices:admin|Admin;user|User"`
    Bio   string `db:"column:bio" admin:"label:Biography"`
}
```

`inspectdb` now emits these tags when discoverable from schema metadata:

- PK as `pk`
- FK as `fk:<table>.<column>`
- single-column index/unique as `index` / `unique`
- composite index/unique as `index:<name>` / `unique:<name>`

## 8.3 Model registration

```go
err := a.RegisterModel(&User{}, model.ModelConfig{
    Icon:         "user",
    ListFields:   []string{"ID", "Email", "Role"},
    SearchFields: []string{"Email"},
    Filters:      []string{"Role"},
    OrderBy:      "created_at desc",
    PageSize:     25,
    ReadOnly:     false,
})
```

## 9. MVC + API

In Nucleus, MVC and API run side by side in the same router. You wire routes
in your composition-root `main.go` (or inside a `nucleus.Module`).

Example (illustrative — not generated by the scaffold):

```go
a.Router.Get("/", controllers.HomePage(tpl))
a.Router.Get("/api/health", controllers.Health)
a.Router.Get("/api/notes", controllers.ListNotes(sqlDB))
a.Router.Post("/api/notes", controllers.CreateNote(sqlDB))
```

Recommendation:

- keep HTTP handlers in `internal/<module>/controller.go` or an `handlers/` package
- keep business logic outside handlers

## 9.1 Unified request context (`router.Context`)

For a single handler entrypoint style, use `router.ContextHandler(...)`.
It wraps `http.ResponseWriter` and `*http.Request` and provides:

- unified parameter access: `Param`, `Query`, `Form`, `Value`
- session helpers (when injected with `router.WithSession(...)`)
- template bind and render helpers (`Set`, `BindData`, `HTML`)
- typed responses (`JSON`, `XML`, `File`, `Download`, `NoContent`)

Example:

```go
tpl, _ := template.ParseFS(templateFS, "templates/*.html")

a.Router.Get("/users/{id}", router.ContextHandler(func(c *router.Context) {
    userID := c.Param("id")
    tenant := c.Value("tenant") // path -> query -> form

    c.Set("Title", "User Detail")
    _ = c.HTML(http.StatusOK, "user.html", map[string]interface{}{
        "UserID": userID,
        "Tenant": tenant,
    })
}, router.WithSession(a.Session), router.WithTemplates(tpl)))
```

## 9.2 RESTful resource routes (`Router.Resource`)

For conventional CRUD routing, register one resource and let the router map
common REST endpoints.

```go
a.Router.Resource("/users", router.ResourceHandlers{
    List: func(w http.ResponseWriter, r *http.Request) {
        router.JSON(w, http.StatusOK, map[string]string{"action": "list"})
    },
    Create: func(w http.ResponseWriter, r *http.Request) {
        router.JSON(w, http.StatusCreated, map[string]string{"action": "create"})
    },
    Retrieve: func(w http.ResponseWriter, r *http.Request) {
        router.JSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id")})
    },
    Update: func(w http.ResponseWriter, r *http.Request) {
        router.JSON(w, http.StatusOK, map[string]string{"action": "update", "id": r.PathValue("id")})
    },
    Delete: func(w http.ResponseWriter, r *http.Request) {
        router.NoContent(w)
    },
})
```

Registered routes:

- `GET /users/`
- `POST /users/`
- `GET /users/{id}`
- `PUT /users/{id}`
- `DELETE /users/{id}`

## 10. Admin Panel

The admin panel is a built-in SPA with CRUD, search, filters, CSV export, bulk actions, session observability, live runtime inspector, and system pulse dashboards.

For detailed setup, capabilities, API routes, and the multi-node cluster lab, see `docs/ADMIN_UI.md` and `docs/ADMIN_CLUSTER_LAB.md`.

## 11. SQL Migrations

Root command:

```bash
nucleus migrate [flags] [action]
```

Flags:

- `--config <path>`
- `--migrations <dir>` (default `migrations`)
- `--force`
- `--yes`

Actions:

- `up [n]`
- `down [n]`
- `steps <n>`
- `status`
- `create <name>`
- `reset`
- `refresh`

Examples:

```bash
nucleus migrate --config nucleus.yml create add_project_owner
nucleus migrate --config nucleus.yml
nucleus migrate --config nucleus.yml status
nucleus migrate --config nucleus.yml down 1
nucleus migrate --config nucleus.yml steps -1
nucleus migrate --config nucleus.yml --force reset
```

## 12. Seeds

Command:

```bash
nucleus seed --config nucleus.yml --seeds seeds
```

Flags:

- `--file <seed.sql>`
- `--dry-run`
- `--force`
- `--yes`

Examples:

```bash
nucleus seed --config nucleus.yml --seeds seeds --dry-run
nucleus seed --config nucleus.yml --seeds seeds --file 001_users.sql
nucleus seed --config nucleus.yml --seeds seeds --force
```

## 13. Admin User Creation

```bash
nucleus createuser --config nucleus.yml \
  --username admin \
  --email admin@example.com \
  --password supersecret123 \
  --superuser=true \
  --no-input
```

Notes:

- without `--no-input`, it prompts interactively
- validates username/email/password
- creates or updates existing user by username/email
- `/admin` authentication mode is automatic:
  - bootstrap mode (no users): admin is accessible without login
  - protected mode (>=1 user): admin requires credentials at `/admin/login`

Change an existing admin password:

```bash
nucleus changepassword admin --config nucleus.yml --password newsecret123 --no-input
```

## 13.1 Cache and sessions

Create SQL table for DB-based cache:

```bash
nucleus createcachetable --config nucleus.yml
nucleus createcachetable --config nucleus.yml --dry-run
```

Clear expired sessions (or all sessions):

```bash
nucleus clearsessions --config nucleus.yml
nucleus clearsessions --config nucleus.yml --all
nucleus clearsessions --config nucleus.yml --dry-run
```

## 13.2 Additional CLI Commands

For i18n (`makemessages`, `compilemessages`), static file management (`collectstatic`, `findstatic`), content type cleanup, geospatial inspection (`ogrinspect`), advanced migration maintenance (`optimizemigration`, `squashmigrations`), and mail/provider configuration, see the individual guides in `docs/`.

## 14. SQL Shell

Ad hoc execution:

```bash
nucleus shell --config nucleus.yml -c "SELECT count(*) FROM users"
nucleus shell --config nucleus.yml --sandbox -c "SELECT count(*) FROM users"
```

Interactive mode:

```bash
nucleus shell --config nucleus.yml
```

It also supports `stdin` input (SQL scripts).
With `--sandbox`, execution is limited to read-only SQL statements.

## 14.1 Background tasks (Asynq)

Background task support is **not scaffolded** by `nucleus new`. To add it:

1. Create `cmd/worker/main.go` as your worker entrypoint.
2. Add task handlers under `internal/tasks/`.
3. Run the worker:

```bash
go run ./cmd/worker
```

Requires `redis_url` in `nucleus.yml`.

`pkg/tasks` defines the `tasks.Manager` and `tasks.Scheduler` interfaces; the
concrete Redis-backed implementation lives in the Asynq provider under
`pkg/tasks/providers/asynq`. Construct a manager from your framework config and
hold it as a `tasks.Manager`:

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/tasks"
    asynqprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

// TaskArticleCreated is an app-defined task type, not a tasks.* symbol.
const TaskArticleCreated = "articles.created"

var manager tasks.Manager
manager, err := asynqprovider.NewManager(tasks.Config{
    RedisURL:    "redis://127.0.0.1:6379/0",
    Concurrency: 10,
}, nil)
if err != nil {
    return err
}
defer manager.Close()
```

For request-to-job correlation in tracing/logging, enqueue jobs with context.
`EnqueueJSONCtx` returns the new task's id as a string:

```go
id, err := manager.EnqueueJSONCtx(r.Context(), TaskArticleCreated, map[string]any{
    "article_id": articleID,
    "title":      title,
})
```

This preserves `request_id`/`user_id`/`traceparent` metadata for worker-side observability.

When a task needs a repeatable queue/retry/retention policy, prefer the explicit helper:

```go
policy := tasks.DefaultEnqueuePolicy()
policy.Queue = "critical"
policy.MaxRetry = 5
policy.Timeout = 2 * time.Minute
policy.Retention = 24 * time.Hour

id, err := manager.EnqueueJSONCtxWithPolicy(r.Context(), TaskArticleCreated, map[string]any{
    "article_id": articleID,
    "title":      title,
}, policy)
```

Generated task handlers also use `tasks.DecodeJSONPayload(...)` so worker glue stays explicit without repeating JSON decode boilerplate in every scaffold.

Current runtime queue operations:

- `pause`
- `unpause`
- `retry` (move retry tasks back to pending)
- `archive-retry` (move retry tasks to archived/dead-letter)
- `retry-archived` (move archived/dead-letter tasks back to pending)
- `purge-archived` (delete archived/dead-letter tasks)

Observability dashboards and alert baseline:

- `docs/OBSERVABILITY_BASELINE.md`

## 14.2 Periodic tasks

For explicit cron-style scheduling, use the `tasks.Scheduler` interface. The
Redis-backed implementation is constructed via the Asynq provider's
`NewScheduler`; register periodic jobs with `RegisterJSON(spec, taskType,
payload, policy)`, which returns the new entry's id as a string:

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/tasks"
    asynqprovider "github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq"
)

var scheduler tasks.Scheduler
scheduler, err := asynqprovider.NewScheduler(asynqprovider.SchedulerConfig{
    RedisURL: "redis://127.0.0.1:6379/0",
})
if err != nil {
    return err
}
defer scheduler.Close()

policy := tasks.DefaultEnqueuePolicy()
policy.Queue = "maintenance"
policy.MaxRetry = 1

_, err = scheduler.RegisterJSON("@every 1m", "sessions.cleanup", map[string]any{
    "scope": "expired",
}, policy)
if err != nil {
    return err
}

if err := scheduler.Start(); err != nil {
    return err
}
```

Current scope:

- explicit scheduler construction from `redis_url` via the Asynq provider
- periodic registration through `Scheduler.RegisterJSON(...)`
- reuse of the same queue/retry/timeout/retention policy subset used by `EnqueueJSONCtxWithPolicy(...)`
- runtime inspection of registered scheduler entries through the Asynq provider's `InspectRuntime(...)`

Not in scope today:

- scaffolded scheduler entrypoints
- cron abstraction across multiple backends

## 14.3 Signals and distributed relay

`pkg/signals` remains the main in-process event bus. For cross-process delivery, Nucleus now also exposes a small Redis relay instead of a hidden event framework.

```go
bus := signals.NewBus(logger)
relay, err := signals.NewRedisRelay(signals.RedisRelayConfig{
    RedisURL: "redis://127.0.0.1:6379/0",
}, logger)
if err != nil {
    return err
}
defer relay.Close()

go func() {
    _ = relay.ForwardToBus(context.Background(), signals.PostCreate, bus)
}()

bus.On(signals.PostCreate, func(event signals.Event) error {
    log.Printf("distributed post-create event for %s", event.ModelName)
    return nil
})

err = relay.Publish(r.Context(), signals.Event{
    Signal:    signals.PostCreate,
    ModelName: "Article",
    Payload: map[string]any{
        "id": articleID,
    },
})
```

Current scope:

- in-process `Bus.Emit(...)` and `Bus.EmitAsync(...)`
- Redis-backed publish/subscribe for one signal per channel
- forwarding remote events back into `signals.Bus`
- request/user/trace correlation propagation through the relay context metadata

Not in scope yet:

- wildcard subscriptions
- broker abstraction across multiple backends

## 14.4 Transactional outbox

For durable SQL-backed message delivery, use `pkg/outbox`:

```go
store, err := outbox.NewStore(sqlDB, outbox.Config{
    Flavor: outbox.FlavorSQLite,
})
if err != nil {
    return err
}

dispatcher, err := outbox.NewDispatcher(store, func(ctx context.Context, msg outbox.Message) error {
    log.Printf("deliver %s (%s)", msg.Topic, msg.ID)
    return nil
}, outbox.DispatcherConfig{
    LeaseOwner: "api-node-a",
})
if err != nil {
    return err
}

_, err = store.Enqueue(ctx, outbox.Entry{
    Topic: "emails.send",
    Payload: map[string]any{
        "to": "dev@example.com",
    },
})
if err != nil {
    return err
}

if _, err := dispatcher.RunOnce(ctx); err != nil {
    return err
}
```

Current scope:

- SQL-backed outbox schema managed automatically by `outbox.NewStore(...)`
- direct enqueue and transactional enqueue through `EnqueueTx(...)`
- runtime inspection through `outbox.InspectRuntime(...)`
- explicit dispatcher with lease ownership, retry backoff, and terminal failure handling
- admin runtime visibility through `/admin/api/system/snapshot`

Not in scope today:

- broker abstraction across multiple durable transports
- automatic application wiring behind `app.New(...)`
- exactly-once semantics across arbitrary external systems

## 15. Generators (`generate`)

```bash
nucleus generate model User
nucleus generate handler User
nucleus generate migration add_users_table
nucleus generate resource Project
```

Flags:

- `--out <dir>`
- `--force`
- `--migrations <dir>`

`resource` creates:

- model (`internal/models/<name>.go`)
- CRUD scaffold controller (`internal/controllers/<name>_handler.go`)
- controller test (`internal/controllers/<name>_handler_test.go`)
- migration up/down

## 16. Diagnostic Commands

## 16.1 routes

```bash
nucleus routes --config nucleus.yml
nucleus routes --config nucleus.yml --json
nucleus routes --config nucleus.yml --path /api --verbose
```

## 16.2 health

```bash
nucleus health --config nucleus.yml
nucleus health --config nucleus.yml --json --timeout 5s
```

## 16.3 config print --effective

Prints the effective merged configuration — struct defaults, file overlay(s),
`NUCLEUS_*` env var overrides, and final resolved values — with per-key
provenance and canonical secret redaction. Sensitive fields (`jwt_secret`,
`access_key_id`, `database_url`, …) are replaced with `[REDACTED]`. Safe to
share in bug reports and runbooks.

```bash
nucleus config print --effective --config nucleus.yml
nucleus config print --effective --config nucleus.yml --json
```

Per-key provenance is rendered as a source annotation:

- `[default]` — struct default; no file or env override was set.
- `[yaml:path:line]` — sourced from a YAML file at the given 1-based line.
- `[toml:path]` / `[json:path]` — sourced from a TOML or JSON file (no line).
- `[env:NUCLEUS_VAR]` — overridden by a `NUCLEUS_`-prefixed environment variable.

The running-server counterpart is `GET /_/config` (ADR-010 Phase 3b), which
serves the same snapshot via HTTP and requires an active admin session. Prefer
the CLI form in CI / pre-deploy contexts where no server is running; prefer the
HTTP form when auditing a live deployment without shell access.

## 17. Production Guardrails

When `env: production`:

- destructive operations (`migrate down/reset/refresh`, `seed`) require confirmation
- in non-interactive CI use `--force` or `--yes`

CI/CD recommendation:

- use `--force` explicitly
- always run `health` and post-deploy smoke tests

## 18. Recommended Development Flow

1. Generate project/module with `nucleus new`.
2. Define models and tags.
3. Register models in `main`.
4. Adjust SQL migrations.
5. Run `migrate` + `seed`.
6. Create admin user.
7. Implement MVC/API handlers.
8. Review routes and health checks.
9. Automate tests.

## 19. Testing

See `docs/TESTING_GUIDE.md` for the full testing strategy, patterns, and CI integration.

## 20. Production Deployment

See `docs/DEPLOYMENT_GUIDE.md` for deployment strategies, artifact verification, and post-deploy health checks.

## 21. Troubleshooting

For common issues and resolution steps, see the individual guides referenced throughout this documentation set.

## 22. Quick Command Reference

```bash
nucleus help
nucleus version
nucleus new <name> [--module ...] [--out ...] [--port ...] [--template mvc] [--force]
nucleus startapp <name> [--out ...] [--migrations ...] [--skip-migration] [--force]
nucleus serve [--config ...] [--host ...] [--port ...]
nucleus migrate [--config ...] [--migrations ...] [--force] [--yes] [action]
nucleus sqlmigrate [--migrations ...] [--down] <migration_id_or_name>
nucleus sqlflush [--config ...]
nucleus sqlsequencereset [--config ...] [tables...]
nucleus flush [--config ...] [--force] [--yes] [--dry-run]
nucleus diffsettings [--config ...] [--all] [--json]
nucleus createcachetable [--config ...] [--table nucleus_cache_entries] [--dry-run]
nucleus remove_stale_contenttypes [--config ...] [--table nucleus_content_types] [--column model] [--keep custom1,custom2] [--dry-run] [--force] [--yes]
nucleus makemessages [--config ...] [--locale es] [--domain messages] [--input .] [--extensions .go,.html,.templ] [--locales-path locales] [--output ...] [--dry-run]
nucleus compilemessages [--config ...] [--locale es] [--domain messages] [--locales-path locales] [--output ...] [--dry-run]
nucleus collectstatic [--config ...] [--output static_collected] [--source internal/web/static] [--clear] [--dry-run]
nucleus findstatic [--config ...] [--source internal/web/static] [--first] [--json] <asset_path_or_glob> [more...]
nucleus optimizemigration [--migrations migrations] [--down] [--write] <migration_id_or_name>
nucleus squashmigrations [--migrations migrations] --from <migration> --to <migration> [--name baseline] [--write] [--archive-old] [--force] [--dry-run] [--print-sql]
nucleus sendtestemail [--config ...] --to dev@example.com[,ops@example.com] [--from ...] [--subject ...] [--body ...] [--timeout 10s] [--dry-run]
nucleus mailproviders [--config ...] [--json]
nucleus plugin list [--config ...] [--timeout 2s] [--json]
nucleus plugin doctor [--config ...] [--timeout 2s] [--json]
nucleus plugin test [--config ...] --provider <name> --capability <domain.action> [--timeout 2s] [--execute] [--json]
nucleus inspectdb [--config ...] [--tables users,posts] [--exclude ...] [--package models] [--output internal/models/inspected.go]
nucleus ogrinspect [--config ...] [--tables places,roads] [--exclude ...] [--package models] [--output internal/models/geospatial.go] [--all]
nucleus dumpdata [--config ...] [--tables users,posts] [--exclude ...] [--output fixtures.json]
nucleus loaddata [--config ...] [--tables users] [--truncate] [--dry-run] [--force] [--yes] <fixture.json>
nucleus seed [--config ...] [--seeds ...] [--file ...] [--dry-run] [--force] [--yes]
nucleus createuser [--config ...] [--username ...] [--email ...] [--password ...] [--superuser] [--no-input]
nucleus changepassword [--config ...] [--username ...] [--password ...] [--no-input] <username>
nucleus clearsessions [--config ...] [--table nucleus_sessions] [--all] [--dry-run]
nucleus shell [--config ...] [--command ...|-c ...] [--timeout 10s] [--sandbox]
nucleus generate [--out ...] [--migrations ...] [--force] <model|handler|migration|resource> <name>
nucleus test [--run ...] [--count 1] [--race] [--v] [--failfast] [--cover] [--timeout ...] [--dry-run] [packages...]
nucleus testserver [--config ...] [--fixture ...] [--tables users] [--truncate] [--dry-run] [--host ...] [--port ...] <fixture.json>
nucleus routes [--config ...] [--path ...] [--json] [--verbose]
nucleus health [--config ...] [--timeout 3s] [--json] [--deploy]
```

Global output options (before command):

```bash
nucleus --output plain|pretty|json <command> ...
nucleus --color auto|always|never <command> ...
nucleus --symbols|--no-symbols <command> ...
nucleus --json <command> ...            # shorthand for --output json
```

Compatibility aliases:

```bash
nucleus runserver [addr:port]
nucleus startproject <name> [new flags]
nucleus makemigrations <name>
nucleus showmigrations [--config ...] [--migrations ...]
nucleus createsuperuser [createuser flags]
nucleus dbshell [shell flags]
nucleus check [health flags]             # health alias
nucleus check --deploy [--config ...]    # deployment hardening checks
```

In projects generated with `nucleus new`, run the server from the project root:

```bash
go run .
```

If your project includes a worker (`cmd/worker/main.go`):

```bash
go run ./cmd/worker
```

## 23. Suggested Next Reading

- documentation: `docs/README.md`
- quick onboarding: `docs/QUICKSTART.md`
- step-by-step tutorial: `docs/DETAILED_TUTORIAL.md`
- recommended layout: `docs/reference/PROJECT_LAYOUT.md`
- CLI best practices: `docs/reference/CLI_BEST_PRACTICES.md`
- API contract inventory: `docs/reference/API_CONTRACT_INVENTORY.md`
- CLI contract matrix: `docs/reference/CLI_CONTRACT_MATRIX.md`
- config key registry: `docs/reference/CONFIG_KEY_REGISTRY.md`
- email providers and plugins: `docs/reference/PLUGIN_SDK.md`
- enterprise and long-term roadmap: `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`
- compatibility SLO policy: `docs/governance/COMPATIBILITY_SLO.md`
- release checklist: `docs/governance/RELEASE_CHECKLIST.md`
- deprecation template and policy: `docs/governance/DEPRECATION_TEMPLATE.md`
- migration assistant conventions: `docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md`
