# Quickstart

Reference date: 2026-04-23.
Status: Current.

This guide gets you from zero to a running GoFrame app quickly.

## Prerequisites

- Go `1.25+`
- Optional Redis (only if running worker/task features)

## 1. Install CLI

```bash
go install github.com/jcsvwinston/GoFrame/cmd/goframe@latest
```

## 2. Create Project

```bash
goframe new myapp --module github.com/acme/myapp --template mvc
cd myapp
go mod tidy
```

The generated project is **self-contained**: it includes a `go.mod` with the
current GoFrame version and compiles without needing the GoFrame source tree
or a `replace` directive.

### Lightweight API alternative

For a minimal core-only project (no admin panel, storage, or mail):

```bash
goframe new myapi --module github.com/acme/myapi --template api
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

## 5. Maintenance (no local CLI install needed)

```bash
go run github.com/jcsvwinston/GoFrame/cmd/goframe@latest migrate --config goframe.yaml
go run github.com/jcsvwinston/GoFrame/cmd/goframe@latest seed --config goframe.yaml --seeds seeds
go run github.com/jcsvwinston/GoFrame/cmd/goframe@latest routes --config goframe.yaml
go run github.com/jcsvwinston/GoFrame/cmd/goframe@latest health --config goframe.yaml
```

## 6. Enterprise SQL Drivers (optional)

MSSQL and Oracle drivers are opt-in via build tags:

```bash
go build -tags mssql  ./cmd/server    # include SQL Server driver
go build -tags oracle ./cmd/server    # include Oracle driver
```

SQLite, PostgreSQL, and MySQL are included by default.

## Next Reading

- [INDEX.md](INDEX.md)
- [reference/DEVELOPER_MANUAL.md](reference/DEVELOPER_MANUAL.md)
- [guides/DETAILED_TUTORIAL.md](guides/DETAILED_TUTORIAL.md)
- [MODULARIZATION.md](MODULARIZATION.md)
