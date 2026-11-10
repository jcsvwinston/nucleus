# pkg/tasks — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Task manager config/runtime, explicit enqueue-policy helpers, explicit scheduler helpers, queue runtime operations, JSON task helpers, and the provider registry (`Provider`, `RegisterProvider`, `LookupProvider`, `RegisteredProviders`) `jobs_provider` resolves through

## Notes

Queue runtime boundary for app code, including explicit queue action helpers, periodic scheduling helpers, and runtime inspection used by admin/runtime operations. The memory and sql providers are built in; every other provider registers by name with `RegisterProvider` from its `init()` (asynq does, from `pkg/tasks/providers/asynq`; its own module at v2.0.0, `providers/tasks-asynq`, which forwards to it until then, DEP-2026-020) and the framework and `nucleus doctor` resolve `jobs_provider` through `LookupProvider`.
