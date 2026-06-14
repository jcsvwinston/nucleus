# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    fleetdesk prototype — S4 COMPLETE (sessions + casbin RBAC + CSRF/CORS); S5 next
BRANCH:       main (nucleus, clean apart from untracked docs/audits/2026-06-14-exhaustive-audit.md);
              main (fleetdesk @ 2dbbe82, clean)
LAST COMMIT:  nucleus  64d28dd  fix(admin): equalize login rejection timing (#126)
              fleetdesk 2dbbe82  feat(security): S4c — CSRF protection + CORS allow-list; Chrome view-model refactor
STATUS:       S4 shipped & verified live (login, roles, deny-override, CSRF); findings #15/#17/#19/#20/#21 FIXED cumulative
NEXT STEP:    S5 — pkg/tasks (usage rollup + report generation handlers, Scheduler, Inspector in admin)
              + pkg/signals (sim.activated → mail + audit, EmitAsync)
              + pkg/mail (smtp dev: mailhog/noop; alert + welcome templates)
              + pkg/storage (local store; report exports via Put/SignedURL)
BLOCKERS:     none
FILES OF INTEREST:
              ~/GolandProjects/fleetdesk/go.mod (pin v0.9.1-0.20260611164013-64d28dd8eeb6),
              ~/GolandProjects/fleetdesk/FINDINGS.md (OPEN: #4 #5 #9 #12 #14 #18 #22 #23 #24 #25 #26 #27;
                framework-friction #24-#27 are v0.9.x PR candidates, #27 is HIGH),
              ~/GolandProjects/fleetdesk/internal/webui/ (auth.go gate+login, authz.go role guards,
                csrf.go, chrome.go, access.go inspector),
              ~/GolandProjects/fleetdesk/internal/platform/session.go (session + CSRF helpers),
              ~/GolandProjects/fleetdesk/rbac_policy.csv (two-layer: anonymous reachability +
                role policies w/ deny-override),
              ~/GolandProjects/fleetdesk/nucleus.yml (session_cookie_secure:false, cors_origins),
              .claude/state/CURRENT_ITERATION.md
NOTES:        Demo login — any tenant, e.g. admin@acme.example / operator@acme.example /
              viewer@acme.example, password "fleetdesk"; admin panel still admin / fleetdesk-demo.
              App URLs need ≥3 host labels (acme.fleetdesk.localhost:8080 / borealis.*).
              Dev loop: islands need npm --prefix web run build THEN go build -o app . (bundle
              embeds at compile time); launch.json runs ./app — rebuild before preview_start.
              gh REST pr-merge 401s → GraphQL mergePullRequest mutation.
              Framework findings #24-#27 are the richest upstream follow-ups (a nucleus PR for #27
              CSRF would directly simplify fleetdesk). #27 is HIGH: session-based CSRFMiddleware
              unusable from module middleware position — session injected into context after module mw.
              An unrelated untracked file docs/audits/2026-06-14-exhaustive-audit.md sits in nucleus
              — left for the maintainer, not part of this work.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-14
