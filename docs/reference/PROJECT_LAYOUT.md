# Recommended Project Layout

Reference date: 2026-04-05.
Status: Current.

Use this as a practical default for MVC + API GoFrame apps.

```text
myapp/
  cmd/
    server/
      main.go
    worker/
      main.go
    scheduler/
      main.go
  internal/
    controllers/
    contracts/
    models/
    services/
    repositories/
    tasks/
    web/
      templates/
      static/
  migrations/
  seeds/
  goframe.yaml
  go.mod
```

Generated API-contract scaffolds also seed:

```text
internal/contracts/
  contracts.go
  *_contract.go
```

## Folder Responsibilities

- `controllers`: HTTP handlers and route-facing logic
- `contracts`: generated API contract registration and OpenAPI-oriented definitions
  - `contracts.go`: package-level aggregator with `Register(doc *openapi.Document)`, `DefaultConfig()`, `NewDocument()`, and `NewDocumentWithConfig(cfg Config)`
  - `*_contract.go`: per-resource or per-app contract files exposing `RegisterXContract(doc *openapi.Document)` and auto-registering with the package aggregator
- `models`: domain entities registered in the model/admin system
- `services`: business workflows and orchestration
- `repositories`: persistence boundaries
- `tasks`: Asynq handlers, periodic registrations, and task glue
- `web/templates`: MVC templates
- `web/static`: app static assets
- `migrations`: SQL schema evolution
- `seeds`: SQL bootstrap/test data

## Minimum to Start

1. `cmd/server/main.go`
2. `goframe.yaml`
3. `migrations/` with at least one migration pair
4. one registered model and one route

If background jobs are needed, also include:

- `cmd/worker/main.go`
- `internal/tasks/`

If periodic tasks are needed, also include:

- `cmd/scheduler/main.go`
- `internal/tasks/`

## Contract Convention

The current experimental OpenAPI lane uses `internal/contracts` as the project convention:

1. each generated contract file exposes an explicit `RegisterXContract(doc *openapi.Document)` function,
2. each generated contract file auto-registers that function in the package aggregator,
3. `internal/contracts/contracts.go` centralizes the document bootstrap via `DefaultConfig()`, `NewDocument()`, and `NewDocumentWithConfig(cfg Config)`,
4. `goframe openapi --out openapi.json` and runtime serving should both use that same document builder,
5. scaffolded contracts should stay explicit, but can use small `pkg/openapi` helpers such as `ObjectSchema`, `ArraySchema`, `DataEnvelopeSchema`, `CollectionEnvelopeSchema`, `IDSchema`, `JSONRequestBody`, `JSONResponse`, `ErrorResponse`, `EmptyResponse`, `PathParameter`, `QueryParameter`, and `SearchQueryParameter` to reduce repetition,
6. scaffolded server apps mount the experimental document explicitly at `/openapi.json` via `app.MountOpenAPI("/openapi.json", contracts.NewDocument)`.
