# Nucleus Technical Specification

Reference date: 2026-08-31.
Status: current `v1.x` baseline — latest release v1.24.1 <!-- x-release-please-version -->.

This document defines the current, implemented technical baseline for Nucleus.
It replaces older design notes that referenced superseded architecture choices.

Maintenance contract: this file is a **baseline**, re-synchronized when an
audit or arc touches the subsystems it describes; the delta record between
baselines is `docs/adrs/` (the newest ADR always wins over this file's
prose). If a subsystem section here cites no ADR newer than the reference
date, the section was reviewed as still accurate on that date.

## 1. Scope and Precedence

This specification is implementation-first.

When documents conflict, precedence is:

1. `README.md`
2. Contract/governance docs in `docs/`:
- `docs/reference/API_CONTRACT_INVENTORY.md`
- `docs/reference/CLI_CONTRACT_MATRIX.md`
- `docs/reference/CONFIG_KEY_REGISTRY.md`
- `docs/governance/COMPATIBILITY_SLO.md`
3. This file (`SPEC.md`)
4. Detailed tutorials/manual examples

## 2. Core Principles

1. stdlib-first runtime design (`net/http`, `database/sql`, `log/slog`, `context`).
2. Explicit configuration and lifecycle; no hidden global singletons.
3. Compatibility by contract for stable API/CLI/config surfaces.
4. Security-by-default posture for production-sensitive features.
5. SQL-first operations and tooling, with deterministic CLI behavior.

## 3. Runtime Architecture

## 3.1 Application Container (`pkg/app`)

`app.New` accepts an optional variadic `...Option` parameter for composable
initialization. It wires and validates:

- config loading/normalization (`pkg/app/config.go`)
- logger (`pkg/observe`)
- SQL database map by alias (`database_default` + `databases.<alias>`)
- mail sender (`pkg/mail`)
- session manager (`pkg/auth`) with selected store (`memory|sql|redis`)
- HTTP router and middleware (`pkg/router`)
- request scope resolver for MultiSite/MultiTenant (`pkg/app/requestscope.go`)
- model registry (`pkg/model`)
- *(admin panel: no longer a built-in default — it ships as the separate `orbit` module, mounted explicitly; see §3.4)*

**Extension pattern (`pkg/app/extensions.go`):**

`app.New(cfg)` without options initializes everything (backward compatible).
`app.New(cfg, app.WithoutDefaults())` initializes only core components.
Extensions can be explicitly attached via `app.WithExtensions(...)`:

```go
// Full-stack (default behavior, backward compatible):
a, err := app.New(cfg)

// Core-only (lightweight API):
a, err := app.New(cfg, app.WithoutDefaults())

// Core + selected extensions:
a, err := app.New(cfg,
    app.WithoutDefaults(),
    app.WithExtensions(myExtension),
)
```

The `Extension` interface:
```go
type Extension interface {
    Name() string
    Attach(a *App) error
    Shutdown(ctx context.Context) error
}
```

Scaffold templates:
- `--template mvc` (default): full-stack with all subsystems
- `--template api`: core-only using `app.WithoutDefaults()`

`App` exposes:

- `DB` (primary alias) and `DBs` (all opened aliases)
- `Database(alias)` and `DatabaseForRequest(r)` helpers
- graceful `Run`/`Shutdown` with shutdown hooks

## 3.2 HTTP and Middleware (`pkg/router`)

Nucleus uses its own router/mux abstractions (not Chi as a runtime dependency):

- route registration + mounting
- request middleware chain
- JSON helpers and HTTP utilities
- CORS/CSRF middleware
- rate limiting (`rate_limit_*`)
- OpenTelemetry HTTP instrumentation
- explicit mounting of experimental OpenAPI JSON documents through `pkg/app.App.MountOpenAPIHandler(pattern, openapi.Handler(provider))`
- request interceptors (ADR-029): `pkg/router/interceptor` is the leaf
  contract package through which a third-party package registers itself in
  `init` and intercepts the request lifecycle by name from configuration —
  the same registry shape as storage/session/auth providers, so an
  interceptor can be *distributed* instead of pasted into a bootstrap

## 3.2b Extension model: registries, vertical slices, leaf contracts

Three decisions define how third-party code plugs in (all post-v1.14):

- **Provider registries (ADR-023).** The replaceable pieces — storage
  backends, session stores, authentication backends, federated identity
  providers, request interceptors — are selected **by name** from
  registries with one shape: a package registers itself in `init`, the
  application imports it for the side effect, configuration names it. Each
  provider owns its own config subtree and binds it strictly (unknown keys
  fail).
- **Vertical-slice modules (ADR-022).** `nucleus.Module[C]` carries not
  just routes/models/jobs/webhooks but also declarative `Policies`
  (RBAC rows joined to the live ruleset, operator's CSV always wins),
  `CSRFExempt` paths, embedded migrations reachable by `nucleus migrate`,
  and page templates. Mounting a module is the whole integration;
  `nucleus generate module` scaffolds that shape.
- **Leaf contract packages (ADR-025/026).** The contracts extension
  authors implement live in dependency-light leaf packages —
  `pkg/plugins/schema`, `pkg/storage/provider`, `pkg/auth/backend`,
  `pkg/router/interceptor` — so implementing a backend does not drag in
  the SDKs the built-ins need. `pkg/auth/backend/backendtest` is the
  conformance suite (ADR-027) that checks the parts of the auth contract
  that are cheap to get wrong and expensive to ship wrong.

## 3.3 Data and Model Layer

`pkg/db`:

- `database/sql`-based DB wrapper
- health checks and telemetry
- SQL migration executor and helpers
- **no database driver linked** (ADR-031): each engine ships as its own
  module — `drivers/postgres`, `drivers/mysql`, `drivers/sqlite`,
  `drivers/mssql`, `drivers/oracle` — and an application imports the one
  it uses for its side effect (`nucleus add <engine>` writes the import).
  `pkg/db` resolves the URL scheme to a `database/sql` driver name and, when
  no driver is registered under it, refuses to start with the `go get` and
  `import _` lines that fix it.
- **driver registry and error classification** (`pkg/db/driver`): a driver
  module registers, next to the driver, the classifier that recognises the
  engine's unique-violation error, so `db.IsUniqueViolation` answers for
  the engine actually linked. PostgreSQL needs none — every PostgreSQL
  driver exposes its SQLSTATE — and `pkg/db/driver/drivertest` is the
  conformance kit a classifier has to pass. The root module keeps the
  predicates in `internal/dbclassify` so the `nucleus` CLI (which links
  every engine, since `nucleus migrate` must work against whatever
  database is in front of it) and the driver modules register the same
  code and cannot drift.

`pkg/model`:

- model metadata extraction from tags
- registry for app/admin integration
- generic CRUD operator
- metadata-driven migration scaffold generation
- model contract features include PK/FK/index metadata (simple + composite)

## 3.4 Admin (extracted to the `orbit` module)

The admin panel is no longer part of the framework core. As of ADR-019 it ships
as the separate `github.com/jcsvwinston/orbit` module, mounted in-process via the
extension/module surface (§3.1); the in-core `pkg/admin` package was removed.

orbit reads the framework's `Runtime` accessors (model registry, managed DB
handles, session manager, RBAC enforcer, observability bus) and serves its own
embedded SPA: Data Studio (model CRUD with tenant-aware filtering, import/export,
bulk actions), a live request/SQL feed (single binary or multi-node via Redis),
session viewer, RBAC management, system metrics, and an audit log. See the orbit
repository for its contract and configuration.

## 3.5 Auth/Authz (`pkg/auth`, `pkg/authz`)

- JWT helpers
- password hashing helpers
- session manager with store backends, selected by name from a registry
  (`auth.RegisterSessionStore`); built in: memory, SQL table store, Redis
- session runtime metadata enrichment (`pod/host/instance`)
- authentication backends selected by name (`auth.RegisterBackend`) and
  consulted as an ORDERED chain (`auth_backends`), where a backend answers
  accepted / rejected / unreachable — the third being what lets a local
  account work while a directory is down
- `auth.UserProvider` adapts an application's own user table into that
  chain (`app.WithUserProvider`)
- the backend contract lives in the leaf package `pkg/auth/backend`
  (ADR-025-style split), with a conformance suite in
  `pkg/auth/backend/backendtest` (ADR-027)
- LDAP ships as its own module, `providers/ldap` (ADR-024): imported for
  its side effect, named in `auth_backends`, released and tagged
  independently of the framework root
- federated sign-in (OIDC/SAML) is a separate seam (ADR-028):
  `auth_federated` declares provider instances, the framework owns the
  state/anti-forgery flow (`FederatedSet.Begin/Complete`), and the
  application mounts the start/callback routes
- Casbin integration points for authorization enforcement

## 3.6 Mail and Plugins (`pkg/mail`, `pkg/plugins`)

Mail:

- drivers: `noop`, `smtp` (vendor-specific senders — SendGrid, SES, Mailgun,
  … — ship as external `nucleus-plugin-<provider>` binaries; the built-in
  `sendgrid` driver was removed, DEP-2026-002)
- capability-style external provider bridge

Plugin runtime:

- provider discovery and capability schema handling
- `nucleus-plugin-<provider>` external naming convention (single, no legacy fallback)

## 3.7 Tasks (`pkg/tasks`)

- Asynq manager and worker runtime
- explicit enqueue policy helpers for queue/retry/timeout/delay/retention
- explicit queue runtime actions for pause/unpause/retry and first dead-letter operations (`archive-retry`, `retry-archived`, `purge-archived`)
- explicit scheduler wrapper for periodic tasks
- enqueue/process instrumentation hooks

## 3.8 Storage (`pkg/storage`)

Provider-agnostic file storage abstraction with a durable interface designed to last through `v1.x`.

Providers are selected by name from a registry
(`storage.RegisterProvider`), so a backend the framework does not ship is
usable without patching it; a registered provider reads its own settings
from `storage.<provider>.*` via `storage.Config.BindProvider`.

Built in:
- **Local filesystem** (development only) — the only provider the core registers

As their own modules (ADR-030; `nucleus add s3|gcs|azure` writes the import):
- **S3-compatible** (AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces) — `providers/storage-s3`
- **GCS** (Google Cloud Storage) — `providers/storage-gcs`
- **Azure Blob Storage** — `providers/storage-azure`

Credential injection (`CredentialSource`):

All sensitive values use the `CredentialSource` type, supporting 4 resolution methods:

| Method | Config key | Use case | Example |
|--------|-----------|----------|---------|
| Direct value | `value` | Testing only | `value: AKIAIOSFODNN7EXAMPLE` |
| Environment variable | `env_var` | Primary for production | `env_var: AWS_SECRET_ACCESS_KEY` |
| File path | `file` | K8s secrets, mounted volumes | `file: /etc/secrets/gcs-sa.json` |
| Secret Manager | `secret_manager` | `jwt_keys` only, through the `providers/secrets-aws` module (`aws-sm:` references); storage and database credentials do not resolve through it yet | `secret_manager: aws-sm:my/secret` |

Resolution priority: `value` → `env_var` → `file` → `secret_manager`.
Only the first non-empty source is used.

Key features:
- Streaming-native (`io.Reader`/`io.ReadCloser`), never holds large files in memory
- Automatic tenant prefixing (e.g. `tenant_acme/uploads/file.pdf`)
- Public/private object visibility
- Public path mapping: maps storage prefixes to web paths (e.g. `/media/*` → `storage/public/media/*`)
- Signed URLs for time-limited access to private objects
- Background TTL-based cleanup of temporary objects (`_tmp/` prefix)
- Cross-provider copy operation

Configuration:
```yaml
storage:
  provider: s3                # s3 | gcs | azure | local
  default: private            # default visibility
  public_url_base: "https://cdn.example.com"
  public_paths:
    /media: storage/public/media/
    /assets: storage/public/assets/
  s3:
    endpoint: ""              # Empty = AWS S3. "http://minio:9000" for MinIO
    bucket: myapp-files
    region: us-east-1
    access_key_id:
      env_var: AWS_ACCESS_KEY_ID        # Cloud Run, Docker, K8s
    secret_access_key:
      env_var: AWS_SECRET_ACCESS_KEY
    # Alternative: file-based (K8s secrets)
    # access_key_id:
    #   file: /etc/secrets/aws-access-key
    use_path_style: false     # true for MinIO
    public_bucket: myapp-public  # Optional separate bucket for public files
  local:
    path: storage/            # Development only
  cleanup:
    enabled: true
    interval: 1h
    prefix: _tmp/
    max_age: 24h
```

Multi-tenant behavior:
- When `multitenant.enabled: true`, all keys are automatically prefixed with tenant ID
- Tenant prefixing is transparent to application code
- Explicit prefix override available via `PutOptions.TenantPrefix`

## 3.9 Outbox (`pkg/outbox`)

- SQL-backed transactional outbox store
- direct + transactional enqueue support
- runtime inspection for admin/ops visibility
- explicit dispatcher with leasing, retry backoff, and terminal failure state

External-bridge status (preview, not for production):
- `KafkaBridge` (`pkg/outbox/bridge_kafka.go`) — present in code as a preview integration point. Its own source notes that this bridge is not wired for production use; configuration, ack semantics, and operational tooling are still subject to change.
- `WebhookBridge` (`pkg/outbox/bridge_webhook.go`) — present in code on the same footing. Suitable for experimentation; not part of the stable contract surface.

Both bridges are kept in the tree because the dispatcher already accommodates pluggable destinations; they are documented here so users do not assume they are production-ready.

## 3.10 Observability (`pkg/observe`, `pkg/observability`)

- `slog` logger setup (`pkg/observe`), OpenTelemetry setup and shutdown
- the core keeps the OpenTelemetry **SDK** and links **no exporter**
  (ADR-031): `exporters/otlp` (traces + metrics over OTLP-HTTP, enabled by
  `otlp_endpoint`) and `exporters/prometheus` (the `metrics_path` scrape
  endpoint) are modules an application imports. An `otlp_endpoint` or an
  operator-written `metrics_path` without its module stops startup with the
  import to add; the untouched `metrics_path` default only logs that
  metrics are not served, and the application boots.
- `pkg/observability`: the in-process event bus carrying HTTP/SQL/session
  events; modules consume it through the stable `nucleus.EventBus` facade,
  and external SQL producers (e.g. an ORM bridge) ingest through
  `EventBus.EmitSQL` (ADR-020)
- SQL statements are observed at the `database/sql` driver level
  (ADR-021), so statements outside the model layer still reach the bus

## 3.11 Signals (`pkg/signals`)

- in-process signal bus for model/domain events
- explicit Redis relay for small distributed publish/subscribe forwarding
- OpenTelemetry setup and shutdown

## 3.12 Experimental API Contracts (`pkg/openapi` + `internal/contracts` convention)

- minimal OpenAPI 3.1 document model for scaffolded project contracts
- one shared source of truth for CLI export and runtime serving
- current supported subset includes paths, operations, JSON request bodies, JSON responses, scaffolded `data`/`count` envelopes, structured JSON error responses, empty responses, component schemas, and explicit path/query parameters including the scaffolded optional `q` search convention
- runtime serving remains explicit through `pkg/app.App.MountOpenAPIHandler` with `openapi.Handler`
- helper functions may reduce repetition, but the generated project contract remains intentionally explicit rather than DSL-driven

## 4. Dependency Reality (from `go.mod`)

The framework is the module at the repository root plus twelve modules an
application adds on demand (ADR-030/031), each with its own `go.mod` and
release tag:

| Module | Registers |
| --- | --- |
| `drivers/postgres`, `drivers/mysql`, `drivers/sqlite`, `drivers/mssql`, `drivers/oracle` | the `database/sql` driver and its unique-violation classifier |
| `exporters/otlp`, `exporters/prometheus` | the OpenTelemetry exporter behind `otlp_endpoint` / `metrics_path` |
| `providers/storage-s3`, `providers/storage-gcs`, `providers/storage-azure` | the storage backend behind `storage.provider` |
| `providers/secrets-aws` | the `aws-sm:` secret reference resolver |
| `providers/ldap` | the `ldap` authentication backend |

Direct runtime dependencies of the root module include:

- Configuration: `koanf` (`v2` + yaml/env/file/struct providers)
- Auth/session/security: `jwt/v5`, `scs/v2`, `casbin/v2`, `validator/v10`, `x/crypto`
- Redis: `go-redis/v9`
- Tasks: `hibiken/asynq`
- Observability: the OpenTelemetry SDK (exporters live in `exporters/*`)
- SQL drivers (`modernc.org/sqlite`, `pgx/v5`, `go-sql-driver/mysql`,
  `go-mssqldb`, `go-ora/v2`): required by the root module for the `nucleus`
  CLI and the test binary, which link every engine through
  `internal/dbclassify`; `pkg/app` reaches none of them, so an application
  built on the framework carries only the driver module it imports.
  Measured on the `nucleus new` scaffold with `drivers/sqlite`: a 60 MB
  binary over 138 modules; without any driver, 31 MB over 87 (the ADR-031
  hello-world numbers describe `pkg/app` alone, not the scaffold).

Not present as current runtime dependencies:

- Chi router
- Bun ORM/migrate
- GORM
- MongoDB driver

## 5. Configuration Contract (Current)

Canonical DB configuration is alias-based only:

```yaml
database_default: default
databases:
  default:
    url: sqlite://nucleus.db
  analytics:
    url: postgres://...
```

Legacy single-URL DB keys are removed from the active contract.

Key contract families:

- server/runtime: `host`, `port`, timeouts, `env`, `debug`
- databases: `database_default`, `databases.<alias>.*`
- multisite: `multisite.*`
- multitenant: `multitenant.*`
- auth/session: `jwt_*`, `session_*`
- admin (extracted to the orbit module, ADR-019): `modules.orbit.*` — the
  in-core `admin_prefix`/`admin_title` keys are `removed`
- mail: `mail_driver`, `smtp_*`, `mail_from`
- security/rate limit: `rate_limit_*`, `csrf_enabled`, `csrf_exempt_paths`
- i18n/static/storage: `default_locale`, `locales_path`, `static_*`, `storage_*`
- observability: `log_*`, `otlp_endpoint`, `metrics_path`, `metrics_public`

Reference registry: `docs/reference/CONFIG_KEY_REGISTRY.md`.

## 6. MultiSite/MultiTenant Contract

MultiSite and MultiTenant are request-scope features in `pkg/app`.

- site resolution supports exact and wildcard host mapping
- tenant resolution supports `subdomain` and `header`
- tenant-to-database alias routing supports explicit mapping and templates
- security default: `multitenant.require_isolated_db: true`

Isolation guardrail behavior:

- startup validation rejects multi-tenant mappings that would share the same DB alias
- request routing rejects shared site DB alias fallback when tenant isolation is required

## 7. CLI Contract Baseline (`cmd/nucleus`, `internal/cli`)

Nucleus ships stable operational CLI coverage for:

- runtime and diagnostics (`serve`, `routes`, `health`)
- scaffolding (`new`, `startapp`, `generate`, `add`)
- experimental API contract export (`openapi`)
- migrations and SQL maintenance
- data import/export/introspection
- auth/admin maintenance commands
- plugin and mail diagnostics
- static/i18n workflows
- test workflows and fixture server

Global output contract:

- `--output plain|pretty|json`
- `--color auto|always|never`
- `--symbols|--no-symbols`
- `--json` shorthand

Critical maintenance commands follow homogeneous output modes including structured JSON status payloads.

Reference lifecycle matrix: `docs/reference/CLI_CONTRACT_MATRIX.md`.

Current experimental API contract lane:

- projects aggregate generated API contracts in `internal/contracts`
- the project's `internal/contracts` package aggregator exposes the package-level document builder (`DefaultConfig`, `NewDocument`, `NewDocumentWithConfig`)
- `nucleus openapi --out openapi.json` exports the current project contract as OpenAPI JSON
- generated server scaffolds can serve that same contract explicitly at `/openapi.json` via `app.MountOpenAPIHandler("/openapi.json", openapi.Handler(contracts.NewDocument))`

## 8. Compatibility Governance

Stable compatibility is governed by:

- API inventory lifecycle tags (`docs/reference/API_CONTRACT_INVENTORY.md`)
- CLI lifecycle matrix (`docs/reference/CLI_CONTRACT_MATRIX.md`)
- config key registry lifecycle tags (`docs/reference/CONFIG_KEY_REGISTRY.md`)
- compatibility SLO (`docs/governance/COMPATIBILITY_SLO.md`)

Automated controls:

- stable contract freeze tests (`contracts/` + `scripts/ci/check_contract_freeze.sh`)
- compatibility harness (`scripts/ci/run_compatibility_harness.sh`)
- release compatibility report generation (`scripts/release/generate_compatibility_report.sh`)

## 9. Release-Readiness Baseline

Minimum release checks:

```bash
go test ./...
bash scripts/ci/check_contract_freeze.sh
bash scripts/ci/run_compatibility_harness.sh --enforce-threshold
bash scripts/release/generate_compatibility_report.sh --output dist/reports/compatibility_report.md --enforce-threshold
bash scripts/release/generate_dependency_impact_report.sh --output dist/reports/dependency_impact_report.md
```

Full rehearsal path:

```bash
bash scripts/release/rehearse_rc.sh
```

Checklist reference: `docs/governance/RELEASE_CHECKLIST.md`.

## 10. Current Explicit Non-Goals

1. No universal ORM abstraction spanning SQL/document/cache.
2. No hidden auto-migrations at runtime.
3. No promise that all exploratory SQL engines are first-class stable contracts.
4. No silent breaking changes on stable surfaces inside a minor/patch line.
