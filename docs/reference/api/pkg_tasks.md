# pkg/tasks — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Task manager config/runtime, explicit enqueue-policy helpers, explicit scheduler helpers, queue runtime operations, JSON task helpers, and the provider registry (`Provider`, `ProviderOptions`, `ProviderRuntime`, `RegisterProvider`, `MustRegisterProvider`, `LookupProvider`, `RegisteredProviders`) through which a jobs provider that ships as its own module — `providers/tasks-asynq` — is selected by `jobs_provider` (DEP-2026-020)

## Notes

Queue runtime boundary for app code, including explicit queue action helpers, periodic scheduling helpers, and runtime inspection used by admin/runtime operations.
