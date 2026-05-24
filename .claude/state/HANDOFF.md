# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    None active. Last completed: Oracle multi-block AutoMigrate execution (PR #78 follow-up #2) — COMPLETE, committed + pushed.
BRANCH:       main (clean working tree once the state commit lands; in sync with origin/main).
LAST COMMIT:  d46d29c fix(db): Oracle multi-block migration execution via ExecScript  (followed by the `chore(state): close Oracle multi-block AutoMigrate iteration` commit that carries this file).
STATUS:       No active iteration, no pending work. The Oracle multi-block fix is committed (d46d29c) and pushed; this file is written POST-commit deliberately so the next /resume sees the true shipped state (breaking the prior stale-"uncommitted"-handoff loop). The recent shipped arc on origin/main: ADR-010 Phase 3a/3b/3.1, Oracle identifier-casing (ADR-011), session_cookie_secure secure-by-default, and Oracle multi-block AutoMigrate (db.ExecScript).
NEXT STEP:    Owner selects the next iteration, then confirm the goal before writing code. Prioritised candidates (see CURRENT_ITERATION.md):
  1. ADR-010 §2 layer 3 — field-semantic validation (ranges/enums/parseable durations). Standalone follow-up on the now-complete merge engine.
  2. ADR-010 Phase 4 — docs-sync + website + new reference applications under a freshly-scoped examples/. Target v0.9.X.
  3. Cloud Secrets Provider plugin extraction (AWS → GCP → Azure → Vault); removes the AWS SDK from core go.mod.
BLOCKERS:     none.
FILES OF INTEREST:
  - .claude/state/CURRENT_ITERATION.md — full prioritised candidate list + carry-forward follow-ups.
  - pkg/db/exec_script.go — db.ExecScript (the just-shipped dialect-aware migration executor), context for the Oracle follow-ups below.
OPEN FOLLOW-UPS (low priority, tracked in CURRENT_ITERATION.md):
  - Route admin-bootstrap PL/SQL through db.ExecScript (single-block-safe today).
  - Tighten Oracle DDL-auto-commit vs the Migrator transaction (pre-existing; caveat-commented).
  - Optionally export the db.ExecScript execer interface.
  - SameSite=None requires Secure=true startup validation.
  - CI required-vs-exploratory reconciliation for mssql+oracle (ci.yml vs CI_MATRIX.md).
  - Oracle reserved-word + dotted-identifier hardening (TODO in pkg/model/meta.go).
NOTES:
  - Loop-break: the prior pattern was /handoff writing an "uncommitted" note that then got committed verbatim, so each /resume re-read a stale "owner must commit". This handoff was written AFTER the fix commit and is committed in the state commit alongside it, so it describes reality. Future sessions: still trust `git log` over this file if in doubt.
  - db.ExecScript design: `/` is a split directive consumed by the executor, never sent to go-ora; Oracle DDL auto-commits so per-block execution is correct. Additive contract (+db.ExecScript), freeze + firewall clean.

Updated: 2026-05-24
