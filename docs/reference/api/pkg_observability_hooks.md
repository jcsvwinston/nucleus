# pkg/observability/hooks — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Instrumentation bridges that publish into the bus — `NewHTTPMiddleware`/`HTTPMiddlewareConfig`, `NewSQLObserver`/`SQLObserverConfig`, `NewSessionRecorder`/`SessionRecorderConfig`/`SessionInfo`; `NewSQLQueryObserver` with the package's own `SQLQueryEvent`/`SQLQueryObserver` types

## Notes

Plugs the framework's existing HTTP middleware, SQL observer, and session manager into `observability.Bus`. Each hook gates on `Bus.HasSubscribers` and sanitizes (redacts request bodies, SQL argument values, session tokens) before emitting. No third-party imports (frozen but not firewalled). `NewSQLObserver` keeps returning `model.SQLQueryObserver` until v2.0.0, when `pkg/model` leaves the public API (DEP-2026-024); `NewSQLQueryObserver` is the same observer over `hooks.SQLQueryEvent`, for code that does not go through `model.CRUD`. **Promoted to `stable` in v1.3.0** (v1 gate §B W1 resolved 2026-07-13) alongside its parent package.
