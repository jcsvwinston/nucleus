# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    examples/ + CLAUDE.md directory-map reconciliation (audit OTH-1) — COMPLETE
              (PR #86 merged).
BRANCH:       main
LAST COMMIT:  ebb3ca3 — "chore(claude.md): reconcile examples/ directory-map with reality (#86)"
STATUS:       examples/ now mirrors reality (mvc_api only tracked); CLAUDE.md +
              examples-maintainer agent reflect today's working scope and the v0.9.X return
              plan; all 12 CI checks green; no work in progress.
NEXT STEP:    Pick the next iteration. Remaining backlog candidates:
              (1) Block 8 audit-roadmap leftovers
                  (docs/audits/2026-05-29-exhaustive-audit.md).
              (2) modules.* env-layer override — NUCLEUS_MODULES__* env vars not yet
                  supported; applyEnvLayer only applies schema-recognised keys today;
                  requires future ADR-010 amendment.
BLOCKERS:     none
FILES OF INTEREST: docs/audits/2026-05-29-exhaustive-audit.md (Block 8 leftovers),
              docs/iterations/2026-05-29-examples-reconciliation.md (immutable archive),
              .claude/state/CURRENT_ITERATION.md (stub + carry-forward backlog)
NOTES:
PR #86 changed only CLAUDE.md (directory-map row, Examples section, dispatch table)
and .claude/agents/examples-maintainer.md (description, Current state, Examples in scope,
output-contract example). +40/−27 net. ~305 MB / ~19,346 untracked local files were
also removed from disk (examples/{fleetmanager,ecommerce_dashboard,showcase_demo}
node_modules + runtime DBs + logs) — zero tracked files deleted. Semver impact: none
(internal operational docs only; no shipped behaviour changed).

The state-file edits produced by THIS handoff (HANDOFF.md, CURRENT_ITERATION.md,
docs/iterations/2026-05-29-examples-reconciliation.md) are intentionally uncommitted
on main. They must be committed via the same branch+PR flow — /handoff reserves
committing for the human.

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED by GitHub. Every change (even
.claude/state/*, docs/*) must follow:
  1. git checkout -b <branch>
  2. git push -u origin <branch>
  3. gh pr create
  4. Wait for "CI Required Gate" green (~7-20 min; full matrix incl. live
     MSSQL/Oracle; GitHub cannot path-exclude required checks)
  5. gh pr merge --squash --delete-branch
  6. git checkout main && git pull
--approvals 0 is deliberate: single-maintainer repo; 1 would lock the maintainer
out (enforce_admins blocks direct push + no second reviewer).

Updated: 2026-05-29
