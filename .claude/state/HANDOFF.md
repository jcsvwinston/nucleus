# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    openapi security schemes (#33) — COMPLETE
              (nucleus PR #138 + fleetdesk consumer side commit 8686574).
              Findings #32 AND #33 now both fully closed.
              Next iteration awaiting owner direction — see
              CURRENT_ITERATION.md for candidates.
BRANCH:       nucleus main (clean apart from untracked
              docs/audits/2026-06-14-exhaustive-audit.md — maintainer's
              call, not this session's work);
              fleetdesk main @ 8686574 (local-only, no remote, clean)
LAST COMMIT:  nucleus  0d3d875  feat(openapi): declare security schemes +
                                requirements (finding #33) [PR #138, merged]
              fleetdesk 8686574  feat(contracts): declare bearer auth in
                                 the OpenAPI contract — close finding #33
STATUS:       #33 closed end-to-end; fleetdesk contract declares bearer
              auth; /openapi.json carries securitySchemes.bearerAuth +
              doc-level security requirement; POST /api/token carries
              security: [] (public override via PublicSecurity())
NEXT STEP:    owner picks — next friction PR #34 (pre-authz identity hook /
              reachability-row footgun) or #27 (CSRF from module mw, HIGH),
              or Data Studio Phase 0 ADR
BLOCKERS:     none
FILES OF INTEREST:
              pkg/openapi/openapi.go (security-scheme surface, experimental)
              pkg/openapi/openapi_test.go
              docs/reference/API_CONTRACT_INVENTORY.md (updated)
              CHANGELOG.md (Unreleased entry added for #33)
              ~/GolandProjects/fleetdesk/internal/contracts/openapi.go
              ~/GolandProjects/fleetdesk/go.mod (pinned 0d3d875 pseudoversion)
              ~/GolandProjects/fleetdesk/FINDINGS.md (#32 + #33 FIXED)
              .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — see NOTES)
              .claude/state/CURRENT_ITERATION.md (candidate next directions)
              docs/iterations/2026-06-18-openapi-security-schemes.md (this session's archive)
              docs/iterations/2026-06-18-fleetdesk-repin-rt-jwt.md (prior session archive)
NOTES:        govulncheck pinned @v1.3.0 in ci.yml — do NOT upgrade to @latest:
              x/vuln v1.4.0 + golang.org/x/tools v0.46.0 panics on
              "ForEachElement called on type containing *types.TypeParam" under
              Go 1.26.4 generics. Use `govulncheck@v1.3.0` locally too.
              TODO: unpin when x/tools publishes a fix for the TypeParam panic.
              findings #32 AND #33 now both fully closed (nucleus + fleetdesk).
              fleetdesk now pins nucleus @ 0d3d875 (pseudoversion
              v0.9.1-0.20260618152739-0d3d8758ecd3).
              pkg/openapi is experimental — no contract baseline change made.
              When pkg/openapi graduates to stable, BearerAuthScheme,
              APIKeyScheme, and SecurityRequirement types must be added to
              the freeze baseline.
              v0.9.0 published (tag 2026-06-09, commit 929234e, on Go proxy);
              all subsequent nucleus changes (PRs #117–#138) live on main —
              no patch release cut yet.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-18
