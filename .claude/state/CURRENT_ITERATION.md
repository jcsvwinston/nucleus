# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

**Get `main` CI green, then cut v0.8.0.**

`main` has been red since ~2026-05-24. Two of three root causes are addressed
locally (unpushed). The immediate unblocking task is fixing 4 stale tests in
`cmd/nucleus/main_test.go`. Once `go test ./...` is green locally and CI
confirms, push the two local commits and resume the parked v0.8.0 release.

## Scope

1. Fix `cmd/nucleus/main_test.go`: 3 scaffold-layout assertions broken by the
   skeleton rework (`f073953`, 2026-05-25) + 1 OpenAPI title mismatch
   (`TestRun_OpenAPIExport` expects "ContractApp API"; skeleton now derives
   title from module path via `defaultOpenAPITitle` in
   `internal/cli/contracts_scaffold.go`).
2. Root-cause the MSSQL live-smoke CI lane failure (real bug vs flake).
3. Push the two local commits once green:
   - `b829855` docs(governance): ADR-012 prometheus exporter + firewall
     hardening + CI_MATRIX truth-fix.
   - `217fed5` fix(db): split multi-statement AutoMigrate scripts for MySQL
     and SQLite (Error-1064 fix).
4. Resume parked v0.8.0 release:
   - Re-promote CHANGELOG `[Unreleased] → [0.8.0] - 2026-05-27` +
     `### Compatibility statement` (was drafted then reverted as premature).
   - Regenerate `docs/reports/`.
   - Annotated `v0.8.0` tag (matching v0.7.0 convention) + push (triggers
     `release.yml`).
5. Governance follow-up: enforce branch protection / required gate on `main`
   so red CI blocks future pushes.

## Acceptance criteria

- [x] `go test ./...` passes locally with zero failures.
- [x] CI `Test And Smoke` lane green on `main` (govulncheck CVEs cleared, `544f39a`).
- [x] CI `DB Matrix Required (mysql)` lane green (VARCHAR + inline-index fixes).
- [x] CI `DB Matrix Live (mssql)` lane green (NVARCHAR fix).
- [x] **Full CI green on `main` — run `26533028754` (commit `0eed39a`) concluded `success`, including the `CI Required Gate`.**
- [ ] CHANGELOG `[0.8.0]` section promoted and accurate. (Not started — v0.8.0 release deferred.)
- [ ] `git tag v0.8.0` pushed; `release.yml` workflow triggered. (Not started — awaiting owner go-ahead next session.)

## Status

### Done

- **Release-prep validation pass** (2026-05-27): contract freeze PASS,
  compatibility harness READY, firewall PASS, docs coverage clean, governance
  no hard blockers (5 WARNs). One gap: missing ADR for prometheus/otel deps —
  resolved by writing ADR-012.
- **ADR-012 authored** (`docs/adrs/ADR-012-prometheus-metrics-exporter.md`,
  commit `b829855`, unpushed): prometheus exporter rationale + firewall entries
  + CI_MATRIX truth-fix. architect-reviewer + contract-guardian PASS.
- **Error-1064 AutoMigrate fix** (`pkg/db/exec_script.go`, commit `217fed5`,
  unpushed): split multi-statement scripts for MySQL and SQLite. code-reviewer
  PASS; regression-tested on in-memory SQLite.
- **Confirmed v0.7.0 is the latest published release** (`ed5689b`, nucleus
  module path). Real next release is v0.8.0 (main is 71 commits past v0.7.0).

### In progress

- **`main` CI is GREEN (first time since ~2026-05-24); v0.8.0 release is the
  only remaining step, DEFERRED to the next session per owner.** Concrete next
  action: scope #4 — promote CHANGELOG `[Unreleased] → [0.8.0] - 2026-05-27`
  + a `### Compatibility statement`, regenerate `docs/reports/`, then an
  annotated `v0.8.0` tag + push (triggers `release.yml`). Recommend a
  `/release-prep` validation pass first and showing the owner the promoted
  CHANGELOG + tag message BEFORE pushing the tag (irreversible-ish).

### Done (2026-05-27, this session)

- **Fixed the 4 stale `cmd/nucleus/main_test.go` tests** (commit `bf7b881`):
  owner chose to accept the module-path-derived OpenAPI title "Contractapp
  API" (test change, no production change); updated scaffold-layout
  assertions to the empty-skeleton reality + relocated the openapi runtime
  sub-test to the project root.
- **Fixed a real `app.New` startup panic** (commit `d5c6203`,
  code-reviewer NITS addressed): `template.Must(ParseGlob)` panicked when
  `TemplatesDir` existed but had no `.html` (the skeleton/generated-project
  case). Now parses only when ≥1 file matches; genuine parse errors return
  via `wrapOp` instead of panicking. New `pkg/app/app_templates_test.go`
  covers both paths. This was surfaced by the stale-test fix.
- **Cleared all remaining red-CI causes → `main` CI fully green (`0eed39a`).**
  Fixed the four blockers (simplest → hardest), each confirmed in CI:
  1. **govulncheck CVEs** (`544f39a`): bumped `golang.org/x/net` v0.55,
     `otlpmetrichttp` v1.43, `go-jose/v4` v4.1.4. `dependency-impact` ACCEPT.
  2. **MySQL Error 1170** (`47dfce4`): key-bound (PK/indexed) string columns
     now `VARCHAR(255)` not `TEXT`.
  3. **MSSQL invalid-key-column** (`47dfce4`): same pattern, `NVARCHAR(255)`
     not `NVARCHAR(MAX)` — same root cause, different dialect.
  4. **MySQL Error 1061 idempotency** (`0eed39a`): indexes now declared INLINE
     in `CREATE TABLE` (no standalone `CREATE INDEX`, which MySQL can't make
     idempotent) so a re-run `AutoMigrate` is a no-op.
  All code-reviewed (NITS addressed), regression tests added per dialect,
  CHANGELOG updated (Security + Fixed). `go.work.sum` synced (`1dd469e`).

### Blocked

- (none) — `main` CI is green; the only remaining work is the v0.8.0 release,
  deferred to the next session per owner.
- MSSQL lane (`DB Matrix Live (mssql)`) failure not yet root-caused (CI-only;
  needs a container).

---

## Candidate next steps after this iteration

### Framework bugs (carry-forward)

- **P1 — `WithoutDefaults()` leaks the admin bootstrap user.**
  `pkg/app/app.go:~272` calls `admin.EnsureBootstrapAdminUser`
  UNCONDITIONALLY, before the `if !o.skipDefaults` guard. Fix: move call
  inside the `!o.skipDefaults` branch.

- **P2 — `Router.Resource("")` under a module `Prefix` panics at startup.**
  `pkg/nucleus/router.go` — `joinPath` should yield `/` (not `""`) when
  prefix+path are both empty.

### ADR-010 §2 layer 5 (module-specific config binding/validation)

Completes the five-layer validator; layer 4 (referential) shipped 2026-05-26.

### Cloud Secrets Provider plugin extraction

Extract AWS/GCP/Azure/Vault adapters out of core `go.mod`.

### SchemaDrift column-type comparison + usage guide

Cross-dialect type-family table; `docs/guides/MODELING_MULTI_DATABASE.md`.

### `go mod tidy` unblock

Resolve the `admin/proto` replace-directive.

### `tasks.Manager` struct→interface DEP

Optional DEP-2026-004; backward-compatible interface extraction.

### Audit §7 minors

503 path test for `/healthz`; endpoints-parity doc-parsing;
`pkg/health/{db,redis,storage}.go` tests.

---

## Carry-forward follow-ups (deferred, not blocking)

_Oracle multi-block AutoMigrate (2026-05-24):_

- **Route admin-bootstrap PL/SQL through `db.ExecScript`.** (architect NIT.)
- **Oracle DDL auto-commit vs the Migrator transaction.** Pre-existing caveat.

_session_cookie_secure (2026-05-23):_

- **Startup validation: `SameSite=None` requires `Secure=true`.** (security-
  auditor LOW.) `pkg/auth/session.go` should reject the combo.

_Oracle identifier-casing (2026-05-23):_

- **CI governance reconciliation (mssql + oracle): required vs exploratory.**
- **Oracle reserved-word + dotted-identifier hardening.**

_Phase 3b / observability (2026-05-22/23):_

- **GCS credential-redaction forward-compat.**
- **Reverse-proxy hardening note for `/_/config`.**
- **Relocate `pkg/observability` to `internal/` post-v1.0.**
- **`Runtime.AutoMigrate` production guard.**

_ADR-010 Phase 1 (still open):_

- **Service-shutdown timeout** — `nucleus.Run`'s `wg.Wait()` has no deadline.
- **`Lifecycle.OnShutdown` context deadline** — no bound.
- **`joinPath` double-slash collapse** — `routerAdapter.joinPath`.

_Internal-docs (low-priority, not blocking):_

- `DETAILED_TUTORIAL.md` flat-handler style predates `nucleus.Module` pattern.
- `DEVELOPER_MANUAL.md §5.3` references `internal/contracts` — could add a
  "you add this when ready" note.

---

## Files of interest

- `cmd/nucleus/main_test.go` — 4 stale tests (IMMEDIATE NEXT TASK)
- `internal/cli/contracts_scaffold.go` (~line 88) — `defaultOpenAPITitle`
- `internal/cli/scaffold/templates/` — skeleton output templates
- `pkg/db/exec_script.go` — committed Error-1064 fix (unpushed)
- `docs/adrs/ADR-012-prometheus-metrics-exporter.md` — committed (unpushed)
- `pkg/app/app.go` (~272) — P1 `WithoutDefaults()` admin-bootstrap leak
- `pkg/nucleus/router.go` — P2 `Resource("")` panic under `Prefix`

---

## Notes / decisions log

- 2026-05-27 (later) — **`main` CI driven fully green.** CI revealed the
  handoff's "2 of 3 causes" was an undercount: beyond the stale cmd/nucleus
  tests, three more lanes were red — govulncheck CVEs (Test And Smoke), MySQL
  Error 1170 (string-in-key → TEXT), and MSSQL (string-in-key → NVARCHAR(MAX)).
  Fixed all (`544f39a`, `47dfce4`), which then exposed MySQL Error 1061
  (non-idempotent standalone CREATE INDEX) — fixed by inlining indexes in
  CREATE TABLE (`0eed39a`). #2 (MySQL) and #3 (MSSQL) turned out to be the
  same bug class in two dialects (Postgres/SQLite already indexable, Oracle
  already VARCHAR2). Run `26533028754` (`0eed39a`) = full `success`. v0.8.0
  release DEFERRED to the next session per owner (chose /handoff over cutting
  the tag now). Verification caveat: MySQL/MSSQL live behaviour is CI-only
  (no local containers) — fixes were unit-tested for SQL shape + confirmed by
  CI.
- 2026-05-27 — **v0.8.0 release-prep pass** completed (validation only).
  Contract freeze PASS, compatibility harness READY, firewall PASS. ADR-012
  authored. Error-1064 fix committed. Discovered main has been red since
  2026-05-24 (no required gate on direct pushes). User decision: get main green
  first, then cut v0.8.0. Two local commits (`b829855`, `217fed5`) parked
  unpushed per policy. Immediate next step: fix the 4 stale
  `cmd/nucleus/main_test.go` tests.
- 2026-05-26 — **ADR-010 §2 layer 4 (referential validation)** implemented,
  reviewed, committed (`a8cf810`), archived at
  `docs/iterations/2026-05-26-adr010-layer4-referential-validation.md`.
- 2026-05-26 — Scaffolder-cleanup arc confirmed COMPLETE and ARCHIVED.
- 2026-05-25 — Scaffolder-cleanup arc closed: string-demo → embedded templates
  → empty skeleton. All committed + pushed.
- 2026-05-24 — ADR-010 Phase 4 Slice 2 complete. Website include-from-source
  wired. All pushed.
- 2026-05-24 — ADR-010 Phase 4 Slice 1 (`examples/mvc_api`) complete.
