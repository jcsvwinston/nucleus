# Signals & Event Bus Guide

Reference date: 2026-04-23.
Status: Current.


> **Where the living narrative is.** The user-facing events documentation —
> signals, the observability bus, and the which-mechanism decision table —
> is the public site (`website/docs/features/events.md`). This guide stays
> for internal depth; when the two disagree, the site wins.

This guide covers Nucleus's current signal model:

- `pkg/signals.Bus` for in-process publish/subscribe
- `pkg/signals.RedisRelay` for small Redis-backed cross-process forwarding

## Overview

Nucleus signals are intentionally small and explicit.

Use them for:

- model lifecycle hooks
- domain events inside one process
- light cross-process forwarding when Redis is already available

The in-process bus is synchronous by default. Remote forwarding is opt-in.

## In-process bus

```go
bus := signals.NewBus(logger)

bus.On(signals.PostCreate, func(event signals.Event) error {
    payload, _ := event.Payload.(map[string]any)
    log.Printf("post-create for %s => %#v", event.ModelName, payload)
    return nil
})

err := bus.Emit(signals.Event{
    Signal:    signals.PostCreate,
    ModelName: "Article",
    Payload: map[string]any{
        "id":    42,
        "title": "Hello",
    },
    Ctx: r.Context(),
})
```

`Bus.Emit(...)`:

- runs handlers in registration order
- stops at the first returned error
- is best for transactional or in-process orchestration

`Bus.EmitAsync(...)`:

- launches each handler in its own goroutine
- logs errors instead of returning them
- is best for non-blocking local reactions

## Model integration

`pkg/model.CRUD` already accepts a `*signals.Bus`.

When a bus is configured, CRUD operations emit:

- `signals.PreCreate` / `signals.PostCreate`
- `signals.PreUpdate` / `signals.PostUpdate`
- `signals.PreDelete` / `signals.PostDelete`

This keeps the Django-style signal model in-process and explicit.

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

err = relay.Publish(r.Context(), signals.Event{
    Signal:    signals.PostCreate,
    ModelName: "Article",
    Payload: map[string]any{
        "id": 42,
    },
})
```

Each signal is published to one Redis channel derived from the signal name.
The relay preserves `request_id`, `user_id`, and `trace_id` from the event context.

## Subscribe and forward

Subscribe directly:

```go
go func() {
    _ = relay.Subscribe(context.Background(), signals.PostCreate, func(event signals.Event) error {
        log.Printf("remote event: %s %s", event.Signal, event.ModelName)
        return nil
    })
}()
```

Or forward remote events back into the local bus:

```go
go func() {
    _ = relay.ForwardToBus(context.Background(), signals.PostCreate, bus)
}()
```

This keeps one local handler model while still allowing distributed delivery.

## Current guarantees

- in-process handlers run in registration order
- `Emit(...)` propagates errors
- `EmitAsync(...)` does not propagate errors
- Redis relay is explicit and JSON-based
- remote events can be re-emitted into the local bus

## Not in scope

- wildcard subscriptions
- durable delivery guarantees
- broker abstraction over multiple backends
- outbox semantics
- automatic wiring from every signal to Redis

## Practical guidance

Prefer:

- `Bus.Emit(...)` for request-scoped or transactional work
- `Bus.EmitAsync(...)` for fire-and-forget local work
- `tasks.EnqueueJSONCtxWithPolicy(...)` when a signal should become a durable background job
- `RedisRelay` only when an event truly needs to cross process boundaries
