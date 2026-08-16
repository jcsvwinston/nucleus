# pkg/tasks — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Task manager config/runtime, explicit enqueue-policy helpers, explicit scheduler helpers, queue runtime operations, and JSON task helpers

## Notes

Queue runtime boundary for app code, including explicit queue action helpers, periodic scheduling helpers, and runtime inspection used by admin/runtime operations.
