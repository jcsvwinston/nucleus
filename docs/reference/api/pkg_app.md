# pkg/app — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Config`, `DefaultConfig`, `LoadConfig`, `NormalizeRuntimeConfig`, `New`, `App` lifecycle methods (`Run`, `Shutdown`, `RegisterModel`, `OnShutdown`); `App.JWT *auth.JWTManager` (nil when no signing material is configured); `CircuitBreakerSpec` — koanf-bindable spec bound under `mail_circuit_breaker` and `storage.circuit_breaker` (shape finalized and promoted to `stable` 2026-07-07, v1 gate A-1d); `Config.MailCircuitBreaker CircuitBreakerSpec`; `Config.Storage.CircuitBreaker CircuitBreakerSpec`; `RequestScope` (struct — `Host`, `Site`, `Tenant`, `DatabaseAlias` string fields — captures the per-request multi-site/multi-tenant routing decision); `RequestScopeFromContext(ctx context.Context) (RequestScope, bool)` (context accessor used by orbit and other pluggable modules to read the resolved scope without importing the full `*app.App`)

## Notes

Core application bootstrap contract. `App.New` builds `App.JWT` from `jwt_keys[]` (multi-key) or `jwt_secret` (legacy fallback); auto-mounts `/.well-known/jwks.json` when ≥1 RS256 key is configured. Also autowraps `mail.Sender.Send` (unless driver is `noop` or empty) and remote `storage.Store` operations (unless provider is `local`) with `pkg/circuit.Breaker` when the respective `circuit_breaker.enabled` flag is `true` (default). `NormalizeRuntimeConfig` is the exported wrapper around the runtime-config normalisation `LoadConfig` performs internally — multi-file `pkg/nucleus.FromConfigFile` calls it so its returned `*Config` is indistinguishable from the env-var path (ADR-010 Phase 2b). `MountAdmin` and `RegisterAdminModels` were removed (ADR-019, 2026-06-21); admin is now opt-in via the orbit module (`github.com/jcsvwinston/orbit`).
