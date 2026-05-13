# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
while in pre-1.0 mode (`v0.x.y`).

## [0.6.0] - 2026-05-09

### Changed

- Renamed: GoFrame → Nucleus. New module path: `github.com/jcsvwinston/nucleus`. New CLI binary: `nucleus`. New canonical config filename: `nucleus.yml` (extension changed from `.yaml`). New public package entry: `pkg/nucleus` (renamed from `pkg/fluent`), `nucleus.New()`. See ADR-003 for rationale.

### Removed

- Legacy plugin discovery prefix `goframe-plugin-*` and legacy mail bridge `goframe-mail-*`. Plugins must use `nucleus-plugin-<provider>`.
- Removed `examples/showcase_demo` (depended on the external Quark module).
- Removed empty `examples/admin_generator`.
- Removed orphan `docs/quark/`.
- Untracked `coverage.out` (now ignored by `.gitignore`).

### Fixed

- README example now imports a real package (`pkg/nucleus`); previously it referenced a non-existent `pkg/goframe`.
- Aligned Go version requirement statements (minimum 1.25; CI continues to test against 1.26.3 as the latest).

### Docs

- Extracted ADR-001 (stdlib-First) and ADR-002 (Django-Inspired CLI) to standalone files under `docs/adrs/`.
- Added ADR-003 (Project Identity — Nucleus).
- Documented Outbox `KafkaBridge`/`WebhookBridge` as preview / not-for-production in SPEC.

## [Unreleased]

### Added

- **Public documentation site** — bootstrapped a Docusaurus 3 (TypeScript)
  site under `website/`, deployed to GitHub Pages at
  <https://jcsvwinston.github.io/nucleus/>. The site adopts the Nucleus
  identity ahead of the code-level rename tracked in
  [`ADR-003`](docs/adrs/ADR-003-project-identity-nucleus.md):
  - `website/docusaurus.config.ts` configured with
    `url=https://jcsvwinston.github.io`, `baseUrl=/nucleus/`,
    `organizationName=jcsvwinston`, `projectName=nucleus`.
  - Landing page (hero, feature grid, code showcase, subsystem grid,
    final CTA) plus a structured docs tree: Introduction, Getting
    started, Concepts (Application, Configuration, Routing, Models &
    DB), Features (Admin, Auth, Observability, Storage & Tasks),
    Architecture (Principles, Compatibility), CLI overview.
  - Custom palette + typography (Inter / JetBrains Mono); custom logo.
  - `.github/workflows/docs.yml` — build-only on PRs, build + deploy to
    GitHub Pages on push to `main` via `actions/deploy-pages@v4`,
    path-scoped to `website/**`. Non-blocking to the framework `CI
    Required Gate`.
  - The authoritative docs tree under `docs/` is unchanged; content
    will be promoted into the site incrementally.
  - Note: requires `Settings → Pages → Source: GitHub Actions` to be
    enabled in the repository (one-time owner action).
- **Track B: Compatibility Harness** — Complete implementation of cross-version validation:
  - Fixture applications: `examples/mvc_api` (minimal API, admin-heavy), `examples/plugins` (plugin-heavy)
  - CI harness: `scripts/ci/run_compatibility_harness.sh` with profile-based testing
  - Golden tests: `contracts/freeze_test.go` enforces no removals from CLI, config, and API baselines
  - Compatibility report: `scripts/release/generate_compatibility_report.sh` generates release artifacts
- **Track C: Critical Dependency Firewall** — Complete implementation of dependency isolation:
  - Adapter boundaries: All critical dependencies wrapped behind framework interfaces
  - Type leak prevention: `contracts/firewall_test.go` with automated AST-based detection
  - Dependency impact report: `scripts/release/generate_dependency_impact_report.sh` with critical dependency tracking
  - Swap drills: SQL driver swap validated (SQLite ↔ PostgreSQL ↔ MySQL)
- **Track D: Enterprise Data Coverage** — Critical command coverage for MSSQL/Oracle:
  - migrate (up, down, status) - Added to exploratory tests
  - fixtures (loaddata, dumpdata) - Added to exploratory tests
  - inspectdb - Already tested in exploratory tests
  - sessions/cache (clearsessions) - Added to exploratory tests
  - Stability drill script operational: `scripts/ci/run_exploratory_stability.sh`
  - Stability report created: `docs/reports/mssql_oracle_stability_report.md`
  - Next step: Execute stability drills to validate promotion thresholds (MSSQL >= 80%, Oracle >= 80%)

- **Standalone scaffold** — `goframe new` now generates a self-contained project:
  - `go.mod` includes `require github.com/jcsvwinston/nucleus <version>`
  - release builds embed the exact version tag via goreleaser ldflags
  - dev builds use `latest` so `go mod tidy` resolves the newest published tag
  - projects compile without a `replace` directive or local GoFrame source
- **Build-tagged enterprise SQL drivers** — MSSQL and Oracle drivers are now opt-in:
  - `pkg/db/driver_mssql.go` (`//go:build mssql`) — register with `-tags mssql`
  - `pkg/db/driver_oracle.go` (`//go:build oracle`) — register with `-tags oracle`
  - SQLite, PostgreSQL, and MySQL remain included by default
- **Composable `app.New()` with Extension pattern** — modular initialization:
  - `Extension` interface in `pkg/app/extensions.go` (Name/Attach/Shutdown lifecycle)
  - `app.New(cfg, ...Option)` now accepts `WithExtensions()` and `WithoutDefaults()`
  - Default subsystems (admin, storage, mail, authz) extracted to `attachDefaultSubsystems()`
  - `app.New(cfg)` without options remains fully backward compatible
- **`--template api` scaffold tier** — lightweight core-only projects:
  - `goframe new myapp --template api` generates a minimal API using `app.WithoutDefaults()`
  - No admin panel, storage, mail, or authz subsystems initialized
  - Ideal for microservices and lightweight REST APIs
- **Unified storage layer** (`pkg/storage`) — provider-agnostic file storage with durable interface:
  - S3-compatible driver (AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces)
  - GCS native driver (Google Cloud Storage)
  - Azure Blob native driver
  - Local filesystem driver (development only)
  - `CredentialSource` with 4 injection methods: `value`, `env_var`, `file`, `secret_manager` (via `env:` prefix)
  - Tenant-aware key prefixing via `TenantStore` wrapper
  - Public path mapping with CDN support (`PublicMapper`)
  - Signed URLs for time-limited private object access
  - TTL-based cleanup of temporary objects (`_tmp/` prefix)
- **Tenant-aware admin CRUD** — automatic tenant filtering and tenant ID injection:
  - Models declare tenant field via `db:"tenant"` tag or `tenant_id` column convention
  - Admin middleware extracts tenant from request scope and applies filter
  - Tenant selector in admin header for multi-tenant deployments
- **RBAC in admin panel** via Casbin enforcer:
  - Policy management API (add/remove policies, assign/remove roles)
  - Permission checking with superuser bypass
  - Configurable via `admin_rbac_policy_file`
- **Audit logging** for all admin CRUD operations:
  - Bounded in-memory store (default 10,000 entries)
  - Filtering by user, model, action with pagination
  - Audit log viewer in admin UI
- **Data Studio import/export** (P3):
  - Export: CSV, JSON, SQL dump with tenant filtering
  - Import: CSV/JSON upload → validation → execute with conflict resolution (skip/update/error)
  - Fixtures: Django-compatible `dumpdata`/`loaddata` format
  - Toolbar buttons: Export selected | Export all | Import | Recent exports dropdown
- **Multi-node safe**: all file operations use shared S3 storage — zero node affinity
- **Admin UI enhancements**:
  - Health check dashboard (DB/Redis connectivity with latency)
  - Migration management UI (status + apply)
  - Deployment detection (standalone/Docker/K8s, cluster topology)
  - Cache management (Redis stats + flush)
  - File storage browser
  - i18n support (EN/ES) with locale selector
  - Export history dropdown with download links
- **Model tenant field detection**: `TenantFieldName()` on `ModelMeta` with `db:"tenant"` tag parsing
- **Admin storage integration**: `PanelConfig.Store` for export/import operations via shared storage
- **CLI ↔ doc parity guard** (`contracts/cli_doc_parity_test.go`): asserts every `nucleus <token>` reference in `website/docs/cli/overview.md` resolves to a primary command in `internal/cli/root.go` or to a Django-style alias. Closes the regression path for fabricated commands (audit `docs/audits/2026-05-12-enterprise-readiness.md`, discrepancies D1 + D2). Exposes `cli.ContractAliasCommandNames()` to mirror the existing `ContractPrimaryCommandNames()` accessor.

### Fixed

- `website/docs/cli/overview.md` no longer documents fabricated commands `nucleus i18n extract|compile`, `nucleus contenttype list`, or the `nucleus fixtures dumpdata|loaddata` namespace — replaced with the real `nucleus makemessages` / `nucleus compilemessages` / `nucleus remove_stale_contenttypes` / `nucleus dumpdata` / `nucleus loaddata` and `nucleus findstatic`. Audit `docs/audits/2026-05-12-enterprise-readiness.md` discrepancies D1, D2.
- `README.md` lifecycle-command count corrected from `34` to `37` (matches the registered `commandSpec` entries in `internal/cli/root.go`).
- **Rate-limit per-tenant** (`pkg/router/ratelimit.go`): `rateLimitKeyFromRequest` now prefixes the bucket key with `tenant:<id>|` when a tenant is resolved into the request scope, so two requests sharing a `user_id` but distinct `tenant_id`s no longer share a bucket. Plumbing crosses the `pkg/app` → `pkg/router` boundary via a new `observe.CtxWithTenantID` / `observe.TenantIDFromCtx` pair (the request-scope middleware in `pkg/app/requestscope.go` now mirrors the resolved tenant into `pkg/observe`, the same channel `UserIDFromCtx` already uses). `observe.WithContext` enriches loggers with a `tenant_id` field when present. Closes audit discrepancy D5; the README promise of "rate-limit per-tenant" is now load-bearing.
- **Core `/healthz` handler** (`pkg/app/healthz.go`): `App.New` now registers `GET /healthz` by default. The handler probes every entry in `a.DBs` via `db.DB.Health` (per-DB timeout 2s) and returns `200` with `{"status":"healthy",...}` when all probes pass, or `503` with `{"status":"unhealthy",...}` when any fails. Suitable for Kubernetes liveness/readiness probes — works under `app.WithoutDefaults()` too. Redis / mail / storage probes are tracked as follow-ups; `website/docs/features/observability.md` is now in sync with the implemented scope. Closes audit discrepancy D3; the README + observability doc promise of `/healthz` is now load-bearing.
- **Endpoints ↔ doc parity guard** (`contracts/endpoints_doc_parity_test.go`): mounts a minimal in-memory app via `app.New(cfg, app.WithoutDefaults())`, then verifies every endpoint documented in `website/docs/features/observability.md` and `website/docs/getting-started/quickstart.md` responds with the expected status. Currently covers `/healthz`; future entries append in lockstep with docs + impl.
- **`pkg/health` package** — new internal abstraction for dependency probes used by `/healthz`. Exposes a `Prober` interface, a `Run(ctx, probes, timeout)` concurrent aggregator, and three concrete constructors: `NewDBProbe`, `NewRedisProbe`, `NewStorageProbe`. Keeps `github.com/redis/go-redis/v9` wrapped — `pkg/app` no longer imports the redis client directly (firewall-friendly). `pkg/app/healthz.go` now derives probes from current `App` state on every request: one `db:<alias>` per entry in `a.DBs`, plus `redis` if `Config.RedisURL` is set, plus `storage` if a `Store` is attached. Per-probe budget remains 2 s; probes run concurrently so total wall time is bounded by the slowest probe. `website/docs/features/observability.md` documents the registration rules and the underlying calls.
- **Circuit-breaker primitive** (`pkg/circuit`) — new standalone package exposing `Config`, `New`, `(*Breaker).Do(ctx, fn)`, `(*Breaker).State()`, the `State` enum (`StateClosed` / `StateOpen` / `StateHalfOpen`), and `ErrOpen`. Standard three-state state machine with configurable failure threshold, cooldown, and half-open probe budget. Race-tested under concurrent probe contention. Intentionally minimal — no event bus, no metrics, no per-call timeout; compose those with `pkg/observe` and the `/metrics` MeterProvider. Use it to wrap calls to mail / object storage / plugin bridges / third-party APIs so a single dependency outage cannot cascade. Documented in `website/docs/features/observability.md`.
- **Multi-driver `AutoMigrate`** (`pkg/model`, `pkg/app/app.go`) — `App.AutoMigrate` now dispatches by `db.DB.System()` and supports SQLite, PostgreSQL, and MySQL. New scaffold builders: `model.BuildPostgresMigrationScaffold` (BIGSERIAL PK, BYTEA / TIMESTAMPTZ types, double-quoted identifiers, `DROP TABLE … CASCADE` on rollback) and `model.BuildMySQLMigrationScaffold` (BIGINT AUTO_INCREMENT PK, LONGBLOB / DATETIME(6) / TINYINT(1) types, backtick-quoted identifiers, MySQL-syntax `DROP INDEX … ON …`). MSSQL and Oracle still return `db.ErrAutoMigrate` — explicit SQL migrations + `nucleus migrate` is the path for those engines, consistent with ADR-001. New exported `(d *DB) System()` accessor; `quickstart.md` admonition updated to reflect the SQLite + Postgres + MySQL coverage and the dev-mode caveats.
- **Migration drift detection** (`pkg/db/migrate.go`, `internal/cli/migrate.go`) — new `Migrator.Drift() ([]DriftEntry, error)` method detects file-level drift: rows in `nucleus_schema_migrations` whose corresponding `.up.sql` file is absent from the migrations directory (typical cause: an operator deleted a migration after applying it). Exposed in the CLI as `nucleus migrate drift`; the command prints a tab-separated row per drifted ID and **exits non-zero** when any drift is reported so CI gates can detect it programmatically. Schema-level drift (actual `information_schema.columns` shape vs migration intent) is a separate, per-dialect check tracked as a follow-up. `website/docs/cli/overview.md` lists the new subcommand.
- **`/metrics` Prometheus endpoint** (`pkg/observe/otel.go`, `pkg/app/app.go`) — `TelemetryConfig` gains a `PrometheusEnabled` flag. When set, `SetupOpenTelemetry` attaches a Prometheus reader to the OTel MeterProvider (alongside the existing OTLP reader, when configured) and returns an additional `http.Handler` value. `App.New` wires the handler at the path configured by `Config.MetricsPath` (default `/metrics`). OTLP push and Prometheus pull coexist on the same MeterProvider — instrumentation code is unchanged. `application/openmetrics-text` content type, registry-scoped, deny-list-friendly. Closes the long-standing "no Prometheus exposition path" gap documented in `observability.md`; that doc is now updated and the endpoints-parity guard in `contracts/` covers `/metrics` end-to-end against a minimal in-memory app.
- **Mail probe in `/healthz`** (`pkg/health/mail.go`, `pkg/mail`) — new optional `mail.HealthChecker` interface (`Healthy(ctx) error`); SMTP implements it natively (TCP dial + HELO + QUIT, no auth, no message sent). `pkg/health.SupportsMailProbe` + `NewMailProbe` register a `mail` row in the `/healthz` response when (and only when) the configured `Sender` opts in. `noop`, `sendgrid` and external plugin senders intentionally do not implement `HealthChecker` today — their `/healthz` rows simply do not appear. Documented registration semantics in `observability.md`.
- **Casbin deny-override** (`pkg/authz/enforcer.go`) — default RBAC model now stamps an `eft` column on every policy and uses the deny-override effect formula `some(where (p.eft == allow)) && !some(where (p.eft == deny))`. Default-deny semantics are preserved (no matching policy → deny). New public method `Enforcer.Deny(sub, obj, act)` lets operators block a specific user even when a broader role's allow rule would otherwise grant access. `AddPolicy` auto-stamps `allow` so callers do not change shape; `RemovePolicy` lifts both allow and deny variants matching the tuple. CSV policy files now require a 4th column (`allow` or `deny`); programmatic callers are unchanged. Documented in `website/docs/features/auth.md`.
- **JWT key rotation + JWKS endpoint** (`pkg/auth/jwt.go`) — `JWTManager` extends from single-secret HS256 to a multi-key keyset that supports rotation without downtime, plus `RS256` for asymmetric signing. New exported surface: `SigningAlgorithm` (HS256, RS256), `SigningKey`, `NewJWTManagerFromKeys`, `RotateKey`, `RemoveKey`, `CurrentKID`, `JWKSHandler`, `JWKS`, plus the wire types `JWKSet` / `JWK`. Tokens issued in multi-key mode carry a `kid` header; `Validate` looks the key up by kid and rejects unknown ones. The legacy `NewJWTManager(secret, expiry, issuer)` path is unchanged and still produces kid-less tokens that validate against the single secret. `JWKSHandler` serves the RFC 7517 JSON Web Key Set at any path the user mounts it on (typically `/.well-known/jwks.json`); HMAC keys are intentionally excluded so the public endpoint cannot leak shared secrets. `website/docs/features/auth.md` documents the single-secret and rotation modes, the operator rotation flow (`RotateKey` → grace window → `RemoveKey`) and the JWKS shape with a worked example. Closes the highest-leverage P0 item from the post-iteration backlog.
- `website/docs/getting-started/quickstart.md` now carries an explicit `:::warning` admonition that `.AutoMigrate()` is SQLite-only — citing the `AutoMigrate intentionally unsupported` comment in `pkg/db/migrate.go` and the matching `ErrAutoMigrate` fallback in `pkg/app/app.go`. Points users at `nucleus migrate` as the multi-driver path. Closes audit discrepancy D8.

### Docs

- Seeded `docs/deprecations/` and `docs/migration_assistants/` with their first concrete entries: `DEP-2026-001-legacy-plugin-prefixes.md` (retroactive record of the `goframe-plugin-*` / `goframe-mail-*` removal shipped in `v0.6.0`) and its paired `MA-2026-001-legacy-plugin-prefix-to-nucleus-plugin.md`. Exercises the formats defined in `docs/governance/DEPRECATION_TEMPLATE.md` and `docs/governance/MIGRATION_ASSISTANT_CONVENTIONS.md` against a real surface.

### Changed

- Documentation reorganized with new `STORAGE_GUIDE.md`, updated `INDEX.md`, `ADMIN_PANEL.md`, and `ENTERPRISE_LONG_TERM_ROADMAP.md`
- Removed outdated historical reports (`ROADMAP_SUPERAR_DJANGO.md`, `GAP_IMPLEMENTATION_STRATEGY.md`, and 5 stale report snapshots)
- `SPEC.md` updated with storage layer, admin import/export, and RBAC documentation
- Admin panel now requires storage configuration for export/import functionality

### Security

- Credentials never stored as plain text in config — resolved at startup via `CredentialSource`
- All exported files stored with `Private` visibility, accessed via time-limited SignedURLs
- Import validation prevents injection of read-only/excluded fields

- Unified request context helpers in `pkg/router`:
  - `ContextHandler` adapter for one-entrypoint handler style
  - optional dependency injection via `router.WithSession(...)` and `router.WithTemplates(...)`
- REST resource route helper in `pkg/router`:
  - `Router.Resource("/users", router.ResourceHandlers{...})` for conventional CRUD route registration
  - automatic mapping for list/create/retrieve/update/delete endpoints
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
- Admin live runtime inspector foundation:
  - `GET /admin/api/live/snapshot` for in-memory request/session runtime snapshots
  - `GET /admin/api/live/ws` for non-blocking WebSocket event stream
  - bounded request ring buffer + in-memory session tracker + non-blocking subscriber drop policy
  - new `/admin#/live` view wired to snapshot + live stream
  - live SQL sniffer from framework CRUD operations (`operation`, `query`, redacted `args`, `duration_ms`, `trace_id`) emitted to snapshot and WebSocket stream
- Admin system pulse snapshot foundation:
  - `GET /admin/api/system/snapshot` for Go runtime + DB pool + startup environment telemetry
  - startup environment viewer with mandatory masking for `KEY|SECRET|PASSWORD|TOKEN`
  - new `/admin#/system` view for goroutine states, memory/GC metrics, and DB pool stats
  - integrated worker/job pool monitor via Asynq runtime inspector (queues, servers, active workers)
  - feature flags runtime control endpoints:
    - `GET /admin/api/system/flags`
    - `POST /admin/api/system/flags`
    - `PUT /admin/api/system/flags/{name}`
    - `DELETE /admin/api/system/flags/{name}`
  - queue runtime operation endpoint with safety guardrails:
    - `POST /admin/api/system/jobs/queues/{name}/actions/{action}` where action is `pause|unpause|retry`
    - explicit acknowledgment payload required; production additionally requires `force=true`
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
- `goframe new` scaffold now writes `go.mod` with `go 1.25` to align with framework minimum support.
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
  - baseline files under `contracts/baseline/` for CLI primary command names, CLI JSON status envelope/data keys for automation-critical commands, config key patterns, and exported symbols from stable API packages
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
  - `go.mod` now declares `github.com/jcsvwinston/nucleus`
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
  - Go version support (minimum 1.25+)

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

[Unreleased]: https://github.com/jcsvwinston/nucleus/compare/v0.5.4...HEAD
[0.5.4]: https://github.com/jcsvwinston/nucleus/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/jcsvwinston/nucleus/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/jcsvwinston/nucleus/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/jcsvwinston/nucleus/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/jcsvwinston/nucleus/compare/v0.5.0-rc1...v0.5.0
[0.5.0-rc1]: https://github.com/jcsvwinston/nucleus/compare/v0.4.0...v0.5.0-rc1
[0.4.0]: https://github.com/jcsvwinston/nucleus/releases/tag/v0.4.0
