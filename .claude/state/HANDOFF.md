# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    fleetdesk prototype — S6 COMPLETE (admin + observability + OpenAPI + rate limit + circuit + body limit); S7 next
BRANCH:       main (nucleus, clean apart from untracked docs/audits/2026-06-14-exhaustive-audit.md);
              main (fleetdesk @ a4ca8b3, clean)
LAST COMMIT:  nucleus  084a4b5  feat(admin): live SQL feed consumes the observability bus (#131)
              fleetdesk a4ca8b3  feat(openapi): /openapi.json + nucleus openapi CLI export
STATUS:       S6 shipped & verified live on port 18080 (/healthz + /metrics + /openapi.json
              all 200; rate limit + breaker tests green); finding #31 newly OPEN (direct
              *sql.DB queries invisible in live SQL feed — level 2 deferred by user);
              admin live SQL feed root cause fixed via PR #131
NEXT STEP:    S7 — E2E smoke green + README documents the coverage matrix + zero `replace`;
              CLI fully exercised (`new`, `generate resource`, `makemigrations`, `migrate`,
              `doctor`, `config`, `openapi`, `serve`); pending browser verification of
              /jobs, /access, admin live view once port 8080 freed; then Data Studio
              Phases 0/A/B/C
BLOCKERS:     none (port 8080 occupied by Docker Desktop PID 1926 is an environment
              annoyance; all live verification done on NUCLEUS_PORT=18080)
FILES OF INTEREST:
              ~/GolandProjects/fleetdesk/go.mod (pin v0.9.1-0.20260616174301-084a4b5689ca),
              ~/GolandProjects/fleetdesk/internal/contracts/ (openapi.go + schemas.go —
                runtime + CLI source of truth),
              ~/GolandProjects/fleetdesk/internal/connectivity/carrier.go (CarrierClient + breaker),
              ~/GolandProjects/fleetdesk/internal/webui/bodylimit.go,
              ~/GolandProjects/fleetdesk/nucleus.yml (rate_limit_requests=600, by_route=true),
              ~/GolandProjects/fleetdesk/FINDINGS.md (#31 OPEN; framework-friction set is
                #24-27, #29-31),
              ~/GolandProjects/fleetdesk/main.go (WithOpenAPI wiring),
              .claude/state/CURRENT_ITERATION.md
NOTES:        Demo login any tenant e.g. admin@acme.example / operator@acme.example /
              viewer@acme.example, password "fleetdesk"; admin panel admin / fleetdesk-demo.
              App URLs need ≥3 host labels (acme./borealis.fleetdesk.localhost:8080) — but
              8080 still occupied by Docker Desktop (PID 1926); use NUCLEUS_PORT=18080 ./app
              for command-line probes (subdomain features won't work — they need the 8080
              binding). /openapi.json is anonymous-readable; CLI: `nucleus openapi --project .`.
              Carrier breaker: SetFailureRate(1.0) to demo trip; ProvisionSIM(ctx, iccid) is
              the pre-flight in activate. gh REST pr-merge 401s → GraphQL mergePullRequest
              mutation. The unrelated untracked docs/audits/2026-06-14-exhaustive-audit.md is
              still in nucleus — maintainer's call. Level-2 SQL capture (#31) deferred by
              user — needs database/sql/driver wrapper + dedup with model.CRUD observer;
              ADR territory.

--- CRITICAL — `main` is PR-only for EVERYONE including the maintainer ---
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-17
