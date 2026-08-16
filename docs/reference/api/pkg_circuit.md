# pkg/circuit — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`Config`, `New`, `Breaker` (`Do(ctx, fn)`, `State()`), `State` enum, `ErrOpen`

## Notes

Standalone three-state circuit breaker; race-tested. `pkg/app` wires this automatically for `mail.Sender.Send` and remote `storage.Store` operations (ADR-004). Operators can disable or tune via `mail_circuit_breaker.*` and `storage.circuit_breaker.*` config keys, or wrap additional external calls directly using `circuit.New`.
