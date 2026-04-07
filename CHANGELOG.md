# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
while in pre-1.0 mode (`v0.x.y`).

## [Unreleased]

### Added

- `pkg/plugins` inventory and capability probe package to discover:
  - built-in mail providers as `mail.send` capability providers
  - generic external plugins (`goframe-plugin-<provider>`)
  - legacy external mail plugins (`goframe-mail-<driver>`)
- New plugin diagnostics command group:
  - `goframe plugin list`
  - `goframe plugin doctor`
  - `goframe plugin test --provider <p> --capability <c>`
- Typed Plugin SDK v1 envelope and baseline capability schemas in `pkg/plugins`:
  - request/response envelopes (`version: v1`)
  - capability payload/output structs for `mail.send`, `queue.publish`, and `webhook.deliver`
  - external plugin executor with exit-code/retriable mapping
- Official Plugin SDK v1 example providers:
  - `examples/plugins/mail` (`goframe-plugin-examplemail`, `mail.send`)
  - `examples/plugins/queue` (`goframe-plugin-examplequeue`, `queue.publish`)
  - usage guide in `docs/PLUGIN_EXAMPLES.md`
- Mail runtime bridge now supports capability plugins:
  - preferred external provider binary `goframe-plugin-<driver>` when `mail.send` is advertised
  - legacy fallback `goframe-mail-<driver>`
- Plugin runtime tests now cover success, provider error mapping, and timeout behavior for external execution.
- Session runtime now supports first-class backend selection via config:
  - `session_store: memory|sql|redis`
  - SQL-backed store with automatic session table bootstrap (`session_table`, default `goframe_sessions`)
  - Redis-backed store (`session_redis_url` or `redis_url` fallback)
  - configurable session cookie settings (`session_cookie_*`) and idle timeout
- Session runtime metadata middleware now records serving-node identity in session state:
  - first/last seen timestamps
  - runtime pod, host, and instance identifiers for shared-session environments
- Admin session observability endpoint and UI:
  - `GET /admin/api/sessions`
  - `/admin` sessions dashboard with active-session table, pod/host attribution, and telemetry windows (real-time, last hour, today)
- Advanced in-process rate limiting dimensions:
  - `rate_limit_burst` for controlled token-bucket burst capacity
  - `rate_limit_by_route` for route-scoped budgets
  - `rate_limit_by_role` for role-scoped budgets (JWT claims)
- Added negative-test coverage for security defaults and edge cases:
  - CSRF token mismatch rejection
  - CORS origin allow/deny behavior
  - session config fallback/invalid-store handling
- SQL matrix integration tests for required DB profiles (`PostgreSQL`/`MySQL`):
  - `pkg/db` runtime connect + ping smoke (`GOFRAME_SQL_MATRIX_URL`)
  - `internal/cli` critical command smoke for migrate/health/fixtures/shell (`GOFRAME_SQL_MATRIX_URL`)
- SQL matrix compatibility tests for exploratory DB profiles (`MS SQL Server`/`Oracle`):
  - explicit unsupported-scheme behavior coverage in `pkg/db`
  - exploratory URL smoke (`GOFRAME_SQL_EXPLORATORY_URL`)
- CI SQL matrix profile reference with local reproduction commands in `docs/CI_MATRIX.md`.
- Compatibility fixture harness script for release gating:
  - `scripts/ci/run_compatibility_harness.sh`
  - fixture profiles: `minimal-api`, `admin-heavy`, `plugin-heavy`
  - markdown report output with threshold enforcement
- New MVC/API fixture smoke test:
  - `TestExampleMVCAPI_Minimal_Smoke` in `examples/mvc_api`
- Expanded exploratory SQL matrix CLI integration coverage for `MSSQL`/`Oracle`:
  - `createcachetable` idempotency validation
  - `sqlflush` and `flush --dry-run` output validation
  - `sqlsequencereset` output validation across exploratory engines
- `sqlsequencereset` for Oracle now emits concrete reset SQL for common sequence naming strategies:
  - `<table>_SEQ`
  - `<table>_ID_SEQ`
  - next sequence value derived from `MAX(id)+1` when `id` column exists
- Automated release report generators:
  - `scripts/release/generate_compatibility_report.sh`
  - `scripts/release/generate_dependency_impact_report.sh`
- Contract-governance documentation set:
  - `docs/API_CONTRACT_INVENTORY.md`
  - `docs/CLI_CONTRACT_MATRIX.md`
  - `docs/CONFIG_KEY_REGISTRY.md`
- Request-scope routing foundation for MultiSite/MultiTenant in `pkg/app`:
  - host/site/tenant resolution middleware
  - `RequestScope` context helpers
  - `App.Database(alias)` and `App.DatabaseForRequest(r)` for DB alias routing
- CLI output contract foundation:
  - global output flags: `--output plain|pretty|json`, `--color auto|always|never`, `--symbols|--no-symbols`, `--json` shorthand
  - pretty/status rendering support for `health`, `routes`, `mailproviders`, and `plugin` command family
  - tests for global output mode/color behavior
- Security-by-default tenant isolation guardrails:
  - startup validation rejects tenant configurations that resolve multiple tenants to one DB alias
  - tenant routing rejects shared site DB alias usage when multitenancy is enabled
- Deprecation and migration-assistant governance docs:
  - `docs/DEPRECATION_TEMPLATE.md`
  - `docs/MIGRATION_ASSISTANT_CONVENTIONS.md`
  - reusable templates:
    - `docs/templates/deprecation_notice.md`
    - `docs/templates/migration_assistant.md`
- DB observability metrics in `pkg/db`:
  - query total/error counters and query duration histogram
  - pool utilization/wait metrics (`open`, `idle`, `in_use`, `wait_count`, `wait_duration_ms`)
- Job observability and tracing in `pkg/tasks`:
  - enqueue and processing lifecycle metrics (`started`, `succeeded`, `retried`, `failed`, duration)
  - producer/consumer spans for enqueue and worker processing
  - request-context correlation helpers via `Manager.EnqueueJSONCtx(...)`
- Observability dashboard and alert recommendations in `docs/OBSERVABILITY_BASELINE.md`.

### Changed

- `goframe generate` now follows the same canonical scaffold layout as `new`/`startapp`:
  - models under `internal/models`
  - controller scaffolds and tests under `internal/controllers`
- `goframe new` scaffold now writes `go.mod` with `go 1.24` to align with framework minimum support.
- `goframe sendtestemail` and deploy health messaging now reference generic plugin naming (`goframe-plugin-<driver>`) with legacy fallback details.
- Documentation consolidated with a canonical docs entrypoint (`docs/INDEX.md`), active-vs-historical separation, and refreshed cross-links.
- Fixed stale local absolute link in `docs/DETAILED_TUTORIAL.md` to a portable relative reference.
- Standardized documentation headers across `docs/` with consistent `Reference date` and `Status` metadata.
- Normalized documentation wording to avoid ambiguous temporal phrasing and align plugin-runtime terminology.
- README and plugin/mail docs updated with capability-based plugin command references.
- `docs/V0.6.0_ROADMAP.md` checklist updated for completed Plugin SDK baseline items.
- `app.New` now wires session middleware by default and exposes `App.Session`.
- `goframe check --deploy` now validates session/cookie production posture (store mode, redis/sql requirements, secure cookie and SameSite combinations).
- Documentation updated with cluster-safe session guidance (`sql`/`redis` for multi-replica environments).
- Roadmap updated with:
  - completed admin session observability item for `v0.6.0`
  - MongoDB adapter exploration listed as non-priority post-`v0.6.0` backlog
  - MS SQL Server and Oracle explicitly tracked as exploratory CI lanes with promotion criteria to first-class support
- Router middleware now supports token-bucket rate limiting with optional route and role dimensions while preserving previous config compatibility.
- CLI test suite now verifies production guardrails in non-interactive runs across destructive commands:
  - `flush`
  - `loaddata --truncate`
  - `migrate down`
  - `migrate steps -N`
  - `migrate refresh`
- CI now includes dedicated SQL matrix jobs:
  - required lanes: PostgreSQL + MySQL
  - exploratory non-blocking lanes: MS SQL Server + Oracle compatibility smoke
- CI now emits a stable required check context `CI Required Gate` that aggregates required lanes for branch protection.
- Added branch-protection automation script `scripts/ci/configure_branch_protection.sh` and merge-policy guidance in `docs/CI_MATRIX.md`.
- HTTP telemetry middleware now stores `trace_id` in `observe` context for downstream correlation.
- GitHub workflows now use current action majors (`checkout/setup-go/setup-node` and GoReleaser action), with Node 24 in CI/release/rehearsal jobs.
- CI now includes a required `compatibility-harness` job and folds it into `CI Required Gate`.
- Rehearsal and release workflows now publish compatibility/dependency report artifacts.
- `scripts/release/rehearse_rc.sh` now generates release-gate reports into `dist/reports/`.
- Compatibility report generation now validates contract-governance document/template presence as a release gate check.
- Database configuration contract is now alias-only:
  - removed legacy keys `database_engine`, `database_url`, `database_max_open`, `database_max_idle`, `database_max_lifetime`
  - canonical runtime keys are `database_default` + `databases.<alias>.*`
- CLI/runtime DB wiring now resolves from the primary alias (`database_default`) rather than legacy single-URL keys.
- `pkg/model` metadata contract now supports:
  - explicit FK declarations (`fk` / `fk:<model|table.column|key=value,...>`)
  - simple and composite index declarations (`index`, `index:<name>`, `unique`, `unique:<name>`)
  - validation for multiple PK declarations, malformed FK specs, and mixed unique/non-unique index groups.
- New metadata-driven SQLite migration scaffold generator in `pkg/model`:
  - deterministic FK constraint names (`fk_<table>_<column>__<ref_table>_<ref_column>`)
  - index creation from model metadata and reverse index drops in `down` scaffolds
  - wired into `goframe generate resource` and `goframe startapp` migration generation.
- `goframe inspectdb` now enriches generated tags with schema metadata:
  - PK emitted as `pk`
  - FK emitted as `fk:<table>.<column>`
  - index metadata emitted as `index`/`unique` (single-column) or named variants for composites.
- New stable-contract freeze guardrails:
  - baseline files under `contracts/baseline/` for CLI primary command names and config key patterns
  - automated no-removal checks in `contracts/freeze_test.go`
  - CI/release integration via `scripts/ci/check_contract_freeze.sh` and required `contract-freeze` job.
- Admin API hardening:
  - action-level authorization checks now cover CSV export and session inventory endpoints
  - bulk delete responses now report per-id failure details (`requested`, `deleted`, `failed`, `errors[]`)
  - list endpoint now validates pagination/search/filter inputs explicitly (`page`, `page_size`, `search`, and filter fields/values)
- Critical maintenance CLI commands now honor a homogeneous output contract across global modes:
  - `createuser`, `changepassword`, `createcachetable`, `clearsessions`, `remove_stale_contenttypes`
  - default `plain` remains backward-compatible in message wording
  - `pretty` uses status-tag rendering and `json` emits structured command status payloads for automation
- `SPEC.md` is now synchronized with current architecture and dependency reality:
  - SQL-first runtime over `database/sql`
  - alias-only DB config contract and multisite/multitenant guardrails
  - current dependency set without stale Chi/Bun/GORM/Mongo references
- Week 6 release-readiness docs now include:
  - latest compatibility harness snapshot (`docs/reports/compatibility_harness_latest.md`)
  - release-readiness execution snapshot (`docs/reports/release_readiness_2026-04-07.md`)
  - explicit critical-dependency review note (`docs/reports/dependency_critical_review_2026-04-07.md`)

## [0.5.5] - 2026-04-05

### Added

- `goframe shell` now supports `--sandbox` mode to allow only read-only SQL statements (`SELECT`/`EXPLAIN`/`SHOW`/`DESCRIBE`).
- Django-style CLI aliases:
  - `runserver` -> `serve`
  - `startproject` -> `new`
  - `makemigrations` -> `migrate create <name>`
  - `showmigrations` -> `migrate status`
  - `createsuperuser` -> `createuser`
  - `dbshell` -> `shell`
  - `check` -> `health`
- `goframe startapp` command to scaffold a new app module inside an existing project.
- `goframe test` command to run `go test` with framework-friendly flags and `--dry-run`.
- New SQL parity commands inspired by Django:
  - `goframe sqlmigrate` (print SQL for specific migration files)
  - `goframe sqlflush` (print generated flush SQL)
  - `goframe sqlsequencereset` (print sequence reset SQL)
  - `goframe flush` (execute flush SQL with production guardrails)
- Fixture parity commands inspired by Django:
  - `goframe dumpdata` (export table data as JSON fixtures)
  - `goframe loaddata` (import JSON fixtures, optional `--truncate` with guardrails)
- `goframe inspectdb` command to introspect SQL schema and generate Go model structs.
- `goframe diffsettings` command to compare effective configuration against framework defaults.
- `goframe health --deploy` / `goframe check --deploy` to run deploy hardening checks.
- `goframe changepassword` command to rotate admin-user passwords (Django-style parity for auth contrib).
- `goframe testserver` command to run fixture-loading (`loaddata`) followed by server startup, with `--dry-run` support.
- `goframe createcachetable` command to provision database-backed cache table schema.
- `goframe clearsessions` command to purge expired sessions (or all sessions via `--all`) from SQL-backed session tables.
- `goframe makemessages` command to extract translatable strings into locale `.po` catalogs.
- `goframe compilemessages` command to compile locale `.po` catalogs into JSON bundles.
- `goframe collectstatic` command to collect static assets into `static_root`, with `--dry-run` and `--clear`.
- `goframe findstatic` command to resolve static assets across discovered source directories, including glob queries.
- `goframe remove_stale_contenttypes` command to purge orphan content-type rows based on current SQL tables, with `--dry-run` and production guardrails.
- `goframe ogrinspect` command to inspect geospatial SQL tables (`geometry`/`geography`) and generate Go model structs.
- `goframe mailproviders` command to list registered mail drivers and external `goframe-mail-<driver>` plugins discovered on `PATH`.
- `goframe optimizemigration` command to normalize and deduplicate SQL statements in migration files.
- `goframe squashmigrations` command to squash a migration range into one `.up.sql`/`.down.sql` pair, with optional source archiving.
- `goframe sendtestemail` command now validates and sends through configurable `mail_driver` (`smtp`, `sendgrid`, or external plugin `goframe-mail-<driver>`), with `--dry-run` mode.
- New `pkg/mail` provider architecture with:
  - provider registry via `mail.RegisterProvider(...)` for in-process extensions
  - built-in drivers `noop`, `smtp`, and `sendgrid`
  - external plugin bridge via executables named `goframe-mail-<driver>` on `PATH`
- `pkg/tasks` baseline with Asynq support for background jobs (enqueue + worker runtime).
- OpenTelemetry bootstrap (`pkg/observe/otel.go`) with OTLP traces/metrics initialization and graceful shutdown wiring from `app.New`.
- HTTP telemetry middleware with spans and request metrics in `pkg/router`.
- Configurable rate limiting middleware (fixed-window) based on user-id (when available) or client IP.
- `goframe new` scaffold now generates `cmd/worker/main.go` and `internal/tasks/article_events.go`, plus Redis/OTel/rate-limit config keys in `goframe.yaml`.
- Enterprise roadmap and alignment status document (`docs/ENTERPRISE_ROADMAP.md`).
- CLI parity matrix document against Django 6.0 (`docs/CLI_DJANGO_PARITY.md`).

### Changed

- `goframe check --deploy` now includes mail readiness checks (`deploy.mail_*`) based on `mail_driver` and provider-required settings.
- `goframe sendtestemail` now accepts `--driver` to override `mail_driver` for one-off provider checks.
- CLI tests now cover `shell --sandbox` for both allowed (`SELECT`) and blocked write statements.
- JWT middleware now enriches request context with `observe` user-id for cross-cutting middleware (logging/rate-limit correlation).
- README, project layout, and developer manual updated to include worker/background jobs, OTel, and rate-limiting usage.
- Documentation filenames standardized to English (`docs/DEVELOPER_MANUAL.md`, `docs/DETAILED_TUTORIAL.md`) and references updated.
- README/manual/CLI best practices updated with Django-style aliases and parity references.
- CLI parity matrix updated to mark `startapp` and `test` alignment progress.
- CLI parity matrix updated to mark SQL parity command alignment progress.
- CLI parity matrix updated to mark fixture command alignment progress.
- CLI parity matrix updated to mark `inspectdb` alignment progress.
- CLI parity matrix updated to mark `diffsettings` and deploy check alignment progress.
- CLI parity matrix updated to mark `changepassword` and `testserver` alignment progress.
- CLI parity matrix updated to mark `createcachetable` and `clearsessions` alignment progress.
- CLI parity matrix updated to mark `makemessages` and `compilemessages` alignment progress.
- CLI parity matrix updated to mark `optimizemigration` and `squashmigrations` alignment progress.
- CLI parity matrix updated to mark `sendtestemail` alignment progress.

## [0.5.4] - 2026-04-01

### Fixed

- `goframe new` now supports `--template mvc`, aligned with the expected scaffolding workflow.
- `goframe new` now returns a clear error when an unsupported template is requested.
- CLI tests now cover supported and unsupported `--template` values.

### Changed

- README and developer manual examples now include `--template mvc` in `goframe new`.
- Root `.gitignore` now ignores `dist/` release rehearsal artifacts.

## [0.5.3] - 2026-03-31

### Fixed

- Public module path alignment for external consumers:
  - `go.mod` now declares `github.com/jcsvwinston/GoFrame`
  - all internal imports updated to the public module path
  - GoReleaser ldflags updated to inject version with the new module path
- CLI scaffold/runtime references updated to the public module path so generated apps can resolve dependencies from `@latest`.

### Changed

- Developer docs and examples aligned with the new public module import path.

## [0.5.2] - 2026-03-31

### Added

- Complete end-user developer manual (`docs/DEVELOPER_MANUAL.md`):
  - installation paths
  - MVC/API/Admin workflow
  - full CLI reference
  - migration/seed operations
  - deployment and troubleshooting guidance

### Changed

- README development guides now include the complete developer manual.

## [0.5.1] - 2026-03-31

### Added

- Cross-OS release asset smoke workflow (`.github/workflows/release_asset_smoke.yml`).

### Changed

- Release asset smoke checks fixed to map tag (`vX.Y.Z`) to artifact naming (`X.Y.Z`).
- Release workflow made idempotent when assets already exist for a tag.
- CI/release/rehearsal workflows force JavaScript actions to run on Node 24.

## [0.5.0] - 2026-03-31

### Added

- Cross-OS release asset smoke workflow (`.github/workflows/release_asset_smoke.yml`).

### Changed

- Promoted `v0.5.0-rc1` to stable after successful artifact execution checks on Linux, macOS, and Windows.

## [0.5.0-rc1] - 2026-03-31

### Added

- Phase 5 release-candidate baseline:
  - CI workflow (`.github/workflows/ci.yml`)
  - tag-based release workflow (`.github/workflows/release.yml`)
  - release rehearsal workflow (`.github/workflows/rehearsal.yml`)
  - GoReleaser config for multi-platform artifacts (`.goreleaser.yaml`)
  - rehearsal script (`scripts/release/rehearse_rc.sh`)
  - versioning strategy docs (`docs/VERSIONING.md`)
  - release checklist (`docs/RELEASE_CHECKLIST.md`)
  - Go version support policy (`docs/GO_VERSION_POLICY.md`)

### Changed

- Project status docs aligned with current roadmap and phase closures.
- `goframe version` now prints build-injected release versions instead of a fixed value.

## [0.4.0] - 2026-03-31

### Added

- Bun-first SQL layer and consolidated migration/seed CLI flow.
- Rich admin SPA with:
  - command palette
  - filters and sorting
  - bulk selected export
  - tabs/detail panels
  - accessibility and recoverable-error hardening
- Runnable example app (`examples/mvc_api`) combining MVC + API + Admin.
- CLI project bootstrap via `goframe new`.
- Smoke E2E test for the official example.

### Fixed

- Admin SPA serving reliability when mounted under `/admin` prefix.

---

[Unreleased]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.4...HEAD
[0.5.4]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/jcsvwinston/GoFrame/compare/v0.5.0-rc1...v0.5.0
[0.5.0-rc1]: https://github.com/jcsvwinston/GoFrame/compare/v0.4.0...v0.5.0-rc1
[0.4.0]: https://github.com/jcsvwinston/GoFrame/releases/tag/v0.4.0
