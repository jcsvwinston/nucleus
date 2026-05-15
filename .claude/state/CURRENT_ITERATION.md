# Current Iteration

> Owned by `session-curator`. Edited by other subagents only via the
> Session Start / Session End protocols (`CLAUDE.md` §2 and §5).

## Goal

No active iteration. The 2026-05-15 MSSQL/Oracle SchemaDrift iteration
is complete (PR #66 merged as `6a9aa00`) and archived at
`docs/iterations/2026-05-15-mssql-oracle-schemadrift.md`. The pkg/app
+ pkg/nucleus inventory pass is complete (PR #65 merged as `0690e14`)
and archived at `docs/iterations/2026-05-15-pkg-app-nucleus-inventory.md`.

## Scope

- in: (TBD — owner to confirm from the queue below)
- out: (TBD)

## Acceptance criteria

- [ ] (TBD)

## Status

### Done (2026-05-15)

- **MSSQL/Oracle SchemaDrift introspection** (PR #66, ADR-009 addendum).
  `Migrator.SchemaDrift` now supports all five engines (SQLite, PG,
  MySQL, MSSQL, Oracle). `ErrSchemaDriftUnsupported` narrowed to fire
  only for engines outside the supported set. Live-DB CI lanes
  (`TestSQLMatrix_SchemaDrift{,_Exploratory}`) exercise the four
  matrix engines on every run. Required-lane AutoMigrate live test
  also retroactively wired into CI.
- **pkg/app + pkg/nucleus inventory** (PR #65). Read-only Phase 1 prep
  for an upcoming Fluent API v2 ADR on pkg/nucleus.
- v0.7.0 released (PRs #56–#59); CSRF hardening (ADR-006, PR #60);
  slog secret redaction (ADR-007, PR #62); CSRF follow-ups + schema
  drift (ADR-008 + ADR-009, PR #63) — see prior archived iterations.

### In progress

- (none)

### Blocked

- (none)

## Candidate next steps (priority order, pending owner confirmation)

1. **`pkg/admin` bootstrap users-table DDL — dialect-aware fix for
   MSSQL/Oracle.** Discovered during PR #66 CI. Specific failures:
   MSSQL `Incorrect syntax near 'nucleus_admin_users'`, Oracle
   `ORA-03076: unexpected item DEFAULT`. `app.New` → admin bootstrap
   → ensure users table fires even with `WithoutDefaults()` (gated by
   `AdminBootstrapEmail`, not by `WithoutDefaults`). Fix replicates
   the dialect-aware discipline of `pkg/model/migration_scaffold_{mssql,oracle}.go`.
   After the fix, re-wire `TestSQLMatrix_AutoMigrate_Exploratory` into
   `.github/workflows/ci.yml` (the NOTE comments in the workflow carry
   the breadcrumb).

2. **Cloud Secrets Provider plugin extraction (AWS, then GCP/Azure/Vault).**
   Following the SendGrid precedent (DEP-2026-002 / MA-2026-002), extract
   `pkg/auth/secrets`'s AWS Secrets Manager resolver to a separate Go
   module so the AWS SDK is no longer a direct dep of the core. Removes
   ~3-5 MB of binary size and ~30 transitive deps from `go.mod` for
   operators who don't use AWS. Three-iteration project:
   - **A.** ADR + Resolver plugin contract (extends `pkg/plugins`
     capability set).
   - **B.** Extract `aws.go` → `github.com/jcsvwinston/nucleus-plugin-aws-secrets`.
     Remove AWS deps from core `go.mod`.
   - **C.** `DEP-2026-005-builtin-aws-secrets-resolver.md` +
     `MA-2026-005-aws-secrets-builtin-to-plugin.md`.

3. **Column-type comparison in SchemaDrift.** Cross-dialect type-family
   compatibility table. Additive to `ExpectedColumn` (the
   additive-growth godoc already documents the forward-compat intent).

4. **SchemaDrift end-to-end usage guide.** Bridge `model.ExtractMeta` →
   `[]db.ExpectedTable` documented in
   `docs/guides/MODELING_MULTI_DATABASE.md`.

5. **`go mod tidy` unblock** — fix the `admin/proto` replace-directive
   issue so the AWS SDK modules carry correct `// direct` annotations
   (or, more elegantly, are gone entirely once candidate #2 lands).

6. **`tasks.Manager` struct→interface DEP** — optional DEP-2026-004
   for the binary-incompatible type-identity change
   (contract-guardian advisory from the v0.7.0 release prep).

7. **Audit §7 menores:** 503 path test for `/healthz`, endpoints-parity
   doc-parsing, individual tests for `pkg/health/{db,redis,storage}.go`.

8. **ADR-010 (Fluent API v2 for pkg/nucleus) — draft is yours.**
   Untracked in primary at `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md`,
   based on the Phase 1 inventory (PR #65). Decision pending: in-place
   rewrite vs `pkg/nucleus/v2` coexistence. Phase 2 onwards starts when
   the ADR is accepted.

## Files of interest

- `docs/iterations/2026-05-15-mssql-oracle-schemadrift.md` — today's
  SchemaDrift archived iteration.
- `docs/iterations/2026-05-15-pkg-app-nucleus-inventory.md` — today's
  inventory archive (input for ADR-010).
- `docs/adrs/ADR-009-schema-drift-detection.md` — SchemaDrift API
  design with the 2026-05-15 addendum.
- `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md` (untracked) —
  owner's draft of Fluent API v2 ADR.
- `pkg/admin/` — target for candidate #1 (admin bootstrap DDL).
- `pkg/auth/secrets/` — target for candidate #2 (Cloud Secrets Provider
  plugin extraction).

## Notes / decisions log

- 2026-05-15 — decided to drop the original "Phase 4 AWS SDK opt-in
  via build tag" candidate in favour of the **plugin extraction** path
  (candidate #2). Build tags would have left AWS deps in `go.mod` and
  only saved binary link size; plugin extraction (matching the SendGrid
  precedent) removes the deps entirely from the supply chain. Skip the
  build-tag stopgap — go directly to extraction.
- 2026-05-15 — `pkg/storage` baseline candidate dropped from the queue
  (was audit §7 task 2): verified that `pkg/storage` is already in
  `contracts/freeze_test.go:161` with 134 entries in the baseline,
  added during PR #63's coordinated rebaseline. No-op.
- 2026-05-15 — admin bootstrap users-table DDL bug surfaced through
  the CI workflow fix in PR #66 (commit `cb06837`). Real pre-existing
  bug; deferred to a dedicated iteration (candidate #1).
