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
  internal/
    controllers/
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

## Folder Responsibilities

- `controllers`: HTTP handlers and route-facing logic
- `models`: domain entities registered in the model/admin system
- `services`: business workflows and orchestration
- `repositories`: persistence boundaries
- `tasks`: Asynq handlers and task glue
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
