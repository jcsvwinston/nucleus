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

- (none — awaiting owner direction)

## Notes / decisions log

- 2026-05-29 — ADR-010 §2 layer 5 COMPLETE. PR #84 squash-merged to main
  (commit 765e486). All 12 CI checks green. The five-layer FromConfigFile
  validator (ADR-010 §2) is now fully shipped. Archived to
  `docs/iterations/2026-05-29-adr010-layer5.md`.
- 2026-05-29 — Fresh slate. Carry-forward backlog preserved below.
- 2026-05-29 — examples/ + CLAUDE.md directory-map reconciliation (audit
  OTH-1) COMPLETE. PR #86 squash-merged to main (commit ebb3ca3). All 12 CI
  checks green. Archived to
  `docs/iterations/2026-05-29-examples-reconciliation.md`.
- 2026-05-29 — docs: align Go floor to 1.26 across shipped docs (audit Block
  8 — README go-version cross-check) COMPLETE. PR #88 squash-merged to main
  (commit 6ce4831). All 12 CI checks green. 7 files changed (+7/−6): README.md,
  docs/QUICKSTART.md, CONTRIBUTING.md, docs/reference/DEVELOPER_MANUAL.md,
  docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md, docs/guides/TESTING_GUIDE.md,
  CHANGELOG.md. Archived to
  `docs/iterations/2026-05-29-block8-go-version-docs.md`.
- 2026-05-29 — **MILESTONE: The entire 2026-05-29 exhaustive audit is now
  fully closed.** Blocks 1-8 all shipped: Blocks 1-7 via PR #82; Block 8
  (FW-6 via #82, OTH-1 via #86, OTH-2 via #88). No outstanding audit items
  remain. Full audit reference: `docs/audits/2026-05-29-exhaustive-audit.md`.

---

## Backlog (carry-forward as of 2026-05-29)

### Framework / ADR follow-ups

- Cloud Secrets Provider plugin extraction (AWS/GCP/Azure/Vault out of core).
- SchemaDrift column-type comparison + `docs/guides/MODELING_MULTI_DATABASE.md`.
- `go mod tidy` unblock — resolve the `admin/proto` replace-directive.
- `tasks.Manager` struct→interface DEP (optional DEP-2026-004).
- Audit §7 minors: 503 path test for `/healthz`; endpoints-parity doc-parsing;
  `pkg/health/{db,redis,storage}.go` tests.

### Deferred carry-forwards (not blocking)

_env-layer override of `modules.*` namespace (discovered 2026-05-29, ADR-010 layer 5):_
- `applyEnvLayer` only applies schema-recognised keys; `NUCLEUS_MODULES__*`
  env vars are not yet supported. Module config env override requires a future
  ADR-010 amendment.

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
