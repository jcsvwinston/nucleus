# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Remediate the 2026-05-29 exhaustive audit (CLI + framework + docs) — COMPLETE (PR #82 merged)
BRANCH:       main
LAST COMMIT:  64897f4 "Audit remediation (2026-05-29): functional CLI + framework + doc fidelity (#82)"
STATUS:       audit-remediation iteration shipped to main; all 12 CI checks green; no work in progress
NEXT STEP:    pick the next iteration — strongest candidate is ADR-010 §2 layer 5 (module-specific
              config binding/validation, completes the five-layer validator; layer 4 shipped
              2026-05-26). Also pending maintainer decisions: examples/ + CLAUDE.md directory-map
              reconciliation (only examples/mvc_api is a tracked Go app; other three trees are
              local/untracked scaffolding); Block 8 audit-roadmap leftovers in
              docs/audits/2026-05-29-exhaustive-audit.md.
BLOCKERS:     none
FILES OF INTEREST: docs/audits/2026-05-29-exhaustive-audit.md (Block 8 leftovers),
              pkg/app/ (ADR-010 layer 5 — five-layer validator, next layer to implement),
              CLAUDE.md (directory map vs examples/ trees),
              .claude/state/CURRENT_ITERATION.md (backlog for next iteration owner)
NOTES:
The audit-remediation iteration (PR #82) landed 11 commits / 37 files covering
FW-1…6, CLI-1…4, DOC-1/2/3, regenerated contract baselines, CHANGELOG [Unreleased]
entries, +6 new test files. main advanced 1702770..64897f4 on 2026-05-29T17:53:23Z.

The state-file edits produced by THIS handoff (HANDOFF.md, CURRENT_ITERATION.md,
docs/iterations/2026-05-29-audit-remediation.md) are intentionally uncommitted on
main. They must be committed via the same branch+PR flow — /handoff reserves
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
