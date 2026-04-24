# Dependency Impact Report

Reference date: 2026-04-23.
Status: Current (updated for build-tag modularization).

This document tracks the critical third-party dependencies that form the stable
surface of GoFrame and records swap drills to prove adapter boundaries work.

## Classification

Dependencies are classified by their proximity to stable public APIs:

| Tier | Description | Examples |
|------|-------------|----------|
| **Critical** | Third-party concrete types reachable from stable public APIs | `scs`, `casbin`, `validator`, `jwt`, `koanf` |
| **Core** | Internal implementation, hidden behind framework abstractions | `pgx`, `mysql`, `sqlite`, `asynq`, `go-redis` |
| **Build-tagged** | Compiled only when explicitly requested via `-tags` | `mssql` (`go-mssqldb`), `oracle` (`go-ora`) |
| **Optional** | Feature-gated, zero impact when not configured | `gcs`, `azure`, `minio`, `otel exporters` |

## Dependency Inventory

### Critical — API Surface

| Dependency | Version | Used By | Swap Risk | Notes |
|------------|---------|---------|-----------|-------|
| `github.com/alexedwards/scs/v2` | v2.9.0 | `pkg/auth` session management | **Medium** | `scs.SessionManager` is embedded in `AdminAuth` and `app.Config`; adapter exists but not framework-owned |
| `github.com/casbin/casbin/v2` | v2.135.0 | `pkg/authz` RBAC enforcement | **Low** | Only `casbin.Enforcer` used internally; no casbin types leak to public API |
| `github.com/go-playground/validator/v10` | v10.25.0 | `pkg/validate` model validation | **Low** | Entirely internal; public API uses generic `ValidationError` types |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | `pkg/auth` JWT parsing | **Low** | Only `jwt.Token` and `jwt.MapClaims` used internally |
| `github.com/knadh/koanf/v2` | v2.1.2 | `pkg/app` config loading | **Low** | Entirely internal; config struct is the public surface |

### Core — Internal Implementation

| Dependency | Version | Used By | Swap Risk | Notes |
|------------|---------|---------|-----------|-------|
| `modernc.org/sqlite` | v1.23.1 | `pkg/db` SQL driver | **Low** | Hidden behind `*sql.DB`; swap is config-only |
| `github.com/go-sql-driver/mysql` | v1.7.0 | `pkg/db` SQL driver | **Low** | Hidden behind `*sql.DB` |
| `github.com/jackc/pgx/v5` | v5.5.5 | `pkg/db` SQL driver | **Low** | Hidden behind `*sql.DB` |
| `github.com/microsoft/go-mssqldb` | v1.8.2 | `pkg/db` SQL driver | **Low** | Hidden behind `*sql.DB`; **requires `-tags mssql`** |
| `github.com/sijms/go-ora/v2` | v2.9.0 | `pkg/db` SQL driver | **Low** | Hidden behind `*sql.DB`; **requires `-tags oracle`** |
| `github.com/hibiken/asynq` | v0.25.1 | `pkg/tasks` job queue | **Medium** | `asynq.Server`/`asynq.Client` are framework-internal |
| `github.com/redis/go-redis/v9` | v9.14.1 | `pkg/auth` session store, cluster | **Medium** | Used directly for Redis session store |
| `github.com/alicebob/miniredis/v2` | v2.37.0 | Test infrastructure | **None** | Dev dependency only |

### Optional — Feature-Gated

| Dependency | Version | Used By | Swap Risk | Notes |
|------------|---------|---------|-----------|-------|
| `cloud.google.com/go/storage` | v1.62.0 | `pkg/storage` GCS driver | **Low** | Behind `Store` interface |
| `github.com/Azure/azure-sdk-for-go/sdk/storage/azblob` | v1.6.4 | `pkg/storage` Azure driver | **Low** | Behind `Store` interface |
| `github.com/minio/minio-go/v7` | v7.0.100 | `pkg/storage` S3 driver | **Low** | Behind `Store` interface |
| `go.opentelemetry.io/otel/*` | v1.43.0 | `pkg/observe` telemetry | **Low-Medium** | SDK types used internally; metric/trace interfaces are stable |

## Swap Drills

### Drill 1: SQL Driver Swap (SQLite → PostgreSQL)

**Goal:** Verify that switching the SQL driver requires only config changes, no app code modifications.

**Method:**
1. Run full test suite with default SQLite config (`sqlite://:memory:`)
2. Run admin + model tests against PostgreSQL via Docker
3. Verify identical test results, identical API responses, identical migration behavior

**Result:** ✅ **Passed at abstraction level.** All SQL drivers (SQLite, PostgreSQL, MySQL, MSSQL, Oracle) connect through `*sql.DB`. The `pkg/model` and `pkg/admin` layers never see driver-specific types. Cross-engine integration tests run in CI via `sql_matrix_test.go` which accepts `DATABASE_URL` env var for live engine testing.

**Evidence checklist:**
- [x] All SQL drivers hidden behind `*sql.DB` — no driver-specific types in public APIs
- [x] `pkg/model/crud.go` uses only `sql.DB`, `sql.Rows`, `sql.Result`
- [x] `pkg/admin/handlers.go` uses only `model.ModelMeta` and `model.CRUD`
- [x] CI matrix tests include multiple Go versions; per-engine tests gated on `DATABASE_URL`
- [ ] Full PostgreSQL live integration test (CI gate — requires Docker runner)

### Drill 2: Session Backend Swap (Memory → Redis)

**Goal:** Verify that switching session backend requires only config changes.

**Method:**
1. Run admin session tests with `session_store: memory` (default)
2. Run admin session tests with `session_store: redis` + `session_redis_url`
3. Verify session persistence, idle timeout, and cluster awareness

**Result:** _Pending execution — tracked as v1.0 gate._

### Drill 3: Validator Library Swap

**Goal:** Prove `pkg/validate` can swap validation libraries without touching `pkg/model` or app code.

**Method:**
1. Current: `go-playground/validator/v10`
2. Create alternate implementation with a different validation library (e.g. `oxisto/govalidator`)
3. Verify identical `ValidationError` output for the same input models

**Result:** _Not yet attempted — adapter already exists via `ValidationError` interface._

## Firewall Tests

Third-party type leak prevention:

| Boundary | Test | Status |
|----------|------|--------|
| `pkg/router` — no `net/http` handler types leak to controllers | Interface uses `http.HandlerFunc` | ✅ Pass |
| `pkg/db` — no driver-specific types in public API | Only `*sql.DB`/`*sql.Rows` exposed | ✅ Pass |
| `pkg/storage` — `Store` interface has no provider types | All drivers implement `Store` | ✅ Pass |
| `pkg/auth` — `scs.SessionManager` bounded to `PanelConfig.Session` | `AdminAuth` interface is stable | ✅ Pass |
| `pkg/authz` — `casbin.Enforcer` never returned from public API | Only `Allowed bool` returned | ✅ Pass |
| `pkg/validate` — `validator.Validate` never exposed | Only `[]ValidationError` returned | ✅ Pass |

## Release Gate

For each release candidate:
1. Run `go mod graph | grep -v indirect` to list direct dependencies
2. Flag any version bump of a **Critical** tier dependency
3. Verify no new third-party types appear in stable public API surfaces
4. Confirm all firewall tests still pass
