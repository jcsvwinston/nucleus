# Deprecation Notice: the untyped door of `pkg/signals` leaves at v2.0.0

- ID: `DEP-2026-025`
- Status: `active`
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`; decision D6 of the owner, 2026-10-16)
- Earliest removal: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md` and the suite's deprecation policy.
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`signals.Bus` is one bus with two doors. The typed one, delivered by A7
(`Define`, `Topic[T]`, `Subscribe`, `Publish`, `PublishAsync`), carries the
payload as the type its topic declares and the context the event was
published with. The untyped one — `Signal`, `Event{Payload any}`, `Handler`,
`Bus.On`, `Bus.Emit`, `Bus.EmitAsync` — moves `any`: every handler asserts
the type it hopes for, and a producer that changes its payload breaks its
subscribers at run time, one event at a time. A7 kept both because
`signals.Event` was published and QADR-0010 holds breaking changes for the
major. The major is where the untyped door closes.

The built-in lifecycle names (`PreCreate` … `PostUpdate`) are emitted by
`pkg/model`, which leaves the public API with the same major (DEP-2026-024);
they leave with it and are not replaced by typed topics.
`observability.Bus` is a different thing — the in-process fan-out of
observability events — and does not change.

What changes and when:

- **This release:** `Signal`, the lifecycle constants, `Event`, `Handler`,
  `Bus.On`, `Bus.Emit`, `Bus.EmitAsync` and `Topic.Name` carry
  `// Deprecated:`; `Topic.String()` is added as the way to read a topic's
  name without the `Signal` type. Everything keeps working, and a typed
  publish still reaches untyped subscribers of the same name and the other
  way round.
- **v2.0.0:** the untyped symbols are removed. `Bus.Clear` takes topic names
  as strings. The Redis relay and the outbox bridge, which feed the bus, use
  an internal publish.

## Affected Surfaces

- `signals.Signal`, the constants `PreCreate`, `PostCreate`, `PreSave`,
  `PostSave`, `PreDelete`, `PostDelete`, `PreUpdate`, `PostUpdate`,
  `signals.Event`, `signals.Handler`, `(*Bus).On`, `(*Bus).Emit`,
  `(*Bus).EmitAsync`, `Topic.Name`.
- `(*Bus).Clear(signals ...Signal)`: its parameter type becomes `string` at
  v2.0.0 (announced here; no symbol goes away).
- Known external holder of a `*signals.Bus`: Orbit (`datasource/nucleus`,
  the panel's `SetSignalBus`), which migrates or retires that use in its 1.x
  minor (`M0-O` of the A12 arc).
- Not affected: `NewBus`, `Bus.Dropped`, `ErrHandlerPanic`, the typed API,
  `NewRedisRelay` and `RedisRelay`.

## Migration Path

- Replacement, for an application's own events:

  ```go
  // before
  bus.On("invoice.paid", func(e signals.Event) error {
      p := e.Payload.(InvoicePaid) // asserts, or panics
      return mail(e.Ctx, p)
  })
  _ = bus.Emit(signals.Event{Signal: "invoice.paid", Payload: InvoicePaid{...}, Ctx: ctx})

  // after
  var invoicePaid = signals.Define[InvoicePaid]("invoice.paid")
  signals.Subscribe(bus, invoicePaid, func(ctx context.Context, p InvoicePaid) error {
      return mail(ctx, p)
  })
  _ = signals.Publish(ctx, bus, invoicePaid, InvoicePaid{...})
  ```

- Replacement, for a subscriber to a model's lifecycle: Quark's hooks
  (`BeforeCreate`, `AfterSave`…) or Quark's event bus, on the Quark model
  that replaces the `pkg/model` one (DEP-2026-024).
- Behavior differences: a typed handler receives the context the event was
  published with (the untyped one carried it inside the event); a payload of
  the wrong shape is an error at subscription time, not a panic in the
  handler.
- Required app changes: the subscription and emission sites.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-025-signals-typed-topics.md`
- Detection rule: `grep -rnE '\.(On|Emit|EmitAsync)\(|signals\.(Event|Handler|Signal)\b' --include='*.go'`;
  staticcheck SA1019 on the markers.
- Suggested rewrite: manual — one `Define` per event name, then the
  subscribe/publish pairs.

## Validation

- Compatibility tests updated: `yes` — `TestBus_OnAndEmit`,
  `TestBus_EmitPropagatesError`, `TestBus_EmitAsync`
  (`pkg/signals/signals_test.go`) pin the untyped door in v1;
  `TestTyped_InteropsWithTheUntypedAPI` (`typed_test.go`) pins that the two
  doors reach each other; `TestTyped_StringIsTheTopicName` pins the new
  accessor. The 2.0 PR deletes the first group with the door.
- Release note updated: through the squash commit.
- Rollback plan documented: `yes` — nothing changes in v1.x.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Removal decision date: the `v2.0.0` major.

## Notes

The umbrella guard reads the markers and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 27).
