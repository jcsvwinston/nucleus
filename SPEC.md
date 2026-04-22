# GoFrame Technical Specification

Reference date: 2026-04-07.
Status: Current pre-v1 baseline.

This document defines the current, implemented technical baseline for GoFrame.
It replaces older design notes that referenced superseded architecture choices.

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

`app.New` wires and validates:

- config loading/normalization (`pkg/app/config.go`)
- logger (`pkg/observe`)
- SQL database map by alias (`database_default` + `databases.<alias>`)
- mail sender (`pkg/mail`)
- session manager (`pkg/auth`) with selected store (`memory|sql|redis`)
- HTTP router and middleware (`pkg/router`)
- request scope resolver for MultiSite/MultiTenant (`pkg/app/requestscope.go`)
- model registry (`pkg/model`)
- embedded admin panel (`pkg/admin`)

`App` exposes:

- `DB` (primary alias) and `DBs` (all opened aliases)
- `Database(alias)` and `DatabaseForRequest(r)` helpers
- graceful `Run`/`Shutdown` with shutdown hooks

## 3.2 HTTP and Middleware (`pkg/router`)

GoFrame uses its own router/mux abstractions (not Chi as a runtime dependency):

- route registration + mounting
- request middleware chain
- JSON helpers and HTTP utilities
- CORS/CSRF middleware
- rate limiting (`rate_limit_*`)
- OpenTelemetry HTTP instrumentation
- explicit mounting of experimental OpenAPI JSON documents through `pkg/app.App.MountOpenAPI`

## 3.3 Data and Model Layer

`pkg/db`:

- `database/sql`-based DB wrapper
- health checks and telemetry
- SQL migration executor and helpers

`pkg/model`:

- model metadata extraction from tags
- registry for app/admin integration
- generic CRUD operator
- metadata-driven migration scaffold generation
- model contract features include PK/FK/index metadata (simple + composite)

## 3.4 Admin (`pkg/admin`)

Embedded admin panel (React + TypeScript + Tailwind CSS) provides:

- Modern login page with theme support (dark/light)
- Model listing + schema endpoint
- CRUD API with tenant-aware filtering (multi-tenant support)
- List/search/filter/order pagination
- Export (CSV, JSON, SQL) and import (CSV/JSON) with validation
- Bulk actions with per-row error reporting
- Action-level authorization hooks (`AdminAuth.Authorize`)
- RBAC integration via Casbin enforcer (optional)
- Audit logging for CRUD operations (in-memory, bounded)
- Session inventory endpoint and UI telemetry
- Live traffic inspection (HTTP, SQL, sessions) with cluster relay
- System pulse (Go runtime, DB pool, feature flags, jobs)
- Health check dashboard (DB, Redis, mail connectivity)
- Migration listing and status
- Job queue details (via Asynq runtime)
- Multi-site listing and management
- Deployment info (standalone/Docker/K8s detection, cluster topology)
- Cache management endpoints
- File storage browser
- Email stats
- Data Studio for export/import operations

**UI Architecture:**
- React 19 with TypeScript
- Vite 6 bundler
- Tailwind CSS 3 styling
- shadcn/ui component library
- Zustand 5 state management
- React Router 7 for navigation
- Recharts 2 for data visualization
- Zero CDN dependencies (all packages installed locally)

Multi-tenant features:
- Tenant field detection via `db:"tenant"` tag or convention (`tenant_id`)
- Auto-filter CRUD queries by tenant when multi-tenant enabled
- Auto-inject tenant ID on record creation
- Tenant selector in UI header
- Tenant context propagated via request middleware

RBAC features:
- Casbin enforcer integration (optional, via `admin_rbac_policy_file`)
- Policy management API (add/remove policies)
- Role management API (assign/remove roles)
- Permission checking
- Superuser bypass
- UI for viewing policies and roles

Audit logging:
- automatic recording of CRUD operations (create/update/delete)
- in-memory bounded store (default 10,000 entries)
- filtering by user, model, action
- pagination support
- audit log viewer UI

## 3.5 Auth/Authz (`pkg/auth`, `pkg/authz`)

- JWT helpers
- password hashing helpers
- session manager with store backends:
- memory
- SQL table store
- Redis store
- session runtime metadata enrichment (`pod/host/instance`)
- Casbin integration points for authorization enforcement

## 3.6 Mail and Plugins (`pkg/mail`, `pkg/plugins`)

Mail:

- drivers: `noop`, `smtp`, `sendgrid`
- capability-style external provider bridge

Plugin runtime:

- provider discovery and capability schema handling
- `goframe-plugin-<provider>` primary external naming
- `goframe-mail-<driver>` legacy compatibility fallback

## 3.8 Storage (`pkg/storage`)

Provider-agnostic file storage abstraction with a durable interface designed to last through `v1.x`.

Supported providers:
- **S3-compatible** (AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces) — fully implemented
- **GCS** (Google Cloud Storage) — fully implemented
- **Azure Blob Storage** — fully implemented
- **Local filesystem** (development only) — fully implemented

Credential injection (`CredentialSource`):

All sensitive values use the `CredentialSource` type, supporting 4 resolution methods:

| Method | Config key | Use case | Example |
|--------|-----------|----------|---------|
| Direct value | `value` | Testing only | `value: AKIAIOSFODNN7EXAMPLE` |
| Environment variable | `env_var` | Primary for production | `env_var: AWS_SECRET_ACCESS_KEY` |
| File path | `file` | K8s secrets, mounted volumes | `file: /etc/secrets/gcs-sa.json` |
| Secret Manager | `secret_manager` | Cloud-native (planned) | `secret_manager: projects/P/secrets/S` |

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

Tasks (`pkg/tasks`):

- Asynq manager and worker runtime
- enqueue/process instrumentation hooks

Observability (`pkg/observe`):

- `slog` logger setup
- OpenTelemetry setup and shutdown

Experimental API contracts (`pkg/openapi` + `internal/contracts` convention):

- minimal OpenAPI 3.1 document model for scaffolded project contracts
- one shared source of truth for CLI export and runtime serving
- current supported subset includes paths, operations, JSON request bodies, JSON responses, scaffolded `data`/`count` envelopes, structured JSON error responses, empty responses, component schemas, and explicit path/query parameters
- helper functions may reduce repetition, but the generated project contract remains intentionally explicit rather than DSL-driven

## 4. Dependency Reality (from `go.mod`)

Direct runtime dependencies include:

- Configuration: `koanf` (`v2` + yaml/env/file/struct providers)
- Auth/session/security: `jwt/v5`, `scs/v2`, `casbin/v2`, `validator/v10`, `x/crypto`
- SQL drivers: `modernc.org/sqlite`, `pgx/v5`, `go-sql-driver/mysql`
- Enterprise exploratory SQL drivers: `go-mssqldb`, `go-ora/v2`
- Redis: `go-redis/v9`
- Tasks: `hibiken/asynq`
- Observability: OpenTelemetry SDK/exporters

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
    url: sqlite://goframe.db
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
- admin: `admin_prefix`, `admin_title`
- mail: `mail_driver`, `smtp_*`, `sendgrid_*`, `mail_from`
- security/rate limit: `rate_limit_*`
- i18n/static/storage: `default_locale`, `locales_path`, `static_*`, `storage_*`
- observability: `log_*`, `otlp_endpoint`, `metrics_path`

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

## 7. CLI Contract Baseline (`cmd/goframe`, `internal/cli`)

GoFrame ships stable operational CLI coverage for:

- runtime and diagnostics (`serve`, `routes`, `health`)
- scaffolding (`new`, `startapp`, `generate`)
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
- `internal/contracts/contracts.go` exposes the package-level document builder (`DefaultConfig`, `NewDocument`, `NewDocumentWithConfig`)
- `goframe openapi --out openapi.json` exports the current project contract as OpenAPI JSON
- generated server scaffolds can serve that same contract explicitly at `/openapi.json` via `app.MountOpenAPI`

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
