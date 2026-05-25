# Modularization: Standalone Scaffold Initiative

Reference date: 2026-04-23.
Status: **In progress**.

## Objective

Make Nucleus-generated projects fully self-contained: compilable, runnable,
and deployable without the Nucleus source tree, without a local `replace`
directive, and without unnecessary dependency weight.

## Phases

### Phase 1: Self-contained go.mod ✅

**Status: Complete.**

The `nucleus new` scaffold now generates a `go.mod` with an explicit
`require github.com/jcsvwinston/nucleus <version>` line.

- Release builds embed the exact version tag (e.g. `v0.5.5`) via goreleaser ldflags.
- Dev builds emit `latest` so `go mod tidy` resolves the newest published tag.
- Users no longer need a `replace` directive to compile scaffolded projects.

**Files changed:**

| File | Change |
|------|--------|
| `internal/cli/new.go` | `newGoModTemplate` now includes `require` line; new `resolveFrameworkVersion()` helper |

**Tests:** All scaffold tests pass (`TestRun_NewProjectScaffold` etc).

---

### Phase 2: Build tags for enterprise SQL drivers ✅

**Status: Complete.**

MSSQL and Oracle drivers are now behind build tags and excluded from the
default build. Projects that need them activate them explicitly.

**Activation:**

```bash
go build -tags mssql   .      # include MSSQL driver
go build -tags oracle  .      # include Oracle driver
go build -tags "mssql,oracle" ./...   # include both (all packages)
```

**Files changed:**

| File | Change |
|------|--------|
| `pkg/db/db.go` | Removed unconditional `go-mssqldb` and `go-ora` imports |
| `pkg/db/driver_mssql.go` | New file: `//go:build mssql` — registers MSSQL driver |
| `pkg/db/driver_oracle.go` | New file: `//go:build oracle` — registers Oracle driver |
| `pkg/db/db_enterprise_test.go` | New file: enterprise driver tests behind build tags |
| `pkg/db/db_test.go` | Removed enterprise test (moved to tagged file) |
| `pkg/db/sql_matrix_test.go` | Removed enterprise candidates test (moved to tagged file) |
| `SPEC.md` | Updated dependency section to document build tags |

**Tests:** All default tests pass; enterprise tests pass with `-tags "mssql,oracle"`.

---

### Phase 3: Composable `app.New()` — Extension pattern ✅

**Status: Complete.**

#### Goal

Transform `app.New()` from "initialize everything" to "initialize core +
opt-in extensions" so that scaffolded apps only compile and import what they
actually use.

#### Current problem

`pkg/app/app.go` unconditionally imports all 10 subsystems at lines 17-27:

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/admin"
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/authz"
    "github.com/jcsvwinston/nucleus/pkg/db"
    "github.com/jcsvwinston/nucleus/pkg/mail"
    "github.com/jcsvwinston/nucleus/pkg/model"
    "github.com/jcsvwinston/nucleus/pkg/observe"
    "github.com/jcsvwinston/nucleus/pkg/openapi"
    "github.com/jcsvwinston/nucleus/pkg/router"
    "github.com/jcsvwinston/nucleus/pkg/storage"
)
```

Even if a scaffolded app only uses `router` + `model`, the Go compiler pulls
in all transitive dependencies (GCS SDK, Azure SDK, Casbin, gopsutil, etc.)
because they share a single Go module.

#### Design

1. **Define `Extension` interface** in `pkg/app`:
   ```go
   type Extension interface {
       Name() string
       Attach(a *App) error
       Shutdown(ctx context.Context) error
   }
   ```

2. **`app.New()` core** initializes only:
   - Config loading/validation
   - Logger (`slog`)
   - Database connections (`pkg/db`)
   - Session manager (`pkg/auth`)
   - Router + middleware (`pkg/router`)
   - Model registry (`pkg/model`)

3. **Extensions register themselves** explicitly:
   ```go
   a, err := app.New(cfg,
       admin.Extension(),    // opt-in admin panel
       storage.Extension(),  // opt-in file storage
   )
   ```

4. **Backward compatibility**: `app.New(cfg)` with no extensions still works
   but does not mount admin/storage/authz by default. A convenience
   `app.WithDefaults()` option can restore the current "everything" behavior.

#### Subtasks

| # | Task | Status |
|---|------|--------|
| 3.1 | Define `Extension` interface in `pkg/app/extensions.go` | ✅ |
| 3.2 | Add `Option` type and `WithExtensions(...)` to `app.New()` | ✅ |
| 3.3 | Extract admin/storage/mail/authz into `attachDefaultSubsystems()` | ✅ |
| 3.4 | `WithoutDefaults()` option for core-only initialization | ✅ |
| 3.5 | Backward compat: `app.New(cfg)` without options works identically | ✅ |
| 3.6 | Add `--template api` scaffold tier (core only, `WithoutDefaults()`) | ✅ |
| 3.7 | Tests for WithoutDefaults, WithExtensions, and extension lifecycle | ✅ |

**Files changed:**

| File | Change |
|------|--------|
| `pkg/app/extensions.go` | New file: `Extension` interface, `Option` type, `WithExtensions()`, `WithoutDefaults()` |
| `pkg/app/app.go` | `New()` accepts `...Option`; default subsystems extracted to `attachDefaultSubsystems()` |
| `pkg/app/app_test.go` | New tests: `TestAppNew_WithoutDefaults_CoreOnly`, `TestAppNew_WithExtensions`, `TestAppNew_WithExtensions_Error` |
| `internal/cli/new.go` | `--template api` support; `newAPIMainTemplate` uses `app.WithoutDefaults()` |
| `cmd/nucleus/main_test.go` | Updated template rejection test for new `api` template |

**Tests:** All existing tests pass (backward compat verified); new extension tests pass.

---

### Phase 4: Go multi-module split — DEFERRED

**Status: Reverted (2026-04-24). Deferred to post-v1.**

An attempt was made to split `pkg/storage`, `pkg/tasks`, and `pkg/authz` into
independent Go modules with their own `go.mod` files. This approach was
**reverted** because:

1. Go requires sub-modules to be published as separate version tags on the
   remote repository (e.g. `pkg/storage/v0.6.0`). Without published tags,
   `go mod tidy` in scaffolded projects fails.
2. Multi-module releases require coordinated tagging infrastructure that
   doesn't exist yet.
3. Phases 1-3 already achieve the stated goal: scaffolded projects are
   self-contained, modular, and can run without the Nucleus binary or source.

#### What was tried and reverted

- `pkg/storage/go.mod`, `pkg/tasks/go.mod`, `pkg/authz/go.mod` (deleted)
- `go.work` workspace (deleted)
- Root `go.mod` `replace` directives (removed)

#### Future path (post-v1)

When the release infrastructure supports coordinated multi-module tagging:

1. Move subsystems to top-level directories with module paths like
   `github.com/jcsvwinston/nucleus/storage`
2. Publish version tags per sub-module
3. Update scaffold templates to generate per-module `require` lines
4. Add CI lane to test sub-modules independently

---

## Documentation impact tracker

All documents have been updated to reflect the current state (Phases 1-3 complete):

| Document | Phase | Status |
|----------|-------|--------|
| `SPEC.md` | 2 | ✅ Build tags for enterprise drivers |
| `SPEC.md` | 3 | ✅ Extension pattern for `app.New()` |
| `docs/QUICKSTART.md` | 1 | ✅ Go version, self-contained scaffold |
| `docs/QUICKSTART.md` | 3 | ✅ Template tiers (`--template api`) |
| `docs/README.md` | 2 | ✅ Link to this document |
| `docs/reference/DEPENDENCY_IMPACT_REPORT.md` | 2 | ✅ MSSQL/Oracle now build-tagged |
| `docs/governance/CI_MATRIX.md` | 2 | ✅ Build tag instructions for enterprise lanes |
| `*exploratory_stability*` | 2 | ✅ Note about build tags |
| `CHANGELOG.md` | all | ✅ All phase entries recorded |

