# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Remediate the 2026-05-29 exhaustive audit (CLI + framework + docs)
BRANCH:       fix/audit-2026-05-29-remediation
LAST COMMIT:  tip of branch (see `git log` — 7 commits applied this session)
STATUS:       in progress — audit remediation applied across 7 commits; awaiting
              maintainer verification + PR
NEXT STEP:    run the verification suite (below); if green, push the branch and
              open the PR per the protected-`main` flow (PR-only, no direct push)
BLOCKERS:     none — but verification is maintainer-side: the agent sandbox has
              no Go toolchain (cannot `go vet`/`go test`/`go build`) and does not
              run `git`.
FILES OF INTEREST: docs/audits/2026-05-29-exhaustive-audit.md,
              pkg/nucleus/router.go, pkg/app/app.go, pkg/auth/session.go,
              internal/cli/ (scaffold go.mod + generate resource + freeze gen),
              contracts/baseline/{cli_primary_commands,api_exported_symbols}.txt,
              website/docs/*, docs/guides/*, docs/reference/DEVELOPER_MANUAL.md,
              CHANGELOG.md, .claude/state/CURRENT_ITERATION.md

NOTES:
What landed this session (7 commits, in order):
  1. docs(audits): docs/audits/2026-05-29-exhaustive-audit.md (full audit).
  2. fix(nucleus) FW-1: Router.Resource("") no longer panics (joinPath floors to
     "/", collapses //; Resource guards empty base) + regression test.
  3. docs(website) DOC-1/2/3: website config blocks → real FLAT schema; homepage
     .Build().Run() → .Start(); non-existent symbols fixed across concepts/features.
  4. docs(guides) DOC-3: ~20 non-existent symbols in AUTH/VALIDATION/
     RATE_LIMITING/TESTING guides replaced with the real shipped API.
  5. fix(framework) FW-2/3/4/6: admin_auth_database resolved only when defaults
     enabled (WithoutDefaults no longer fails on a stray alias); SameSite=None
     forces Secure (WARN) + startup validation rejects the combo; app-level
     Lifecycle.OnShutdown bounded by a deadline; CORS never emits Allow-Origin:*
     with Allow-Credentials:true (reflects origin). +4 tests.
  6. fix(cli,contracts) CLI-1/2/3/4 + FW-5: generated go.mod now `go 1.26` +
     `toolchain go1.26.3` (interpolated); framework dep pinned to v0.8.0 (not
     `latest`) + network build smoke; `generate resource` codegen compiles
     (writeError arity + router.FromHTTP adapter); freeze generator captures
     type-associated consts; CLI freeze baseline + matrix cover config/doctor/
     openapi/wizard.
  7. docs DOC-3: fictional tasks/scheduler API in DEVELOPER_MANUAL +
     TESTING_GUIDE corrected to the real pkg/tasks interfaces + asynq provider.

Semver: patch/minor — fixes + security hardening + additive contract coverage;
no stable symbol removed or renamed (freeze gate green). CHANGELOG [Unreleased]
updated; [0.8.0] date cosmetic fix applied (2026-05-27 → 2026-05-28).

VERIFICATION SUITE (run before pushing — needs an active Go toolchain >= 1.26.3):
```
go vet ./...
go test ./...
go test ./pkg/nucleus/... -run 'Resource|JoinPath'
go test ./pkg/app/... ./pkg/auth/... ./pkg/nucleus/... ./pkg/router/...
go test ./internal/cli/...                 # offline scaffold/generate smokes (needs active Go >=1.26.3)
NUCLEUS_NETWORK_TESTS=1 go test ./internal/cli/ -run TestRunNewGeneratesBuildableProject_PublishedModule
go test ./contracts/...
NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts/ -run TestContractFreeze_APIExportedSymbols && git diff contracts/baseline/api_exported_symbols.txt
cd website && npm ci && npm run build
# manual scaffold smoke:
go run ./cmd/nucleus new demo --module example.com/demo --out /tmp && cd /tmp/demo && go mod tidy && go build ./...
```
If the freeze rebaseline diff is non-empty, confirm it is the expected additive
delta (type-associated consts now captured) and commit
contracts/baseline/api_exported_symbols.txt in the same PR.

Maintainer follow-ups (not blocking the PR — see CURRENT_ITERATION.md "Pending on
maintainer"): decide the examples/ + CLAUDE.md directory-map question (only
examples/mvc_api is a tracked Go app; the other three trees are local/untracked
scaffolding) — route as its own change, do NOT fold into this PR; and schedule
the Block 8 audit-roadmap leftovers as a follow-up iteration.

*** CRITICAL — `main` is PR-only for EVERYONE including the maintainer ***
enforce_admins=true, required check "CI Required Gate" strict=true,
required_approving_review_count=0, required_conversation_resolution=true.
Direct `git push origin main` is REJECTED by GitHub. Every change (even
.claude/state/*, docs/*) must follow:
  1. git checkout -b <branch>            # already on fix/audit-2026-05-29-remediation
  2. git push -u origin <branch>
  3. gh pr create
  4. Wait for "CI Required Gate" green (~7-20 min; full matrix incl. live
     MSSQL/Oracle; GitHub cannot path-exclude required checks)
  5. gh pr merge --squash --delete-branch
  6. git checkout main && git pull
--approvals 0 is deliberate: single-maintainer repo; 1 would lock the maintainer
out (enforce_admins blocks direct push + no second reviewer).

Updated: 2026-05-29
