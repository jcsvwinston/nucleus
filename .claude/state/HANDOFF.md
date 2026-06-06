# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Real-app readiness remediation (R1-R8) per ADR-013 — COMPLETE AND MERGED.
              No active iteration. Start a new one.
BRANCH:       main (no open feature branch; fix/readiness-2026-05-31 squash-merged
              and deleted; security-fix branch from PR #91 merged and deleted)
LAST COMMIT:  2b2f7dc — squash-merge of PR #90 (fix/readiness-2026-05-31)
              Before that: 33a3ae9 — squash-merge of PR #91 (security gate unblock)
STATUS:       done — all acceptance criteria met; main is clean and green on go1.26.4
NEXT STEP:    No carried-over work. Start the next iteration from a clean slate.
              Known deferred follow-ups from ADR-013 (record in CURRENT_ITERATION.md
              when prioritised):
                1. Wire Module.Migrations into `nucleus migrate` (ADR-013 Phase 2+).
                2. Implement Jobs/Webhooks (ADR-013 Phase 2+).
                3. Unify `generate resource` to the feature-folder layout.
                4. Add cors_origins[] / cors_allow_credentials to
                   contracts/baseline/config_key_patterns.txt (deliberate curated add).
BLOCKERS:     none
FILES OF INTEREST:
              docs/iterations/2026-06-06-readiness-r1-r8-adr013.md (archived record),
              docs/adrs/ADR-013-real-app-readiness.md (design decisions),
              docs/audits/2026-05-31-real-app-readiness.md (original findings + runbook),
              contracts/baseline/api_exported_symbols.txt (updated: +3 symbols),
              CHANGELOG.md (Unreleased entries for R1-R8 + Go 1.26.4 + react-router bump)
NOTES:        Security advisory context: GO-2026-5037/5038/5039 fixed by go1.26.4;
              react-router-dom bumped to 7.17.0 (within ^7.1.0). Both resolved in PR #91
              before readiness PR #90 could merge. Future PRs start from a clean advisory
              baseline as of go1.26.4 + react-router-dom 7.17.0.

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

Updated: 2026-06-06
