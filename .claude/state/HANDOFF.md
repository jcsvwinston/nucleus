# Handoff — last session closing note

> Owned by `session-curator`. Overwritten at the end of every session
> by `/handoff`. Read first by `/resume` at the start of the next one.

ITERATION:    Oracle model-scaffold identifier-casing → unquoted-uppercase + ADR-011 — COMPLETE, UNCOMMITTED (owner must commit before starting new work).
BRANCH:       main
LAST COMMIT:  06f76df chore(state): close ADR-010 Phase 3.1 iteration  [Phase 3.1 was committed AND pushed earlier this session (d28094c + 06f76df). The ONLY uncommitted change set is the Oracle identifier-casing work below.]
STATUS:       done — all acceptance criteria met, full iteration loop green (architect WARN→addressed, code NITS→addressed, security PASS, contract/freeze PASS, test-runner PASS). State files archived. The working tree contains ONLY the Oracle identifier-casing diff (the scaffold fix + ADR-011 + tests + CI + CHANGELOG + state). Owner commits the two-commit sequence below.
NEXT STEP:    OWNER MUST COMMIT the Oracle change set (the only uncommitted work). Two-commit sequence:

  COMMIT 1 (fix — Oracle):
    git add pkg/model/migration_scaffold_oracle.go pkg/model/migration_scaffold_dialects_test.go pkg/model/meta.go pkg/db/schema_drift.go .github/workflows/ci.yml CHANGELOG.md docs/adrs/ADR-011-oracle-identifier-casing.md
    git commit -m "fix(model): Oracle scaffold emits unquoted identifiers (ADR-011)

BuildOracleMigrationScaffold quoted every identifier, creating
case-sensitive lowercase tables invisible to the CRUD layer, the
migrations bootstrap, and USER_TAB_COLUMNS introspection (all of which
use unquoted/UPPER-folded names). Emit unquoted identifiers so the whole
Oracle path is consistent; re-enable the Oracle AutoMigrate CI lane."

  COMMIT 2 (state — Oracle):
    git add .claude/state/CURRENT_ITERATION.md .claude/state/HANDOFF.md docs/iterations/2026-05-23-oracle-identifier-casing-adr011.md
    git commit -m "chore(state): close Oracle identifier-casing iteration"

BLOCKERS:     none.
FILES OF INTEREST (Oracle change set — UNCOMMITTED):
  - pkg/model/migration_scaffold_oracle.go — modified: BuildOracleMigrationScaffold emits unquoted identifiers; quoteOracleIdentifier → oracleIdentifier pass-through.
  - pkg/model/migration_scaffold_dialects_test.go — modified: scaffold tests updated to assert unquoted output; pass-through test added.
  - pkg/model/meta.go — modified: oracleIdentifier pass-through function; TODO(ADR-011 follow-up) on reserved-word handling in isValidIdentifierLike.
  - pkg/db/schema_drift.go — modified: corrected stale Oracle comment (scaffold no longer double-quotes).
  - .github/workflows/ci.yml — modified: Oracle TestSQLMatrix_AutoMigrate_Exploratory lane re-enabled; NOTE breadcrumb updated.
  - CHANGELOG.md — modified: Fixed entry for Oracle identifier-casing under Unreleased.
  - docs/adrs/ADR-011-oracle-identifier-casing.md — new: ADR-011 pinning unquoted-uppercase strategy.

NOTES:
  - Oracle design: scaffold emits UNQUOTED identifiers (Oracle folds to UPPER). `quoteOracleIdentifier` renamed to `oracleIdentifier` as a pass-through — this is the single choke point for the reserved-word follow-up (ADR-011). Matches CRUD (pkg/model/crud.go bare identifiers), migrations bootstrap (pkg/db/migrate.go unquoted), introspection (schema_drift.go UPPER(...)). ADR-011 documents the rationale, the rejected quoted-everywhere alternative, and the reserved-word caveat.
  - Contract freeze: no exported-symbol change — baseline untouched, freeze PASS.
  - Security: isValidIdentifierLike is the injection gate; quoting was never the security boundary. Pre-existing LOW: dotted-identifier pass-through allows schema-qualified table names in DDL (tracked as follow-up).
  - Oracle live lane: can only be fully verified in CI (requires an Oracle container). Local test-runner PASS covers the unit/scaffold tests.
  - Two new follow-ups recorded in candidate list: (a) CI required-vs-exploratory reconciliation for mssql+oracle — PRE-EXISTING contradiction, not introduced; (b) Oracle reserved-word + dotted-identifier hardening via oracleIdentifier choke point.
  - Next iteration: owner selects from the prioritised candidate list in CURRENT_ITERATION.md. Top candidates: Oracle multi-block AutoMigrate execution (#1), session_cookie_secure default flip (#2), ADR-010 layer 3 field-semantic validation (#3).

Updated: 2026-05-23
