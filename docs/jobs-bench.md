# Jobs bench — what background work, events and real time can do today

This is the numerator of the A7 gate ("jobs, events and real time"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-09-18 against v1.29.0.** Run it with:

```bash
go test ./internal/jobsbench/ -run TestJobsBench -v
go test ./internal/jobsbench/ -run TestJobsBenchSummary -v   # the table below
```

The bench is not prose. Every control is a Go probe in `internal/jobsbench/`
that boots an application and asks the route, drives the public API of
`pkg/tasks`, `pkg/signals` or `pkg/outbox` and checks the answer, or asks the
configuration layer for a key and reads the refusal it gets back.
`TestJobsBench` asserts the **recorded verdict** rather than success, so
closing a gap turns the suite red with "this one is present now, update the
verdict" — which is what keeps this page honest.

There was already a capability table for this: the maturity audit of
2026-09-03 scored Nucleus jobs/events at 3 of 5 by reading the code. This
bench replaces that reading with measurement.

## The verdicts

| verdict | meaning |
|---|---|
| **present** | the control exists and its probe exercised it end to end |
| **partial** | a piece exists; the case records exactly what is missing |
| **absent** | no surface at all — the probe measures the absence, never the lack of a grep hit |

A control that cannot be probed does not belong in the bench.

## The result

**12 of 40 controls present. 4 partial. 24 absent.**

| family | present | partial | absent | of |
|---|---|---|---|---|
| queue | 4 | 2 | 7 | 13 |
| events | 6 | 0 | 6 | 12 |
| realtime | 1 | 2 | 5 | 8 |
| ops | 1 | 0 | 6 | 7 |
| **total** | **12** | **4** | **24** | **40** |

## What the shape of it says

**The queue is in-process.** A job runs with nothing but the application
(JOB-01), is retried with a growing wait (JOB-05), can be scheduled for later
(JOB-11) and bounded by its own deadline (JOB-12). Everything an operator
needs *after* that is missing: a job accepted before a restart does not run
after it (JOB-02), there is no SQL-backed provider to make it durable
(JOB-03), no named queues (JOB-04), no dead letter queue (JOB-07), nothing to
requeue from (JOB-08), and a job whose type no worker handles is counted and
dropped (JOB-10). Durability today means running asynq, which means operating
a Redis — which is exactly the dependency Solid Queue and Oban removed.

**There are three event paths, and no bus.** A signal reaches an in-process
handler (EVT-01), a relay carries one to another replica (EVT-05), the outbox
enqueues inside the caller's transaction (EVT-06), retries a failed delivery
keeping the reason (EVT-08) and can be requeued (EVT-09). But an outbox topic
and a signal of the same name are unrelated (EVT-07): `pkg/outbox` delivers to
bridges, `pkg/signals` to handlers, and `pkg/observability` is a third path
with its own shape. An application picks a transport, not a subscription.

**Real time is the application's problem.** An SSE stream works and survives
the middleware stack (RT-02, RT-03) — every line of it hand-written. A
WebSocket route can hijack the connection (RT-01), and then owes itself the
handshake, the framing and the ping/pong. There is no channel: nothing
broadcasts to a topic (RT-04), nothing authorises a join (RT-05), nothing
knows who is connected (RT-06), and nothing carries a message to the sockets
another replica holds (RT-07).

**Operationally, background work is invisible.** `/healthz` answers per
dependency (OPS-03) and that is the whole of it: no `/livez` and no `/readyz`
to tell "restart me" apart from "don't route to me yet" (OPS-01, OPS-02), no
profiler (OPS-04), and — measured with a real meter provider and a manual
reader while a job ran — **zero series** (OPS-05). The seven `jobs.*`
instruments exist inside the asynq provider only.

## Two defects this bench measured deterministically

**NU-77** was found by a red CI on Linux and does not reproduce on macOS. Its
two causes do, every time:

- OPS-06 reads `PRAGMA busy_timeout` off the connection the framework hands
  out and gets **0 ms**: the sqlite DSN is passed through bare, so a second
  writer fails instead of waiting.
- OPS-07 asks, from inside a module's `OnStart`, whether the outbox table
  exists yet — and it does. The dispatcher reached the database before any
  module could migrate. Those are the two writers.

A flake in CI is now two assertions that hold on any machine.

## What this bench learned about measuring

Four probes were rewritten before this page was published, because their first
version measured less than their title claimed — the AUD-05 mistake of the
admin bench, repeated in four new shapes:

- **the metrics probe** asked `/metrics` of a booted application and read a
  404. That measures the Prometheus exporter being an opt-in module this
  package does not import, not whether jobs are instrumented. It now installs
  a meter provider with a manual reader and collects.
- **the retry probe** enqueued a message and observed it pending. Pending is
  where a message starts; nothing about a failed delivery had been measured.
  It now runs a dispatcher pass against a handler that fails, and checks the
  attempt counter and the stored reason.
- **the relay probe** built a relay with an empty config and called the error
  it got back a capability. It now runs two buses and two relays over a
  miniredis and watches an event cross.
- **the boot-order probe** asserted that the runtime exposes an outbox, and
  logged a sentence about ordering it had not measured. It now takes the
  measurement from inside a module's `OnStart`.

One more shape worth remembering: `signals.RedisRelay.ForwardToBus` **blocks**
— it is the receive loop, not a subscription that returns one — so a probe
that called it inline hung the whole suite for four minutes before the test
timeout printed the stack.
