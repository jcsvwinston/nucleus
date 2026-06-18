# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    CSRF session-key fix + fleetdesk adoption (#27) — COMPLETE
              (nucleus PR #142 merged @ a6beffc + fleetdesk consumer
              side commit 7e9666b). Finding #27 closed end-to-end.
              Next iteration awaiting owner direction — see
              CURRENT_ITERATION.md for candidates.
BRANCH:       nucleus main (clean apart from untracked
              docs/audits/2026-06-14-exhaustive-audit.md — maintainer's
              call, not this session's work);
              fleetdesk main @ 7e9666b (local-only, no remote, clean)
LAST COMMIT:  nucleus  a6beffc  fix(router): CSRFToken honors any session key;
                                token available on origin shortcut (#27) (#142)
              fleetdesk 7e9666b  refactor(webui): adopt framework session-mode
                                 CSRF — close finding #27
STATUS:       #27 closed end-to-end; fleetdesk dropped hand-rolled CSRF
              (platform.EnsureCSRFToken + module csrf middleware) and adopted
              router.CSRFMiddleware; token-mismatch now 419 (was 403);
              platform.CSRFSessionKey exported; SaveUser rotates on login.
              DIAGNOSIS NOTE: original root-cause (session absent in module
              middleware context) was disproved — actual bugs were a hard-coded
              "csrf_token" key in CSRFToken() and skipped injection on the
              same-origin shortcut path. Fix is surgical; no session-order changes.
              Findings ledger: 20 FIXED / 13 OPEN (#32, #33, #24, #27 FIXED).
NEXT STEP:    run /resume, await owner direction on the next finding;
              recommended #26 (RequireRole JSON 403 — SSR-unfriendly; gates
              fleetdesk SSR guards adopting Router.With per the #24 close)
BLOCKERS:     none
FILES OF INTEREST:
              pkg/router/csrf.go (CSRFToken fix; csrfTokenKey injection)
              pkg/router/csrf_session_test.go (3 new regression tests)
              pkg/authz/ (finding #26 — RequireRole SSR-unfriendly)
              pkg/auth/ (finding #34 — pre-authz identity hook)
              ~/GolandProjects/fleetdesk/FINDINGS.md (20 FIXED / 13 OPEN)
              ~/GolandProjects/fleetdesk/internal/webui/csrf.go (frameworkCSRF)
              ~/GolandProjects/fleetdesk/go.mod (pinned
                v0.9.1-0.20260618174317-a6beffc15524)
              .github/workflows/ci.yml (govulncheck pinned @v1.3.0 — see NOTES)
              .claude/state/CURRENT_ITERATION.md (candidate next directions)
              docs/iterations/2026-06-18-csrf-session-key-and-fleetdesk-adoption.md
NOTES:        findings #32, #33, #24, #27 all closed end-to-end.
              #26 (RequireRole JSON / module middleware cannot render SSR page)
              is the gap blocking fleetdesk from adopting With for SSR guards —
              deferred explicitly when #24 closed; now the primary gating item.
              fleetdesk re-pin: v0.9.1-0.20260618174317-a6beffc15524.
              govulncheck pinned @v1.3.0 in ci.yml — do NOT upgrade to @latest:
              x/vuln v1.4.0 + golang.org/x/tools v0.46.0 panics on
              "ForEachElement called on type containing *types.TypeParam" under
              Go 1.26.4 generics. Use `govulncheck@v1.3.0` locally too.
              TODO: unpin when x/tools publishes a fix for the TypeParam panic.
              nucleus main is PR-only (enforce_admins=true, required check
              "CI Required Gate" strict=true). Direct git push origin main is
              REJECTED. Every change: branch → push → gh pr create →
              wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-18
