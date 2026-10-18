# Signals & Event Bus Guide

Reference date: 2026-10-18.
Status: Current.


> **Where the living narrative is.** The user-facing events documentation —
> signals, the observability bus, and the which-mechanism decision table —
> is the public site (`website/docs/features/events.md`). This guide stays
> for internal depth; when the two disagree, the site wins.

This guide covers Nucleus's signal model:

- `pkg/signals.Bus` for in-process publish/subscribe over typed topics
- `pkg/signals.RedisRelay` for small Redis-backed cross-process forwarding

## Overview

Nucleus signals are intentionally small and explicit.

Use them for:

- domain events inside one process
- light cross-process forwarding when Redis is already available

The bus is yours: nothing in the framework constructs one, and the typed API
is the only door. The untyped one of v1 — `Signal`, `Event{Payload any}`,
`Handler`, `Bus.On`, `Bus.Emit`, `Bus.EmitAsync` — left at v2.0.0
(DEP-2026-025), and with it the model lifecycle names `pkg/model` emitted
(`PreCreate` … `PostUpdate`, DEP-2026-024). A subscriber to a model's
lifecycle uses Quark's hooks on the Quark model.

## In-process bus

A topic is a name plus a payload type, so the compiler is what notices a
producer and a subscriber that disagree:

```go
type InvoicePaid struct {
    ID     string `json:"id"`
    Amount int    `json:"amount"`
}

var invoicePaid = signals.Define[InvoicePaid]("invoice.paid")

bus := signals.NewBus(logger)

signals.Subscribe(bus, invoicePaid, func(ctx context.Context, p InvoicePaid) error {
    return receipts.Send(ctx, p.ID)
})

err := signals.Publish(r.Context(), bus, invoicePaid, InvoicePaid{ID: "inv-1", Amount: 4200})
```

`signals.Publish(...)`:

- runs handlers in registration order
- stops at the first returned error and returns it
- turns a panicking handler into an error wrapping `ErrHandlerPanic`
- is best for transactional or in-process orchestration

`signals.PublishAsync(...)`:

- launches each handler in its own goroutine
- logs errors instead of returning them
- never blocks the publisher: past the in-flight limit a dispatch is dropped,
  counted (`Bus.Dropped`) and logged
- is best for non-blocking local reactions

The handler receives the context the event was published with, so work it
starts belongs to the request or the job that caused it. `Bus.Clear` takes
topic names — `invoicePaid.String()`.

## Distributed relay

When an event needs to cross process boundaries, use `RedisRelay` explicitly:

```go
relay, err := signals.NewRedisRelay(signals.RedisRelayConfig{
    RedisURL: "redis://127.0.0.1:6379/0",
}, logger)
if err != nil {
    return err
}
defer relay.Close()

err = signals.RelayPublish(r.Context(), relay, invoicePaid, InvoicePaid{ID: "inv-1"})
```

Each topic is published to one Redis channel derived from its name. The
relay preserves `request_id`, `user_id`, and `trace_id` from the publishing
context and puts them back on the receiving handler's context.

## Subscribe and forward

Subscribe directly, typed:

```go
go func() {
    _ = signals.RelaySubscribe(context.Background(), relay, invoicePaid, func(ctx context.Context, p InvoicePaid) error {
        log.Printf("remote event: %s", p.ID)
        return nil
    })
}()
```

Or forward remote events into the local bus, where the typed subscribers of
that name decode them:

```go
go func() {
    _ = relay.ForwardToBus(context.Background(), invoicePaid.String(), bus)
}()
```

This keeps one local handler model while still allowing distributed delivery.
Both are receive loops: they return when the context ends, at the first
message that does not decode, and at the first handler error.

## Current guarantees

- in-process handlers run in registration order
- `Publish(...)` propagates errors, panics included
- `PublishAsync(...)` does not propagate errors and does not block
- a payload that travelled (the relay, or the outbox's bus bridge) arrives
  as JSON and is decoded into the subscriber's topic type
- Redis relay is explicit and JSON-based
- remote events can be re-published into the local bus

## Not in scope

- wildcard subscriptions
- durable delivery guarantees
- broker abstraction over multiple backends
- outbox semantics (the outbox is a transport of the bus, `outbox.NewBusBridge`)
- automatic wiring from every topic to Redis

## Practical guidance

Prefer:

- `signals.Publish(...)` for request-scoped or transactional work
- `signals.PublishAsync(...)` for fire-and-forget local work
- `tasks.EnqueueJSONCtxWithPolicy(...)` when an event should become a durable background job
- `RedisRelay` only when an event truly needs to cross process boundaries
