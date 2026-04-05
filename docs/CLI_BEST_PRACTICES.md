# Complete CLI: Best Practices (MVC + API)

Reference date: 2026-04-05.
Status: Current.

This document summarizes patterns observed in mature framework CLIs and how they map to GoFrame.

## Benchmarked frameworks

- Django `django-admin` / `manage.py`
- Rails `bin/rails`
- Laravel Artisan
- Phoenix Mix (`mix phx.*`, `mix ecto.*`)

Official sources are listed at the end of this document.

## Common patterns in large frameworks

1. Discoverability and consistent help
- One root command with a short, clear command list.
- Help per subcommand (`help <cmd>` or `<cmd> --help`).
- Stable, predictable command names.

2. Core operational commands
- Local server (`serve`/`server`/`runserver`).
- Full migration lifecycle (`up`, `down/rollback`, `status`, `steps`).
- Route listing (`routes`, `route:list`, `phx.routes`).
- Interactive shell/console for inspection and debugging.
- Health/check commands for environment validation.

3. Code generation (scaffolding)
- Generators for models, handlers/controllers, and migrations.
- Predictable, easy-to-edit output structure.
- Accidental overwrite prevention (explicit `--force` mode).

4. Operational safety and automation
- Non-interactive mode for CI/CD (`--no-input`).
- Safety flags for destructive production actions (`--force`, usually with prior confirmation in other frameworks).
- Consistent exit codes (0 success, non-zero error) for pipelines.

5. Migration semantics
- Timestamp/version ordering in file names.
- Migration tracking table.
- Partial rollback support (`step`) and status output.

## Current GoFrame CLI status

Implemented in `cmd/goframe` + `internal/cli`:

- `serve`:
  - starts the server with `--config`, `--host`, `--port`.
- `migrate`:
  - `up`, `down`, `steps`, `status`, `create`, `reset`, `refresh`.
  - production guardrails with `--force` / `--yes` for destructive actions.
- `sqlmigrate` / `sqlflush` / `sqlsequencereset` / `flush`:
  - SQL migration inspection and data/sequence maintenance.
  - `flush` includes production guardrails (`--force` / `--yes`).
- `diffsettings`:
  - compares effective configuration against framework defaults.
- `createcachetable`:
  - creates SQL cache table for DB-based cache backends.
  - supports `--dry-run` to inspect generated SQL.
- `remove_stale_contenttypes`:
  - operational cleanup of stale content types vs current SQL schema.
  - supports `--dry-run` and production guardrails with `--force` / `--yes`.
- `ogrinspect`:
  - SQL-first introspection of geospatial tables to generate Go structs.
  - filters by `geometry/geography` columns by default (`--all` includes everything).
- `makemessages` / `compilemessages`:
  - Django-style i18n flow (`.po` source and compiled `.json`).
  - `makemessages` extracts strings from code/templates; `compilemessages` compiles one or more locales.
- `collectstatic` / `findstatic`:
  - Django-style static cycle to gather assets and resolve effective paths.
  - `collectstatic` supports `--dry-run` and `--clear`; `findstatic` supports pattern search and `--first`.
- `optimizemigration` / `squashmigrations`:
  - SQL-first migration maintenance (statement normalization and range squash).
  - `squashmigrations` supports plan mode and source migration archiving.
- `sendtestemail`:
  - operational check of configured `mail_driver` (`smtp`, `sendgrid`, or external plugin `goframe-plugin-<driver>` with legacy fallback `goframe-mail-<driver>`).
  - supports `--dry-run` and temporary `--driver` override.
- `mailproviders`:
  - lists registered drivers and detected external plugins in `PATH` (`goframe-mail-<driver>`).
  - useful for diagnosing mail extensions in local/CI environments.
- `plugin list` / `plugin doctor` / `plugin test`:
  - capability-based plugin inventory and diagnostics (`goframe-plugin-<provider>` + legacy `goframe-mail-<driver>`).
  - `plugin test` provides discovery and optional execute-smoke mode for external generic plugins.
- `inspectdb`:
  - SQL schema introspection and Go struct generation with `db` tags.
- `dumpdata` / `loaddata`:
  - fixture JSON export/import by table.
  - `loaddata --truncate` includes production guardrails (`--force` / `--yes`).
- `routes`:
  - route listing, prefix filtering, and JSON output.
- `health`:
  - dependency/DB check with timeout and text/JSON output.
  - `--deploy` adds configuration hardening checks, including mail readiness and session/cookie production posture.
- `generate`:
  - scaffolds `model`, `handler`, `migration`, and `resource` (base CRUD).
- `new`:
  - full MVC + API + Admin project bootstrap with recommended structure.
- `startapp`:
  - module scaffold in an existing project (`internal/models/controllers/tasks/web/templates`).
- `seed`:
  - executes ordered SQL seed files.
  - production guardrails with `--force` / `--yes`.
- `createuser`:
  - create/update admin user, non-interactive mode with `--no-input`.
- `changepassword`:
  - password rotation for existing admin users (`--no-input` in CI).
- `clearsessions`:
  - removes expired sessions by default, or all sessions with `--all`.
  - supports `--dry-run` to review SQL before execution.
- `shell`:
  - interactive and non-interactive mode (`-c` or stdin).
  - `--sandbox` mode limits to read-only SQL statements.
- `test`:
  - `go test` wrapper with project defaults and flags (`--run`, `--race`, `--cover`, `--timeout`, `--dry-run`).
- `testserver`:
  - loads fixtures (`loaddata`) and starts a local server in one command.
  - supports `--dry-run` to validate the plan without starting the server.
- extensibility:
  - external commands `goframe-<name>` in `PATH` (plugin-like behavior).
- Django-style aliases:
  - `runserver`, `startproject`, `makemigrations`, `showmigrations`, `createsuperuser`, `dbshell`, `check`.

## Gaps for future hardening

1. Advanced shell UX
- Persistent history and multiline editing.

2. CLI test coverage across SQL engine matrix
- SQLite is already covered; extend CI to PostgreSQL/MySQL.

## Practical v1 CLI readiness criteria

- [x] Core commands available.
- [x] Root and subcommand help.
- [x] Consistent exit codes for automation.
- [x] Migrations with status and rollback.
- [x] Basic scaffold generation.
- [x] Unified production guardrails (`--force`/`--yes` + interactive confirmation).
- [x] Per-project plugins/custom commands (via `goframe-<name>`).

## Official sources

- Django: [django-admin and manage.py](https://docs.djangoproject.com/en/6.0/ref/django-admin/)
- Django: [Custom management commands](https://docs.djangoproject.com/en/6.0/howto/custom-management-commands/)
- Rails: [The Rails Command Line](https://guides.rubyonrails.org/command_line.html)
- Rails: [Active Record Migrations](https://guides.rubyonrails.org/active_record_migrations.html)
- Laravel: [Artisan Console](https://laravel.com/docs/12.x/artisan)
- Laravel: [Database Migrations](https://laravel.com/docs/12.x/migrations)
- Laravel: [Routing (`route:list`)](https://laravel.com/docs/12.x/routing)
- Laravel: [Database Seeding](https://laravel.com/docs/12.x/seeding)
- Phoenix: [Phoenix Mix Tasks (`mix phx.routes`, generators)](https://hexdocs.pm/phoenix/1.4.17/phoenix_mix_tasks.html)
- Phoenix/Ecto: [Ecto in Phoenix (`mix ecto.migrate`, `mix ecto.rollback`)](https://hexdocs.pm/phoenix/ecto.html)

Detailed GoFrame vs Django 6.0 comparison:

- `docs/CLI_DJANGO_PARITY.md`
