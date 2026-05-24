# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    None active. Two iterations COMPLETE and committed but UNPUSHED — see NEXT STEP.
BRANCH:       main (clean working tree apart from this handoff's state edits; 2 commits AHEAD of origin/main — unpushed).
LAST COMMIT:  76f1d4c docs(website): fix 3 P0 inaccuracies + expand configuration/models-and-database/intro/principles
STATUS:       No active iteration. The working tree is clean, but `main` is 2 commits ahead of origin/main (unpushed): `a5ad7e6` (docs-content-verifier subagent + CLAUDE.md §9/§10 anti-falsehood discipline) and `76f1d4c` (website P0-inaccuracy fixes). These were committed in a session after the layer-3 push and did NOT update the live state files, so this handoff reconciled them from `git log`. The layer-3 validator (ffeb609 + 9412807) is already on origin. RECONCILE RULE confirmed once more: trust `git log` over a stale state file.
NEXT STEP:    PUSH the 2 unpushed commits: `git push origin main` (origin/main is at 9412807; local HEAD is 76f1d4c). This handoff also leaves uncommitted edits to `.claude/state/CURRENT_ITERATION.md` + `HANDOFF.md` (reconciling the website-audit work into the state) — commit them as `chore(state): reconcile website-audit iteration into state` and push. After that, the owner selects the next iteration. Prioritised candidates (see CURRENT_ITERATION.md):
  1. ADR-010 Phase 4 — docs-sync + website + new reference applications under a freshly-scoped examples/ (now that the website P0 falsehoods are fixed and docs-content-verifier guards body content).
  2. ADR-010 §2 layer 4 — referential validation (module Requires → DB aliases; auth providers; observability exporters; the smtp_port>0-when-mail_driver=smtp cross-field check).
  3. Cloud Secrets Provider plugin extraction.
BLOCKERS:     none.
FILES OF INTEREST:
  - .claude/state/CURRENT_ITERATION.md — full candidate list + carry-forward follow-ups; "most recent completed" now leads with the unpushed website-audit iteration.
  - docs/iterations/2026-05-24-website-audit-y-process-hardening.md — the archive of the unpushed website-audit iteration (already committed in a5ad7e6).
  - .claude/agents/docs-content-verifier.md — NEW subagent (committed, unpushed): body-content fact-checker for Go symbols / YAML keys / Go-version claims. CLAUDE.md §9/§10 now MANDATE it before publishing any doc page — doc-updater + website-curator must hand off to it.
NOTES:
  - docs-content-verifier is now available as an Agent type and is wired into the doc/website iteration loop (CLAUDE.md §9 anti-falsehood discipline, §10 specialized-subagent dispatch policy). Any future ADR-010 Phase 4 / docs / website work MUST route body-content verification through it.
  - Recent shipped arc on origin/main (pushed): ADR-010 Phase 3a/3b/3.1, Oracle identifier-casing (ADR-011), session_cookie_secure, Oracle multi-block (db.ExecScript), ADR-010 §2 layer-3 validation. UNPUSHED on top: the website-audit + process-hardening iteration (a5ad7e6 + 76f1d4c).
  - Open low-priority follow-ups: ADR-010 §2 layer 4 referential (incl. smtp_port cross-field); admin-bootstrap via db.ExecScript; Oracle DDL-auto-commit vs Migrator tx; export ExecScript execer; SameSite=None+Secure validation; CI required-vs-exploratory reconciliation (mssql+oracle); Oracle reserved-word + dotted-identifier hardening.

Updated: 2026-05-24
