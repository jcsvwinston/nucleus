# GoFrame Final Developer Manual

Reference date: 2026-03-31.

This is the main guide to build, operate, and deploy applications with GoFrame.

## 1. Objective

GoFrame is a Go web framework inspired by Django, focused on:

- MVC + REST API applications
- integrated admin panel
- operational lifecycle CLI (scaffold, migrations, seed, inspection)
- Bun-first SQL foundation with a stable model contract

## 2. Current Scope (Actual State)

As of today, GoFrame includes:

- `pkg/app`: application container (config, logger, router, DB, admin, lifecycle)
- `pkg/db`: SQL connectivity (Bun-first, GORM-compatible), health checks, file-based SQL migrations
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
- `docs/PROJECT_LAYOUT.md`
- `docs/RELEASE_CHECKLIST.md`

## 3. Requirements

## 3.1 Runtime and toolchain

- Minimum supported Go version: `1.23`
- Recommended Go for development/release: `1.26.x`
- Node.js: required for admin UI syntax checks in CI/rehearsal

Full policy:

- `docs/GO_VERSION_POLICY.md`

## 3.2 Database

Supported SQL engines by URL:

- SQLite: `sqlite://app.db` (or `:memory:`)
- PostgreSQL: `postgres://...` or `postgresql://...`
- MySQL: `mysql://...`

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
- `http://localhost:8080/admin`

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
- `Models`
- `Admin`

Key methods:

- `RegisterModel(...)`
- `MountAdmin()`
- `Run(ctx)`
- `Shutdown(ctx)`
- `OnShutdown(fn)`

## 6.2 Router and lifecycle

- `chi`-based router
- `Run` starts HTTP server
- clean shutdown by context cancel or `SIGINT/SIGTERM`

## 6.3 Auto-mounted admin

`app.New` mounts admin automatically at `admin_prefix` (default `/admin`).

## 7. Configuration (`goframe.yaml`)

Minimum example:

```yaml
database_engine: bun
database_url: sqlite://app.db
redis_url: redis://127.0.0.1:6379/0
host: 0.0.0.0
port: 8080
env: development
log_level: info
log_format: text
otlp_endpoint: ""
rate_limit_requests: 0
rate_limit_window: 1m
admin_prefix: /admin
admin_title: My Admin
```

Frequent fields:

- server: `host`, `port`, `read_timeout`, `write_timeout`, `idle_timeout`
- database: `database_engine`, `database_url`, `database_max_open`, `database_max_idle`, `database_max_lifetime`
- queue/background: `redis_url`
- auth/session: `jwt_secret`, `jwt_expiry`, `session_lifetime`
- admin: `admin_prefix`, `admin_title`
- mail: `mail_driver`, `mail_from`, `smtp_*`, `sendgrid_api_key`, `sendgrid_endpoint`
- observability: `log_level`, `log_format`, `metrics_path`, `otlp_endpoint`
- HTTP hardening: `rate_limit_requests`, `rate_limit_window`
- environment: `env`, `debug`

Environment override prefix: `GOFRAME_`.
Example:

- `GOFRAME_PORT=9090`
- `GOFRAME_DATABASE_URL=postgres://...`

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

`db` / `gorm` (storage metadata):

- `column:<name>`
- `primaryKey` / `pk`
- `required`
- `readonly`

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
    Email string `db:"column:email;required" validate:"required,email" admin:"list,search"`
    Role  string `db:"column:role" admin:"list,filter,choices:admin|Admin;user|User"`
    Bio   string `db:"column:bio" admin:"label:Biography"`
}
```

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

## 10. Admin Panel

## 10.1 Setup

The panel is created and mounted from `app.New`.

Configuration:

- `admin_prefix` (default `/admin`)
- `admin_title`

## 10.2 Capabilities

- model listing
- per-model schema
- CRUD
- filters and search
- CSV export
- bulk actions

## 10.3 Admin API

Main routes:

- `GET /admin/api/models`
- `GET /admin/api/models/{name}/schema`
- `GET /admin/api/models/{name}`
- `POST /admin/api/models/{name}`
- `GET /admin/api/models/{name}/{id}`
- `PUT /admin/api/models/{name}/{id}`
- `DELETE /admin/api/models/{name}/{id}`
- `POST /admin/api/models/{name}/bulk`
- `GET /admin/api/models/{name}/export`

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

Change an existing admin password:

```bash
goframe changepassword admin --config goframe.yaml --password newsecret123 --no-input
```

## 13.1 Cache and sessions (Django parity)

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

## 13.2 i18n (Django parity)

Extract translatable strings into `.po` catalogs:

```bash
goframe makemessages --config goframe.yaml --locale es --input .
goframe makemessages --config goframe.yaml --locale es --domain messages --extensions .go,.html,.templ
goframe makemessages --config goframe.yaml --dry-run
```

Compile `.po` catalogs to `.json` bundles:

```bash
goframe compilemessages --config goframe.yaml --locale es
goframe compilemessages --config goframe.yaml
goframe compilemessages --config goframe.yaml --dry-run
```

## 13.3 Static files (`collectstatic`, `findstatic`)

Collect static assets into `static_root`:

```bash
goframe collectstatic --config goframe.yaml
goframe collectstatic --config goframe.yaml --output public/assets --clear
goframe collectstatic --config goframe.yaml --dry-run
```

Find static assets by path (or glob pattern):

```bash
goframe findstatic --config goframe.yaml app.css
goframe findstatic --config goframe.yaml "js/*.js"
goframe findstatic --config goframe.yaml --first admin.css
```

## 13.4 Content type cleanup (`remove_stale_contenttypes`)

Delete orphan records in content type table:

```bash
goframe remove_stale_contenttypes --config goframe.yaml --dry-run
goframe remove_stale_contenttypes --config goframe.yaml
goframe remove_stale_contenttypes --config goframe.yaml --table goframe_content_types --column model --keep custom_type
```

`--dry-run` prints SQL and candidate entries without deleting data.

## 13.5 Geospatial inspection (`ogrinspect`)

Generate Go structs for tables with geospatial columns (`geometry`/`geography`):

```bash
goframe ogrinspect --config goframe.yaml --output internal/models/geospatial.go
goframe ogrinspect --config goframe.yaml --tables places,roads
goframe ogrinspect --config goframe.yaml --all --output internal/models/all_from_ogrinspect.go
```

By default, it filters geospatial tables.
Use `--all` to include non-geospatial tables.

## 13.6 Advanced migration maintenance

Optimize a SQL migration by removing no-op/comments/exact duplicates:

```bash
goframe optimizemigration --migrations migrations add_users_table
goframe optimizemigration --migrations migrations --write add_users_table
goframe optimizemigration --migrations migrations --down --write add_users_table
```

Squash a migration range:

```bash
goframe squashmigrations --migrations migrations --from init --to add_users --name baseline
goframe squashmigrations --migrations migrations --from init --to add_users --name baseline --write
goframe squashmigrations --migrations migrations --from init --to add_users --name baseline --write --archive-old
```

## 13.7 Provider-based test email (`mail_driver`)

Validate configured mail provider with a test email:

```bash
goframe sendtestemail --config goframe.yaml --to dev@example.com --dry-run
goframe sendtestemail --config goframe.yaml --driver sendgrid --to dev@example.com --dry-run
goframe sendtestemail --config goframe.yaml --to dev@example.com --subject "mail check"
goframe sendtestemail --config goframe.yaml --to dev1@example.com,dev2@example.com --timeout 15s
```

Core-supported drivers:

- `noop` (safe default for development; does not actually send)
- `smtp`
- `sendgrid`

SMTP example:

```yaml
mail_driver: smtp
mail_from: noreply@example.com
smtp_host: smtp.example.com
smtp_port: 587
smtp_user: user
smtp_pass: secret
```

SendGrid example:

```yaml
mail_driver: sendgrid
mail_from: noreply@example.com
sendgrid_api_key: SG.xxxxx
sendgrid_endpoint: https://api.sendgrid.com/v3/mail/send
```

External plugin support:

- set `mail_driver: mailgun` (or any custom name)
- add executable `goframe-mail-mailgun` to `PATH`
- GoFrame sends JSON to `stdin` with `from`, `to`, `subject`, `body`, `headers`
- exit code `0` means accepted; non-zero means operational error

Capability-based plugin diagnostics:

- add executable `goframe-plugin-<provider>` to `PATH` for generic capability discovery
- use `plugin list` to inspect available providers/capabilities
- use `plugin doctor` to validate runtime wiring
- use `plugin test` to smoke-check a provider capability

Quick diagnostics of available providers:

```bash
goframe mailproviders --config goframe.yaml
goframe mailproviders --config goframe.yaml --json
goframe plugin list --config goframe.yaml
goframe plugin doctor --config goframe.yaml
goframe plugin test --provider sendgrid --capability mail.send
```

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

- model
- CRUD scaffold handler
- handler test
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

Local:

```bash
go test ./...
```

Official example smoke test:

```bash
go test ./examples/mvc_api -run TestExampleMVCAPIAdmin_Smoke -v
```

Release rehearsal:

```bash
./scripts/release/rehearse_rc.sh
```

## 20. Real Deployment

## 20.1 Recommended strategy

1. Download release binary for target OS/architecture.
2. Verify `checksums.txt`.
3. Provision `goframe.yaml`.
4. Run your app binary (`cmd/server`) or equivalent service.

## 20.2 Post-deploy health

Validate:

- app endpoint (`/`)
- API endpoint (`/api/health`)
- admin (`/admin`)
- `goframe health --json`

## 20.3 Release artifacts

Each release publishes:

- 6 packaged binaries (3 OS x 2 architectures)
- SHA256 checksums

## 21. Troubleshooting

## 21.1 `go install` fails due to module path

If you get module mismatch errors, review `module` in `go.mod` and temporarily use:

- release binaries, or
- local build from this repository

## 21.2 `admin` has no models

Common cause:

- `RegisterModel(...)` was not called

## 21.3 `migrate` does not apply changes

Review:

- `migrations` directory
- `.up.sql` / `.down.sql` files
- status via `goframe migrate status`

## 21.4 `health` returns degraded

Review:

- `database_url`
- connectivity
- credentials

## 21.5 `check --deploy` fails on mail

If report includes `deploy.mail_*` components, review:

- `mail_driver` (avoid `noop` in production)
- valid `mail_from`
- driver-specific settings:
  - SMTP: `smtp_host` + `smtp_port`
  - SendGrid: `sendgrid_api_key`

## 21.6 `remove_stale_contenttypes` does not delete rows

Review:

- target table (`--table`)
- model name column (`--column`)
- `--dry-run` output to validate generated SQL

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

Django-style aliases:

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

- quick onboarding: `docs/QUICKSTART.md`
- step-by-step tutorial: `docs/DETAILED_TUTORIAL.md`
- recommended layout: `docs/PROJECT_LAYOUT.md`
- CLI best practices: `docs/CLI_BEST_PRACTICES.md`
- CLI parity with Django: `docs/CLI_DJANGO_PARITY.md`
- email providers and plugins: `docs/MAIL_PROVIDERS.md`
- enterprise roadmap: `docs/ENTERPRISE_ROADMAP.md`
- release checklist: `docs/RELEASE_CHECKLIST.md`
