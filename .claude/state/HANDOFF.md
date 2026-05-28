# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    P1 — WithoutDefaults() admin-bootstrap leak — FIXED (PR pending)
BRANCH:       fix/without-defaults-admin-bootstrap-leak (off main @ 5c25dd5)
LAST COMMIT:  fix commit being created now; prior main HEAD is 5c25dd5 (#80)
STATUS:       P1 fixed, full iteration loop green, awaiting PR open + CI
NEXT STEP:    Open/merge the PR (wait for "CI Required Gate" green ~7-20 min),
              then pick next backlog item — strongest candidates:
              1. Admin-auth DB resolution gating: gate pkg/app/app.go ~255-271
                 (resolving admin_auth_database -> adminAuthSQLDB) behind
                 !o.skipDefaults (architect/code/security flagged SHOULD).
              2. P2 — Router.Resource("") panic: pkg/nucleus/router.go joinPath
                 should yield "/" not "" when prefix+path are both empty.
              3. ADR-010 §2 layer 5 — module-specific config validation.
              REMEMBER: all changes must go through the PR workflow (see NOTES).
BLOCKERS:     none
FILES OF INTEREST: pkg/app/app.go (~255-300), pkg/app/app_test.go,
              pkg/nucleus/router.go, .claude/state/CURRENT_ITERATION.md
NOTES:        *** CRITICAL — main is PR-only for EVERYONE including the maintainer ***
              enforce_admins=true, required check "CI Required Gate" strict=true,
              required_approving_review_count=0, required_conversation_resolution=true.
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
              Minor cosmetic discrepancy (do NOT fix in isolation — needs its own PR):
              CHANGELOG.md dates [0.8.0] as 2026-05-27 but iteration archive + release
              object say 2026-05-28. Fix opportunistically next time CHANGELOG is touched.

Updated: 2026-05-28
