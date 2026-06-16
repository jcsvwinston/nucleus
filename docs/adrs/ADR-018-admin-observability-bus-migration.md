# ADR-018: Admin Live View Consumes the Observability Bus

Status: Accepted (transitional)
Date: 2026-06-16
Related: pkg/observability (Phase 2 of the admin refactor), the planned
`admin/agent` (Phase 3, referenced in `pkg/observability/doc.go`)

## Context

The admin live view shows two real-time feeds — HTTP requests and SQL
statements — backed by ring buffers in `pkg/admin/live.go`.

The SQL feed was populated by a **per-CRUD observer** (`Panel.onModelSQLQuery`),
installed only on the CRUD instances the admin panel itself creates for Data
Studio (`getCRUD`). Consequently the live SQL view captured **only the admin's
own browsing** — never the queries an application's own handlers run through
`model.CRUD` (REST resources, app-side CRUD). Those queries already flowed to
the process-wide `pkg/observability` bus (the framework's default SQL observer,
installed in `pkg/app`), but the live view did not consume the bus. Application
SQL was therefore invisible in the panel — a gap surfaced by the fleetdesk
prototype, whose UI runs almost entirely on direct/`model.CRUD` queries the
admin never issued.

The inline comments in `pkg/app/app.go` and `pkg/model/crud.go` anticipated the
fix: *"when pkg/admin's live view is retired in a future phase, [the bus]
becomes the single SQL feed."* Phase 3 plans an `admin/agent` sub-package as the
bus's primary subscriber.

## Decision

As an incremental step toward that end state, `pkg/admin.Panel` subscribes
directly to the observability bus and drains `KindSQLStatement` events into the
existing SQL ring buffer:

- `Panel.ConsumeObservability(bus *observability.Bus) func()` subscribes
  (filter = `KindSQLStatement`), runs a drain goroutine that records each event
  via `recordBusSQL`, and returns an idempotent stop function (also invoked by
  `Panel.Close`). It is wired once in `app.New`, after panel construction.
- When the bus is connected (`observConnected`), `getCRUD` **skips** installing
  the per-CRUD observer, so admin-issued queries are recorded once (via the
  bus) rather than twice. The per-CRUD observer remains as a fallback for
  `Panel` instances built without `app.New` (unit tests, standalone embedding).

The drain goroutine stops via a `done` channel because the bus deliberately does
not close subscription channels (closing while `Emit` may hold a reference would
race); after `done` it drains buffered events to honour their pool-refcount
`Release` obligations. The stop is guarded by `sync.Once`.

## Consequences

- The observability bus is now the single source of truth for the live SQL
  feed across the **whole application**, not just admin-owned CRUDs.
- `ConsumeObservability` is a **transitional** exported method on the
  `transitional`/unfrozen `pkg/admin` package. When the Phase 3 `admin/agent`
  lands as the bus's primary subscriber, it will own this subscription and
  `ConsumeObservability` is expected to be retired or moved.
- Direct `*sql.DB` queries (`db.QueryContext`/`ExecContext` that bypass
  `model.CRUD`) are still invisible — the bus only sees `model.CRUD`. Capturing
  those needs driver-level instrumentation in `pkg/db` (a separate, larger
  step; tracked as a follow-up).
- No contract rebaseline (`pkg/admin` is not frozen); no migration; no config
  key; no `examples/*` change (internal plumbing, no module-author-visible
  behaviour change).

## Alternatives considered

- **Keep the per-CRUD observer and also subscribe to the bus.** Rejected:
  admin-issued queries would be double-recorded. The skip-when-connected guard
  is simpler than de-duplicating in the ring buffer.
- **Wait for `admin/agent` (Phase 3).** Rejected: the application-SQL gap is
  user-visible today and the bus already carries the data; this step is small,
  reversible, and recorded here as transitional.
