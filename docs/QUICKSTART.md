# Quickstart

Reference date: 2026-04-23.
Status: Current.

This guide gets you from zero to a running Nucleus app quickly.

## Prerequisites

- Go `1.25+`
- Optional Redis (only if running worker/task features)

## 1. Install CLI

```bash
go install github.com/jcsvwinston/nucleus/cmd/nucleus@latest
```

## 2. Create Project

```bash
nucleus new myapp --module github.com/acme/myapp --template mvc
cd myapp
go mod tidy
```

The generated project is **self-contained**: it includes a `go.mod` with the
current Nucleus version and compiles without needing the Nucleus source tree
or a `replace` directive.

### Lightweight API alternative

For a minimal core-only project (no admin panel, storage, or mail):

```bash
nucleus new myapi --module github.com/acme/myapi --template api
cd myapi
go mod tidy
```

## 3. Run App

```bash
go run ./cmd/server
```

Optional worker (requires Redis):

```bash
go run ./cmd/worker
```

## 4. Verify Endpoints

- `http://localhost:8080/` — web landing page
- `http://localhost:8080/api/articles` — JSON API
- `http://localhost:8080/admin` — admin panel
- `http://localhost:8080/healthz` — unauthenticated liveness/readiness probe (200 + JSON per-dependency)
- `http://localhost:8080/metrics` — Prometheus/OpenMetrics scrape endpoint (disable with `metrics_path: ""`)

## 5. Maintenance (no local CLI install needed)

```bash
go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest migrate --config nucleus.yml
go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest seed --config nucleus.yml --seeds seeds
go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest routes --config nucleus.yml
go run github.com/jcsvwinston/nucleus/cmd/nucleus@latest health --config nucleus.yml
```

## 6. Enterprise SQL Drivers (optional)

MSSQL and Oracle drivers are opt-in via build tags:

```bash
go build -tags mssql  ./cmd/server    # include SQL Server driver
go build -tags oracle ./cmd/server    # include Oracle driver
```

SQLite, PostgreSQL, and MySQL are included by default.

## 7. AutoMigrate — dev mode only

`app.New(cfg).AutoMigrate(&Article{})` derives `CREATE TABLE IF NOT EXISTS` statements from struct tags and runs them against the configured database. **Supported dialects: SQLite, PostgreSQL, MySQL.** MSSQL and Oracle return `db.ErrAutoMigrate` — use explicit SQL migration files plus `nucleus migrate` for those engines.

`AutoMigrate` is `CREATE IF NOT EXISTS` only: it never alters existing tables. For production schema evolution use explicit migration files (`migrations/*.up.sql`) — they are reversible, reviewable in PR diffs, and the only path the framework offers compatibility guarantees on.

`nucleus migrate drift` surfaces any applied migration that has since lost its `.up.sql` file on disk — wire it into CI predeploy to catch the most common form of schema drift.

## Next Reading

- [README.md](README.md) - Documentation overview and navigation
- [reference/DEVELOPER_MANUAL.md](reference/DEVELOPER_MANUAL.md) - Core concepts reference
- [guides/DETAILED_TUTORIAL.md](guides/DETAILED_TUTORIAL.md) - Step-by-step tutorial
- [MODULARIZATION.md](../MODULARIZATION.md) - Extension and modularization patterns
