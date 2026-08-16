# pkg/observe — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Logger/context correlation helpers (incl. `CtxWithTenantID`/`TenantIDFromCtx`, and `CtxWithModelObserved`/`IsModelObserved` — the SQL de-dup marker between model.CRUD and driver-level instrumentation, ADR-021), OTel setup entrypoint with optional Prometheus reader; secret-redaction surface — `NewLoggerWithRedaction`, `RedactionConfig{Disabled, ExtraKeys, Placeholder}`, `DefaultRedactedKeys()`, `RedactionPlaceholder`

## Notes

`SetupOpenTelemetry` returns `(shutdown, metricsHandler, err)`. When `TelemetryConfig.PrometheusEnabled` is set, the handler is mounted by `pkg/app` at `Config.MetricsPath`. `NewLogger` redacts secret-keyed log attributes by default (ADR-007); `NewLoggerWithRedaction` is the additive constructor for extending/overriding/disabling that.
