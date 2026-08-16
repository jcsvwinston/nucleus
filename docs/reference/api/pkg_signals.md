# pkg/signals — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

In-process event bus types/APIs plus explicit Redis relay helpers for distributed forwarding

## Notes

Used by model hooks, domain events, and small distributed pub/sub bridges.
