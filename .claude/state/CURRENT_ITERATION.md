# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. ADR-010 §2 ("Config loading + merge engine") is
**feature-complete** — all four sub-iterations have landed:

- **Phase 2a** — single-file `FromConfigFile` loader (PR #73 → `2b650f3`).
- **Phase 2b** — multi-file merge engine + TOML/JSON + operators +
  null + non-nullable security + `WithConfigStrict` (PR #74 → `7bb1c51`).
- **Phase 2c** — `WithUnknownFields` + `NUCLEUS_ENV=production`
  strict override + startup WARN (PR #75 → `4032771`).
- **Phase 2d** — module migration namespacing (`NewModuleMigrator`)
  (PR #76 → `af1fcc0`).

Phases 2b/2c/2d archived together at
`docs/iterations/2026-05-20-adr010-phase2bcd-config-loader-completion.md`.

Next ADR-010 phase is **Phase 3** (`/_/config` endpoint +
`nucleus config print --effective`). Candidate #1 (`pkg/admin`
MSSQL/Oracle bootstrap DDL fix) remains the top-ranked non-ADR-010
alternative if owner wants to interleave.

## Scope

- in: (TBD — owner to confirm scope when iteration is registered)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD)

## Status

### Done (2026-05-20)

- **ADR-010 Phase 2b / 2c / 2d** completed and merged (PRs #74 / #75 /
  #76). See the archived iteration for the full per-phase record.
  ADR-010 §2 is now feature-complete; layers 1+2 of the five-layer
  validator are done, layer 3 (range/enum) is a standalone follow-up.

### Done (earlier — see prior archives)

- v0.7.0 (PRs #56–#59); CSRF hardening (ADR-006); slog redaction
  (ADR-007); CSRF follow-ups + schema drift (ADR-008 + ADR-009);
  MSSQL/Oracle SchemaDrift (#66); pkg/app+pkg/nucleus inventory (#65);
  ADR-010 Phase 1 + examples purge (#71); ADR-010 Phase 2a (#73).

### In progress

- (none)

### Blocked

- (none)

## Candidate next steps (priority order, pending owner confirmation)

1. **`pkg/admin` bootstrap users-table DDL — dialect-aware fix for
   MSSQL/Oracle.** Real bug discovered during PR #66 CI: MSSQL
   `Incorrect syntax near 'nucleus_admin_users'`, Oracle
   `ORA-03076: unexpected item DEFAULT`. Still blocks
   `TestSQLMatrix_AutoMigrate_Exploratory` from re-enablement. Fix
   replicates the dialect-aware discipline of
   `pkg/model/migration_scaffold_{mssql,oracle}.go`. After the fix,
   re-wire `TestSQLMatrix_AutoMigrate_Exploratory` into
   `.github/workflows/ci.yml`.

2. **Freeze-scanner constructor gap** (surfaced during Phase 2d
   review). `contracts/freeze_test.go::exportedSymbolsForPackage`
   iterates `docPkg.Funcs` but not `typ.Funcs`, so `NewXxx`
   constructors (`NewMigrator`, `NewModuleMigrator`, `db.New`,
   `router.New`, … — `go/doc` files them under the returned type) are
   invisible to the baseline freeze. Mechanical fix: add a
   `for _, fn := range typ.Funcs` loop in the type loop, then reseed
   the baseline with `NUCLEUS_UPDATE_CONTRACT_BASELINE=1`. The reseed
   diff will be large (many previously-untracked constructors). Worth
   doing soon — the freeze test cannot currently catch a removed
   constructor.

3. **ADR-010 Phase 3 — `/_/config` + `nucleus config print
   --effective`.** Compliance items #6, #12, #13. Auth-gated by
   `WithAdmin()` (Casbin default-deny per ADR-004); redaction via
   `observe.DefaultRedactedKeys()` (ADR-007). Requires per-key source
   tracking the Phase 2 loader does not yet capture — that's the
   substantive new work.

4. **ADR-010 §2 layer 3 — field-semantic validation.** Ranges, enums,
   parseable durations (ADR-010 §96 validation layer 3). Out of the
   four-phase slicing; standalone follow-up on top of the now-complete
   merge engine.

5. **`session_cookie_secure` default `false`** (Phase 2b security-
   auditor MED-1). Pre-existing security default; the non-nullable
   mechanism doesn't cover it because the default is already
   permissive. Flip to `true` (breaking for local-dev plain HTTP) or
   add to the non-nullable set. Could fold into candidate #1.

6. **ADR-010 Phase 4 — Docs-sync + website + new reference
   applications under a freshly-scoped `examples/`.** Target: v0.9.X.
   New examples authored, website docs rewritten, manifest pattern
   introduced, compatibility-harness fixture profiles (`minimal-api`,
   `admin-heavy`, `plugin-heavy`) restored.

7. **Cloud Secrets Provider plugin extraction (AWS → GCP → Azure →
   Vault).** Three-iteration project following the SendGrid precedent
   (DEP-2026-002 / MA-2026-002). Removes AWS SDK from core `go.mod`.

8. **Column-type comparison in `SchemaDrift`.** Cross-dialect
   type-family compatibility table. Additive to `ExpectedColumn`.

9. **SchemaDrift end-to-end usage guide.** Bridge `model.ExtractMeta`
   → `[]db.ExpectedTable` documented in
   `docs/guides/MODELING_MULTI_DATABASE.md`.

10. **`go mod tidy` unblock.** Fix the `admin/proto` replace-directive
    issue so AWS SDK modules carry correct annotations (or, more
    elegantly, are gone entirely once candidate #7 lands).

11. **`tasks.Manager` struct→interface DEP** — optional DEP-2026-004
    for the binary-incompatible type-identity change.

12. **Audit §7 menores** — 503 path test for `/healthz`,
    endpoints-parity doc-parsing, individual tests for
    `pkg/health/{db,redis,storage}.go`.

## Carry-forward follow-ups (ADR-010 Phase 1, still open)

Non-blocker findings from the Phase 1 iteration loop, not touched by
Phase 2 (none of the Phase 2 work entered these code paths):

- **Service-shutdown timeout** — `nucleus.Run`'s `wg.Wait()` after
  `cancelServices()` has no deadline. A misbehaving service that
  ignores ctx cancellation blocks `Lifecycle.OnShutdown` indefinitely.
- **`Lifecycle.OnShutdown` context deadline** — derived from
  `context.Background()` with no bound.
- **`joinPath` double-slash collapse** — `routerAdapter.joinPath`
  produces `/x/x/123` when `prefix=/x` and `p=/x/123`.

## Files of interest

- `docs/iterations/2026-05-20-adr010-phase2bcd-config-loader-completion.md`
  — this session's archived iteration.
- `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md` — Status records
  Phases 1–2d landed; Phase 3 / Phase 4 remain.
- `pkg/nucleus/config.go`, `pkg/nucleus/nucleus.go` — the Phase 2
  loader + builder surface.
- `pkg/db/migrate.go` — `NewModuleMigrator` + namespacing.
- `pkg/admin/` — target for candidate #1.
- `contracts/freeze_test.go` — target for candidate #2 (scanner gap).

## Notes / decisions log

- 2026-05-20 — ADR-010 §2 four-phase slicing complete. Phases 2b
  (#74), 2c (#75), 2d (#76) merged in a single session, each its own
  feature PR with the full 9-subagent iteration loop. State-close
  deferred to this `/handoff` per the #61/#64/#68/#70/#73 convention.
- 2026-05-20 — ADR-010 §16 clarified: migration namespacing applies
  to both tracking tables, not only `migrationsChecksumsTable`.
- 2026-05-20 — Freeze-scanner constructor gap promoted to candidate
  #2: it leaves every `pkg/* NewXxx` constructor untracked by the
  contract freeze. Mechanical fix + large reseed.
