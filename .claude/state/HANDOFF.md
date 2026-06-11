# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    fleetdesk prototype — S3 COMPLETE (SSR+CRUD+React islands); S4 next
BRANCH:       main (nucleus, clean except this handoff's state files);
              main (fleetdesk @ be15965, clean)
LAST COMMIT:  nucleus  85560e5  fix(ci): drift guard SIGPIPE flake — spurious DANGLING under pipefail (#123)
              fleetdesk be15965  chore: pin nucleus @6b3ea75 (PR #122 bare /admin allow); FINDINGS #19 FIXED
STATUS:       S3 shipped & verified live; findings #11/#13/#16/#19 FIXED cumulative; #18 newly OPEN
NEXT STEP:    S4 — sessions + casbin RBAC (admin/operator/viewer replacing anonymous rows
              in rbac_policy.csv) + CSRF + findings #15/#17 + set cors_origins allow-list
              BEFORE session cookies (security-audit note: wildcard CORS + credentials
              would be a cross-tenant leak)
BLOCKERS:     none
FILES OF INTEREST:
              ~/GolandProjects/fleetdesk/go.mod (pin v0.9.1-0.20260611064010-6b3ea757c461),
              ~/GolandProjects/fleetdesk/FINDINGS.md (#19 FIXED; OPEN: #4 #5 #9 #12 #14 #15 #17 #18),
              ~/GolandProjects/fleetdesk/rbac_policy.csv (anonymous rows S4 replaces),
              ~/GolandProjects/fleetdesk/web/ (islands; dist gitignored except .gitkeep),
              ~/GolandProjects/fleetdesk/internal/webui/islands.go,
              pkg/authz/policies.go (bare /admin exact-match fix — PR #122),
              .claude/state/CURRENT_ITERATION.md
NOTES:        Islands dev loop: npm --prefix web run build THEN go build -o app .
              (bundle embeds at COMPILE time; launch.json "fleetdesk" runs ./app —
              rebuild before preview_start to pick up island changes).
              Admin: /admin works bare now (PR #122); creds admin / fleetdesk-demo.
              App URLs need ≥3 host labels (acme.fleetdesk.localhost:8080 or borealis.*).
              gh REST pr-merge 401s → use GraphQL mergePullRequest mutation.
              Drift-guard advisory job now deterministic (PR #123); all checks green.
              Demo data: 722 MB usage inserted into tenant_acme.db for today
              (live-chart demo — usage-chart island polling verified).
              F-13 (P3): CLAUDE.md §directory-map still says cmd/goframe/ — fix
              opportunistically in any docs PR.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-11
