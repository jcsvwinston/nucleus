# pkg/observability — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

In-process observability fan-out bus — `Bus`, `NewBus`, `Subscription`, `SubscribeOptions`, `Stats`, generic `RingBuffer[T]`, the `Event` interface and typed events (`HTTPRequestEvent`, `SQLStatementEvent`, `SessionChangeEvent`, `CustomEvent`) with their pooled `Acquire*` constructors, `EventKind`/`SessionChangeKind` enums, `Filter`, `DefaultSubscriberChannelSize`

## Notes

In-process event bus — consumed by `pkg/app`, `pkg/observability/hooks`, and pluggable modules (such as orbit) that subscribe for live HTTP/SQL events via `nucleus.Runtime.Observability()`. Hot-path design: `Bus.HasSubscribers(kind)` short-circuits event construction to a single atomic load when nobody is watching, and events are pooled. No third-party imports (frozen but not firewalled). **Promoted to `stable` in v1.3.0** (v1 gate §B W1 resolved 2026-07-13): the exported symbol shapes are frozen for the life of v1.x; the pooled/refcount internals stay free to optimize (unexported). Modules may still prefer the narrower `nucleus.EventBus` facade for value-copy semantics.
