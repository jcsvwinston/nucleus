# pkg/signals — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

In-process event bus over typed topics — `NewBus`, `Bus` (`Clear(topics ...string)`, `Dropped`), `Define`, `Topic[T]` (`String`), `Subscribe`, `Publish`, `PublishAsync`, `ErrHandlerPanic` — plus the explicit Redis relay for distributed forwarding: `NewRedisRelay`, `RedisRelayConfig`, `RedisRelay` (`Channel`, `ForwardToBus`, `Close`), `RelayPublish`, `RelaySubscribe` and the sentinels `ErrRedisURLRequired`, `ErrSignalRequired` (an empty topic name), `ErrNilHandler`, `ErrNilBus`, `ErrNilRelay`.

## Notes

Used for an application's own domain events and for small distributed pub/sub bridges; the outbox delivers onto the same bus through `outbox.NewBusBridge`. The untyped door of v1 (`Signal`, `Event`, `Handler`, `Bus.On`, `Bus.Emit`, `Bus.EmitAsync`, `Topic.Name`) and the model lifecycle names `pkg/model` emitted left at v2.0.0 (DEP-2026-025, DEP-2026-024); a subscriber to a model's lifecycle uses Quark's hooks.
