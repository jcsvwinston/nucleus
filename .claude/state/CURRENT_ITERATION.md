# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

_(No active iteration — awaiting owner direction.)_

## Scope

_(Not set.)_

## Acceptance criteria

_(Not set.)_

## Status

### Done

_(Nothing in-flight this session — this file was reset on 2026-05-26 after
the scaffolder-cleanup arc was confirmed complete and all archives verified.)_

### In progress

_(None.)_

### Blocked

_(None.)_

---

## Candidate next steps (priority order, pending owner confirmation)

### Tag v0.6.0 (recommended first)

The GoFrame→Nucleus rename is done; `pkg/nucleus.New()` is the fluent entry
point; ADR-003 is merged; the scaffolder emits a clean empty skeleton.
Tagging `v0.6.0` closes the post-rename roadmap milestone and unblocks the
scaffolder smoke: `nucleus new ... && go mod tidy` only resolves correctly
after `v0.6.0` is published on the module proxy.

### Framework bugs (surfaced by running `examples/mvc_api` end-to-end, 2026-05-24)

- **P1 — `WithoutDefaults()` leaks the admin bootstrap user.**
  `pkg/app/app.go:~272` calls `admin.EnsureBootstrapAdminUser`
  UNCONDITIONALLY, before the `if !o.skipDefaults` guard. Any service built
  with `.WithoutDefaults()` still creates the `nucleus_admin_users` table and
  prints a generated admin password to stderr on first boot — a correctness +
  minor security-hygiene bug affecting every lightweight service.
  Fix: move the `EnsureBootstrapAdminUser` call inside the `!o.skipDefaults`
  branch. `pkg/app`-only change; no public-contract change.
  Note (verified on live mvc_api, 2026-05-24): the admin *panel* IS correctly
  gated by `skipDefaults` — this is a leaked-orphaned-user bug (row + password
  on stderr), NOT an exposed-admin-portal bug.

- **P2 — `Router.Resource("")` under a module `Prefix` panics at startup.**
  `pkg/nucleus/router.go` `Resource("")` → `joinPath("")=""` →
  `mux.Get("")` → invalid `"GET "` pattern → `net/http.ServeMux` panic.
  Fix: `joinPath` should yield `/` (not `""`) when prefix+path are both empty,
  or `Resource` should normalise/reject an empty base.

### ADR-010 §2 layer-4 referential validation

Cross-field checks: module `Requires` → configured DB aliases; auth providers;
observability exporters; `smtp_port > 0` when `mail_driver=smtp`. The
penultimate validator layer (layer 5 = module-specific).

### Cloud Secrets Provider plugin extraction

Extract AWS/GCP/Azure/Vault adapters out of core `go.mod`; removes the AWS SDK
from core.

### SchemaDrift column-type comparison + usage guide

Cross-dialect type-family compatibility table in `SchemaDrift`; end-to-end
usage guide in `docs/guides/MODELING_MULTI_DATABASE.md`.

### `go mod tidy` unblock

Resolve the `admin/proto` replace-directive that prevents a clean `go mod tidy`.
(Also addressed once v0.6.0 is tagged, per the scaffolder smoke caveat.)

### `tasks.Manager` struct→interface DEP

Optional DEP-2026-004; backward-compatible interface extraction.

### Audit §7 minors

503 path test for `/healthz`; endpoints-parity doc-parsing;
`pkg/health/{db,redis,storage}.go` tests.

---

## Carry-forward follow-ups (deferred, not blocking)

_Oracle multi-block AutoMigrate (2026-05-24):_

- **Route admin-bootstrap PL/SQL through `db.ExecScript`.** (architect NIT.)
  `pkg/admin/bootstrap_admin.go`'s `ensureBootstrapAdminUsersTable` Execs a
  single-block Oracle PL/SQL DDL directly (safe today) but bypasses
  `ExecScript`, so a future second block would silently fail. Route it through
  `ExecScript` to make "all Oracle PL/SQL through the splitter" unconditional.
- **Oracle DDL auto-commit vs the Migrator transaction.** `applyMigration`/
  `rollbackMigration` wrap DDL + tracking-row inserts in a `*sql.Tx`, but
  Oracle DDL auto-commits, so they are not atomic on Oracle. Pre-existing;
  carries a caveat comment. Tightening (non-transactional DDL + separate DML
  tx for Oracle) is a follow-up.

_session_cookie_secure (2026-05-23):_

- **Startup validation: `SameSite=None` requires `Secure=true`.** (security-auditor
  LOW.) `pkg/auth/session.go` does not reject `session_cookie_samesite: none` +
  `session_cookie_secure: false`; browsers drop such cookies silently. A
  validation error in `NewSessionManager`/`buildSessionManager` would catch it.

_Oracle identifier-casing (2026-05-23):_

- **CI governance reconciliation (mssql + oracle): required vs exploratory.**
  (architect-reviewer PRE-EXISTING.) `ci.yml` lists both in `ci-required-gate.needs`
  but `docs/governance/CI_MATRIX.md` classifies both as "exploratory, non-blocking".
  Owner call: promote both to required in CI_MATRIX, or remove from the gate.
- **Oracle reserved-word + dotted-identifier hardening.** (architect WARN-2 +
  security-auditor LOW.) `isValidIdentifierLike` (`pkg/model/meta.go`, carries a
  `TODO(ADR-011 follow-up)`) accepts Oracle reserved words (`comment`/`number`/
  `date`/…) and accepts `.` in table names. Fix: selective quoting at the
  `oracleIdentifier` choke point + split the allowlist (name = no dot; FK ref =
  dot allowed).

_Phase 3b / observability (2026-05-22/23):_

- **GCS credential-redaction forward-compat.** (security-auditor.) If a future
  iteration wires `pkg/storage.GCSConfig` into `app.Config`, the nested
  `credentials.value` leaf is not in `observe.DefaultRedactedKeys()` and would
  leak via `/_/config` + logs. Add `value` (or a structural rule) to the
  canonical set in the same PR.
- **Reverse-proxy hardening note for `/_/config`.** `docs/guides/DEPLOYMENT_GUIDE.md`
  production checklist could note that `/_/config` (like `/metrics`) should be
  blocked at the reverse-proxy for non-internal traffic.
- **Relocate `pkg/observability` to `internal/` post-v1.0.** Internal-facing
  plumbing; `experimental` buys time, but the eventual right move is relocation
  during the Phase 4 modularization pass.
- **`Runtime.AutoMigrate` production guard.** (architect WARN, Gap-1.) Optional
  `slog.Warn` when `NUCLEUS_ENV=production` and a module calls `rt.AutoMigrate`.
  Low-cost, future slice.

_ADR-010 Phase 1 (still open):_

- **Service-shutdown timeout** — `nucleus.Run`'s `wg.Wait()` after
  `cancelServices()` has no deadline.
- **`Lifecycle.OnShutdown` context deadline** — derived from
  `context.Background()` with no bound.
- **`joinPath` double-slash collapse** — `routerAdapter.joinPath` produces
  `/x/x/123` when `prefix=/x` and `p=/x/123`.

_Internal-docs (doc-updater, low-priority, not blocking):_

- `DETAILED_TUTORIAL.md` still composes the app in a flat `handlers/`/`models/`
  style predating the `nucleus.Module` pattern — a deeper rewrite would align it
  beyond merely fixing false claims.
- `DEVELOPER_MANUAL.md §5.3` references `internal/contracts` — accurate as a
  framework feature; could add a "you add this when ready" note.

---

## Files of interest

- (TBD — no active iteration)
- `pkg/app/app.go` (~272) — P1 `WithoutDefaults()` admin-bootstrap leak
- `pkg/nucleus/router.go` — P2 `Resource("")` panic under `Prefix`
- `examples/mvc_api/` — the only reference app

---

## Notes / decisions log

- 2026-05-26 — Scaffolder-cleanup arc confirmed COMPLETE and ARCHIVED. No active
  iteration. HANDOFF reconciled from stale 2026-05-24 copy (pointed at `9e27243`)
  to HEAD (`c80def2`). `CURRENT_ITERATION.md` reset to empty-iteration state;
  live backlog and carry-forward items carried forward and pruned.
- 2026-05-25 — Scaffolder-cleanup arc closed: string-demo → embedded templates →
  empty skeleton (no hardcoded example in core). All committed + pushed. Internal-
  doc skeleton sweep also done (`09f2067`). Archives at
  `docs/iterations/2026-05-25-scaffolder-skeleton.md` +
  `docs/iterations/2026-05-25-scaffolder-rearch-embed.md`.
- 2026-05-24 — ADR-010 Phase 4 Slice 2 (scaffolder/example/docs convergence)
  complete. Phase 4 Gap-1 (`nucleus.Runtime`) complete. Website include-from-source
  wired (`remark-code-import`). All pushed.
- 2026-05-24 — ADR-010 Phase 4 Slice 1 (`examples/mvc_api` reference app) complete.
  Running the server (not just unit tests) caught a startup panic + wrong doc
  commands. Framework gaps P1/P2/Gap-1 recorded as follow-ups.
