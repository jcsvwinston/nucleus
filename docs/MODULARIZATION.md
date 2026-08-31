# Modularization: Standalone Scaffold Initiative

Reference date: 2026-04-23. Closed: 2026-08-31.
Status: **Completed — historical record.** The objective was met (scaffolds
are self-contained: pinned `require`, no `replace`, enterprise drivers
behind build tags) and the initiative stopped producing work months ago;
this file is kept as the acta of how it happened, not as a plan.

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

### Phase 2: Enterprise SQL drivers, first behind build tags, now as modules ✅

**Status: Superseded by ADR-031.**

This phase originally put the SQL Server and Oracle drivers behind the build
tags `mssql` and `oracle`, so a default build excluded them. That worked, and
it had one flaw that only showed up in use: **a build tag is invisible**.
Nothing in the source says it exists, so a build that forgot it compiled fine
and failed at run time with `unknown driver "sqlserver"` — a message that
names neither the tag nor anything to search for.

ADR-031 replaced the tags with modules, one per engine, alongside the other
three drivers that used to be compiled in unconditionally:

```
drivers/postgres   drivers/mysql   drivers/sqlite   drivers/mssql   drivers/oracle
```

Activation is now an import rather than a flag, the way `database/sql` drivers
have always been wired:

```go
import _ "github.com/jcsvwinston/nucleus/drivers/mssql"
```

or `nucleus add mssql`, which runs the `go get` and writes the import.

Two things carried over from this phase and are worth keeping in view. The
first is that a driver module registers **two** things — the driver and how
that driver reports a unique-constraint violation — because without the
second, `db.IsUniqueViolation` answers `false` instead of failing. The second
is that the tagged files this phase created (`driver_mssql.go`,
`errors_classify_nomssql.go` and their Oracle counterparts) are gone: the
`no`-variants existed only to give the tagged build a constant-false stub, and
a registry needs no such thing.


---

### Phase 3: Composable `app.New()` — Extension pattern ✅

**Status: Complete.**

#### Goal

Transform `app.New()` from "initialize everything" to "initialize core +
opt-in extensions" so that scaffolded apps only compile and import what they
actually use.

#### Problem at the time

`pkg/app/app.go` unconditionally imported every subsystem:

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/auth"
    "github.com/jcsvwinston/nucleus/pkg/authz"
    "github.com/jcsvwinston/nucleus/pkg/db"
    "github.com/jcsvwinston/nucleus/pkg/mail"
    "github.com/jcsvwinston/nucleus/pkg/model"
    "github.com/jcsvwinston/nucleus/pkg/observe"
    "github.com/jcsvwinston/nucleus/pkg/router"
    "github.com/jcsvwinston/nucleus/pkg/storage"
    // …plus, at the time, pkg/admin (removed from the core in ADR-019)
    // and pkg/openapi (decoupled in DEP-2026-008).
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
       admin.Extension(),    // opt-in admin panel — since extracted to the separate orbit module (ADR-019)
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

