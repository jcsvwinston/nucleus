# ADR-021: Driver-level SQL instrumentation for the live feed

Reference date: 2026-07-13.
Status: Accepted.
Related: [ADR-018](ADR-018-admin-observability-bus-migration.md) (the live view
consumes the observability bus; its Consequences section flagged this exact
follow-up), [ADR-020](ADR-020-eventbus-public-sql-ingest.md) (public SQL ingest
on the `Runtime` surface), [ADR-007](ADR-007-log-redaction.md) (redaction
discipline). Closes v1.0-gate waiver **W2** (`docs/V1_GATE.md` §B), which
committed this improvement and had come due at v1.2.

## Context

The observability bus feeds the live SQL view (orbit's, and any bus
subscriber's) from a single point: the process-wide `model.SQLQueryObserver`
that `model.CRUD` fires for every statement it runs (ADR-018). That covers all
ORM traffic — enriched with the model name CRUD knows — but it is blind to
statements that never pass through CRUD:

- outbox dispatch (`pkg/outbox`),
- SQL-backed session stores (`pkg/auth/session_store_sql.go`),
- migrations and schema-drift checks (`pkg/db`),
- any raw `db.QueryContext` / `db.ExecContext` an application runs directly.

ADR-018 named the fix — "driver-level instrumentation in `pkg/db`" — and
deferred it as "a separate, larger step." This ADR is that step.

## Decision

Wrap the `database/sql` driver, opt-in, so direct statements reach the same bus
without changing the CRUD path.

1. **Wrapper in `pkg/db` (`instrument.go`).** When `Config.StatementObserver`
   is non-nil, `db.New` builds the `*sql.DB` via `sql.OpenDB` over a wrapping
   connector instead of `sql.Open`. The wrapped `driver.Conn` implements every
   optional `database/sql/driver` interface and forwards to the base conn when
   the base implements it (returning `driver.ErrSkip` / a benign default
   otherwise), so no driver capability is lost. Observation happens in
   `QueryContext`/`ExecContext` on the conn (direct path) and on the wrapped
   `Stmt` (prepared path); `database/sql` uses exactly one of those per logical
   operation, so a statement is observed once.

2. **`StatementObserver` is observability-agnostic.** `pkg/db` stays low-level:
   the callback type (`func(ctx, StatementInfo)`) and `StatementInfo`
   (`Operation`, `Query`, `Args []any`, `Duration`, `Err`, `RowsAffected`) know
   nothing about the bus. `pkg/app` builds the callback and bridges it to the
   existing `model.SQLQueryObserver` (the hooks adapter), reusing its
   sanitize + correlation + emit + `HasSubscribers` gate. Args reach the
   observer raw; the observer sanitizes (ADR-007) — the driver layer does no
   redaction.

3. **De-duplication by context marker.** CRUD already emits its statements
   (with the model name). To avoid recording them twice, `model.CRUD` stamps
   the context it hands to `database/sql` with `observe.CtxWithModelObserved`,
   and the wrapper skips emission when `observe.IsModelObserved(ctx)` is true.
   A statement without the marker is a genuine bypass — exactly what this layer
   surfaces. This mirrors ADR-018's "skip-when-connected" precedent: the
   producer that would otherwise double-record suppresses its second write at
   the source, rather than de-duplicating downstream.

4. **Opt-in, off by default (`sql_driver_instrumentation`).** Nil observer →
   the driver is not wrapped at all → the hot path is byte-for-byte the stock
   `database/sql` path (zero cost, unchanged behaviour: the feed shows CRUD
   traffic only, as before). Enabled → a small per-direct-statement cost (two
   `time.Now`, an operation classification, an args value-copy); the expensive
   work (16-arg sanitize, pooled-event build, emit) still runs only when a bus
   subscriber is attached.

## Why both observers coexist (not "replace CRUD observer with the wrapper")

The wrapper sees only SQL text, so it cannot supply `ModelName`. Replacing the
CRUD-layer observer with the wrapper would blank the model column for all ORM
traffic — a regression. So CRUD keeps emitting model-enriched events, and the
wrapper emits only the (model-less) bypass traffic the CRUD layer never sees.
The marker is what keeps the two from overlapping.

## Alternatives considered

- **Have `pkg/db` emit to the bus directly.** Rejected: it would couple the
  low-level `pkg/db` to `pkg/observability`/`hooks` and duplicate the arg
  sanitizer. The agnostic callback keeps the layering and reuses one sanitizer.
- **A per-connection tag instead of a context marker.** Rejected: no
  connection-level tagging infrastructure exists, and the context already flows
  into the driver's `QueryerContext`/`ExecerContext`, so a context value is the
  cheapest signal that reaches the wrapper.
- **Always-on.** Rejected: the framework's "zero cost when nobody is watching"
  invariant is for the default path; wrapping the driver unconditionally would
  add cost to every direct statement even for apps that never open the feed.
  Opt-in keeps the default free.

## Consequences

- New stable surface (additive, frozen): `db.StatementObserver`,
  `db.StatementInfo`, `db.Config.StatementObserver`, `app.Config`'s
  `SQLDriverInstrumentation` key, and `observe.CtxWithModelObserved` /
  `observe.IsModelObserved`.
- `pkg/model` and `pkg/db` now import `pkg/observe` (the correlation-context
  home) for the marker — no new third-party surface; both stay firewalled.
- v1.0-gate **W2** is resolved.
