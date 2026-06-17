# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    fleetdesk prototype — COMPLETE (all 18 acceptance-criteria satisfied,
              S1–S7b). Next iteration awaiting owner direction — see
              CURRENT_ITERATION.md for candidates.
BRANCH:       nucleus main (clean apart from untracked
              docs/audits/2026-06-14-exhaustive-audit.md — maintainer's call,
              not this session's work);
              fleetdesk main @ 6c09cc0 (local-only, no remote, clean)
LAST COMMIT:  nucleus  0fe23e5  chore(state): handoff 2026-06-17 — S6 complete (#132)
              fleetdesk 6c09cc0  feat(s7): close the prototype — JWT API auth,
                                 E2E smoke, coverage matrix
STATUS:       done — prototype iteration closed; CURRENT_ITERATION.md reset to
              awaiting-direction stub; archive at
              docs/iterations/2026-06-17-fleetdesk-prototype-s7-close.md
NEXT STEP:    owner picks a candidate direction (a/b/c in CURRENT_ITERATION.md)
              and starts a new iteration; suggested first move for direction (b):
              open a nucleus PR for Runtime.JWT() (#32) — smallest, cleanest,
              follows the existing Session/Authorizer/Mailer/Storage pattern
BLOCKERS:     none
FILES OF INTEREST:
              ~/GolandProjects/fleetdesk/FINDINGS.md (open friction ledger)
              ~/GolandProjects/fleetdesk/e2e_smoke_test.go (build tag `e2e`;
                run: go test -tags e2e -run TestE2ESmoke .)
              ~/GolandProjects/fleetdesk/internal/apiauth/ (JWT issuance + bearer mw)
              ~/GolandProjects/fleetdesk/internal/platform/ (Authenticate, APIAuth,
                ErrStatus — shared credential check)
              ~/GolandProjects/fleetdesk/README.md (coverage matrix + demo logins)
              .claude/state/CURRENT_ITERATION.md (candidate next directions)
              docs/iterations/2026-06-17-fleetdesk-prototype-s7-close.md (full archive)
NOTES:        Demo logins — web UI: admin@acme.example / ops@acme.example /
              viewer@acme.example, password "fleetdesk"; eve@acme.example is
              inactive (returns 401); admin panel: admin / fleetdesk-demo;
              borealis tenant uses same pattern (@borealis.example).
              API: POST /api/token {email, password} → {"token":"..."};
              use Authorization: Bearer <token> on /api/* routes.
              App URLs need ≥3 host labels
              (http://acme.fleetdesk.localhost:8080/); port 8080 may be occupied
              by Docker Desktop — use NUCLEUS_PORT=18099 (or any free port) for
              CLI probes. Subdomain routing does NOT work on non-8080 ports.
              v0.9.0 published (tag 2026-06-09, commit 929234e, on Go proxy);
              9 S1–S6 nucleus fixes live on main ahead of the v0.9.0 tag — no
              patch release cut yet.
              fleetdesk pins nucleus v0.9.1-0.20260616174301-084a4b5689ca
              (pseudoversion, no replace directive).
              `nucleus routes` only lists framework routes, not module-registered
              ones (CLI does not run main.go Mount calls) — minor observation,
              not a formal finding.
              finding #31 (direct *sql.DB queries invisible in live feed) is
              level-2 deferred by user — needs pkg/db driver wrapper + dedup;
              ADR territory.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-17
