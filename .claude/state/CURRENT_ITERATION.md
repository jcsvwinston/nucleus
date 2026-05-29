# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

<awaiting owner direction — no active iteration>

## Scope

- in: …
- out: …

## Acceptance criteria

- [ ] …

## Status

### Done
- (none yet)

### In progress
- (none — awaiting owner direction)

### Blocked
- (none)

## Files of interest

- (to be filled in when iteration is scoped)

## Notes / decisions log

- 2026-05-29 — Iteration "Remediate the 2026-05-29 exhaustive audit" COMPLETE.
  PR #82 squash-merged; `main` is at commit `64897f4`. Archived to
  `docs/iterations/2026-05-29-audit-remediation.md`.

---

## Backlog (carry-forward from 2026-05-29-audit-remediation)

### Pending on maintainer (Carlos)

- **Decide the `examples/` + `CLAUDE.md` directory-map question.** Only
  `examples/mvc_api` is a tracked Go app (in the root module, built/tested by
  CI). The other three example trees are local/untracked scaffolding that does
  not match what `CLAUDE.md`'s directory map advertises (`mvc_api`,
  `fleetmanager`, `ecommerce_dashboard`, `showcase_demo`, `plugins/…`). Decide
  whether to track them, drop them, or correct the directory map. NOTE: editing
  `CLAUDE.md` is a self-contained housekeeping change — route as its own branch
  + PR.
- **Block 8 leftovers** from the audit roadmap
  (`docs/audits/2026-05-29-exhaustive-audit.md`) remain unstarted — schedule
  as a follow-up iteration.

### Framework / ADR follow-ups

- **ADR-010 §2 layer 5** — module-specific config binding/validation. Completes
  the five-layer validator; layer 4 (referential) shipped 2026-05-26. This is
  the strongest next candidate.
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

_Oracle identifier-casing (2026-05-23):_
- CI governance reconciliation (mssql + oracle): required vs exploratory.
- Oracle reserved-word + dotted-identifier hardening.

_Phase 3b / observability (2026-05-22/23):_
- GCS credential-redaction forward-compat.
- Reverse-proxy hardening note for `/_/config`.
- Relocate `pkg/observability` to `internal/` post-v1.0.
- `Runtime.AutoMigrate` production guard.

_ADR-010 Phase 1 (remaining):_
- Service-shutdown timeout — `nucleus.Run`'s `wg.Wait()` has no deadline.
  (NOTE: the app-level `Lifecycle.OnShutdown` deadline shipped as FW-6 in
  2026-05-29-audit-remediation; the `wg.Wait()` service-shutdown bound is the
  still-open sibling.)

_Internal-docs (low-priority):_
- `DETAILED_TUTORIAL.md` flat-handler style predates `nucleus.Module` pattern.
- `DEVELOPER_MANUAL.md §5.3` references `internal/contracts`.

---

> **IMPORTANT — `main` is PR-only (branch protection active since 2026-05-28).**
> Every change — including `.claude/state/*` and `docs/*` — must go through:
> create branch → push → `gh pr create` → wait for `CI Required Gate` green
> (~7–20 min, full matrix incl. live MSSQL/Oracle) → self-merge
> (`gh pr merge --squash --delete-branch`) → `git checkout main && git pull`.
> Direct `git push origin main` is REJECTED.
> Settings: `enforce_admins=true`, required check "CI Required Gate"
> `strict=true`, `required_approving_review_count=0`,
> `required_conversation_resolution=true`.
