# Quickstart

Reference date: 2026-05-25.
Status: Current.

This guide gets you from zero to a running Nucleus app quickly.

## Prerequisites

- Go `1.26+` (matches the `go 1.26.4` directive in `go.mod`)
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

`nucleus new` generates a **minimal skeleton**: a composition-root `main.go`,
`nucleus.yml`, `.gitignore`, and an empty `migrations/` directory. The mvc
template also includes `rbac_policy.csv` and mounts the admin panel with
default-deny Casbin. The api template uses `WithoutDefaults()` and serves only
`/healthz`. There is no pre-built demo content; add features as modules and
model your first one on `examples/mvc_api`.

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
go run .
```

If you have added a worker process to your project (not scaffolded by default):

```bash
go run ./cmd/worker
```

## 4. Verify Endpoints

- `http://localhost:8080/healthz` — unauthenticated liveness/readiness probe (200 + JSON per-dependency); always present
- `http://localhost:8080/admin` — admin panel (mvc template only)
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
go build -tags mssql  .    # include SQL Server driver
go build -tags oracle .    # include Oracle driver
```

SQLite, PostgreSQL, and MySQL are included by default.

## 7. AutoMigrate — dev mode only

`(*app.App).AutoMigrate(models ...any)` derives idempotent `CREATE TABLE` statements from struct tags and runs them against the configured database. `app.New(cfg)` returns `(*App, error)` — check the error before calling `AutoMigrate`. **Supported dialects: SQLite, PostgreSQL, MySQL, MSSQL, and Oracle** — each via its own dialect-aware scaffold builder. SQLite/Postgres/MySQL use `CREATE TABLE IF NOT EXISTS`; MSSQL wraps the CREATE in `IF OBJECT_ID(..., 'U') IS NULL`; Oracle uses a PL/SQL block that swallows ORA-00955. The result is safe to re-run on every dialect.

`AutoMigrate` never alters existing tables: it only creates them when absent. For production schema evolution use explicit migration files (`migrations/*.up.sql`) — they are reversible, reviewable in PR diffs, and the only path the framework offers compatibility guarantees on.

`nucleus migrate drift` reports two failure modes:

- **`missing_up_file`** — a migration is marked applied in `nucleus_schema_migrations` but its `.up.sql` is gone from the working tree.
- **`checksum_mismatch`** — the migration was applied with one content and the file on disk has changed since (someone edited `.up.sql` in place after it was applied).

Wire `nucleus migrate drift` into CI predeploy to catch both forms.

## Next Reading

- [README.md](README.md) - Documentation overview and navigation
- [reference/DEVELOPER_MANUAL.md](reference/DEVELOPER_MANUAL.md) - Core concepts reference
- [guides/DETAILED_TUTORIAL.md](guides/DETAILED_TUTORIAL.md) - Step-by-step tutorial
- [MODULARIZATION.md](../MODULARIZATION.md) - Extension and modularization patterns
