# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

Remediate the 2026-05-29 exhaustive audit (CLI + framework + docs) until the
framework is 100% functional with what is implemented and the docs are a
faithful reflection of the shipped behaviour.

## Scope

- in: `pkg/nucleus` (router `joinPath`/`Resource`), `pkg/app` (admin-auth DB
  gating, app-level `OnShutdown` deadline), `pkg/auth/session.go`
  (`SameSite=None`→`Secure`), CORS credentials/wildcard handling, `internal/cli`
  (scaffold `go.mod` pin + toolchain, `generate resource` codegen,
  freeze-generator const capture), `contracts/` (CLI freeze baseline + matrix),
  `website/docs/*` + `docs/guides/*` + `DEVELOPER_MANUAL.md` + `TESTING_GUIDE.md`
  (faithfulness fixes), `CHANGELOG.md`, this state file.
- out: new features, ADR-010 §2 layer 5 (module-specific config validation),
  the Block 8 leftovers from the audit roadmap, the `examples/` reintroduction
  question (maintainer decision — see below).

## Acceptance criteria

- [x] FW-1 — `Router.Resource("")` no longer panics; `joinPath` floors to `/`
      and collapses `//`; regression test added.
- [x] FW-2 — admin-auth DB (`admin_auth_database`) resolved only when defaults
      are enabled; `WithoutDefaults()` no longer fails on a stray alias.
- [x] FW-3 — `SameSite=None` forces `Secure` (WARN) + startup validation
      rejects the `none`+insecure combo.
- [x] FW-4 — CORS never emits `Allow-Origin: *` with
      `Allow-Credentials: true` (reflects the request origin instead); test added.
- [x] FW-5 — freeze generator captures type-associated consts; CLI freeze
      baseline + matrix cover `config`/`doctor`/`openapi`/`wizard`.
- [x] FW-6 — app-level `Lifecycle.OnShutdown` now bounded by a deadline; test added.
- [x] CLI-1/2 — generated `go.mod` is `go 1.26` + `toolchain go1.26.3`
      (interpolated); framework dep pinned to `v0.8.0` (not `latest`);
      network build smoke added.
- [x] CLI-3 — `generate resource` codegen compiles (`writeError` arity +
      `router.FromHTTP` adapter).
- [x] DOC-1/2/3 — website config blocks rewritten to the real flat schema;
      homepage example `.Build().Run()`→`.Start()`; ~20 non-existent symbols
      across AUTH/VALIDATION/RATE_LIMITING/TESTING guides + Concepts/Features
      pages replaced with the shipped API; DEVELOPER_MANUAL + TESTING_GUIDE
      tasks/scheduler API corrected to the real `pkg/tasks` interfaces + asynq.
- [x] `CHANGELOG.md` carries the `[Unreleased]` entries (patch/minor; no
      breaking removals) and the `[0.8.0]` date cosmetic fix (2026-05-27 →
      2026-05-28).
- [ ] Verification suite green (maintainer-side — no Go toolchain in the agent
      sandbox; see HANDOFF for the command block).
- [ ] PR opened and merged via the protected-`main` flow.

## Status

### Done

- 2026-05-29 — `docs(audits)`: added `docs/audits/2026-05-29-exhaustive-audit.md`
  (full audit) — the source of truth for this remediation.
- 2026-05-29 — `fix(nucleus)` FW-1: `Router.Resource("")` no longer panics
  (`joinPath` floors to `/`, collapses `//`; `Resource` guards empty base) +
  regression test.
- 2026-05-29 — `docs(website)` DOC-1/2/3: rewrote website config blocks to the
  real flat schema; homepage example `.Build().Run()`→`.Start()`; fixed
  non-existent symbols across concepts/features pages.
- 2026-05-29 — `docs(guides)` DOC-3: replaced ~20 non-existent symbols in
  AUTH/VALIDATION/RATE_LIMITING/TESTING guides with the real shipped API.
- 2026-05-29 — `fix(framework)` FW-2/3/4/6: admin-auth DB resolved only when
  defaults enabled; `SameSite=None` forces `Secure` (WARN) + startup validation;
  app-level `Lifecycle.OnShutdown` bounded by a deadline; CORS never emits
  `Allow-Origin:*` with `Allow-Credentials:true` (reflects origin). +4 tests.
- 2026-05-29 — `fix(cli,contracts)` CLI-1/2/3/4 + FW-5: generated `go.mod` now
  `go 1.26` + `toolchain go1.26.3` (interpolated); framework dep pinned to
  `v0.8.0` (not `latest`) + network build smoke; `generate resource` codegen
  now compiles (`writeError` arity + `router.FromHTTP` adapter); freeze
  generator now captures type-associated consts; CLI freeze baseline + matrix
  now cover `config`/`doctor`/`openapi`/`wizard`.
- 2026-05-29 — `docs` DOC-3: corrected fictional tasks/scheduler API in
  DEVELOPER_MANUAL + TESTING_GUIDE to the real `pkg/tasks` interfaces + asynq
  provider.
- 2026-05-29 — `CHANGELOG.md`: added the `[Unreleased]` Security / Fixed /
  Changed / Documentation entries for the 7 commits above and corrected the
  `[0.8.0]` date (2026-05-27 → 2026-05-28).

### In progress

- Awaiting Carlos's local verification (the agent sandbox has no Go toolchain
  and does not run `git`). Once green, open the PR via the protected-`main`
  flow and merge.

### Blocked

- (none)

## Pending on maintainer (Carlos)

- **Run the verification suite.** Full command block is in
  `.claude/state/HANDOFF.md` (`go vet`, `go test ./...`, the targeted
  `pkg/nucleus` Resource/JoinPath run, the per-package security/fix lanes, the
  offline + network CLI scaffold smokes, `contracts/...`, the website
  `npm ci && npm run build`, and a manual `nucleus new` → `go build` smoke).
- **Regenerate the API freeze baseline** and confirm the diff is the expected
  additive delta (type-associated consts now captured), then commit it:

  ```
  NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts/ -run TestContractFreeze_APIExportedSymbols
  git diff contracts/baseline/api_exported_symbols.txt
  ```

- **Decide the `examples/` + `CLAUDE.md` directory-map question.** Only
  `examples/mvc_api` is a tracked Go app (in the root module, built/tested by
  CI). The other three example trees are local/untracked scaffolding that does
  not match what `CLAUDE.md`'s directory map advertises (`mvc_api`,
  `fleetmanager`, `ecommerce_dashboard`, `showcase_demo`, `plugins/…`). Decide
  whether to track them, drop them, or correct the directory map. NOTE: editing
  `CLAUDE.md` is out of scope for this housekeeping pass — route it as its own
  change.
- **Block 8 leftovers** from the audit roadmap (`docs/audits/2026-05-29-exhaustive-audit.md`)
  remain unstarted — schedule as a follow-up iteration.

## Files of interest

- `docs/audits/2026-05-29-exhaustive-audit.md` (the audit being remediated)
- `pkg/nucleus/router.go`
- `pkg/app/app.go`
- `pkg/auth/session.go`
- `internal/cli/` (scaffold `go.mod`, `generate resource` codegen, freeze generator)
- `contracts/baseline/cli_primary_commands.txt`, `contracts/baseline/api_exported_symbols.txt`
- `website/docs/*`, `docs/guides/*`, `docs/reference/DEVELOPER_MANUAL.md`, `docs/guides/TESTING_GUIDE.md`
- `CHANGELOG.md`

## Notes / decisions log

- 2026-05-29 — This iteration supersedes the 2026-05-28 WithoutDefaults
  admin-bootstrap-leak iteration; FW-2 here (gate the admin-auth DB resolution
  behind `!skipDefaults`) closes the SHOULD follow-up that the prior iteration's
  architect/code/security reviewers flagged.
- 2026-05-29 — Semver read: patch/minor. Bug fixes + security hardening +
  additive contract coverage; no stable symbol removed or renamed (freeze gate
  green). The `SameSite=None`/insecure startup-validation rejection and the
  CORS wildcard-plus-credentials change are behaviour changes that fail/redirect
  previously-broken configs loudly — note them in release notes but they are not
  breaking removals.
- 2026-05-29 — All verification is maintainer-side: the agent sandbox has no Go
  toolchain and may not run `git`; nothing here was compiled or committed.

---

> **IMPORTANT — `main` is PR-only (branch protection active since 2026-05-28).**
> Every change — including `.claude/state/*` and `docs/*` — must go through:
> create branch → push → `gh pr create` → wait for `CI Required Gate` green
> (~7–20 min, full matrix incl. live MSSQL/Oracle) → self-merge
> (`gh pr merge --squash --delete-branch`) → `git checkout main && git pull`.
> Direct `git push origin main` is REJECTED.

## Carry-forward backlog

### Framework / ADR follow-ups

- **ADR-010 §2 layer 5** — module-specific config binding/validation. Completes
  the five-layer validator; layer 4 (referential) shipped 2026-05-26.
- **`examples/` reintroduction + `CLAUDE.md` directory-map reconciliation** —
  see "Pending on maintainer" above.
- Cloud Secrets Provider plugin extraction (AWS/GCP/Azure/Vault out of core).
- SchemaDrift column-type comparison + `docs/guides/MODELING_MULTI_DATABASE.md`.
- `go mod tidy` unblock — resolve the `admin/proto` replace-directive.
- `tasks.Manager` struct→interface DEP (optional DEP-2026-004).
- Audit §7 minors: 503 path test for `/healthz`; endpoints-parity doc-parsing;
  `pkg/health/{db,redis,storage}.go` tests.

### Deferred carry-forwards (not blocking)

_Oracle multi-block AutoMigrate (2026-05-24):_
- Route admin-bootstrap PL/SQL through `db.ExecScript`. (architect NIT.)
- Oracle DDL auto-commit vs the Migrator transaction.

_Oracle identifier-casing (2026-05-23):_
- CI governance reconciliation (mssql + oracle): required vs exploratory.
- Oracle reserved-word + dotted-identifier hardening.

_Phase 3b / observability (2026-05-22/23):_
- GCS credential-redaction forward-compat.
- Reverse-proxy hardening note for `/_/config`.
- Relocate `pkg/observability` to `internal/` post-v1.0.
- `Runtime.AutoMigrate` production guard.

_ADR-010 Phase 1 (remaining):_
- Service-shutdown timeout — `nucleus.Run`'s `wg.Wait()` has no deadline.
  (NOTE: the app-level `Lifecycle.OnShutdown` deadline shipped here as FW-6;
  the `wg.Wait()` service-shutdown bound is the still-open sibling.)

_Internal-docs (low-priority):_
- `DETAILED_TUTORIAL.md` flat-handler style predates `nucleus.Module` pattern.
- `DEVELOPER_MANUAL.md §5.3` references `internal/contracts`.
