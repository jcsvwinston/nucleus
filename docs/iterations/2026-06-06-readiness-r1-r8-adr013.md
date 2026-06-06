# Iteration: Real-app readiness remediation (R1–R8) per ADR-013

> Archived: 2026-06-06 (date PRs merged to main).
> Branch: `fix/readiness-2026-05-31` (squash-merged, deleted).
> Main HEAD after merge: `2b2f7dc`.

## Goal

Remediate all R1–R8 gaps identified in the 2026-05-31 real-app readiness audit
(`docs/audits/2026-05-31-real-app-readiness.md`) so the framework is solid for
building real applications. Design decisions recorded in
`docs/adrs/ADR-013-real-app-readiness.md`.

## Scope

- **In:** R1–R8 framework surface gaps (boot WARN guards, `serve --without-defaults`,
  CORS origins config, RBAC filename discovery, scaffold template comments,
  ADR, docs, CHANGELOG).
- **Out (deferred, per ADR-013):** wiring `Module.Migrations` into `nucleus migrate`;
  implementing Jobs/Webhooks (Phase 2+); unifying `generate resource` to the
  feature-folder layout; recording `cors_origins[]` / `cors_allow_credentials` in
  `contracts/baseline/config_key_patterns.txt` (deliberate add, not auto-regen).

## Acceptance criteria — all met

- [x] R1/R2: Boot WARN emitted when a module declares `Migrations` (non-empty `fs.FS`),
      `Jobs`, or `Webhooks`; Phase-2 stub calls panic-guarded with `debug.Stack()`.
- [x] R3: `nucleus serve --without-defaults` flag wired to `app.WithoutDefaults()`;
      documented in `CLI_CONTRACT_MATRIX.md` and `CLI_BEST_PRACTICES.md`.
- [x] R4: `cors_origins []string` + `cors_allow_credentials bool` config keys live;
      `router.WithCORSCredentials` exported; allow-all + credentials reflects origin
      (never emits literal `*`), matching FW-6 logic in `pkg/router/corsmw.go`.
- [x] R5: Both `rbacPolicyPath` (in `pkg/app/app.go`) and `doctor` command recognise
      `rbac_policy.csv`, `config/rbac_policy.csv`, and `rbac/rbac_policy.csv`.
- [x] R6/R7/R8: ADR-013 written; scaffold templates updated with CORS + first-boot
      admin password comments; two-layout (flat vs feature-folder) docs added.
- [x] Contract baseline regenerated (additions-only: `app.Config.CORSOrigins`,
      `app.Config.CORSAllowCredentials`, `router.WithCORSCredentials`).
- [x] 17 new tests across 4 files green.
- [x] Website drift guard clean; `npm run build` green.
- [x] CI Required Gate green on PR #90; squash-merged.
- [x] Security advisories resolved (PR #91 merged first): `react-router-dom`
      7.14.0→7.17.0; Go 1.26.3→1.26.4 across `go.mod`, `go.work`, admin submodules,
      CI workflow, and 4 active docs.

## What was done (chronological)

1. **2026-05-31** — Blind-written branch `fix/readiness-2026-05-31` produced in
   Cowork sandbox (no Go toolchain). Four logical commits: framework R1/R2/R4/R5;
   CLI R3/R5; docs + ADR + website; state. NOT pushed (no GitHub creds in sandbox).

2. **2026-06-06** — Code/maintainer session:
   - `go build ./...` / `go vet ./...` / `go test ./...` all green on go1.26.4.
   - Contract freeze baseline regenerated (additions-only, 3 symbols).
   - 17 new tests added: `pkg/nucleus/readiness_warn_test.go` (R1/R2 WARN +
     panic-guard), `pkg/router/cors_credentials_test.go` (R4 `WithCORSCredentials`
     wiring), `pkg/app/rbac_discovery_test.go` (R5 discovery), `internal/cli/serve_test.go`
     (R3 flag validation).
   - Code-reviewer finding applied: `debug.Stack()` added to `safeStubCall` in
     `pkg/nucleus/nucleus.go`. Reviewer's CORS "blockers" were false positives —
     the existing FW-6 logic in `pkg/router/corsmw.go` already handles
     allow-all + credentials safely; `corsAllowCredentials: true` default preserved
     as deliberate and spec-safe.
   - CLI docs updated: `docs/reference/CLI_CONTRACT_MATRIX.md` (serve row) and
     `docs/reference/CLI_BEST_PRACTICES.md`.
   - Website drift guard clean; `npm run build` green.
   - PR #90 opened (`fix/readiness-2026-05-31` → main).

3. **2026-06-06** — Two unrelated security advisories broke required CI gates on
   every open PR:
   - `react-router` npm advisory: RCE/open-redirect/DoS vs 7.0.0–7.14.2.
   - Go stdlib govulncheck: GO-2026-5037/5038/5039 (crypto/x509, mime,
     net/textproto), fixed in go1.26.4.
   - PR #91 opened and merged first: bumped `react-router-dom` 7.14.0→7.17.0
     (lockfile-only, within `^7.1.0`); bumped Go 1.26.3→1.26.4 across `go.mod`,
     `go.work`, three `admin/*` submodule `go.mod` files, seven version refs in
     `.github/workflows/ci.yml`, and four active docs
     (`docs/QUICKSTART.md`, `docs/reference/DEVELOPER_MANUAL.md`, `README.md`,
     `website/docs/getting-started/installation.md`). Historical audit/iteration
     snapshots left untouched per CLAUDE.md §9.
   - Verified locally on go1.26.4: build/vet/test green, `govulncheck` 0 vulns,
     `npm audit` 0 vulns. Merged #91 → main (`33a3ae9`).

4. **2026-06-06** — Rebased `fix/readiness-2026-05-31` onto updated main (clean,
   no conflicts); re-verified on go1.26.4; force-pushed. CI green → squash-merged
   PR #90 (`2b2f7dc`). Both feature branches deleted (local + remote).

## Deferred follow-ups (carried to future iterations)

- Wire `Module.Migrations` into `nucleus migrate` (ADR-013 Phase 2+).
- Implement Jobs/Webhooks (ADR-013 Phase 2+).
- Unify `generate resource` output to the feature-folder layout.
- Record `cors_origins[]` / `cors_allow_credentials` in
  `contracts/baseline/config_key_patterns.txt` (deliberate curated add).

## Key files changed

- `pkg/nucleus/nucleus.go`, `pkg/nucleus/module.go` — R1/R2 WARN guards
- `internal/cli/serve.go` — R3 flag
- `pkg/app/config.go`, `pkg/app/app.go` — R4 CORS keys, R5 RBAC discovery
- `pkg/router/router.go`, `pkg/router/middleware.go` — R4 CORS wiring
- `internal/cli/doctor.go` — R5 discovery
- `internal/cli/scaffold/templates/{api,mvc}/nucleus.yml.tmpl` — R6/R7 scaffold comments
- `contracts/baseline/api_exported_symbols.txt` — baseline regen
- `docs/adrs/ADR-013-real-app-readiness.md` — design record
- `docs/reference/CLI_CONTRACT_MATRIX.md`, `docs/reference/CLI_BEST_PRACTICES.md` — R3 docs
- `docs/reference/CONFIG_KEY_REGISTRY.md` — R4 keys
- `docs/reference/PROJECT_LAYOUT.md` — two-layout docs
- `website/docs/getting-started/project-structure.md`, `website/docs/cli/overview.md` — site mirror
- `go.mod`, `go.work`, `admin/*/go.mod`, `.github/workflows/ci.yml` — Go 1.26.4 bump
- `CHANGELOG.md` — user-facing entries
