# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Enable branch protection on `main` — COMPLETE.
BRANCH:       chore/handoff-branch-protection (closeout branch; not yet merged).
              origin/main HEAD is a79c413 (PR #79 squash merge).
LAST COMMIT:  a79c413 — PR #79 squash merge (CI_MATRIX doc + v0.8.0 state archive)
STATUS:       done — branch protection active on main since 2026-05-28.
              enforce_admins=true, required check "CI Required Gate" strict=true,
              required_approving_review_count=0, required_conversation_resolution=true.
              Previously main had NO protection (HTTP 404). Solo-PR flow proven
              end-to-end via PR #79 (all 10 CI lanes green, self-merged, 0 approvals).
NEXT STEP:    Pick next backlog item. Strongest candidates:
              1. P1 — WithoutDefaults() admin-bootstrap leak:
                 pkg/app/app.go:~272 calls admin.EnsureBootstrapAdminUser
                 unconditionally before the !o.skipDefaults guard. Move call inside.
              2. P2 — Router.Resource("") panic under module Prefix:
                 pkg/nucleus/router.go — joinPath should yield "/" not "" when
                 prefix+path are both empty.
              3. ADR-010 §2 layer 5 — module-specific config validation (last layer).
              REMEMBER: all of these must go through the PR workflow below.
BLOCKERS:     none.
FILES OF INTEREST: pkg/app/app.go (~272), pkg/nucleus/router.go,
              docs/governance/CI_MATRIX.md, docs/iterations/2026-05-28-branch-protection.md
NOTES:        *** CRITICAL — main is now PR-only for EVERYONE including the maintainer ***
              Direct `git push origin main` is REJECTED by GitHub.
              Every change (even .claude/state/*, docs/*) must follow:
                1. git checkout -b <branch>
                2. git push -u origin <branch>
                3. gh pr create
                4. Wait for "CI Required Gate" green (~7-20 min; full matrix incl.
                   live MSSQL/Oracle; GitHub cannot path-exclude required checks)
                5. gh pr merge --squash --delete-branch
                6. git checkout main && git pull
              --approvals 0 is deliberate: single-maintainer repo; 1 would lock the
              maintainer out (enforce_admins blocks direct push + no second reviewer).
              Full iteration archive at docs/iterations/2026-05-28-branch-protection.md.

Updated: 2026-05-28
