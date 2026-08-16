# pkg/observability/hooks — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Instrumentation bridges that publish into the bus — `NewHTTPMiddleware`/`HTTPMiddlewareConfig`, `NewSQLObserver`/`SQLObserverConfig`, `NewSessionRecorder`/`SessionRecorderConfig`/`SessionInfo`

## Notes

Plugs the framework's existing HTTP middleware, SQL observer, and session manager into `observability.Bus`. Each hook gates on `Bus.HasSubscribers` and sanitizes (redacts request bodies, SQL argument values, session tokens) before emitting. No third-party imports (frozen but not firewalled). **Promoted to `stable` in v1.3.0** (v1 gate §B W1 resolved 2026-07-13) alongside its parent package.
