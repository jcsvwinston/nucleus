---
sidebar_position: 5
title: Events & signals
covers:
  - pkg/signals.NewBus
  - pkg/signals.Bus
  - pkg/signals.Bus.Clear
  - pkg/signals.Bus.Dropped
  - pkg/signals.ErrHandlerPanic
  - pkg/signals.Define
  - pkg/signals.Topic
  - pkg/signals.Topic.String
  - pkg/signals.Subscribe
  - pkg/signals.Publish
  - pkg/signals.PublishAsync
  - pkg/signals.NewRedisRelay
  - pkg/signals.RedisRelay
  - pkg/signals.RedisRelay.ForwardToBus
  - pkg/signals.RedisRelay.Close
  - pkg/signals.RedisRelayConfig
  - pkg/signals.RelayPublish
  - pkg/signals.RelaySubscribe
  - pkg/nucleus.EventBus
  - pkg/nucleus.SQLEvent
  - pkg/nucleus.HTTPEvent
  - pkg/nucleus.Runtime.Observability
  - pkg/nucleus.Runtime.Outbox
config_keys:
  - outbox.enabled
  - outbox.bridges.<n>.config.pattern
---

# Events & signals

Nucleus has four event mechanisms, each built for a different job. Three of
them are in the API freeze — `pkg/signals`, the observability bus
(`pkg/observability`, reached through `nucleus.EventBus`) and `pkg/tasks`.
`pkg/outbox` is marked transitional, so the no-removal guarantee does not
cover it yet.

Only the observability bus is there without you wiring anything: the
application builds it at startup. Signals fire where your own code publishes
them, on a bus you built, and the outbox stays off until
`outbox.enabled: true`. This page exists so you pick the right one
deliberately, instead of reaching for whichever one you met first.

## Which mechanism do I use?

| You want | Use | Delivery |
| --- | --- | --- |
| React to a domain change in-process ("after an invoice is paid, …") | **Signals** (`pkg/signals`) | Synchronous (`Publish`, a handler error aborts the caller) or fire-and-forget (`PublishAsync`, dropped when subscribers fall behind) |
| Watch what the app is doing — SQL statements, HTTP requests — e.g. a live feed | **Observability bus** (`nucleus.EventBus` over `pkg/observability`) | In-process broadcast; slow consumers drop events |
| An integration event that must survive a crash and reach another system | **Transactional outbox** (`pkg/outbox`) | Durable, at-least-once, committed with your transaction |
| Turn an event into background work with retries | **Tasks** (`pkg/tasks`) | Queued (memory, SQL or Asynq/Redis) |

Rules of thumb: if losing the event is acceptable, signals or the bus; if
it is not, the outbox. If the consumer is *your own code reacting to
domain changes*, signals; if it is *telemetry or a feed*, the bus. The
outbox row is not the whole story: the outbox can also act as a
*transport for the bus*, so the same subscribers run — after the commit,
with retries. That combination is [at the end of this page](#the-outbox-as-a-transport-for-the-bus).

## Signals (`pkg/signals`)

An in-process publish/subscribe bus over typed topics. A topic is a name
plus a payload type, so the compiler is what notices a producer and a
subscriber that disagree, instead of a handler asserting the type it hopes
for and finding out at run time, one event at a time.

:::info The untyped door left at v2.0.0 (DEP-2026-025)
`Signal`, `Event{Payload any}`, `Handler`, `Bus.On`, `Bus.Emit`,
`Bus.EmitAsync` and `Topic.Name` are gone; `Define`, `Subscribe`, `Publish`
and `PublishAsync` are the bus. The built-in model lifecycle signals
(`PreCreate` … `PostUpdate`) left with `pkg/model` (DEP-2026-024): a
subscriber to a model's lifecycle uses Quark's hooks on the Quark model. The
migration assistant is `docs/migration_assistants/MA-2026-025-signals-typed-topics.md`
in the repository.
:::

The bus is yours, not the framework's. Nothing in Nucleus constructs one and
there is no `rt.Signals()`: you build it in your bootstrap and hand it to
the code that publishes and subscribes. Code that needs the bus later — a
module's `OnStart`, the outbox bridge at the end of this page — receives it
from whatever constructed it.

```go
import (
    "context"

    "github.com/jcsvwinston/nucleus/pkg/signals"
)

type Invoice struct {
    ID     string `json:"id"`
    Amount int    `json:"amount"`
}

var InvoicePaid = signals.Define[Invoice]("invoice.paid")

bus := signals.NewBus(logger)

signals.Subscribe(bus, InvoicePaid, func(ctx context.Context, inv Invoice) error {
    return receipts.Send(ctx, inv.ID) // receipts is your own package
})

err := signals.Publish(ctx, bus, InvoicePaid, Invoice{ID: "inv-1", Amount: 4200})
```

- `Publish` runs the handlers in registration order, stops at the first
  error and returns it, so a subscriber can refuse the operation that
  published the event — right for transactional orchestration.
- `PublishAsync` launches each handler in its own goroutine and logs errors
  instead of returning them — right for fire-and-forget local reactions.

The handler receives the context the event was published with as its first
argument, so work it starts belongs to the request or job that caused it;
when an event carries no context, the handler gets `context.Background()`.
`Bus.Clear` takes topic names: `InvoicePaid.String()` is the string `Define`
was given.

The typed functions are no-ops on a nil bus, and they do not say so:
`Subscribe` registers nothing, `PublishAsync` returns, and `Publish` returns
`nil` — an event nobody received reads as a success. Since the bus is
application-owned, check it is non-nil where you build it rather than at
each call site.

The bus is keyed by name. Two topics with the same name reach the same
subscribers whatever their type parameter, and the subscriber's side decodes
what arrives — which is how an event that travelled reaches a typed handler.

#### A payload that arrives as generic data

In-process, a typed subscriber receives exactly the value that was
published: the delivery path type-asserts it and hands it over untouched.
An event that *travelled* does not arrive as that value. The Redis relay
decodes its envelope into generic data, so a struct comes back as a
`map[string]any`; the outbox stores the payload as JSON and delivers it as
`[]byte`, published under the message's topic. A typed subscriber should not
have to know which door the event came in by, so the delivery path
converts:

- The payload already being of the topic's type is the direct case — no
  copy, no encoding.
- A `nil` payload becomes the zero value, and the handler runs.
- `[]byte` is JSON-decoded into the topic's type.
- Anything else is re-encoded to JSON and decoded into the topic's type —
  this is what turns a relayed `map[string]any` back into your struct.

A payload that does not decode is an error, wrapped with the topic's name
and returned the way the handler's own error would have been: to the
`Publish` caller on the synchronous path, to the log on the asynchronous
one, and to the outbox dispatcher when the outbox is the transport.

The limit is worth stating plainly: the conversion is a JSON decode, not a
validation. Only what the JSON encoding of the topic's type carries
survives the trip, and a document that decodes cleanly but shares no
fields with that type produces a zero value rather than an error. The
type parameter guarantees that *publishers* and *subscribers* agree at
compile time; across a transport it buys decoding, not verification.

### When a subscriber panics

A handler that panics does not unwind into whatever published the event — a
write, or the request that triggered it. On the synchronous path the
panic is recovered and returned as an error wrapping
`signals.ErrHandlerPanic`, with the stack logged where it happened (the
error cannot carry it):

```go
if err := signals.Publish(ctx, bus, InvoicePaid, inv); err != nil {
    if errors.Is(err, signals.ErrHandlerPanic) {
        // the subscriber is broken — not a business rule refusing the write
    }
    return err
}
```

`ErrHandlerPanic` is a distinct error so the caller can tell "a subscriber
refused this operation" from "a subscriber is broken". A panic aborts the
chain exactly like a returned error: handlers registered after the
panicking one do not run, and the operation that published the event fails
instead of continuing with half its subscribers.

The asynchronous path recovers too, but there is no caller to return to:
`PublishAsync` logs `handler panicked` and moves on, and the other handlers
of the same event — each in its own goroutine — are unaffected. So both delivery paths survive a broken subscriber; only the
synchronous one turns the panic into a value your code can act on.

### Publishing under load

`PublishAsync` does not block the publisher. That matters because the
publisher is usually a write inside a request: if publishing waited for a
free execution slot, the slowest subscriber would become latency for the
user.

Not blocking has to stay bounded, so a bus has two fixed ceilings, neither
of them configurable: at most 64 handler invocations run at once, and at
most 4096 dispatches exist at all — running, or waiting for an execution
slot. A burst therefore queues rather than being lost. Past the in-flight
ceiling a dispatch is **dropped**: counted, and logged at error level with
the topic and the running total.

The unit is a dispatch, not an event. `PublishAsync` admits each subscriber
separately, so an event with three handlers takes three slots and can
reach some of them and be dropped for the rest.

That error log is the only thing that reports a drop as it happens, and it
depends on the bus having a logger: `signals.NewBus(nil)` is legal and
drops silently — the same nil logger also swallows the asynchronous handler
errors and the panic records described above. Nothing turns drops into a
metric or a health check either, so read the counter as well — export it,
or check it where you already report health:

```go
if n := bus.Dropped(); n > 0 {
    slog.Warn("signal dispatches dropped", "total", n)
}
```

`Dropped` returns the cumulative count for the bus since it was created.
Events are dropped, not queued indefinitely, and that is the deliberate
trade: the asynchronous contract already says errors do not propagate, so
dropping is the failure that matches it, while blocking the publisher would
turn a subscriber's problem into the user's. If losing the event is not
acceptable, the asynchronous path is the wrong one — use `Publish`, which
returns what the handlers returned, or the outbox.

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

// Publish to the other replicas:
err = signals.RelayPublish(ctx, relay, InvoicePaid, Invoice{ID: "inv-1", Amount: 4200})

// Subscribe remotely, typed, or forward remote events into the local bus,
// where the typed subscribers of that name decode them:
go func() {
    if err := relay.ForwardToBus(context.Background(), InvoicePaid.String(), bus); err != nil {
        logger.Error("signal forwarder stopped", "error", err)
    }
}()
```

Each topic maps to one Redis channel. `request_id`, `user_id` and
`trace_id` ride along and are put back on the context the receiving handler
gets — they are read from the context you pass to `RelayPublish`, so
publishing through the relay with a bare `context.Background()` sends an
envelope with no correlation at all. Payloads cross as JSON and arrive as
generic decoded data rather than as the value that was published, so a typed
subscriber — `RelaySubscribe`, or a `Subscribe` on the bus `ForwardToBus`
feeds — goes through the re-encode case in [A payload that arrives as
generic data](#a-payload-that-arrives-as-generic-data).

The relay is deliberately small: no durable delivery, no wildcard
subscriptions, no broker abstraction. `ForwardToBus` and `RelaySubscribe`
return — and stop receiving — at the first handler that fails and at the
first message that does not decode; it returns that error rather than skipping the message and
continuing. That is why the example above logs the error instead of
discarding it: a forwarder is something you supervise and restart, not
something you start and forget. The moment you need delivery
guarantees you have left signals territory — use the
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

`rt.Observability()` is non-nil for an application the framework built —
`app.New` constructs the bus unconditionally — and returns nil only on a
runtime with no application behind it. That is why this example calls it
straight, while the `rt.Outbox()` example further down checks for nil: the
outbox is absent whenever it is disabled.

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
should enqueue a task or an outbox entry rather than doing the work inline
— the signal is the in-process hook, the queue is the durability.

### The outbox as a transport for the bus

The names in this section are `pkg/outbox`'s, transitional as the
introduction says: they are what ships today.

Without the bus bridge, an outbox topic and a signal of the same name are
unrelated, which makes the choice between them a choice of *transport*
rather than of *subscription*: pick the outbox and your in-process
listeners never hear the event; pick the bus and the handlers run when you
emit — before the commit, and just as surely for a transaction that then
rolls back — with nothing left to deliver if the process dies first. The bus bridge removes that choice. It is an
outbox bridge that delivers a committed message onto the bus, so an event
enqueued inside a transaction reaches the same handlers as one published
directly.

Register it in Go and route topics to it — configuration builds webhook
bridges only. A module's `OnStart` is the place: the dispatcher starts
after every module's `OnStart` has run, so no message is leased before its
route exists. The `bus` below is the one your bootstrap
built and handed to the module — the runtime does not carry a bus, so it
cannot supply one here.

```go
import (
    "context"
    "errors"

    "github.com/jcsvwinston/nucleus/pkg/nucleus"
    "github.com/jcsvwinston/nucleus/pkg/outbox"
)

OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
    ob := rt.Outbox()
    if ob == nil {
        return errors.New("this module needs outbox.enabled: true")
    }
    // bus is the *signals.Bus this module was constructed with.
    bridge, err := outbox.NewBusBridge("bus", bus)
    if err != nil {
        return err
    }
    if err := ob.RegisterBridge(bridge); err != nil {
        return err
    }
    ob.AddRoute("invoice.*", "bus")
    return nil
},
```

Publishing then means enqueueing in the transaction that makes the event
true:

```go
_, err := ob.EnqueueTx(ctx, tx, outbox.Entry{
    Topic:   InvoicePaid.String(),
    Payload: Invoice{ID: "inv-1", Amount: 4200},
})
```

Four things change about delivery when the outbox carries the event:

- **The handlers run after the commit, on the dispatcher.** Not in the
  request, and not in the publishing goroutine. An event whose transaction
  rolls back is never delivered; one whose transaction commits is
  delivered even if the process dies immediately afterwards.
- **A handler's error fails the delivery.** The bridge emits
  synchronously on purpose: the dispatcher decides what a failed delivery
  means — retry with backoff, and the dead letter when the attempts run
  out — and it can only do that if the error reaches it. An asynchronous
  emit would report success for work nobody did.
- **Delivery is at-least-once, and retries re-run the whole chain.** A
  message whose third subscriber fails is retried with the first two
  having already run, so handlers on a bridged topic must tolerate being
  called twice. What is retried is the message, across every bridge its
  topic matched — not this bridge alone: the dispatcher sends to all
  matching bridges and fails the message if any of them returns an error.
  A webhook bridge declared in configuration without a `pattern` is routed
  as `*`, which matches every topic, so an unreachable endpoint re-runs
  your bus handlers on each attempt. Give such a bridge an explicit
  pattern if you do not want that.
- **The payload arrives as the JSON the outbox stored.** The bridge
  publishes the stored bytes under the message's topic, and each typed
  subscriber of that name decodes them into its own type. The context the
  handler receives is the dispatcher's, so request-scoped values are not in
  it unless you put them in the payload.

A subscriber that stays broken dead-letters its messages rather than losing
them, and there are two commands for that end state: `nucleus doctor --check
outbox` reports how many messages are in the `failed` state (a count for the
whole table, not per topic), and `nucleus outbox requeue` returns failed
messages to `pending` with their retry budget reset once the handler is
fixed.

When you would want this: an event that your own in-process code consumes,
but whose loss between "the row is committed" and "the subscribers ran" is
not acceptable. When you would not: a purely local reaction where a lost
event is a lost log line, which is what the bus alone is for — the bridge
buys durability at the cost of a database round trip per event and
handlers that must be idempotent.
