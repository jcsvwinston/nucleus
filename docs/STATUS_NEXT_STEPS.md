# Status and Next Steps

Last updated: 2026-04-16

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

Completed and verified.

Completed in the first cut:

- Redis health checks now perform real connectivity checks
- cache stats now return real Redis runtime information
- cache flush now executes a real flush against the configured Redis database
- storage browsing now uses the configured `storage.Store` when available
- focused tests were added for Redis health, cache flush, and storage browsing

Completed in the second pass:

- admin migrations now execute through `db.Migrator` when a runtime database is available
- migration listing now reports applied state from the runtime migrator
- email stats now reflect the effective mail runtime configuration instead of returning a placeholder note

### Point 5: explicit application layer

In progress.

Completed in the first cut:

- `new`, `startapp`, and `generate` now materialize `services` and `repositories` as real scaffold files
- `generate` supports explicit `service` and `repository` targets
- resource scaffolds now include model, handler, service, repository, and migration pieces together
- `startapp` now uses the local module path when available so generated HTTP controllers can depend on `services` instead of falling back to direct SQL wiring
- CLI tests assert those architectural layers are generated

Still pending in the next cut:

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

Continue point 5 with this order:

1. tighten the actual conventions between controllers, services, repositories, and tasks
2. define the first explicit service input/output contract boundary
3. review whether generated handlers should depend on services by default across all scaffolds
4. run verification: `go test ./...` and `npm run build`
5. commit and push the next point 5 batch
