# GoFrame

[![CI](https://github.com/jcsvwinston/GoFrame/actions/workflows/ci.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/ci.yml)
[![Rehearsal](https://github.com/jcsvwinston/GoFrame/actions/workflows/rehearsal.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/rehearsal.yml)
[![Release Asset Smoke](https://github.com/jcsvwinston/GoFrame/actions/workflows/release_asset_smoke.yml/badge.svg)](https://github.com/jcsvwinston/GoFrame/actions/workflows/release_asset_smoke.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/jcsvwinston/GoFrame.svg)](https://pkg.go.dev/github.com/jcsvwinston/GoFrame)

Enterprise-oriented web framework for Go, inspired by Django.

GoFrame combines `chi` routing, Bun-first SQL access, auto-generated admin, background jobs with Asynq, and a Django-style CLI workflow (`manage.py`-like experience for Go teams).

## Why GoFrame

- Fast start, long-term structure: scaffold apps quickly and keep a clean architecture as teams grow.
- SQL-first by design: Bun is the official runtime path, with practical CLI tools for migrations, fixtures, and schema introspection.
- Built-in operations mindset: health checks, deploy checks, static handling, i18n flow, and release rehearsal are first-class.
- Extensible platform: external CLI commands (`goframe-<name>`) and capability-based provider plugins (`goframe-plugin-<provider>`, legacy `goframe-mail-<driver>`).

## What You Get Today

- App container (`pkg/app`) with lifecycle, config, logger, router, DB, admin mount, and mail sender wiring.
- HTTP stack (`pkg/router`) with security middleware, CSRF, rate limit, and OTel HTTP telemetry.
- Auth/Authz (`pkg/auth`, `pkg/authz`) with JWT/session support and Casbin integration points.
- Model system (`pkg/model`) with metadata extraction, registry, generic CRUD.
- Embedded admin UI (`pkg/admin`) for CRUD, schema, filters, CSV export, and bulk operations.
- Task runtime (`pkg/tasks`) with Asynq manager + worker scaffold.
- Mail layer (`pkg/mail`) with `noop`, `smtp`, `sendgrid`, and plugin fallback (`goframe-mail-<driver>`) plus capability discovery via `pkg/plugins`.
- Rich CLI (`cmd/goframe`) with Django-style aliases and operational commands.

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

If you do not need background jobs yet, you can run only the server.

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

Django-style aliases are available:

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
  - `goframe-mail-<driver>` (mail compatibility bridge)

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

## Project Status

GoFrame is actively developed and stable for pre-1.0 usage (`v0.x`).

Current baseline includes:

- Bun-first SQL runtime and migration flow
- Admin UI v1 with strong CRUD ergonomics
- OTel baseline (traces/metrics) and structured logging
- Release automation with CI/rehearsal/smoke workflows

Roadmap and alignment status:

- [docs/ENTERPRISE_ROADMAP.md](docs/ENTERPRISE_ROADMAP.md)
- [docs/V0.6.0_ROADMAP.md](docs/V0.6.0_ROADMAP.md)

## Documentation

Start here:

- [docs/QUICKSTART.md](docs/QUICKSTART.md)
- [docs/DETAILED_TUTORIAL.md](docs/DETAILED_TUTORIAL.md)
- [docs/DEVELOPER_MANUAL.md](docs/DEVELOPER_MANUAL.md)
- [docs/PLUGIN_SDK.md](docs/PLUGIN_SDK.md)

CLI and parity references:

- [docs/CLI_BEST_PRACTICES.md](docs/CLI_BEST_PRACTICES.md)
- [docs/CLI_DJANGO_PARITY.md](docs/CLI_DJANGO_PARITY.md)
- [docs/MAIL_PROVIDERS.md](docs/MAIL_PROVIDERS.md)

Release and governance docs:

- [docs/RELEASE_CHECKLIST.md](docs/RELEASE_CHECKLIST.md)
- [docs/VERSIONING.md](docs/VERSIONING.md)
- [docs/GO_VERSION_POLICY.md](docs/GO_VERSION_POLICY.md)
- [CHANGELOG.md](CHANGELOG.md)

## Compatibility

- Minimum supported Go: `1.23`
- Recommended for development/release: `1.26.x`

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
