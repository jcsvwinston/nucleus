# API Contract Inventory

Reference date: 2026-06-21.
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

| Surface | Lifecycle | Contract |
| --- | --- | --- |
| `pkg/app` | `stable` | `Config`, `DefaultConfig`, `LoadConfig`, `NormalizeRuntimeConfig`, `New`, `App` lifecycle methods (`Run`, `Shutdown`, `RegisterModel`, `OnShutdown`) — [full contract](api/pkg_app.md) |
| `pkg/nucleus` | `stable` | `App{}` canonical struct embedding `app.Config` plus Go-only wiring fields (`Modules map[string]ModuleSpec`, `Middleware []Middleware`, `Services []ServiceRe… — [full contract](api/pkg_nucleus.md) |
| `pkg/db` | `stable` | `db.New`, `db.DB` (incl. `Health`, `System`), `db.Config` (incl. `StatementObserver` — opt-in driver-level SQL instrumentation, ADR-021), `db.StatementObserv… — [full contract](api/pkg_db.md) |
| `pkg/model` | `stable` | `BaseModel`, metadata extraction, registry, CRUD interfaces and hooks — [full contract](api/pkg_model.md) |
| `pkg/router` | `stable` | Router construction, middleware hooks, unified request context helpers (`Context`, `ContextHandler`), rendering/binding/pagination helpers — [full contract](api/pkg_router.md) |
| `pkg/auth` | `stable` | JWT manager (single-secret + multi-key rotation), `SigningAlgorithm`, `SigningKey`, `NewJWTManagerFromKeys`, `RotateKey`, `RemoveKey`, `CurrentKID`, `JWKSHan… — [full contract](api/pkg_auth.md) |
| `pkg/authz` | `stable` | `Enforcer` (creation + Casbin-backed enforcement), `AddPolicy` (allow), `Deny` (explicit deny override), `RemovePolicy` (drops both effects), authz middlewar… — [full contract](api/pkg_authz.md) |
| `pkg/mail` | `stable` | Mail sender abstraction, provider registry (`RegisterProvider`), sender construction (`NewSender`), `Config` (incl. `Config.CircuitBreaker CircuitBreakerConf… — [full contract](api/pkg_mail.md) |
| `pkg/storage` | `stable` | `Store` interface (`Put`, `Get`, `Delete`, `Exists`, `List`, `PublicURL`, `SignedURL`, `Copy`), `New`, `Config` (incl. `Config.CircuitBreaker CircuitBreakerC… — [full contract](api/pkg_storage.md) |
| `pkg/plugins` | `stable` | Plugin SDK v1 envelopes/capability constants, inventory/probe/runtime execution APIs — [full contract](api/pkg_plugins.md) |
| `pkg/openapi` | `experimental` | Minimal OpenAPI 3.1 document model, JSON serialization helpers, runtime document handler helpers, and small schema/response/parameter/security helpers for sc… — [full contract](api/pkg_openapi.md) |
| `pkg/tasks` | `stable` | Task manager config/runtime, explicit enqueue-policy helpers, explicit scheduler helpers, queue runtime operations, and JSON task helpers — [full contract](api/pkg_tasks.md) |
| `pkg/outbox` | `transitional` | SQL-backed outbox store, runtime inspection, and dispatcher APIs (`NewStore`, `Enqueue`, `EnqueueTx`, `InspectRuntime`, `NewDispatcher`, `Run`, `RunOnce`) — [full contract](api/pkg_outbox.md) |
| `pkg/observe` | `stable` | Logger/context correlation helpers (incl. `CtxWithTenantID`/`TenantIDFromCtx`, and `CtxWithModelObserved`/`IsModelObserved` — the SQL de-dup marker between m… — [full contract](api/pkg_observe.md) |
| `pkg/observability` | `stable` | In-process observability fan-out bus — `Bus`, `NewBus`, `Subscription`, `SubscribeOptions`, `Stats`, generic `RingBuffer[T]`, the `Event` interface and typed… — [full contract](api/pkg_observability.md) |
| `pkg/observability/hooks` | `stable` | Instrumentation bridges that publish into the bus — `NewHTTPMiddleware`/`HTTPMiddlewareConfig`, `NewSQLObserver`/`SQLObserverConfig`, `NewSessionRecorder`/`S… — [full contract](api/pkg_observability_hooks.md) |
| `pkg/health` | `stable` | `Prober` interface, `Run(ctx, probes, timeout)` aggregator, `NewDBProbe`, `NewRedisProbe`, `NewStorageProbe`, `NewMailProbe`, `SupportsMailProbe`, `Result` — [full contract](api/pkg_health.md) |
| `pkg/nucleustest` | `experimental` | In-process test kit (DX-22): `Start`/`StartApp` boot a nucleus application inside the test process on a free loopback port, `Server` (`Stop`, `Client`, `URL`, `MintToken`) — [full contract](api/pkg_nucleustest.md) |
| `pkg/circuit` | `stable` | `Config`, `New`, `Breaker` (`Do(ctx, fn)`, `State()`), `State` enum, `ErrOpen` — [full contract](api/pkg_circuit.md) |
| `pkg/errors` | `stable` | Domain error constructors + HTTP writer — [full contract](api/pkg_errors.md) |
| `pkg/validate` | `stable` | Validation entrypoint + custom rule registration — [full contract](api/pkg_validate.md) |
| `pkg/signals` | `stable` | In-process event bus types/APIs plus explicit Redis relay helpers for distributed forwarding — [full contract](api/pkg_signals.md) |
| ~~`pkg/admin`~~ | `removed` | Extracted to the separate `orbit` Go module (ADR-019, 2026-06-21). Mount via `orbit.Module(...)` from `github.com/jcsvwinston/orbit`. The in-core package is no longer part of the public API surface. |

## Extension Points

| Extension Point | Lifecycle | Contract |
| --- | --- | --- |
| In-process mail provider registration (`mail.RegisterProvider`) | `stable` | Register custom provider factories without forking framework internals. |
| External capability plugins (`nucleus-plugin-<provider>`) using SDK `v1` | `stable` | Capability envelopes, exit-code mapping, and probe flow are contracted. |
| External CLI command bridge (`nucleus-<name>`) | `transitional` | Command delegation interface is supported but kept intentionally minimal. |

## Explicit Non-Contract Surfaces

These surfaces are intentionally outside compatibility guarantees:

- `internal/*` packages and all non-exported implementation details.
- Frontend implementation details of pluggable modules (e.g. orbit's embedded SPA).
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

The `api_exported_symbols.txt` baseline covers exactly the `pkg/*` packages tagged `stable` in this inventory; `experimental` and `transitional` packages are intentionally excluded so their surfaces can still tighten before `v1.0`. Promoting a package's lifecycle to `stable` here is therefore a coupled change: add it to the `packages` slice in `contracts/freeze_test.go` and run the baseline refresh below in the same change.

Intentional baseline refresh workflow:

```bash
NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts -run '^TestContractFreeze_APIExportedSymbols_NoRemovals$' -count=1
bash scripts/ci/check_contract_freeze.sh
```
