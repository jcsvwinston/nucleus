# Status and Next Steps

Last updated: 2026-04-15

## Current baseline

The current consolidation line is cumulative:

- `codex/point-4-admin-runtime` starts from `codex/point-3-resource-crud`
- `codex/point-3-resource-crud` already includes `codex/point-2-scaffold-alignment`
- `codex/point-2-scaffold-alignment` already includes `codex/point-1-doc-parity`

Work should continue on the newest consolidation branch to avoid reopening older branches and creating merge conflicts.

## Completed work

### Point 1: documentation and implementation parity

Completed and verified.

Scope closed:

- aligned documentation paths with the repository layout
- unified the active documentation baseline
- fixed mismatches in documented defaults and runtime defaults

### Point 2: scaffold alignment with documented architecture

Completed and verified.

Scope closed:

- `goframe new` now creates documented structural directories
- `goframe startapp` now creates the shared service/repository/static structure
- tests assert the generated layout

### Point 3: generated resources must be usable by default

Completed and verified.

Scope closed:

- `goframe generate resource` no longer emits `501 not implemented` handlers
- generated resources now expose a small working CRUD scaffold
- generated tests cover the CRUD lifecycle
- CLI tests compile the generated scaffold in a temporary module

## Pending work

### Point 4: make admin operational features real

This is the next active implementation target.

Recommended first cut:

- make Redis health checks real instead of reporting only that a URL is configured
- make cache endpoints return basic real runtime information when Redis is configured
- make cache flush perform a real flush operation when authorized and configured
- make storage browsing use the configured `storage.Store` when available
- add focused tests for Redis/cache/storage behavior using local test doubles

Expected second pass within point 4:

- decide whether admin migrations remain read-only or can safely execute `db.Migrator`
- replace email stats placeholder behavior with a runtime-backed implementation

### Point 5: explicit application layer

After point 4, the next architectural step is:

- formalize service conventions
- formalize repository conventions
- align controllers, services, repositories, and tasks
- update scaffolds and generators to reflect that architecture

### Point 6: API contracts

After the application layer is clearer:

- generate OpenAPI from framework conventions
- expose automatic API documentation
- prepare generated clients and contract checks

### Point 7 and beyond: distributed primitives

Longer-term work:

- stronger async primitives
- pub/sub, cron, retries, dead-letter handling, and outbox support
- more declarative infrastructure integration
- service catalog, topology, and stronger runtime observability

## Recommended start for tomorrow

Start with point 4 in this order:

1. implement real Redis health checks
2. implement real cache stats and cache flush
3. implement storage browsing through `storage.Store`
4. add focused tests
5. run verification: `go test ./...` and `npm run build`
6. commit and push the point 4 batch
