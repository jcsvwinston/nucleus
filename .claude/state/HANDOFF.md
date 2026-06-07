# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Exhaustive audit v2 (2026-06-07) — COMPLETE and archived to
              docs/iterations/2026-06-07-exhaustive-audit-v2.md.
              CURRENT_ITERATION.md is PRE-SEEDED with the next iteration
              (F-3 CRUD placeholder portability) — NOT started; confirm the
              goal with the maintainer before writing code (§2.3).
BRANCH:       audit/2026-06-07-exhaustive-v2 (local, uncommitted — Carlos
              lands it; see NEXT STEP). main itself is untouched at d42bf19.
LAST COMMIT:  d42bf19 on main.
STATUS:       Audit done. Report: docs/audits/2026-06-07-exhaustive-audit-v2.md.
              Functional verdict: root module builds/vets/tests clean (SQLite);
              freeze+firewall+harness green; CLI works end-to-end. Two
              correctness holes gate enterprise-class: F-3 (CRUD `?` not
              portable to PG/Oracle) and F-4 (firewall blind to /vN imports;
              real casbin/jwt embeds in frozen types). Plus SEC-1 (CORS
              reflect+credentials default) and guide drift (8/24 pages).
NEXT STEP:    1. Land the audit PR (Carlos, on the Mac — a stale GoLand
                 .git/index.lock blocks the sandbox):
                   cd /Users/jcsv/GolandProjects/GoFrame/GoFrame
                   rm -f .git/index.lock
                   git checkout audit/2026-06-07-exhaustive-v2
                   git add docs/audits/2026-06-07-exhaustive-audit-v2.md \
                           docs/iterations/2026-06-07-exhaustive-audit-v2.md \
                           .claude/state/CURRENT_ITERATION.md \
                           .claude/state/HANDOFF.md
                   git commit -m "docs(audit): 2026-06-07 exhaustive audit v2 (executed lanes) + seed F-3 iteration"
                   git push -u origin audit/2026-06-07-exhaustive-v2
                   gh pr create --fill
                   # CI Required Gate green → gh pr merge --squash --delete-branch
                   git checkout main && git pull
              2. Open Claude Code in the repo and run /resume — it will pick
                 up the seeded F-3 iteration from CURRENT_ITERATION.md.
                 Confirm scope, then implement in small slices + /iterate.
              DO NOT commit the working-tree go.sum / admin/*/go.mod churn
              (local `go mod download` artifacts) nor the superseded untracked
              draft docs/audits/2026-06-07-exhaustive-audit.md (keep or delete
              at Carlos's discretion).
REMEDIATION QUEUE (after F-3, one branch+PR each, severity order):
              F-4 firewall /vN name resolution + casbin/jwt leak disposition
                  (contract-guardian + migration-assistant; possibly an ADR);
              SEC-1 router corsAllowCredentials default → false (+ fix the
                  misleading R4 comment in pkg/app/app.go);
              DOC-1/2 RATE_LIMITING + MULTISITE guide rewrites (+AUTH keys),
                  WEB-1 storage.Metadata→PutOptions on the website;
              CLI-V2-1 scaffold toolchain derived from go.mod + freshness test;
              GOV-1 COMPATIBILITY_SLO promotion update + reference-date sweep;
              then: body-content §9 checks into check-coverage.sh + CI lane.
BLOCKERS:     none.
FILES:        docs/audits/2026-06-07-exhaustive-audit-v2.md (report),
              docs/iterations/2026-06-07-exhaustive-audit-v2.md (archived
              iteration), pkg/model/crud.go (F-3 target),
              contracts/firewall_test.go (F-4 target).
NOTES:        cmd/ is `cmd/nucleus`; CLAUDE.md §directory-map still says
              `cmd/goframe/` (F-13, P3 — fix opportunistically in any docs PR).
              Cowork-sandbox specifics (offline Go cache, GOCACHE must be on
              local disk, virtiofs FD limit) are recorded in the audit report
              §0 and in auto-memory; irrelevant when working from Code on the
              Mac with normal toolchain+network.

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED. Every change follows branch → push →
gh pr create → wait CI green → gh pr merge --squash --delete-branch.

Updated: 2026-06-07
