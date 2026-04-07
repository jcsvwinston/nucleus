# GoFrame

[![CI](https://github.com/jcsvwinston/GoFrame/actions/workflows/ci.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/ci.yml)
[![Rehearsal](https://github.com/jcsvwinston/GoFrame/actions/workflows/rehearsal.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/rehearsal.yml)
[![Release Asset Smoke](https://github.com/jcsvwinston/GoFrame/actions/workflows/release_asset_smoke.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/release_asset_smoke.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jcsvwinston/GoFrame.svg)](https://pkg.go.dev/github.com/jcsvwinston/GoFrame)

Enterprise-oriented web framework for Go, built for long-lived systems.

GoFrame combines a native router stack, SQL-first data access over `database/sql`, auto-generated admin, background jobs with Asynq, and an operations-first CLI for real production workflows.

## Why GoFrame

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
- Embedded admin UI (`pkg/admin`) for CRUD, schema inspection, filters, CSV export, bulk operations, and session observability.
- Task runtime (`pkg/tasks`) with Asynq manager + worker scaffold.
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
    models/
    tasks/
    web/
  migrations/
  seeds/
  goframe.yaml
```

Reference: [docs/PROJECT_LAYOUT.md](docs/PROJECT_LAYOUT.md)

## Strategic Direction

GoFrame is actively developed and stable for pre-1.0 usage (`v0.x`).

Canonical strategy and governance docs:

- [docs/ENTERPRISE_LONG_TERM_ROADMAP.md](docs/ENTERPRISE_LONG_TERM_ROADMAP.md)
- [docs/COMPATIBILITY_SLO.md](docs/COMPATIBILITY_SLO.md)
- [docs/VERSIONING.md](docs/VERSIONING.md)

## Documentation

Start here:

- [docs/INDEX.md](docs/INDEX.md)
- [docs/QUICKSTART.md](docs/QUICKSTART.md)
- [docs/DEVELOPER_MANUAL.md](docs/DEVELOPER_MANUAL.md)
- [docs/PROJECT_LAYOUT.md](docs/PROJECT_LAYOUT.md)
- [docs/MODELING_MULTI_DATABASE.md](docs/MODELING_MULTI_DATABASE.md)
- [docs/API_CONTRACT_INVENTORY.md](docs/API_CONTRACT_INVENTORY.md)
- [docs/CLI_CONTRACT_MATRIX.md](docs/CLI_CONTRACT_MATRIX.md)
- [docs/CONFIG_KEY_REGISTRY.md](docs/CONFIG_KEY_REGISTRY.md)
- [docs/PLUGIN_SDK.md](docs/PLUGIN_SDK.md)

Operations and release docs:

- [docs/CI_MATRIX.md](docs/CI_MATRIX.md)
- [docs/RELEASE_CHECKLIST.md](docs/RELEASE_CHECKLIST.md)
- [docs/DEPRECATION_TEMPLATE.md](docs/DEPRECATION_TEMPLATE.md)
- [docs/MIGRATION_ASSISTANT_CONVENTIONS.md](docs/MIGRATION_ASSISTANT_CONVENTIONS.md)
- [docs/GO_VERSION_POLICY.md](docs/GO_VERSION_POLICY.md)
- [CHANGELOG.md](CHANGELOG.md)

## Compatibility

- Minimum supported Go: `1.24`
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
