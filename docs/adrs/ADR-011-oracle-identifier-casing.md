# ADR-011: Oracle Identifier Casing — Unquoted (Upper-Folded)

**Status:** Accepted
**Date:** 2026-05-23
**Reference date:** 2026-05-23
**Supersedes:** No
**Related:** PR #78 (the admin-bootstrap DDL + Oracle scaffold `/`-terminator fixes that surfaced this bug); ADR-009 (schema-level drift detection — the introspection path this interacts with); the disabled `TestSQLMatrix_AutoMigrate_Exploratory` Oracle CI lane.

## Context

The framework had **two competing Oracle identifier strategies** in flight, and they were mutually incompatible:

1. **The migration scaffold** (`pkg/model/migration_scaffold_oracle.go`, `BuildOracleMigrationScaffold`) wrapped *every* identifier in double quotes — `CREATE TABLE "ci_automig_live_users" ("id" NUMBER …)`. In Oracle a double-quoted identifier is **case-sensitive** and stored exactly as written, so this produced lower-snake-case tables and columns.

2. **Everything else in the Oracle path** emits **unquoted** identifiers, which Oracle folds to **UPPER CASE** at parse time:
   - The CRUD runtime layer (`pkg/model/crud.go`) builds `SELECT … FROM <table>`, `INSERT INTO <table>`, etc. with bare, interpolated identifiers. This is the **hard constraint**: a bare `SELECT … FROM users` resolves to `USERS` and *cannot* find a quoted-lowercase `"users"` table.
   - The migrations/checksums bootstrap (`pkg/db/migrate.go`) creates `nucleus_schema_migrations` / `nucleus_schema_migration_checksums` unquoted → stored `UPPER`.
   - Schema introspection (`pkg/db/schema_drift.go`, and the live `automigrate_live_test.go`) matches names with `WHERE TABLE_NAME = UPPER(:1)` against `USER_TABLES` / `USER_TAB_COLUMNS`.

The collision is observable end-to-end: AutoMigrate runs the scaffold, creating `"ci_automig_live_users"`; the post-migrate verification then queries `WHERE TABLE_NAME = UPPER('ci_automig_live_users')` = `CI_AUTOMIG_LIVE_USERS` and finds **nothing**. The table exists but is invisible to the rest of the framework. This was the third bug in the PR #78 chain and is documented in the (previously disabled) Oracle `TestSQLMatrix_AutoMigrate_Exploratory` CI lane breadcrumb.

## Decision

**Adopt unquoted identifiers as the framework's single Oracle identifier strategy.** Oracle's natural upper-case folding is the canonical form; every layer relies on it, so the scaffold — the lone outlier — is brought into line.

Concretely:

- `BuildOracleMigrationScaffold` emits identifiers **verbatim and unquoted**. The former `quoteOracleIdentifier` helper is renamed `oracleIdentifier` and reduced to a documented pass-through, kept as the **single choke point** for identifier emission (see Consequences for why this matters).
- No change is required to the CRUD layer, the migrations bootstrap, or introspection — they were already correct. The `schema_drift.go` Oracle comment is corrected (it previously described the scaffold as double-quoting).
- The Oracle `TestSQLMatrix_AutoMigrate_Exploratory` CI lane line is re-enabled.

### Rationale

- **The CRUD layer cannot be made to quote cheaply, and is the runtime truth.** Runtime queries are bare today; a quoted-lowercase table is dead on arrival for them. Aligning the scaffold to unquoted makes scaffolded tables usable at runtime with zero CRUD changes.
- **Consistency with the rest of the path.** Migrations bootstrap and introspection already assume upper-folded names; the scaffold becomes consistent rather than special.
- **Operational norms.** Oracle DBAs expect upper-cased identifiers in the data dictionary; quoted-lowercase schemas are a well-known operational footgun (every ad-hoc query must then quote with exact case).
- **Smaller, safer change.** The fix touches one emission site plus tests, versus rewriting every Oracle query path.

### Rejected alternative — quote everywhere

Make the CRUD layer, the migrations bootstrap, *and* introspection all quote identifiers (consistent quoted-lowercase). This would preserve the model's casing and immunise the framework against reserved words, but:

- it is a far larger blast radius (every Oracle `SELECT`/`INSERT`/`UPDATE`/`DELETE`, every introspection `WHERE`, every bootstrap DDL),
- it diverges from Oracle operational norms, and
- it is higher-risk for a database lane that is *exploratory* (not yet a required gate for AutoMigrate).

The casing benefit does not justify the cost and risk for a pre-`v1.0`, exploratory Oracle tier.

## Consequences

**Positive**

- Scaffolded Oracle tables are visible to introspection and usable by the CRUD layer; the AutoMigrate round-trip closes.
- One coherent identifier strategy across the Oracle path; the `schema_drift.go` `UPPER`-or-literal hedge is now genuinely just a hedge for hand-rolled quoted DDL, not a workaround for our own scaffold.
- The Oracle AutoMigrate exploratory CI lane is unblocked.

**Negative / caveats**

- **Reserved-word vulnerability (pre-existing, not introduced here).** Unquoted identifiers break on Oracle reserved words (a column named `comment`, `number`, `date`, `level`, …). This vulnerability already existed in the bare-identifier CRUD layer regardless of the scaffold; this ADR does not widen it (the scaffold's quoted form was the only thing *not* exposed, but it was unusable anyway). `isValidIdentifierLike` does not yet reject reserved words. Handling this — most likely *selective* quoting of reserved words at the `oracleIdentifier` choke point **and** the corresponding CRUD-layer quoting — is tracked as a separate follow-up. Keeping `oracleIdentifier` as a single function (rather than inlining bare strings) is deliberate: it is exactly where that selective-quoting logic will land.
- Model-declared mixed-case identifiers lose their case in Oracle (folded to upper). This is consistent with how Oracle treats unquoted DDL and with the other supported engines' effective behaviour for the framework's snake_case identifiers.

**Neutral**

- No public Go API changes (`oracleIdentifier` is unexported; the scaffold's `(up, down, error)` signature is unchanged). No contract-baseline impact.
