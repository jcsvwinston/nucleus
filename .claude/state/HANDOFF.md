# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    fleetdesk prototype — S3 in progress (React islands remaining)
BRANCH:       main (nucleus); main (fleetdesk @ ~/GolandProjects/fleetdesk)
LAST COMMIT:  db9759d  fix(admin): surface rejected-login feedback through the SPA (finding #16) (#120)
STATUS:       in progress — S3 partially done (Tickets CRUD complete, React islands not started)
NEXT STEP:    S3 remainder: Vite scaffold in fleetdesk, go:embed serving of
              built assets, first React island (live usage sparkline on
              dashboard). If user redirects, proceed to S4 (sessions + casbin
              RBAC + CSRF + findings #15/#17).
BLOCKERS:     none
FILES OF INTEREST:
              ~/GolandProjects/fleetdesk/go.mod,
              ~/GolandProjects/fleetdesk/FINDINGS.md,
              ~/GolandProjects/fleetdesk/templates/chrome.html,
              pkg/router/context.go (BindForm — PR #118),
              pkg/admin/login.go (injectHeadMeta — PR #120),
              .claude/state/CURRENT_ITERATION.md
NOTES:        Four nucleus PRs merged today (#117 #118 #119 #120), all squash-
              merged via GraphQL (gh REST pr-merge 401d intermittently all
              evening; GraphQL mergePullRequest mutation is the reliable path).
              nucleus pin in fleetdesk: v0.9.1-0.20260610184553-db9759d5822a.
              Findings ledger: #11 FIXED, #13 FIXED, #16 FIXED.
              OPEN findings: #4 (CLI docs), #5 (migrate UX), #9 (admin SPA
              dist — Data Studio Phase 0 ADR), #12 (templates_dir+subdomain
              docs), #14 (mux-level body limits, S6), #15 (BaseModel mass
              assignment json-tag fallback, S4; fleetdesk mitigates t.ID=0),
              #17 (login timing oracle LOW, S4).
              Preview server on port 8080 dies on tooling session recycle —
              benign; restart with preview_start. nohup alternative pending
              user decision.
              App URLs require ≥3 host labels:
              http://acme.fleetdesk.localhost:8080/ (borealis. also seeded);
              admin at /admin. Credentials: admin / fleetdesk-demo.
              F-13 (P3): CLAUDE.md §directory-map says cmd/goframe/ — fix
              opportunistically in any docs PR.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-10
