# GoFrame Developer Manual

Reference date: 2026-04-07.
Status: Current.

This is the main guide to build, operate, and deploy applications with GoFrame.

## 1. Objective

GoFrame is a Go web framework built for long-lived production systems, focused on:

- MVC + REST API applications
- integrated admin panel
- operational lifecycle CLI (scaffold, migrations, seed, inspection)
- SQL-native foundation with a stable model contract

## 2. Current Scope

Current GoFrame scope includes:

- `pkg/app`: application container (config, logger, router, DB, admin, lifecycle)
- `pkg/db`: SQL connectivity (`database/sql` runtime), health checks, file-based SQL migrations
- `pkg/model`: model registry, reflection-based metadata, generic CRUD, hooks
- `pkg/admin`: embedded admin panel (SPA + CRUD API)
- `pkg/tasks`: async task base layer with Asynq
- `pkg/observe`: structured logging + OpenTelemetry bootstrap (OTLP traces/metrics)
- `pkg/router`: HTTP guardrails (`CSRF`, security headers, configurable rate limiting)
- `cmd/goframe`: modular CLI
- official runnable example: `examples/mvc_api`

Related documents:

- `docs/QUICKSTART.md`
- `docs/DETAILED_TUTORIAL.md`
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

- `https://github.com/jcsvwinston/GoFrame/releases`

Assets per release:

- `goframe_<version>_linux_amd64.tar.gz`
- `goframe_<version>_linux_arm64.tar.gz`
- `goframe_<version>_darwin_amd64.tar.gz`
- `goframe_<version>_darwin_arm64.tar.gz`
- `goframe_<version>_windows_amd64.zip`
- `goframe_<version>_windows_arm64.zip`
- `checksums.txt`

Recommended validation:

1. Verify checksum.
2. Run `goframe version`.

## 4.2 From source code

```bash
git clone https://github.com/jcsvwinston/GoFrame.git
cd GoFrame
go build -o goframe ./cmd/goframe
./goframe version
```

## 4.3 Canonical import path note

Currently, `go.mod` declares:

- `module github.com/jcsvwinston/GoFrame`

If you consume GoFrame as a dependency in an external project, keep imports/scripts aligned with that module path until canonical module migration is fully closed.

## 5. First Project in 5 Minutes

## 5.1 Generate scaffold

```bash
goframe new myapp --module example.com/myapp --out . --port 8080 --template mvc
cd myapp
go mod tidy
```

Main generated structure:

- `cmd/server/main.go`
- `cmd/worker/main.go`
- `internal/models/article.go`
- `internal/controllers/home_page.go`
- `internal/controllers/article_api.go`
- `internal/contracts/contracts.go`
- `internal/contracts/article_contract.go`
- `internal/tasks/article_events.go`
- `internal/web/templates/home.html`
- `migrations/000001_create_articles.up.sql`
- `migrations/000001_create_articles.down.sql`
- `seeds/001_articles.sql`
- `goframe.yaml`

## 5.2 Run the app

```bash
go run ./cmd/server
go run ./cmd/worker
```

Default endpoints:

- `http://localhost:8080/`
- `http://localhost:8080/api/health`
- `http://localhost:8080/api/articles`
- `http://localhost:8080/openapi.json`
- `http://localhost:8080/admin`

## 5.3 Export and serve the experimental OpenAPI contract

Generated projects now include an experimental but real contract lane based on `pkg/openapi` and `internal/contracts`.

Export the current project contract:

```bash
goframe openapi --out openapi.json
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
- runtime serving stays explicit through `app.MountOpenAPI(...)` for now; GoFrame does not auto-mount OpenAPI documents behind the application's back

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
cfg, _ := app.LoadConfig("goframe.yaml")
a, _ := app.New(cfg)
```

`App` exposes:

- `Config`
- `Logger`
- `Router`
- `DB`
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

- native GoFrame router stack over `net/http`
- `Run` starts HTTP server
- clean shutdown by context cancel or `SIGINT/SIGTERM`

## 6.3 Auto-mounted admin

`app.New` mounts admin automatically at `admin_prefix` (default `/admin`).

## 7. Configuration

Configuration is managed via `goframe.yaml` with environment variable overrides (`GOFRAME_`).

For the complete key reference, see `docs/reference/CONFIG_KEY_REGISTRY.md`.

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

In GoFrame, MVC and API run side by side in the same router.

Example:

```go
a.Router.Get("/", controllers.HomePage(tpl))
a.Router.Get("/api/health", controllers.Health)
a.Router.Get("/api/articles", controllers.ListArticles(sqlDB))
a.Router.Post("/api/articles", controllers.CreateArticle(sqlDB))
```

Recommendation:

- keep HTTP handlers in `internal/controllers` or `handlers`
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
goframe migrate [flags] [action]
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
goframe migrate --config goframe.yaml create add_project_owner
goframe migrate --config goframe.yaml
goframe migrate --config goframe.yaml status
goframe migrate --config goframe.yaml down 1
goframe migrate --config goframe.yaml steps -1
goframe migrate --config goframe.yaml --force reset
```

## 12. Seeds

Command:

```bash
goframe seed --config goframe.yaml --seeds seeds
```

Flags:

- `--file <seed.sql>`
- `--dry-run`
- `--force`
- `--yes`

Examples:

```bash
goframe seed --config goframe.yaml --seeds seeds --dry-run
goframe seed --config goframe.yaml --seeds seeds --file 001_users.sql
goframe seed --config goframe.yaml --seeds seeds --force
```

## 13. Admin User Creation

```bash
goframe createuser --config goframe.yaml \
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
goframe changepassword admin --config goframe.yaml --password newsecret123 --no-input
```

## 13.1 Cache and sessions

Create SQL table for DB-based cache:

```bash
goframe createcachetable --config goframe.yaml
goframe createcachetable --config goframe.yaml --dry-run
```

Clear expired sessions (or all sessions):

```bash
goframe clearsessions --config goframe.yaml
goframe clearsessions --config goframe.yaml --all
goframe clearsessions --config goframe.yaml --dry-run
```

## 13.2 Additional CLI Commands

For i18n (`makemessages`, `compilemessages`), static file management (`collectstatic`, `findstatic`), content type cleanup, geospatial inspection (`ogrinspect`), advanced migration maintenance (`optimizemigration`, `squashmigrations`), and mail/provider configuration, see the individual guides in `docs/`.

## 14. SQL Shell

Ad hoc execution:

```bash
goframe shell --config goframe.yaml -c "SELECT count(*) FROM users"
goframe shell --config goframe.yaml --sandbox -c "SELECT count(*) FROM users"
```

Interactive mode:

```bash
goframe shell --config goframe.yaml
```

It also supports `stdin` input (SQL scripts).
With `--sandbox`, execution is limited to read-only SQL statements.

## 14.1 Background tasks (Asynq)

The `goframe new` scaffold generates:

- `cmd/worker/main.go`: worker entrypoint
- `internal/tasks/article_events.go`: sample handler registration

Run:

```bash
go run ./cmd/worker
```

Requires `redis_url` in `goframe.yaml`.

For request-to-job correlation in tracing/logging, enqueue jobs with context:

```go
info, err := manager.EnqueueJSONCtx(r.Context(), tasks.TaskArticleCreated, map[string]any{
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

info, err := manager.EnqueueJSONCtxWithPolicy(r.Context(), tasks.TaskArticleCreated, map[string]any{
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

For explicit cron-style scheduling, use the scheduler wrapper in `pkg/tasks`:

```go
scheduler, err := tasks.NewScheduler(tasks.SchedulerConfig{
    RedisURL: "redis://127.0.0.1:6379/0",
})
if err != nil {
    return err
}
defer scheduler.Close()

policy := tasks.DefaultEnqueuePolicy()
policy.Queue = "maintenance"
policy.MaxRetry = 1

_, err = scheduler.Register(tasks.PeriodicTask{
    Spec:     "@every 1m",
    TaskType: "sessions.cleanup",
    Payload: map[string]any{
        "scope": "expired",
    },
    Policy: policy,
})
if err != nil {
    return err
}

if err := scheduler.Start(); err != nil {
    return err
}
```

Current scope:

- explicit scheduler construction from `redis_url`
- periodic registration through `PeriodicTask`
- reuse of the same queue/retry/timeout/retention policy subset used by `EnqueueJSONCtxWithPolicy(...)`
- runtime inspection of registered scheduler entries through `tasks.InspectRuntime(...)`

Not in scope yet:

- scaffolded scheduler entrypoints
- cron abstraction across multiple backends
- outbox-backed periodic delivery guarantees

## 14.3 Signals and distributed relay

`pkg/signals` remains the main in-process event bus. For cross-process delivery, GoFrame now also exposes a small Redis relay instead of a hidden event framework.

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
- delivery guarantees or outbox semantics

## 15. Generators (`generate`)

```bash
goframe generate model User
goframe generate handler User
goframe generate migration add_users_table
goframe generate resource Project
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
goframe routes --config goframe.yaml
goframe routes --config goframe.yaml --json
goframe routes --config goframe.yaml --path /api --verbose
```

## 16.2 health

```bash
goframe health --config goframe.yaml
goframe health --config goframe.yaml --json --timeout 5s
```

## 17. Production Guardrails

When `env: production`:

- destructive operations (`migrate down/reset/refresh`, `seed`) require confirmation
- in non-interactive CI use `--force` or `--yes`

CI/CD recommendation:

- use `--force` explicitly
- always run `health` and post-deploy smoke tests

## 18. Recommended Development Flow

1. Generate project/module with `goframe new`.
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
goframe help
goframe version
goframe new <name> [--module ...] [--out ...] [--port ...] [--template mvc] [--force]
goframe startapp <name> [--out ...] [--migrations ...] [--skip-migration] [--force]
goframe serve [--config ...] [--host ...] [--port ...]
goframe migrate [--config ...] [--migrations ...] [--force] [--yes] [action]
goframe sqlmigrate [--migrations ...] [--down] <migration_id_or_name>
goframe sqlflush [--config ...]
goframe sqlsequencereset [--config ...] [tables...]
goframe flush [--config ...] [--force] [--yes] [--dry-run]
goframe diffsettings [--config ...] [--all] [--json]
goframe createcachetable [--config ...] [--table goframe_cache_entries] [--dry-run]
goframe remove_stale_contenttypes [--config ...] [--table goframe_content_types] [--column model] [--keep custom1,custom2] [--dry-run] [--force] [--yes]
goframe makemessages [--config ...] [--locale es] [--domain messages] [--input .] [--extensions .go,.html,.templ] [--locales-path locales] [--output ...] [--dry-run]
goframe compilemessages [--config ...] [--locale es] [--domain messages] [--locales-path locales] [--output ...] [--dry-run]
goframe collectstatic [--config ...] [--output static_collected] [--source internal/web/static] [--clear] [--dry-run]
goframe findstatic [--config ...] [--source internal/web/static] [--first] [--json] <asset_path_or_glob> [more...]
goframe optimizemigration [--migrations migrations] [--down] [--write] <migration_id_or_name>
goframe squashmigrations [--migrations migrations] --from <migration> --to <migration> [--name baseline] [--write] [--archive-old] [--force] [--dry-run] [--print-sql]
goframe sendtestemail [--config ...] --to dev@example.com[,ops@example.com] [--from ...] [--subject ...] [--body ...] [--timeout 10s] [--dry-run]
goframe mailproviders [--config ...] [--json]
goframe plugin list [--config ...] [--timeout 2s] [--json]
goframe plugin doctor [--config ...] [--timeout 2s] [--json]
goframe plugin test [--config ...] --provider <name> --capability <domain.action> [--timeout 2s] [--execute] [--json]
goframe inspectdb [--config ...] [--tables users,posts] [--exclude ...] [--package models] [--output internal/models/inspected.go]
goframe ogrinspect [--config ...] [--tables places,roads] [--exclude ...] [--package models] [--output internal/models/geospatial.go] [--all]
goframe dumpdata [--config ...] [--tables users,posts] [--exclude ...] [--output fixtures.json]
goframe loaddata [--config ...] [--tables users] [--truncate] [--dry-run] [--force] [--yes] <fixture.json>
goframe seed [--config ...] [--seeds ...] [--file ...] [--dry-run] [--force] [--yes]
goframe createuser [--config ...] [--username ...] [--email ...] [--password ...] [--superuser] [--no-input]
goframe changepassword [--config ...] [--username ...] [--password ...] [--no-input] <username>
goframe clearsessions [--config ...] [--table goframe_sessions] [--all] [--dry-run]
goframe shell [--config ...] [--command ...|-c ...] [--timeout 10s] [--sandbox]
goframe generate [--out ...] [--migrations ...] [--force] <model|handler|migration|resource> <name>
goframe test [--run ...] [--count 1] [--race] [--v] [--failfast] [--cover] [--timeout ...] [--dry-run] [packages...]
goframe testserver [--config ...] [--fixture ...] [--tables users] [--truncate] [--dry-run] [--host ...] [--port ...] <fixture.json>
goframe routes [--config ...] [--path ...] [--json] [--verbose]
goframe health [--config ...] [--timeout 3s] [--json] [--deploy]
```

Global output options (before command):

```bash
goframe --output plain|pretty|json <command> ...
goframe --color auto|always|never <command> ...
goframe --symbols|--no-symbols <command> ...
goframe --json <command> ...            # shorthand for --output json
```

Compatibility aliases:

```bash
goframe runserver [addr:port]
goframe startproject <name> [new flags]
goframe makemigrations <name>
goframe showmigrations [--config ...] [--migrations ...]
goframe createsuperuser [createuser flags]
goframe dbshell [shell flags]
goframe check [health flags]             # health alias
goframe check --deploy [--config ...]    # deployment hardening checks
```

In projects generated with `goframe new`, you also have:

```bash
go run ./cmd/server
go run ./cmd/worker
```

## 23. Suggested Next Reading

- documentation map: `docs/INDEX.md`
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
