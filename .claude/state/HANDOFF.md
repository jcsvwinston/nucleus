# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Router.With() per-route middleware (#24) — COMPLETE
              (nucleus PR #140 merged @ 05fb701 + fleetdesk consumer
              side commit c5d969f). Finding #24 closed end-to-end.
              Next iteration awaiting owner direction — see
              CURRENT_ITERATION.md for candidates.
BRANCH:       nucleus main (clean apart from untracked
              docs/audits/2026-06-14-exhaustive-audit.md — maintainer's
              call, not this session's work);
              fleetdesk main @ c5d969f (local-only, no remote, clean)
LAST COMMIT:  nucleus  05fb701  feat(nucleus): Router.With() — per-route
                                middleware on the fluent router (#140)
              fleetdesk c5d969f  refactor(webui): drop the nopResponseWriter
                                 role-guard hack — close finding #24
STATUS:       #24 closed end-to-end; fleetdesk dropped nopResponseWriter
              (direct role check via Enforcer.RequireRole, symmetric with
              requirePerm); Router.With() validated by nucleus regression
              suite (incl. Resource bypass concern — disproved by test)
NEXT STEP:    owner picks — recommended #27 (CSRF HIGH, same root class as
              #26/#34) or #26 (RequireRole SSR-friendly denial, which would
              let fleetdesk SSR guards adopt With), or Data Studio Phase 0
BLOCKERS:     none
FILES OF INTEREST:
              pkg/nucleus/router.go (With interface + routerAdapter impl)
              pkg/nucleus/router_with_test.go (regression suite)
              contracts/baseline/api_exported_symbols.txt (+1 Router.With)
              docs/adrs/ADR-010 (per-route-middleware amendment)
              docs/reference/API_CONTRACT_INVENTORY.md
              CHANGELOG.md (Unreleased entry for Router.With)
              website/docs/concepts/routing.md
              ~/GolandProjects/fleetdesk/internal/webui/ (nopResponseWriter removed)
              ~/GolandProjects/fleetdesk/FINDINGS.md (#32, #33, #24 FIXED)
              ~/GolandProjects/fleetdesk/go.mod (pinned 05fb701 pseudoversion)
              .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — see NOTES)
              .claude/state/CURRENT_ITERATION.md (candidate next directions)
              docs/iterations/2026-06-18-router-with-per-route-middleware.md (this session's archive)
NOTES:        findings #32, #33, #24 all closed end-to-end.
              #26 (RequireRole JSON / module middleware cannot render SSR page)
              is the gap blocking fleetdesk from adopting With for SSR guards —
              relevant to any future #26 work.
              govulncheck pinned @v1.3.0 in ci.yml — do NOT upgrade to @latest:
              x/vuln v1.4.0 + golang.org/x/tools v0.46.0 panics on
              "ForEachElement called on type containing *types.TypeParam" under
              Go 1.26.4 generics. Use `govulncheck@v1.3.0` locally too.
              TODO: unpin when x/tools publishes a fix for the TypeParam panic.
              v0.9.0 published (tag 2026-06-09, commit 929234e, on Go proxy);
              all subsequent nucleus changes (PRs #117–#140) live on main —
              no patch release cut yet. fleetdesk pins 05fb701.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-18
