# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Runtime.JWT() accessor — COMPLETE (PR #134 + CI unblock PR #135).
              Next iteration awaiting owner direction — see
              CURRENT_ITERATION.md for candidates.
BRANCH:       nucleus main (clean apart from untracked
              docs/audits/2026-06-14-exhaustive-audit.md — maintainer's
              call, not this session's work);
              fleetdesk main @ 6c09cc0 (local-only, no remote, clean,
              unchanged this session)
LAST COMMIT:  nucleus  efddf6c  feat(nucleus): Runtime.JWT() — module access
                                to the framework's JWT manager (#134)
              fleetdesk 6c09cc0  feat(s7): close the prototype — JWT API auth,
                                 E2E smoke, coverage matrix
STATUS:       done — Runtime.JWT() iteration closed; CURRENT_ITERATION.md reset
              to awaiting-direction stub; archive at
              docs/iterations/2026-06-18-runtime-jwt-accessor.md
NEXT STEP:    owner picks a candidate direction; recommended first move =
              re-pin fleetdesk to nucleus @ efddf6c and refactor
              internal/apiauth to use rt.JWT() (closes finding #32 end-to-end),
              OR proceed to the next friction PR (#33 openapi security schemes
              or #34 pre-authz identity hook)
BLOCKERS:     none
FILES OF INTEREST:
              ~/GolandProjects/fleetdesk/internal/apiauth/ (candidate rt.JWT() refactor)
              ~/GolandProjects/fleetdesk/go.mod (needs pin bump to efddf6c)
              ~/GolandProjects/fleetdesk/FINDINGS.md (open friction ledger)
              pkg/nucleus/runtime.go (Runtime.JWT() just landed)
              .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — see NOTES)
              .claude/state/CURRENT_ITERATION.md (candidate next directions)
              docs/iterations/2026-06-18-runtime-jwt-accessor.md (full archive)
NOTES:        govulncheck pinned @v1.3.0 in ci.yml — do NOT upgrade to @latest:
              x/vuln v1.4.0 + golang.org/x/tools v0.46.0 panics on
              "ForEachElement called on type containing *types.TypeParam" under
              Go 1.26.4 generics. Use `govulncheck@v1.3.0` locally too.
              TODO: unpin when x/tools publishes a fix for the TypeParam panic.
              fleetdesk finding #32 is fixed upstream (PR #134) but fleetdesk
              still pins the older pseudoversion
              (v0.9.1-0.20260616174301-084a4b5689ca) and runs its own
              auth.NewJWTManager in internal/apiauth — re-pin + refactor +
              mark FINDINGS #32 FIXED when convenient.
              v0.9.0 published (tag 2026-06-09, commit 929234e, on Go proxy);
              fixes in PRs #117–#120, #122, #123, #125–#127, #129, #131,
              #134, #135 all live on main ahead of the tag — no patch release
              cut yet.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-18
