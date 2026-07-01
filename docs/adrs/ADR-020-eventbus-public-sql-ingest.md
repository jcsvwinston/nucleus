# ADR-020: Public SQL Ingest on the EventBus Runtime Surface

Reference date: 2026-07-01.
Status: Accepted.
Related: [ADR-018](ADR-018-admin-observability-bus-migration.md) (the live view
consumes the observability bus), [ADR-019](ADR-019-extract-admin-to-orbit-module.md)
(the `Runtime`/`EventBus` surface orbit mounts through; sets the home for
finding **#31**, live SQL coverage), [ADR-010](ADR-010-fluent-api-v2-pkg-nucleus.md)
(the `pkg/nucleus` first-party surface). Suite context:
[QADR-0006](../../../docs/adr/QADR-0006-integracion-quark-orbit.md) (Quark↔Orbit
integration) names this ingest as its blocking prerequisite, and
[QADR-0005](../../../docs/adr/QADR-0005-secuenciacion-convergencia.md) puts any
`Runtime` surface change inside the v1.0 gate.

## Context

`pkg/nucleus.EventBus` is the first-party, stable view of the framework's
in-process observability bus, returned by `Runtime.Observability()`. Until now it
exposed **subscribe only** — `SubscribeSQL()` and `SubscribeHTTP()` — because its
sole consumer (orbit's live view) is a reader: it drains `SubscribeSQL()` into the
live SQL feed (`orbit/internal/admin/live_eventbus.go`).

The underlying bus already has an emit side: `observability.Bus.Emit(Event)`
(`pkg/observability/bus.go`) publishes to every matching subscriber, and the
internal `SQLStatementEvent` (`KindSQLStatement`) is the value SQL producers push.
But `observability` is classified experimental and never appears on the public
surface — so an **external producer** had no supported way to put a SQL statement
onto the feed. The capability existed; it was simply not destapped.

That gap blocks the Quark↔Orbit integration (QADR-0006). The plan there is "don't
teach Orbit about Quark; teach the bus": a ctx-aware `quark.Middleware`, living in
an opt-in `orbit/quarkbridge` module, maps each executed statement to a SQL event
and emits it onto Nucleus's bus, so it shows up in the live feed with no change to
Orbit. That bridge needs a public emit entry point on the `Runtime` surface — one
that does **not** force it to import the experimental `pkg/observability` package
or take on the bus's pooled-event `Release` discipline.

## Decision

Add a single method to the `EventBus` interface (and its unexported `busAdapter`):

```go
EmitSQL(ev SQLEvent)
```

- It is the **emit counterpart to `SubscribeSQL`**, and the mirror of the existing
  `toSQLEvent` translation: a new `fromSQLEvent` helper builds a pooled
  `*observability.SQLStatementEvent` (via `AcquireSQLStatementEvent`) from the
  first-party `SQLEvent` value and hands it to `Bus.Emit`. `toSQLEvent` copies the
  bus's backing array **out** on the read side; `fromSQLEvent` copies the
  producer's `Args` **in** on the write side, so the caller keeps ownership of its
  slice and there is no aliasing in either direction.
- The adapter owns the bus's `Release` discipline internally (`Bus.Emit` takes
  ownership of the pooled event and releases it), exactly as `subscribeTyped` does
  on the read side. The external producer works only with plain `SQLEvent` values.
- The producer sets `EmittedAt` (typically the query completion time) and, when
  correlation is wanted, `RequestID`/`TraceID`/`UserID`. Args are copied verbatim:
  the ingest does **not** redact on emit, so a producer that carries sensitive
  arguments must sanitize them first (the same emit-time contract the framework's
  own SQL hook honours; see `SQLEvent.Args`).

No new configuration, no new package, no dependency: this reuses machinery that
already shipped.

## Consequences

- **`pkg/nucleus` gains one exported interface method.** Adding a method to
  `EventBus` is **additive for consumers** — orbit and any other caller *receive*
  the interface from `Runtime.Observability()`; they do not implement it (the only
  implementation, `busAdapter`, is unexported). So no existing caller breaks. The
  contract-freeze baseline (`contracts/baseline/api_exported_symbols.txt`) is
  updated to record `EventBus.EmitSQL`; the freeze test only flags **removals**, so
  the addition is clean, and the new symbol is now a frozen stable member.
- **Runtime surface → v1.0 gate.** Per QADR-0005 this is a `Runtime`-surface
  change and lands inside the v1.0 convergence gate. It is small, additive, and
  reversible.
- **Unblocks QADR-0006 Case 1** (real-time SQL feed) without any change to Orbit's
  drain path or to Quark's core: the bridge lives in `orbit/quarkbridge`, imports
  the public `EmitSQL`, and never touches `pkg/observability`.
- **No behaviour change for existing feeds.** The framework's own SQL hook still
  emits on the internal bus as before; `EmitSQL` is a second, additive producer on
  the same feed. Events from both sources are indistinguishable to subscribers
  except by their fields (e.g. `NodeID`, `ModelName`).
- No migration, no config key, no `examples/*` change (the ingest is plumbing for
  an external bridge, not a module-author-visible default).

## Alternatives considered

- **Expose an accessor to the raw `*observability.Bus`** (as QADR-0006 floated as
  an option). Rejected: it would leak the experimental type onto the stable
  surface, defeating the whole point of `EventBus` (insulating consumers from
  pre-v1.0 churn and from the pooled-event `Release` discipline), and it would trip
  the firewall test that keeps third-party/experimental types off stable APIs. A
  narrow `EmitSQL(SQLEvent)` keeps the surface first-party and value-typed.
- **Redact `Args` inside `EmitSQL`.** Rejected: the bus's contract is that events
  are pre-sanitized at the producer, and the producer (the Quark bridge) already
  owns Quark's `RedactionMode` and applies it with full type context. Re-redacting
  in the adapter would be redundant, lossy, and would diverge from how the
  framework's own hook emits.
- **Emit `HTTP`/custom kinds too, for symmetry.** Deferred: there is no consumer
  need yet. `SubscribeSQL`/`EmitSQL` is the pair QADR-0006 requires; adding the
  others speculatively would freeze surface no one uses. They can be added the same
  way if a producer appears.
