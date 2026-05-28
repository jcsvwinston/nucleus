# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Gate `admin.EnsureBootstrapAdminUser` behind `!o.skipDefaults` so that
`app.WithoutDefaults()` no longer leaks the admin bootstrap user, table, or
one-time password.

## Scope

- in: `pkg/app/app.go`, `pkg/app/app_test.go`, `CHANGELOG.md`
- out: CLI changes, router changes, ADR-010 layer 5, anything unrelated to the
  admin-bootstrap guard

## Acceptance criteria

- [x] `admin.EnsureBootstrapAdminUser` call in `New()` is inside
      `if !o.skipDefaults { … }`.
- [x] `TestAppNew_WithoutDefaults_DoesNotBootstrapAdmin` passes (asserts
      `nucleus_admin_users` table absent when `WithoutDefaults()` is used).
- [x] Full iteration loop green: architect-reviewer PASS, code-reviewer
      NITS-only (addressed), security-auditor PASS, contract-guardian PASS
      (freeze 6/6), test-runner full suite green.
- [x] `CHANGELOG.md` carries a `[Unreleased] → Security` entry (patch bump).

## Status

### Done

- 2026-05-28 — Wrapped `admin.EnsureBootstrapAdminUser` in `New()` behind
  `if !o.skipDefaults` (`pkg/app/app.go`).
- 2026-05-28 — Added `TestAppNew_WithoutDefaults_DoesNotBootstrapAdmin`
  regression test (`pkg/app/app_test.go`).
- 2026-05-28 — Added `[Unreleased] Security` entry to `CHANGELOG.md`.
- 2026-05-28 — Full iteration loop ran green (architect PASS, code PASS,
  security PASS, contract-guardian PASS freeze 6/6, test-runner green).

### In progress

- PR open / CI gate pending on branch `fix/without-defaults-admin-bootstrap-leak`.

### Blocked

- (none)

## Files of interest

- `pkg/app/app.go` (~255-300)
- `pkg/app/app_test.go`
- `CHANGELOG.md`

## Notes / decisions log

- 2026-05-28 — architect-reviewer, code-reviewer, and security-auditor all
  flagged (SHOULD, non-blocking) that the admin-auth DB resolution at
  `pkg/app/app.go` ~255-271 (resolving `admin_auth_database` →
  `adminAuthSQLDB`) still runs unconditionally under `WithoutDefaults()`.
  Harmless today (no new DB connection; only consumers are already gated),
  but a core-only app that sets `admin_auth_database` to a bad alias will
  fail at startup. Added as a follow-up item in the backlog below.

---

## Carry-forward backlog (from 2026-05-28-branch-protection)

> **IMPORTANT — `main` is now PR-only (branch protection active as of
> 2026-05-28).** Every change — including `.claude/state/*` and `docs/*` —
> must go through: create branch → push → `gh pr create` → wait for
> `CI Required Gate` green (~7–20 min, full matrix incl. MSSQL/Oracle) →
> self-merge (`gh pr merge --squash --delete-branch`) → `git checkout main
> && git pull`. Direct `git push origin main` is REJECTED.

### Framework polish / follow-ups

- **Gate admin-auth DB resolution behind `!o.skipDefaults`.**
  `pkg/app/app.go` ~255-271 resolves `admin_auth_database` →
  `adminAuthSQLDB` unconditionally. A core-only app that sets
  `admin_auth_database` to a bad alias will fail at startup even when
  `WithoutDefaults()` is used. Move this resolution inside the
  `!o.skipDefaults` branch (or guard with an early return). Flagged
  SHOULD (non-blocking) by architect + code-reviewer + security-auditor
  on 2026-05-28.

### Framework bugs (P2)

- **P2 — `Router.Resource("")` under a module `Prefix` panics at startup.**
  `pkg/nucleus/router.go` — `joinPath` should yield `/` (not `""`) when
  prefix+path are both empty.

### ADR-010 §2 layer 5 (module-specific config binding/validation)

Completes the five-layer validator; layer 4 (referential) shipped 2026-05-26.

### Other carry-forward

- Cloud Secrets Provider plugin extraction (AWS/GCP/Azure/Vault out of core).
- SchemaDrift column-type comparison + `docs/guides/MODELING_MULTI_DATABASE.md`.
- `go mod tidy` unblock — resolve the `admin/proto` replace-directive.
- `tasks.Manager` struct→interface DEP (optional DEP-2026-004).
- Audit §7 minors: 503 path test for `/healthz`; endpoints-parity doc-parsing;
  `pkg/health/{db,redis,storage}.go` tests.

### Deferred carry-forwards (not blocking)

_Oracle multi-block AutoMigrate (2026-05-24):_
- Route admin-bootstrap PL/SQL through `db.ExecScript`. (architect NIT.)
- Oracle DDL auto-commit vs the Migrator transaction.

_session_cookie_secure (2026-05-23):_
- Startup validation: `SameSite=None` requires `Secure=true`. (security-auditor
  LOW.) `pkg/auth/session.go` should reject the combo.

_Oracle identifier-casing (2026-05-23):_
- CI governance reconciliation (mssql + oracle): required vs exploratory.
- Oracle reserved-word + dotted-identifier hardening.

_Phase 3b / observability (2026-05-22/23):_
- GCS credential-redaction forward-compat.
- Reverse-proxy hardening note for `/_/config`.
- Relocate `pkg/observability` to `internal/` post-v1.0.
- `Runtime.AutoMigrate` production guard.

_ADR-010 Phase 1 (still open):_
- Service-shutdown timeout — `nucleus.Run`'s `wg.Wait()` has no deadline.
- `Lifecycle.OnShutdown` context deadline — no bound.
- `joinPath` double-slash collapse — `routerAdapter.joinPath`.

_Internal-docs (low-priority):_
- `DETAILED_TUTORIAL.md` flat-handler style predates `nucleus.Module` pattern.
- `DEVELOPER_MANUAL.md §5.3` references `internal/contracts`.
