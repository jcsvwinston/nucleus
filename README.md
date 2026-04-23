# GoFrame

[![CI](https://github.com/jcsvwinston/GoFrame/actions/workflows/ci.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/ci.yml)
[![Rehearsal](https://github.com/jcsvwinston/GoFrame/actions/workflows/rehearsal.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/rehearsal.yml)
[![Release Asset Smoke](https://github.com/jcsvwinston/GoFrame/actions/workflows/release_asset_smoke.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/release_asset_smoke.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jcsvwinston/GoFrame.svg)](https://pkg.go.dev/github.com/jcsvwinston/GoFrame)

Enterprise-oriented web framework for Go, built for long-lived systems.

GoFrame combines a native router stack, SQL-first data access over `database/sql`, auto-generated admin, background jobs with Asynq, and an operations-first CLI for real production workflows.

Strategically, GoFrame aims to fuse two strengths into one Go framework:

- Django's application and operations ergonomics
- Encore's platform-oriented developer experience for production systems

## Why GoFrame

- Strategic goal: bring Django + Encore together in one Go-native framework without giving up stdlib-first runtime design.
- Long-term upgrade safety: explicit compatibility policy and release gates for stable contracts.
- SQL-first by design: runtime and tooling aligned to `database/sql` with practical migration/fixture/introspection commands.
- Built for operations: deploy checks, health diagnostics, observability baseline, and release rehearsal baked in.
- Extensible platform: capability-based provider plugins and external CLI extensions.

## What You Get Today

- App container (`pkg/app`) with lifecycle, config, logger, router, DB, admin, session, and mail wiring.
- Multi-DB runtime wiring with named aliases (`database_default` + `databases.<alias>.url`).
- MultiSite/MultiTenant request scope resolution (`host`/subdomain/header) with tenant-aware DB alias routing helpers.
- HTTP stack (`pkg/router`) with security middleware, CSRF, advanced rate-limit dimensions, and OTel HTTP telemetry.
- Auth/Authz (`pkg/auth`, `pkg/authz`) with JWT, server-side sessions (`memory|sql|redis`), and Casbin integration points.
- Model system (`pkg/model`) with metadata extraction, registry, and generic CRUD.
- Embedded admin UI (`pkg/admin`) for CRUD, schema inspection, filters, CSV export, bulk operations, session observability, task runtime visibility, outbox state, and distributed topology.
- Task runtime (`pkg/tasks`) with Asynq manager, explicit enqueue/scheduler helpers, runtime ops, and worker scaffold.
- Transactional outbox runtime (`pkg/outbox`) with SQL-backed enqueue, delivery dispatcher, and admin-visible runtime inspection.
- Mail layer (`pkg/mail`) with `noop`, `smtp`, `sendgrid`, and external plugin runtime (`goframe-plugin-<driver>` with legacy fallback `goframe-mail-<driver>`), plus capability discovery via `pkg/plugins`.
- Rich CLI (`cmd/goframe`) with scaffolding, operations, data lifecycle, plugin diagnostics, and testing workflows.

## Install

### CLI from source

```bash
go install github.com/jcsvwinston/GoFrame/cmd/goframe@latest
goframe version
```

### CLI from releases

See GitHub Releases:

- https://github.com/jcsvwinston/GoFrame/releases

## Quick Start

### 1. Create a new project

```bash
goframe new myapp --module github.com/acme/myapp --template mvc
cd myapp
go mod tidy
```

### 2. Run server and worker

```bash
go run ./cmd/server
go run ./cmd/worker
```

If you do not need background jobs yet, run only the server.

### 3. Open the app

- Web: `http://localhost:8080/`
- API: `http://localhost:8080/api/health`
- Admin: `http://localhost:8080/admin`

Admin access defaults:

- Bootstrap mode: if there are no rows in `goframe_admin_users`, `/admin` is accessible to help initial setup.
- Protected mode: once at least one admin user exists (`goframe createuser`), `/admin` requires login at `/admin/login`.

## CLI Highlights

GoFrame ships a broad set of framework commands, including:

- Core runtime: `serve`, `routes`, `health`, `check --deploy`
- Scaffolding: `new`, `startapp`, `generate`
- Migrations: `migrate`, `sqlmigrate`, `sqlflush`, `sqlsequencereset`, `flush`, `optimizemigration`, `squashmigrations`
- Data/fixtures/schema: `dumpdata`, `loaddata`, `inspectdb`, `ogrinspect`
- Auth/admin ops: `createuser`, `changepassword`, `clearsessions`, `remove_stale_contenttypes`
- i18n/static: `makemessages`, `compilemessages`, `collectstatic`, `findstatic`
- Mail ops: `sendtestemail`, `mailproviders`
- Plugin ops: `plugin list`, `plugin doctor`, `plugin test`
- Dev/test: `shell`, `test`, `testserver`

Compatibility aliases are also available:

- `runserver`, `startproject`, `makemigrations`, `showmigrations`, `createsuperuser`, `dbshell`, `check`

For full usage:

```bash
goframe help
```

Global output controls are available for every command:

```bash
goframe --output plain|pretty|json <command> ...
goframe --color auto|always|never <command> ...
goframe --symbols|--no-symbols <command> ...
```

## Mail Providers and Plugins

GoFrame supports multiple mail delivery strategies:

- Built-in: `noop`, `smtp`, `sendgrid`
- In-process registration via `mail.RegisterProvider(...)`
- External plugin binaries via:
  - `goframe-plugin-<provider>` (capability-based discovery)
  - `goframe-mail-<driver>` (legacy compatibility bridge)

Useful commands:

```bash
goframe sendtestemail --config goframe.yaml --to dev@example.com --dry-run
goframe sendtestemail --config goframe.yaml --driver sendgrid --to dev@example.com --dry-run
goframe mailproviders --config goframe.yaml
goframe mailproviders --config goframe.yaml --json
goframe plugin list --config goframe.yaml
goframe plugin doctor --config goframe.yaml
goframe plugin test --provider sendgrid --capability mail.send
```

## Architecture

Recommended generated project layout:

```text
myapp/
  cmd/
    server/
    worker/
  internal/
    controllers/
    contracts/
    models/
    services/
    repositories/
    tasks/
    web/
  migrations/
  seeds/
  goframe.yaml
```

Reference: [docs/reference/PROJECT_LAYOUT.md](docs/reference/PROJECT_LAYOUT.md)

Generated module-aware scaffolds now also seed:

- `internal/contracts` for OpenAPI-oriented contract registration
- `internal/services` for application use cases
- `internal/repositories` for persistence boundaries

## Strategic Direction

GoFrame is actively developed and stable for pre-1.0 usage (`v0.x`).

Canonical strategy and governance docs:

- [docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md](docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md)
- [docs/governance/COMPATIBILITY_SLO.md](docs/governance/COMPATIBILITY_SLO.md)

## Documentation

Start here:

- [docs/INDEX.md](docs/INDEX.md)
- [docs/QUICKSTART.md](docs/QUICKSTART.md)
- [docs/reference/DEVELOPER_MANUAL.md](docs/reference/DEVELOPER_MANUAL.md)
- [docs/reference/PROJECT_LAYOUT.md](docs/reference/PROJECT_LAYOUT.md)
- [SPEC.md](SPEC.md)
- [docs/guides/MODELING_MULTI_DATABASE.md](docs/guides/MODELING_MULTI_DATABASE.md)
- [docs/reference/API_CONTRACT_INVENTORY.md](docs/reference/API_CONTRACT_INVENTORY.md)
- [docs/reference/CLI_CONTRACT_MATRIX.md](docs/reference/CLI_CONTRACT_MATRIX.md)
- [docs/reference/CONFIG_KEY_REGISTRY.md](docs/reference/CONFIG_KEY_REGISTRY.md)
- [docs/reference/PLUGIN_SDK.md](docs/reference/PLUGIN_SDK.md)

Operations and release docs:

- [docs/governance/CI_MATRIX.md](docs/governance/CI_MATRIX.md)
- [docs/governance/RELEASE_CHECKLIST.md](docs/governance/RELEASE_CHECKLIST.md)
- [docs/governance/DEPRECATION_TEMPLATE.md](docs/governance/DEPRECATION_TEMPLATE.md)
- [docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md](docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md)
- [docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md](docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md)
- [CHANGELOG.md](CHANGELOG.md)

## Compatibility

- Minimum supported Go: `1.25`
- Recommended for development/release: `1.26.x`
- Multi-DB config uses `database_default` + `databases.<alias>.url`.
- MultiTenant defaults to `require_isolated_db: true` to prevent shared-DB tenant routing.
- Core-supported SQL URLs: `sqlite://`, `postgres://`/`postgresql://`, `mysql://`
- Enterprise exploratory SQL URLs: `sqlserver://`/`mssql://`, `oracle://`

## Contributing

Contributions are welcome.

- Open issues for bugs, regressions, and feature requests.
- Keep changes small and test-backed.
- Run before opening a PR:

```bash
go test ./...
bash scripts/release/rehearse_rc.sh
```

## License

MIT
