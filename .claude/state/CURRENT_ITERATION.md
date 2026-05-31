# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**Real-app readiness remediation (R1–R8) per ADR-013** — make the framework
"fino" (solid) for building real applications. Per the maintainer's directive,
**no real-app testing happens until this is verified green.**

Source of findings: `docs/audits/2026-05-31-real-app-readiness.md`.
Design decisions: `docs/adrs/ADR-013-real-app-readiness.md`.

## Scope

- **in:** the working-tree changes already applied (list below).
- **out (deferred, separate ADR/iteration — see ADR-013):** wiring
  `Module.Migrations` into `nucleus migrate`; implementing `Jobs`/`Webhooks`
  (Phase 2+); unifying the `generate resource` layout to the feature-folder
  convention.

## Status

### Done — IMPLEMENTED in the working tree, UNCOMMITTED + UNVERIFIED

> Produced in a Cowork session with **no Go toolchain** (sandbox proxy blocks
> go.dev/proxy.golang.org). Code/maintainer must compile, test, and land. The
> specialized subagents were emulated per CLAUDE.md §10 (each generic agent read
> the relevant `.claude/agents/*.md` and adopted its role).

- **R1/R2 — boot WARN guards** (`pkg/nucleus/nucleus.go`, `pkg/nucleus/module.go`):
  a module that declares `Migrations` (non-empty `fs.FS`), `Jobs`, or `Webhooks`
  now emits one boot `WARN` (no longer a silent no-op). Behavior unchanged
  (SQL-first; Jobs/Webhooks still deferred). Bonus: the Phase-2 `spec.Jobs(nil)`/
  `spec.Webhooks(nil)` stub calls are now panic-guarded. New **unexported**
  `moduleIntrospector` interface — no exported surface change.
- **R3 — `serve --without-defaults`** (`internal/cli/serve.go`): new optional
  bool flag → `app.New(cfg, app.WithoutDefaults())`; default (full-stack)
  unchanged. **CLI surface addition.**
- **R4 — CORS origins config** (`pkg/app/config.go`, `pkg/app/app.go`,
  `pkg/router/router.go`, `pkg/router/middleware.go`,
  `docs/reference/CONFIG_KEY_REGISTRY.md`): new keys `cors_origins []string` +
  `cors_allow_credentials bool`; empty `cors_origins` = back-compat allow-all.
  New exported symbols: `app.Config.CORSOrigins`, `app.Config.CORSAllowCredentials`,
  `router.WithCORSCredentials`.
- **R5 — RBAC filename discovery** (`pkg/app/app.go` `rbacPolicyPath`,
  `internal/cli/doctor.go`): both now recognize `rbac_policy.csv` (the scaffolded
  name) + `config/`,`rbac/` variants.
- **R6/R7/R8 + ADR + CHANGELOG** (`docs/adrs/ADR-013-real-app-readiness.md` +
  `docs/adrs/README.md`, `internal/cli/scaffold/templates/{api,mvc}/nucleus.yml.tmpl`,
  `templates/_common/README.md.tmpl`, `docs/reference/PROJECT_LAYOUT.md`,
  `examples/mvc_api/README.md`, `CHANGELOG.md`): ADR records all decisions;
  scaffold comments for CORS + the first-boot admin password; two-layouts +
  example-cwd docs.

### Pending on Code / maintainer — bring it to "buen puerto"

1. **[DONE] Branch + commit** — `fix/readiness-2026-05-31` exists with 4 commits
   (framework R1/R2/R4/R5; cli R3/R5; docs+ADR+website; state). **NOT pushed**
   (sandbox has no GitHub creds). First action: `git push -u origin
   fix/readiness-2026-05-31` from your machine.
2. **Compile + test:** `go build ./...` ; `go vet ./...` ; `go test ./...`. Fix nits.
3. **Regenerate the freeze baseline** for the 3 new exported symbols (do NOT
   hand-edit, §7): `NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts/ -run
   TestContractFreeze_APIExportedSymbols` → review diff (additions only:
   `pkg/app field:Config.CORSOrigins`, `field:Config.CORSAllowCredentials`,
   `pkg/router func:WithCORSCredentials`).
4. **CLI surface follow-up** for `serve --without-defaults` (not yet done):
   add to `docs/reference/CLI_CONTRACT_MATRIX.md` (serve row) and
   `docs/reference/CLI_BEST_PRACTICES.md` (CLAUDE.md §3).
5. **Website mirror — edits APPLIED** (`website/docs/getting-started/project-structure.md`,
   `website/docs/cli/overview.md` are in the changeset). Code must run
   `cd website && npm run build` to verify (no npm in the Cowork session).
6. **Tests to add:** `internal/cli/serve_test.go` (both branches); `pkg/nucleus`
   readiness-WARN test (`fstest.MapFS` migrations + nil/empty + a panicking
   Jobs/Webhooks closure → assert WARN, no panic); `pkg/router` CORS-credentials;
   `pkg/app` CORS wiring + RBAC discovery.
7. **Acceptance:** run the shakedown runbook in
   `docs/audits/2026-05-31-real-app-readiness.md` §4.
8. PR(s) → CI Required Gate green → merge.

### Blocked

- (none) — but verification is maintainer-side (no Go toolchain in the Cowork
  sandbox).

## Notes / decisions log

- 2026-05-31 — Readiness review (`docs/audits/2026-05-31-real-app-readiness.md`)
  found the happy path WORKS; the gaps were operational/declared-but-inert
  surface. ADR-013 records the directions. One subagent mis-cited "ADR-011" for
  this work (the real one is ADR-013; ADR-011 is oracle-casing) — corrected in
  `config.go`/`app.go`/`CONFIG_KEY_REGISTRY.md`. The website mirror (step 5) DID
  persist (both website pages are in `git status`); only `npm run build`
  verification remains.
