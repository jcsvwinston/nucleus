# API Contract Inventory

Reference date: 2026-05-13.
Status: Current.

This file defines lifecycle tags for Nucleus public API surfaces and documents extension points and non-contract zones.

## Lifecycle Tags

- `stable`: treated as compatibility contract; avoid breaking changes (pre-`v1.0` breaks require explicit migration notes).
- `transitional`: public and supported, but still maturing; breaking adjustments may occur with explicit release notes.
- `experimental`: no compatibility guarantee yet; intended for early validation and feedback.

Policy references:

- `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`
- `docs/governance/COMPATIBILITY_SLO.md`

## Public Package Inventory

| Surface | Lifecycle | Contract Scope | Notes |
| --- | --- | --- | --- |
| `pkg/app` | `stable` | `Config`, `DefaultConfig`, `LoadConfig`, `New`, `App` lifecycle methods (`Run`, `Shutdown`, `RegisterModel`, `MountAdmin`, `OnShutdown`); `App.JWT *auth.JWTManager` (nil when no signing material is configured); `CircuitBreakerSpec` (`transitional`) — koanf-bindable spec bound under `mail_circuit_breaker` and `storage.circuit_breaker`; `Config.MailCircuitBreaker CircuitBreakerSpec` (`transitional`); `Config.Storage.CircuitBreaker CircuitBreakerSpec` (`transitional`) | Core application bootstrap contract. `App.New` builds `App.JWT` from `jwt_keys[]` (multi-key) or `jwt_secret` (legacy fallback); auto-mounts `/.well-known/jwks.json` when ≥1 RS256 key is configured. Also autowraps `mail.Sender.Send` (unless driver is `noop` or empty) and remote `storage.Store` operations (unless provider is `local`) with `pkg/circuit.Breaker` when the respective `circuit_breaker.enabled` flag is `true` (default). |
| `pkg/db` | `stable` | `db.New`, `db.DB` (incl. `Health`, `System`), migrator APIs (`NewMigrator`, migration lifecycle methods, `Drift`/`DriftEntry`/`DriftKindMissingUpFile`), SQL URL support | URL schemes `sqlite://`, `postgres://`/`postgresql://`, `mysql://`, `sqlserver://`/`mssql://`, `oracle://` are all `stable` (MSSQL/Oracle promoted to required CI gate 2026-05-12). |
| `pkg/model` | `stable` | `BaseModel`, metadata extraction, registry, CRUD interfaces and hooks; dialect-aware migration scaffolds (`BuildSQLiteMigrationScaffold`, `BuildPostgresMigrationScaffold`, `BuildMySQLMigrationScaffold`) | Foundation for model/admin integration. Multi-driver `AutoMigrate` dispatches on `db.DB.System()`. |
| `pkg/router` | `stable` | Router construction, middleware hooks, unified request context helpers (`Context`, `ContextHandler`), rendering/binding/pagination helpers; rate-limit middleware keys per-tenant when a tenant is resolved in context | Request/response helper behavior is contract surface. |
| `pkg/auth` | `stable` | JWT manager (single-secret + multi-key rotation), `SigningAlgorithm`, `SigningKey`, `NewJWTManagerFromKeys`, `RotateKey`, `RemoveKey`, `CurrentKID`, `JWKSHandler`, `JWKS`, `JWKSet`, `JWK`; claims context helpers, session manager/store APIs, `ContractAliasCommandNames` | Multi-store session surface is contracted (`memory`, `sql`, `redis`). RS256 + JWKS exposes the asymmetric public key set for relying parties. `App.New` (in `pkg/app`) consumes this package to build `App.JWT` from config; application code accesses the manager via `App.JWT` rather than constructing it directly unless non-config key loading is needed. |
| `pkg/authz` | `stable` | `Enforcer` (creation + Casbin-backed enforcement), `AddPolicy` (allow), `Deny` (explicit deny override), `RemovePolicy` (drops both effects), authz middleware helpers | Default-deny with deny-override semantics. |
| `pkg/mail` | `stable` | Mail sender abstraction, provider registry (`RegisterProvider`), sender construction (`NewSender`), `Config` (incl. `Config.CircuitBreaker CircuitBreakerConfig`); `CircuitBreakerConfig{Enabled, FailureThreshold, Cooldown, HalfOpenMaxConcurrent}` (`transitional`) | Built-in providers plus external capability plugins (`nucleus-plugin-<provider>` advertising `mail.send`) are supported. `App.New` autowraps `Send` with a `pkg/circuit.Breaker` by default; `Healthy` (SMTP HELO probe) bypasses the breaker so `/healthz` observes recovery while `Send` is short-circuited. `noop` driver is never wrapped. |
| `pkg/storage` | `stable` | `Store` interface (`Put`, `Get`, `Delete`, `Exists`, `List`, `PublicURL`, `SignedURL`, `Copy`), `New`, `Config` (incl. `Config.CircuitBreaker CircuitBreakerConfig`); `CircuitBreakerConfig{Enabled, FailureThreshold, Cooldown, HalfOpenMaxConcurrent}` (`transitional`); `PutOptions`, `ObjectInfo`, `Visibility`, `CredentialSource`, `ListOptions`, `ListResult`, `ErrNotFound`, `TenantStore`, `NewWithTenant`, `TenantKey`, `Cleaner`, `NewCleaner`, `CleanupTempKey`, `IsTempKey`, `PublicMapper`, `NewPublicMapperForConfig` | Provider-agnostic file storage (`local`, `s3`, `gcs`, `azure`). `App.New` autowraps remote provider operations (Put/Get/Delete/Exists/List/Copy/SignedURL) with a `pkg/circuit.Breaker` by default; `local` provider is never wrapped; `PublicURL` is pass-through (pure string composition); `ErrNotFound` from `Get`/`Exists` is not counted as a breaker failure. |
| `pkg/plugins` | `stable` | Plugin SDK v1 envelopes/capability constants, inventory/probe/runtime execution APIs | SDK `v1` contract is intended stable through `v1.x`. |
| `pkg/openapi` | `experimental` | Minimal OpenAPI 3.1 document model, JSON serialization helpers, runtime document handler helpers, and small schema/response/parameter helpers for scaffolded contracts | Official experimental base layer for project-level API contracts via `internal/contracts`, `nucleus openapi`, and explicit runtime serving; the current helper subset covers repeated JSON schema shapes, shared `data`/`count` response envelopes, structured JSON error responses, empty responses, and explicit path/query parameters including the scaffolded optional `q` search convention, but the overall surface may still expand before `v1.0`. |
| `pkg/tasks` | `stable` | Task manager config/runtime, explicit enqueue-policy helpers, explicit scheduler helpers, queue runtime operations, and JSON task helpers | Queue runtime boundary for app code, including explicit queue action helpers, periodic scheduling helpers, and runtime inspection used by admin/runtime operations. |
| `pkg/outbox` | `transitional` | SQL-backed outbox store, runtime inspection, and dispatcher APIs (`NewStore`, `Enqueue`, `EnqueueTx`, `InspectRuntime`, `NewDispatcher`, `Run`, `RunOnce`) | Small transactional outbox surface intended for durable intra-app delivery; public and supported, but still early enough that non-essential ergonomics may tighten before `v1.0`. |
| `pkg/observe` | `stable` | Logger/context correlation helpers (incl. `CtxWithTenantID`/`TenantIDFromCtx`), OTel setup entrypoint with optional Prometheus reader | `SetupOpenTelemetry` returns `(shutdown, metricsHandler, err)`. When `TelemetryConfig.PrometheusEnabled` is set, the handler is mounted by `pkg/app` at `Config.MetricsPath`. |
| `pkg/health` | `stable` | `Prober` interface, `Run(ctx, probes, timeout)` aggregator, `NewDBProbe`, `NewRedisProbe`, `NewStorageProbe`, `NewMailProbe`, `SupportsMailProbe`, `Result` | Dependency probe abstraction consumed by `App.handleHealthz`. Keeps `github.com/redis/go-redis/v9` wrapped per firewall rules. |
| `pkg/circuit` | `stable` | `Config`, `New`, `Breaker` (`Do(ctx, fn)`, `State()`), `State` enum, `ErrOpen` | Standalone three-state circuit breaker; race-tested. `pkg/app` wires this automatically for `mail.Sender.Send` and remote `storage.Store` operations (ADR-004). Operators can disable or tune via `mail_circuit_breaker.*` and `storage.circuit_breaker.*` config keys, or wrap additional external calls directly using `circuit.New`. |
| `pkg/errors` | `stable` | Domain error constructors + HTTP writer | Error payload shape and status mapping are public behavior. |
| `pkg/validate` | `stable` | Validation entrypoint + custom rule registration | Shared validation boundary for handlers/models. |
| `pkg/signals` | `stable` | In-process event bus types/APIs plus explicit Redis relay helpers for distributed forwarding | Used by model hooks, domain events, and small distributed pub/sub bridges. |
| `pkg/admin` | `transitional` | Admin panel mount/handler integration surface plus operational runtime snapshot behavior | Embedded UI details can evolve faster than core runtime APIs, but `/admin` is expected to remain a faithful operational reflection of framework capabilities such as data CRUD, jobs, outbox state, and distributed live topology. |

## Extension Points

| Extension Point | Lifecycle | Contract |
| --- | --- | --- |
| In-process mail provider registration (`mail.RegisterProvider`) | `stable` | Register custom provider factories without forking framework internals. |
| External capability plugins (`nucleus-plugin-<provider>`) using SDK `v1` | `stable` | Capability envelopes, exit-code mapping, and probe flow are contracted. |
| External CLI command bridge (`nucleus-<name>`) | `transitional` | Command delegation interface is supported but kept intentionally minimal. |

## Explicit Non-Contract Surfaces

These surfaces are intentionally outside compatibility guarantees:

- `internal/*` packages and all non-exported implementation details.
- Frontend implementation details under `pkg/admin/ui/*` (except mounted admin route behavior).
- Test helper APIs and environment variables used only by tests/CI harnesses.
- Generated scaffold internals beyond documented structure expectations (`docs/reference/PROJECT_LAYOUT.md`).

## Contract Review Rule

When changing any `stable` surface:

1. update this inventory if contract shape changes,
2. add/adjust compatibility tests,
3. include migration notes in `CHANGELOG.md` if behavior is user-visible.

## Freeze Enforcement

Stable no-removal freeze is enforced in `contracts/freeze_test.go` with baselines under `contracts/baseline/`:

- `cli_primary_commands.txt`
- `cli_json_status_keys.txt` (stable command-status JSON envelope/data keys for automation-critical commands)
- `config_key_patterns.txt`
- `api_exported_symbols.txt` (stable packages only)

Intentional baseline refresh workflow:

```bash
NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts -run '^TestContractFreeze_APIExportedSymbols_NoRemovals$' -count=1
bash scripts/ci/check_contract_freeze.sh
```
