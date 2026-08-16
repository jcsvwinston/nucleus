# pkg/mail — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

Mail sender abstraction, provider registry (`RegisterProvider`), sender construction (`NewSender`), `Config` (incl. `Config.CircuitBreaker CircuitBreakerConfig`); `CircuitBreakerConfig{Enabled, FailureThreshold, Cooldown, HalfOpenMaxConcurrent}` (shape finalized and promoted to `stable` 2026-07-07, v1 gate A-1d)

## Notes

Built-in providers plus external capability plugins (`nucleus-plugin-<provider>` advertising `mail.send`) are supported. `App.New` autowraps `Send` with a `pkg/circuit.Breaker` by default; `Healthy` (SMTP HELO probe) bypasses the breaker so `/healthz` observes recovery while `Send` is short-circuited. `noop` driver is never wrapped.
