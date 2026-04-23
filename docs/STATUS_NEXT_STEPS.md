# Status and Next Steps

Last updated: 2026-04-22

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

Completed and verified.

Completed in the first cut:

- `new`, `startapp`, and `generate` now materialize `services` and `repositories` as real scaffold files
- `generate` supports explicit `service` and `repository` targets
- resource scaffolds now include model, handler, service, repository, and migration pieces together
- `startapp` now uses the local module path when available so generated HTTP controllers can depend on `services` instead of falling back to direct SQL wiring
- generated services now own their first explicit input/output contracts instead of leaking repository result types directly to controllers
- module-aware `generate resource` now emits repository-backed services and handlers that delegate into those services instead of keeping state inside the HTTP layer
- module-aware `generate handler` now generates service-backed handlers and creates the companion service scaffold when it is still missing
- module-aware task scaffolds now delegate decoded background payloads into `services` instead of keeping async reactions outside the application layer
- generated module-aware scaffolds now also seed `internal/contracts` and minimal OpenAPI registration using `pkg/openapi`
- generated services now depend on repository interfaces in the main scaffolds instead of concrete repository structs
- CLI tests assert those architectural layers are generated

Completed in the second cut:

- `new`, `startapp`, and `generate resource` now share more uniform application-layer conventions for list/create flows instead of mixing ad hoc method signatures
- scaffolded services now expose explicit list inputs (`ListXInput`) in the same spirit as their create/update inputs
- scaffolded repositories now expose explicit list/create parameter structs where a repository layer exists, so controllers, services, repositories, and contracts describe the same flow more uniformly
- scaffolded list handlers now pass normalized query input through the service layer instead of reaching directly into repository-specific filtering
- module-aware `startapp` repositories are now small but usable in-memory scaffolds, which keeps the first application-layer slice coherent before a real persistence implementation is wired in

### Point 6: API contracts

Completed and verified.

Completed in the first cut:

- `pkg/openapi` is now the official experimental base layer for project-level API contracts
- generated projects now seed `internal/contracts/contracts.go` as the package-level contract aggregator
- scaffolded contract files auto-register through that aggregator while still exposing explicit `RegisterXContract(doc *openapi.Document)` functions
- `goframe openapi --out openapi.json` now exports a project OpenAPI document from the registered contracts
- CLI tests now verify generated contract compilation, exported document shape, and scaffold/export consistency

Completed in the second cut:

- the experimental subset now includes small reusable helpers for common JSON error responses, empty `204` responses, and explicit query parameters
- scaffolded contracts now expose more homogeneous `tags`, `summary`, `description`, and collection `operationId` conventions
- generated resource handlers now align with the framework's structured JSON error envelope instead of returning ad hoc error strings
- structural contract checks now validate shared metadata, error schemas, and empty responses beyond basic string matching
- docs now describe the supported subset and extension conventions more explicitly

Completed in the third cut:

- `new`, `startapp`, and `generate resource` now converge on the same scaffolded JSON response envelopes: collections use `{data, count}` and singular payloads use `{data}`
- `pkg/openapi` now exposes small envelope helpers so scaffolded contracts can declare that convention explicitly without repeating literal object schemas
- structural OpenAPI export checks now assert the shared envelope shape and reject older scaffold variants such as `items`/`total`, `resource`, or `item`

Completed in the fourth cut:

- scaffolded list operations now declare a shared optional `q` query parameter and generated handlers honor it end-to-end through controllers, services, repositories, and exported contracts
- `pkg/openapi` now exposes `SearchQueryParameter(...)` so the supported search-query convention stays explicit and lightweight
- runtime OpenAPI serving remains an explicit application decision via `app.MountOpenAPI(...)`; the framework now documents that explicit-only convention instead of adding hidden auto-mount behavior
- generated API clients remain intentionally out of scope for the current experimental lane; the current scope closes at explicit document export/serve plus scaffolded contract coherence

### Point 7: distributed primitives

In progress.

Completed in the first cut:

- `pkg/tasks` queue operations now expose one explicit source of truth for supported runtime actions
- the admin runtime lane now accepts the same supported queue actions as `pkg/tasks` instead of duplicating a separate whitelist
- the first dead-letter operational baseline is now real: retry queues can be archived, archived tasks can be re-run, and archived tasks can be purged
- focused tests now lock the supported queue-action catalog and keep admin/runtime behavior aligned

Completed in the second cut:

- `pkg/tasks` now exposes explicit `EnqueuePolicy` helpers so queue, retries, timeout, delay, and retention can be declared as one readable unit instead of ad hoc Asynq options
- task enqueue policies are validated before enqueue and covered by focused tests against a real Redis-compatible runtime via `miniredis`
- `pkg/signals` now includes an explicit Redis relay for distributed pub/sub, so signal events can be published across processes without replacing the in-process bus
- the Redis relay can forward remote events back into `signals.Bus`, which gives GoFrame a first small distributed event bridge aligned with the existing Django-style signal model

Completed in the third cut:

- `pkg/tasks` now exposes an explicit scheduler wrapper for periodic tasks instead of requiring raw Asynq scheduler wiring in application code
- periodic tasks can now be registered through one small `PeriodicTask` contract that reuses the same `EnqueuePolicy` subset used by normal task enqueueing
- `pkg/tasks.InspectRuntime(...)` now discovers registered scheduler entries, so runtime inspection includes cron-style task registrations as well as queues and workers
- focused tests now verify periodic registration and enqueue activity against a real Redis-compatible runtime via `miniredis`

Still pending in Point 7:

- outbox support and delivery guarantees
- richer distributed observability and topology surfaces

## Recommended start for tomorrow

Point 5 and Point 6 are closed at the current experimental baseline.
Point 7 has now started with a first real dead-letter/runtime-ops slice.

Recommended next focus:

1. continue Point 7 with the same small-slice discipline
2. keep the same discipline: narrow slice, one source of truth, strong structural tests
3. run verification: `go test ./...` and `npm run build`
4. commit and push the next batch
