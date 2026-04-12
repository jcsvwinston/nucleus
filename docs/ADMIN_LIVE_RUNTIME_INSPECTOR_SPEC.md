# Admin Live Runtime Inspector Specification

Reference date: 2026-04-08.
Status: **Implemented** (2026-04-11). See [ADMIN_PANEL.md](ADMIN_PANEL.md) for current documentation.

## Goal

Build an embedded `/admin` panel focused on:

- dynamic business-data administration,
- real-time runtime visibility,
- operational debugging for production incidents,
- long-term upgrade durability (stable contracts, additive evolution).

## Core Constraints

1. No massive telemetry persistence in framework storage.
- historical/long-retention telemetry belongs to external OpenTelemetry backends.
- admin runtime views use in-memory data only.

2. Zero-overhead by default.
- collectors must be non-blocking for request/DB hot paths.
- when no admin consumer exists, events are dropped quickly.

3. Security by default.
- `/admin/*` remains auth-protected in protected mode.
- sensitive environment values must be masked.
- payload logging must support redaction/censoring.

4. Durable contracts.
- add new capabilities without breaking stable APIs/CLI/config contracts.
- admin surface should move from `transitional` to `stable` only after test-backed acceptance criteria are met.

## Technical Stack

- Backend: Go
- UI: HTMX + WebSockets + Go templates (`html/template` or `templ`)
- Runtime stores:
  - `sync.Map` for active session/runtime maps
  - bounded ring buffers for recent events

## Module A: Auto-Admin Business Data

1. Reflection-based auto CRUD:
- inspect registered model structs and `admin` tags to render list/form metadata.
- keep explicit model registration as the source of truth.

2. Dynamic filters and pagination:
- query, filter, sort, page from URL params with strict validation.
- only allow declared filterable fields.

3. RBAC:
- role-based policy checks for model/view/action access.
- keep action-level authorization hooks.

4. Bulk actions:
- register named bulk actions in Go.
- execute over selected rows with per-id error reporting.

## Module B: Live Traffic (In Memory)

1. Active sessions tracker:
- keep active sessions in `sync.Map`.
- show user id, IP, user-agent, last route, last seen.

2. Identity to trace mapping:
- show `trace_id` associated with current session/request context.

3. Live SQL sniffer:
- non-blocking event capture around DB execution:
  - SQL text,
  - args (redacted),
  - duration (ms),
  - status/error.
- dispatch to connected admin WebSocket subscribers only.

4. Live request/response watcher:
- bounded ring buffer with last N HTTP events:
  - route,
  - status code,
  - duration,
  - censored payload preview.

## Module C: System Pulse

1. Goroutine explorer:
- surface `pprof` goroutine snapshot and grouped state counts.

2. DB pool stats:
- expose `db.Stats()` live values (`InUse`, `Idle`, `WaitCount`, etc.).

3. Memory and GC:
- expose selected `runtime.ReadMemStats` counters and trends.

4. Environment/config viewer:
- static snapshot from startup/runtime config.
- auto-mask keys containing `KEY`, `SECRET`, `PASSWORD`, `TOKEN`.

5. Worker/job pool monitor:
- expose active workers, queued jobs, in-progress jobs (when task runtime is enabled).

6. Live feature flags:
- runtime boolean flags toggled in-memory without restart.
- include audit metadata in memory/log stream.

## Required Implementation Patterns

1. Observer + middleware pattern:
- provide standard `net/http` middleware hooks for instrumentation.

2. Non-blocking event pipeline:
- bounded channels + drop policy + lightweight fanout.

3. Secure routing envelope:
- mount under `/admin/*`.
- easy wrapping with auth middleware and optional allowlist/IP policy.

4. UI isolation:
- self-contained admin assets/styles to avoid collisions with app assets.

## Delivery Plan (Incremental)

1. Slice S1: live event bus foundation
- WebSocket hub + ring buffers + non-blocking publish/drop metrics.

2. Slice S2: live sessions + request watcher
- active sessions table + last-request stream + trace id correlation.

3. Slice S3: SQL sniffer + DB pool + memory/GC cards
- query feed + runtime/system pulse summary cards.

4. Slice S4: goroutine explorer + env/config masked viewer
- operational debugging views and secure env rendering.

5. Slice S5: auto-admin metadata expansion + RBAC/bulk refinements
- reflection-driven UX improvements and action governance.

6. Slice S6: worker monitor + live feature flags
- background runtime operations control.

## Acceptance Gates

1. Performance:
- instrumentation paths are non-blocking and bounded.

2. Security:
- sensitive values are masked and payload redaction is enforced.

3. Compatibility:
- no breaking changes on stable framework contracts.
- admin contract promotion requires explicit contract tests.

4. Operability:
- dashboard remains usable in local/dev and containerized production profiles.
