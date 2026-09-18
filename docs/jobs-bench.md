# Jobs bench — what background work, events and real time can do today

This is the numerator of the A7 gate ("jobs, events and real time"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-09-18 against v1.29.0, and kept current as the arc closes
its gaps: the numbers below are what the suite produced on its last run.** Run
it with:

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

**26 of 40 controls present. 2 partial. 12 absent.**

| family | present | partial | absent | of |
|---|---|---|---|---|
| queue | 13 | 0 | 0 | 13 |
| events | 10 | 0 | 2 | 12 |
| realtime | 1 | 2 | 5 | 8 |
| ops | 2 | 0 | 5 | 7 |
| **total** | **26** | **2** | **12** | **40** |

The queue family moved from 4 to 11 across the arc's first two working
sessions: S1 closed the three ways the in-process provider used to lose an
accepted job while the process was still running (JOB-07, JOB-08, JOB-10), and
S2 added the durable provider (JOB-02, JOB-03, JOB-04, JOB-06).

Four of those controls are measured against the SQL provider rather than the
default one, and the cases say so. The criterion is the one the auth and admin
benches already use: a capability an application can have by configuration — an
opt-in the framework ships — is one it HAS. What the DEFAULT provider does is
still measured, by JOB-01, JOB-05 and JOB-07 through JOB-10.

## What the shape of it says

**The queue is in-process, and since S1 it does not throw work away.** A job
runs with nothing but the application (JOB-01), is retried with a growing wait
(JOB-05), can be scheduled for later (JOB-11) and bounded by its own deadline
(JOB-12). A job whose type no worker handles is now held until that handler
registers (JOB-10), a job that exhausts its retries is kept with its last
error (JOB-07), and held jobs can be put back (JOB-08) — measured by watching
the job RUN again, not by an action that returns without an error.

**And since S2 there is a durable queue on the database the application already
has.** A job accepted before a restart runs after it (JOB-02), the provider
exists and needs no broker (JOB-03), queues are named and served in the order
they are configured (JOB-04), and the retry curve travels with the job
(JOB-06). **S3 made the queue visible**: the in-process provider's snapshot
reports what is actually pending and running instead of hard zeros (JOB-09),
and every provider records the same `jobs.*` metrics (OPS-05) — they used to
live inside the asynq provider, so an application without a Redis had nothing
to alert on. What the queue family still lacks is deduplication (JOB-13).

**The bus stopped hurting its emitters in S5.** A panicking subscriber is now
an error rather than an unwind through the model save that emitted (EVT-03),
and emitting asynchronously returns to the caller instead of waiting for the
slowest subscriber (EVT-12) — with a written policy for what a full bus does,
which is drop and say so.

**S6 made it one bus with two doors.** A signal reaches an in-process handler
(EVT-01), a relay carries one to another replica (EVT-05), the outbox enqueues
inside the caller's transaction (EVT-06), retries a failed delivery keeping the
reason (EVT-08) and can be requeued (EVT-09) — and now a message written to the
outbox **reaches the same handlers** as one published directly (EVT-07), so an
application subscribes instead of choosing a transport. Payloads are typed
(EVT-02) through an API delivered alongside the untyped one, because `Event`
and `Handler` are published and QADR-0010 holds breaking changes until the
major at the close of A12. `pkg/observability` stays a separate path on
purpose: it carries SQL and HTTP traces, not application events.

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
