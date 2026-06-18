# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    fleetdesk re-pin + apiauth rt.JWT() refactor — COMPLETE
              (finding #32 closed end-to-end; no nucleus code change).
              Next iteration awaiting owner direction — see
              CURRENT_ITERATION.md for candidates.
BRANCH:       nucleus main (clean apart from untracked
              docs/audits/2026-06-14-exhaustive-audit.md — maintainer's
              call, not this session's work);
              fleetdesk main @ 3567dac (local-only, no remote, clean)
LAST COMMIT:  nucleus  bc926eb  chore(state): handoff 2026-06-18 — archive
                                Runtime.JWT() iteration + reset
                                CURRENT_ITERATION (#136)  [no nucleus code
                                change this session]
              fleetdesk 3567dac  refactor(apiauth): use the framework's
                                 rt.JWT() — close finding #32
STATUS:       finding #32 closed end-to-end; fleetdesk pins efddf6c and
              uses rt.JWT() (no own JWTManager); E2E smoke 12/12 green
NEXT STEP:    owner picks — next friction PR (#33 openapi security schemes
              or #34 pre-authz identity hook) or Data Studio Phase 0 ADR
BLOCKERS:     none
FILES OF INTEREST:
              ~/GolandProjects/fleetdesk/internal/apiauth/ (refactored; uses rt.JWT())
              ~/GolandProjects/fleetdesk/go.mod (pinned efddf6ce3dbb)
              ~/GolandProjects/fleetdesk/FINDINGS.md (#32 FIXED; other findings open)
              pkg/openapi/ (security-scheme gap — finding #33)
              pkg/auth/ (pre-authz identity hook — finding #34)
              .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — see NOTES)
              .claude/state/CURRENT_ITERATION.md (candidate next directions)
              docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md (this session's archive)
              docs/iterations/2026-06-18-runtime-jwt-accessor.md (prior session; nucleus side)
NOTES:        govulncheck pinned @v1.3.0 in ci.yml — do NOT upgrade to @latest:
              x/vuln v1.4.0 + golang.org/x/tools v0.46.0 panics on
              "ForEachElement called on type containing *types.TypeParam" under
              Go 1.26.4 generics. Use `govulncheck@v1.3.0` locally too.
              TODO: unpin when x/tools publishes a fix for the TypeParam panic.
              fleetdesk now pins nucleus @ efddf6c (pseudoversion
              v0.9.1-0.20260618065917-efddf6ce3dbb) and sources the JWT
              manager exclusively from rt.JWT(). nucleus.yml JWT config lives
              under top-level keys (jwt_secret, jwt_expiry, jwt_issuer);
              the modules.apiauth block is gone. Finding #32 fully closed.
              v0.9.0 published (tag 2026-06-09, commit 929234e, on Go proxy);
              all subsequent nucleus changes (PRs #117–#120, #122, #123,
              #125–#127, #129, #131, #134, #135) live on main — no patch
              release cut yet.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-18
