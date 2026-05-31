# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Real-app readiness remediation (R1-R8) per ADR-013 — COMMITTED on
              branch `fix/readiness-2026-05-31`, NOT pushed, NOT yet verified
              with a Go toolchain. Full completion checklist in
              `.claude/state/CURRENT_ITERATION.md`.
BRANCH:       fix/readiness-2026-05-31 (4 commits; branched off main 6bf4f0a)
LAST COMMIT:  3bf64ef — "chore(state): readiness iteration spec + handoff for Code"
              Commits on the branch (oldest first):
                dd9195b fix(framework): real-app readiness R1/R2/R4/R5 (ADR-013)
                82f5565 feat(cli): serve --without-defaults + doctor rbac discovery
                9aa3eaa docs(readiness): ADR-013 + config/CLI/layout docs + website + CHANGELOG
                3bf64ef chore(state): readiness iteration spec + handoff
STATUS:       A readiness review (`docs/audits/2026-05-31-real-app-readiness.md`)
              found the real-app happy path WORKS; the gaps were operational /
              declared-but-inert surface. Fixes for R1-R8 are COMMITTED on this
              branch but were produced WITHOUT a Go toolchain (the Cowork sandbox
              blocks go.dev/proxy), so they compile/test ONLY after you verify.
              The branch could NOT be pushed from the sandbox (no GitHub
              credentials there) — push from your machine.
NEXT STEP:    0) PUSH the branch (sandbox lacks creds):
                 `git push -u origin fix/readiness-2026-05-31`
              1) [DONE] branch + 4 logical commits (above).
              2) `go build ./... && go vet ./... && go test ./...` (fix nits).
              3) Regenerate the freeze baseline for 3 new exported symbols
                 (`NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts/ -run
                 TestContractFreeze_APIExportedSymbols`; additions only:
                 app.Config.CORSOrigins, app.Config.CORSAllowCredentials,
                 router.WithCORSCredentials), then commit it.
              4) Add `serve --without-defaults` to CLI_CONTRACT_MATRIX.md +
                 CLI_BEST_PRACTICES.md (CLAUDE.md §3 new-CLI-surface rule).
              5) Website mirror edits APPLIED (project-structure.md,
                 cli/overview.md); run `cd website && npm run build` to verify.
              6) Add tests: internal/cli/serve_test.go (both branches);
                 pkg/nucleus readiness-WARN (fstest.MapFS migrations + nil/empty
                 + a panicking Jobs/Webhooks closure → assert WARN, no panic);
                 pkg/router CORS-credentials; pkg/app CORS wiring + RBAC discovery.
              7) Run the §4 shakedown runbook as acceptance; then PR → merge.
BLOCKERS:     none. Verification is maintainer-side (no Go in the sandbox); push
              is maintainer-side (no GitHub creds in the sandbox).
FILES OF INTEREST:
              docs/audits/2026-05-31-real-app-readiness.md (findings + runbook),
              docs/adrs/ADR-013-real-app-readiness.md (decisions),
              .claude/state/CURRENT_ITERATION.md (the completion checklist).
NOTES:        Deferred to separate ADR/iterations (per ADR-013): wire
              Module.Migrations into `nucleus migrate`; implement Jobs/Webhooks
              (Phase 2+); unify the generate-resource layout to feature-folder.
              Per the maintainer directive: NO real-app testing until this branch
              is verified green and merged.

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED by GitHub. Every change (even
.claude/state/*, docs/*) must follow:
  1. git checkout -b <branch>      (here: already on fix/readiness-2026-05-31)
  2. git push -u origin <branch>
  3. gh pr create
  4. Wait for "CI Required Gate" green (~7-20 min; full matrix incl. live
     MSSQL/Oracle; GitHub cannot path-exclude required checks)
  5. gh pr merge --squash --delete-branch
  6. git checkout main && git pull

Updated: 2026-05-31
