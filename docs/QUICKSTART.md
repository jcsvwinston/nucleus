# Quickstart

Reference date: 2026-04-05.
Status: Current.

This guide gets you from zero to a running GoFrame app quickly.

## Prerequisites

- Go `1.23+`
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

## 3. Run App

```bash
go run ./cmd/server
```

Optional worker:

```bash
go run ./cmd/worker
```

## 4. Verify Endpoints

- `http://localhost:8080/`
- `http://localhost:8080/api/health`
- `http://localhost:8080/admin`

## 5. Useful Next Commands

```bash
goframe migrate --config goframe.yaml
goframe seed --config goframe.yaml --seeds seeds
goframe routes --config goframe.yaml
goframe health --config goframe.yaml
```

## Next Reading

- [INDEX.md](INDEX.md)
- [DEVELOPER_MANUAL.md](DEVELOPER_MANUAL.md)
- [DETAILED_TUTORIAL.md](DETAILED_TUTORIAL.md)
- [CLI_BEST_PRACTICES.md](CLI_BEST_PRACTICES.md)
