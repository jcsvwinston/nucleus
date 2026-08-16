# pkg/outbox — contract

Lifecycle: `transitional`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

SQL-backed outbox store, runtime inspection, and dispatcher APIs (`NewStore`, `Enqueue`, `EnqueueTx`, `InspectRuntime`, `NewDispatcher`, `Run`, `RunOnce`)

## Notes

Small transactional outbox surface intended for durable intra-app delivery; public and supported, but still early enough that non-essential ergonomics may tighten before `v1.0`. **Outside the v1.0 promise** (v1 gate A-1b, 2026-07-08): remains `transitional` through v1.0 — nobody has inventoried which ergonomics still need tightening, and promoting without that list would freeze blind; promotion is tracked for v1.x once that inventory exists. Nuance: stable `pkg/app.Config` carries `OutboxConfig` (koanf `outbox`), so the *config shape* freezes with `pkg/app` at v1.0 while the feature's Go surface stays outside the promise — a documented, contained coupling (config keys are additive-friendly), unlike the openapi type coupling DEP-2026-008 removed.
