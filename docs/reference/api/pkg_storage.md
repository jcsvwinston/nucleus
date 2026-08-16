# pkg/storage — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Store` interface (`Put`, `Get`, `Delete`, `Exists`, `List`, `PublicURL`, `SignedURL`, `Copy`), `New`, `Config` (incl. `Config.CircuitBreaker CircuitBreakerConfig`); `CircuitBreakerConfig{Enabled, FailureThreshold, Cooldown, HalfOpenMaxConcurrent}` (shape finalized and promoted to `stable` 2026-07-07, v1 gate A-1d); `PutOptions`, `ObjectInfo`, `Visibility`, `CredentialSource`, `ListOptions`, `ListResult`, `ErrNotFound`, `TenantStore`, `NewWithTenant`, `TenantKey`, `Cleaner`, `NewCleaner`, `CleanupTempKey`, `IsTempKey`, `PublicMapper`, `NewPublicMapperForConfig`

## Notes

Provider-agnostic file storage (`local`, `s3`, `gcs`, `azure`). `App.New` autowraps remote provider operations (Put/Get/Delete/Exists/List/Copy/SignedURL) with a `pkg/circuit.Breaker` by default; `local` provider is never wrapped; `PublicURL` is pass-through (pure string composition); `ErrNotFound` from `Get`/`Exists` is not counted as a breaker failure.
