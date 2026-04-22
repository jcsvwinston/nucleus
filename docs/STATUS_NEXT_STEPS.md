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

In progress.

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

Still pending in the next cut:

- formalize service conventions further
- formalize repository conventions
- align controllers, services, repositories, tasks, and contracts more uniformly
- extend API contract generation beyond the first OpenAPI lane

### Point 6: API contracts

In progress.

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

Still pending in the next cut:

- deepen explicit query-parameter usage only where generated handlers can honor it cleanly
- decide whether runtime documentation exposure should remain explicit-only (`MountOpenAPI`) or gain one additional documented convention without adding hidden automation
- prepare generated clients beyond the current document-export/serve lane

### Point 7 and beyond: distributed primitives

Longer-term work:

- stronger async primitives
- pub/sub, cron, retries, dead-letter handling, and outbox support
- more declarative infrastructure integration
- service catalog, topology, and stronger runtime observability

## Recommended start for tomorrow

Continue point 6 with this order:

1. deepen the OpenAPI lane beyond the current scaffold-first subset
2. decide whether runtime exposure should stay explicit-only or gain one additional documented convention
3. keep tightening conventions between controllers, services, repositories, tasks, and contracts
4. run verification: `go test ./...` and `npm run build`
5. commit and push the next point 6 batch
