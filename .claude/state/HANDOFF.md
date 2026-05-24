# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    None active. Last completed: ADR-010 §2 layer-3 field-semantic validation — COMPLETE, committed + pushed (completes the five-layer config validator).
BRANCH:       main (clean working tree once the state commit lands; in sync with origin/main).
LAST COMMIT:  ffeb609 feat(nucleus): config field-semantic validation (ADR-010 §2 layer 3)  (followed by the `chore(state): close ADR-010 layer-3 validation iteration` commit that carries this file).
STATUS:       No active iteration, no pending work. The layer-3 validator is committed (ffeb609) and pushed; this file is written POST-commit so the next /resume sees the true shipped state. Recent shipped arc on origin/main: ADR-010 Phase 3a/3b/3.1, Oracle identifier-casing (ADR-011), session_cookie_secure, Oracle multi-block (db.ExecScript), and ADR-010 §2 layer-3 validation (the five-layer config validator is now complete).
NEXT STEP:    Owner selects the next iteration, then confirm the goal before writing code. Prioritised candidates (see CURRENT_ITERATION.md):
  1. ADR-010 Phase 4 — docs-sync + website + new reference applications under a freshly-scoped examples/. Target v0.9.X. The natural arc-closing piece for ADR-010.
  2. ADR-010 §2 layer 4 — referential validation (module Requires → DB aliases; auth providers; observability exporters; the carried-forward smtp_port>0-when-mail_driver=smtp cross-field check). Penultimate validator layer.
  3. Cloud Secrets Provider plugin extraction (AWS → GCP → Azure → Vault); removes the AWS SDK from core go.mod.
BLOCKERS:     none.
FILES OF INTEREST:
  - .claude/state/CURRENT_ITERATION.md — full prioritised candidate list + carry-forward follow-ups.
  - pkg/nucleus/validate_semantics.go — the layer-3 validator (context for ADR-010 §2 layer 4, the next validator layer).
OPEN FOLLOW-UPS (low priority, tracked in CURRENT_ITERATION.md):
  - ADR-010 §2 layer 4 referential validation incl. smtp_port>0-when-mail_driver=smtp (candidate #2).
  - Route admin-bootstrap PL/SQL through db.ExecScript; tighten Oracle DDL-auto-commit vs Migrator tx; optionally export the ExecScript execer interface.
  - SameSite=None requires Secure=true startup validation.
  - CI required-vs-exploratory reconciliation for mssql+oracle (ci.yml vs CI_MATRIX.md).
  - Oracle reserved-word + dotted-identifier hardening (TODO in pkg/model/meta.go).
NOTES:
  - Loop-break: this handoff was written AFTER the feat commit (ffeb609) and is committed in the state commit alongside it, so it describes reality — no stale "uncommitted" note for the next /resume.
  - Layer-3 design: hand-written validateSemantics (not pkg/validate), nucleus-layer only (FromConfigFile + Run), enum sets pinned from the consumers, empty/zero/Port:0 allowed, previously-silent misconfigs now fail loud at load. Additive contract (+ErrInvalidConfigValue), freeze + firewall clean.

Updated: 2026-05-24
