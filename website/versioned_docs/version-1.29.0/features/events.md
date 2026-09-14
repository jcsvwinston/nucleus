---
sidebar_position: 5
title: Events & signals
covers:
  - pkg/signals.NewBus
  - pkg/signals.Bus
  - pkg/signals.Event
  - pkg/signals.NewRedisRelay
  - pkg/signals.RedisRelay
  - pkg/signals.RedisRelayConfig
  - pkg/nucleus.EventBus
  - pkg/nucleus.SQLEvent
  - pkg/nucleus.HTTPEvent
  - pkg/nucleus.Runtime.Observability
config_keys: []
---

# Events & signals

Nucleus has four event mechanisms, each built for a different job. They are
all stable and wired by default — this page exists so you pick the right
one instead of discovering three of them by accident.

## Which mechanism do I use?

| You want | Use | Delivery |
| --- | --- | --- |
| React to a domain change in-process ("after an Article is created, …") | **Model signals** (`pkg/signals`) | Synchronous (`Emit`) or fire-and-forget (`EmitAsync`), same process |
| Watch what the app is doing — SQL statements, HTTP requests — e.g. a live feed | **Observability bus** (`nucleus.EventBus` over `pkg/observability`) | In-process broadcast; slow consumers drop events |
| An integration event that must survive a crash and reach another system | **Transactional outbox** (`pkg/outbox`) | Durable, at-least-once, committed with your transaction |
| Turn an event into background work with retries | **Tasks** (`pkg/tasks`) | Queued (memory or Asynq/Redis) |

Rules of thumb: if losing the event is acceptable, signals or the bus; if
it is not, the outbox. If the consumer is *your own code reacting to
domain changes*, signals; if it is *telemetry or a feed*, the bus.

## Model signals (`pkg/signals`)

An in-process publish/subscribe bus, integrated with the model layer: when
a CRUD operator is built with a bus, every create/update/delete emits
`PreCreate`/`PostCreate`, `PreUpdate`/`PostUpdate`,
`PreDelete`/`PostDelete` — the Django-style signal model, explicit and
in-process.

```go
import "github.com/jcsvwinston/nucleus/pkg/signals"

bus := signals.NewBus(logger)

bus.On(signals.PostCreate, func(event signals.Event) error {
    log.Printf("post-create for %s => %#v", event.ModelName, event.Payload)
    return nil
})

err := bus.Emit(signals.Event{
    Signal:    signals.PostCreate,
    ModelName: "Article",
    Payload:   map[string]any{"id": 42, "title": "Hello"},
    Ctx:       r.Context(),
})
```

- `Emit` runs handlers in registration order, stops at the first error,
  and propagates it — right for transactional orchestration.
- `EmitAsync` launches each handler in its own goroutine and logs errors
  instead of returning them — right for fire-and-forget local reactions.

### Crossing a process boundary

When an event genuinely needs to leave the process and Redis is already
around, the relay is the explicit opt-in:

```go
relay, err := signals.NewRedisRelay(signals.RedisRelayConfig{
    RedisURL: "redis://127.0.0.1:6379/0",
}, logger)
if err != nil {
    return err
}
defer relay.Close()

// Publish, subscribe remotely, or forward remote events into the local bus:
go func() { _ = relay.ForwardToBus(context.Background(), signals.PostCreate, bus) }()
```

Each signal maps to one Redis channel; `request_id`, `user_id` and
`trace_id` from the event context ride along. The relay is deliberately
small: no durable delivery, no wildcard subscriptions, no broker
abstraction. The moment you need delivery guarantees you have left
signals territory — use the
[outbox](./storage-and-tasks.md#transactional-outbox-pkgoutbox).

## The observability bus (`nucleus.EventBus`)

The framework instruments itself: HTTP requests, SQL statements and
session events flow over an in-process bus (`pkg/observability`). Modules
consume it through the stable `nucleus.EventBus` facade — the same surface
orbit's live SQL/HTTP view is built on — via `rt.Observability()`:

```go
OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
    bus := rt.Observability()

    events, cancel := bus.SubscribeSQL()
    go func() {
        defer cancel()
        for ev := range events {
            // ev is a detached copy you own: Query, Operation, ModelName,
            // RequestID/TraceID/UserID for correlation.
            log.Printf("sql: %s %s", ev.Operation, ev.Query)
        }
    }()
    return nil
},
```

Three properties to design around:

- **Each `Subscribe*` returns a channel and a cancel func** — call cancel
  when done; the channel closes after it.
- **A slow consumer drops events** rather than blocking producers. This is
  a feed, not a queue.
- **`EmitSQL` is the ingest side**: an external producer that runs SQL
  outside the framework's own layer (an ORM bridge, for instance) can
  surface its statements in the same feed. Bound arguments are expected to
  be sanitized by the producer. This is exactly how
  [Quark's statements reach orbit's live view](./using-quark.md#optional-bridges-orbit).

The bus carries telemetry, and its delivery reflects that. Do not use it
for domain logic ("when an order row is inserted, send the email") — that
is signals or the outbox, depending on whether losing the event is
acceptable.

## Durable events: the outbox

For integration events that must not be lost, `pkg/outbox` writes the
event in the **same SQL transaction** as your domain change and delivers
it later through configured bridges — at-least-once, with retries and a
dead-letter state. Full treatment:
[Storage & background tasks → Transactional outbox](./storage-and-tasks.md#transactional-outbox-pkgoutbox).

A useful composition: a signal handler that must trigger durable work
should enqueue a task or an outbox entry rather than doing the work
inline — the signal is the in-process hook, the queue is the durability.
