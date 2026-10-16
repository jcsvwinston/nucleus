# Migration Assistant: the untyped door of `pkg/signals` → `Define` / `Subscribe` / `Publish`

- ID: `MA-2026-025`
- Pairs with: `docs/deprecations/DEP-2026-025-signals-untyped-door.md`
- Severity: `medium` (a type assertion that used to happen in the handler
  moves to the topic's declaration; a handler that relied on receiving every
  payload type under one name needs one topic per type).
- Status: `current`

---

## Scope

Code that calls `Bus.On`, `Bus.Emit`, `Bus.EmitAsync`, builds a
`signals.Event`, declares a `signals.Handler`, names a `signals.Signal` or the
lifecycle constants, or calls `Topic.Name()`.

Out of scope: `Define`, `Subscribe`, `Publish`, `PublishAsync`, `NewBus`,
`Dropped`, the Redis relay.

## Detection

```bash
grep -rnE '\.(On|Emit|EmitAsync)\(|signals\.(Event|Handler|Signal|Pre[A-Z]\w+|Post[A-Z]\w+)\b|\.Name\(\)' --include='*.go' .
staticcheck ./...   # SA1019 on the markers
```

## Rewrite

| Before | After | Kind |
|---|---|---|
| `bus.On("x", func(e signals.Event) error { p := e.Payload.(P); … })` | `var x = signals.Define[P]("x")` once; `signals.Subscribe(bus, x, func(ctx context.Context, p P) error { … })` | manual |
| `bus.Emit(signals.Event{Signal: "x", Payload: p, Ctx: ctx})` | `signals.Publish(ctx, bus, x, p)` | manual |
| `bus.EmitAsync(…)` | `signals.PublishAsync(ctx, bus, x, p)` | manual |
| `topic.Name()` | `topic.String()` | automatic |
| `bus.On(signals.PostSave, …)` on a `pkg/model` model | Quark hooks on the Quark model (MA-2026-024) | manual |
| `bus.Clear(signals.Signal("x"))` | `bus.Clear("x")` at v2.0.0 (the parameter type changes to `string`) | automatic |

## Verification

```bash
go build ./... && go vet ./... && go test ./...
grep -rnE '\.(On|Emit|EmitAsync)\(' --include='*.go' . | grep -v _test.go | wc -l   # 0
```

## Rollback

Both doors work until v2.0.0 and reach each other; a half-migrated code base
keeps working.
