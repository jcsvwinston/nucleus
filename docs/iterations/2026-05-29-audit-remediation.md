# Iteration Archive — 2026-05-29 Audit Remediation

> Archived by `session-curator` on 2026-05-29.
> Status at archival: **COMPLETE** — PR #82 squash-merged to `main`
> (commit `64897f4`, "Audit remediation (2026-05-29): functional CLI +
> framework + doc fidelity (#82)"). All 12 CI checks passed.

---

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
  question (maintainer decision).

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
- [x] Verification suite green — all 12 CI checks passed on PR #82 (CI
      Required Gate, Admin Observability Skeleton, Build (Docusaurus),
      Compatibility Harness, Contract Freeze, DB Matrix Live (mssql), DB
      Matrix Live (oracle), DB Matrix Required (mysql), DB Matrix Required
      (postgresql), Test And Smoke (1.26.3), Website Docs Drift (advisory);
      Deploy to GitHub Pages skipped as expected for non-main run).
- [x] PR opened and merged via the protected-`main` flow — PR #82
      squash-merged 2026-05-29T17:53:23Z; `main` advanced
      `1702770..64897f4`.

## Status at archival: COMPLETE

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
  Changed / Documentation entries and corrected the `[0.8.0]` date
  (2026-05-27 → 2026-05-28).
- 2026-05-29 — PR #82 squash-merged to `main`; `main` now at commit `64897f4`.

## Notes / decisions log

- 2026-05-29 — This iteration supersedes the 2026-05-28 WithoutDefaults
  admin-bootstrap-leak iteration; FW-2 here closes the SHOULD follow-up that
  the prior iteration's architect/code/security reviewers flagged.
- 2026-05-29 — Semver: patch/minor. Bug fixes + security hardening + additive
  contract coverage; no stable symbol removed or renamed (freeze gate green).
  The `SameSite=None`/insecure startup-validation rejection and the CORS
  wildcard-plus-credentials change are behaviour changes that fail/redirect
  previously-broken configs loudly — noted in release notes but not breaking
  removals.
- 2026-05-29 — All CI passed: 12/12 checks green on PR #82.
