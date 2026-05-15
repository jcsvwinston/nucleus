# ADR-009: Schema-Level Drift Detection

**Status:** Accepted
**Date:** 2026-05-15
**Supersedes:** No
**Related:** Audit `docs/audits/2026-05-14-post-sprint-readiness.md` §3 row 9 / §7 task 8 (recommended this work); file-level drift in `pkg/db/migrate.go` (the sibling check this ADR complements).

## Context

`pkg/db/migrate.go` already reports two kinds of file-level migration drift through `Migrator.Drift()`:

- `DriftKindMissingUpFile` — the database remembers a migration the disk forgot.
- `DriftKindChecksumMismatch` — the `.up.sql` on disk differs from the one applied to the database.

Both are *file*-level: they answer "do the migration files match what was applied?" but not "does the **live database shape** match what the application **expects to see**?". The 2026-05-14 audit explicitly flagged this gap (§3 row 9: "Drift detection migraciones — parcial (file-level)"; §7 task 8: "Drift detection schema-level — Checksum del .up.sql aplicado vs. archivo vivo. La estructura DriftEntry ya existe en pkg/db/migrate.go:171-176; basta extender."). The audit's framing of "basta extender" was optimistic — extending file-level `DriftEntry` would have conflated two structurally different checks (a missing file vs. a missing live column), so this ADR adds a separate, parallel `SchemaDriftEntry` type and a separate method.

The real-world drifts this needs to catch:

1. A column added to the model after the initial migration but never followed by a new migration file — `AutoMigrate` does not ALTER existing tables, so the new column never makes it into the DB.
2. A column added to the live DB by an ad-hoc `ALTER TABLE` (psql session, sidecar migration, a manual fix during an incident) but never reflected back into the model.
3. A column whose nullability has drifted out-of-band — someone relaxed a `NOT NULL` constraint with a manual `ALTER`, or the migration that originally created it had a bug that produced a different polarity than the model claims.
4. A model declaring a table that does not exist at all — the deployment never ran `AutoMigrate` after a model was added.

## Decision

### 1. New method on `Migrator` — `SchemaDrift(ctx, []ExpectedTable) ([]SchemaDriftEntry, error)`

Add a separate method rather than extending `Drift()` for two reasons:

- The data shape is different. `DriftEntry` is keyed by migration ID with file-level fields (`ExpectedChecksum`, `ActualChecksum`); `SchemaDriftEntry` is keyed by `(table, column)` with constraint-level fields (`Expected`, `Actual` polarity strings). Sharing a struct would force one of them to carry a half-populated payload.
- The cost profile is different. File-level `Drift()` is cheap and pure (file I/O + a few queries to the bookkeeping tables) and is safe to run on every deploy. Schema-level drift requires per-dialect introspection queries against `information_schema` (or `pragma_table_info` for SQLite), one round-trip per checked table. Callers should be able to choose one without paying for the other.

### 2. Model-agnostic structured input — `[]ExpectedTable`

The natural input would be `[]any` of model structs, paired with a call to `model.ExtractMeta` inside `SchemaDrift`. That implementation drafts cleanly until you compile the tests: `pkg/model/model_test.go` already imports `pkg/db` (to construct a test DB for the model fixtures), so adding a reverse `pkg/db → pkg/model` import in production code completes the cycle and breaks `pkg/model`'s test build.

The fix is to invert the dependency. `SchemaDrift` accepts a model-agnostic `[]ExpectedTable` (table name + column list + nullability), and callers bridge from `model.ExtractMeta` themselves. `ExpectedTable` and `ExpectedColumn` are deliberately minimal so the surface can grow additively (a future `Type`, `Default`, or `CheckConstraint` field on `ExpectedColumn` is a non-breaking addition) — the godoc warns callers to use field-named struct literals so positional initializers never tie consumers to today's field count.

Trade-off: callers do one extra step (write the bridge). In return, the package boundary is clean, there is no implicit reflection contract embedded in `pkg/db`, and the API is usable for non-`pkg/model` schema sources (manual specs, third-party ORMs, schema migration tools).

### 3. Drift kinds — four cases, no type comparison

Four distinct kinds are emitted, each with a well-defined cause:

- `DriftKindSchemaMissingTable` — the live DB does not have a table the caller expects. AutoMigrate was never run, or the migration was rolled back.
- `DriftKindSchemaMissingColumn` — the table exists but a column the caller expects is absent. AutoMigrate does not ALTER; this is the "column added to the model after the initial migration" case.
- `DriftKindSchemaExtraColumn` — the table has a column the caller does not expect. The ad-hoc-DDL case.
- `DriftKindSchemaColumnNullability` — both sides have the column, but one says `NOT NULL` and the other says nullable. The `Expected`/`Actual` fields carry the polarity strings (`"not_null"` or `"nullable"`).

**Column-type comparison is explicitly out of scope.** Cross-dialect type families (BIGINT vs INT vs BIGSERIAL vs NUMBER vs NVARCHAR vs VARCHAR vs TEXT) require a per-dialect compatibility table that does not exist today and would be invasive to build. Nullability is deterministic across dialects and can be compared today; types should wait for a follow-up iteration that owns the per-dialect mapping.

### 4. Dialect support — three engines now, two on a sentinel

SQLite, PostgreSQL, and MySQL are fully supported. The introspection paths are:

- SQLite: `pragma_table_info(?)` with a pre-existence check against `sqlite_master`. The SQLite-specific quirk that `INTEGER PRIMARY KEY` columns report `notnull=0` is normalised: when the PRAGMA's `pk` column is non-zero, the comparator treats the column as `NOT NULL` (which is the truth — those columns are an alias for ROWID, never null).
- PostgreSQL: `information_schema.columns` filtered by `table_schema = current_schema()`.
- MySQL: `information_schema.columns` filtered by `table_schema = DATABASE()`.

MSSQL and Oracle return `ErrSchemaDriftUnsupported`, a documented sentinel callers can `errors.Is` against. The introspection queries for those engines are not difficult — MSSQL uses `INFORMATION_SCHEMA.COLUMNS` with `@p1`-bound parameters; Oracle uses `USER_TAB_COLUMNS` with `:1`-bound parameters and `NULLABLE` returning `'Y'`/`'N'` rather than `'YES'`/`'NO'`. The reason they are deferred is not technical complexity, it is verification scope: the live-DB AutoMigrate test added in this same iteration runs on the matrix-required (PG/MySQL) and matrix-exploratory (MSSQL/Oracle) CI lanes; a SchemaDrift implementation for MSSQL/Oracle deserves the same level of live-DB coverage before it ships, and that is more than this iteration can take on.

### Alternatives considered

- **Extend `Drift()` and `DriftEntry`.** Rejected — file-level and schema-level drift have structurally different payloads and different cost profiles; sharing the type would force half-populated entries either way.
- **Take `[]any` of models and call `model.ExtractMeta` internally.** Rejected — creates a `pkg/db ↔ pkg/model` test-path cycle. The `[]ExpectedTable` structured input is the architectural fix.
- **Include column-type comparison in this iteration.** Rejected — cross-dialect type compatibility is its own rabbit hole and would inflate scope past what the audit recommended.
- **Implement MSSQL/Oracle introspection in this iteration.** Rejected on verification grounds (see above), not technical grounds.

## Consequences

### Positive

- The "model says column X exists but the live DB does not" case is now diagnosable through a single library call. The audit's §3 row 9 gap closes.
- The API surface is clean: model-agnostic, stdlib-only, no third-party dependency added.
- The sentinel error pattern lets callers gracefully degrade on MSSQL/Oracle without parsing error strings.

### Negative

- **MSSQL/Oracle return the sentinel, not real drift information.** Operators on those engines cannot rely on schema drift detection until a follow-up iteration adds the introspection. The sentinel is documented and `errors.Is`-checkable so the gap is at least observable, not silent.
- **Column types are not compared.** A column whose type drifted (e.g. `VARCHAR(255)` → `TEXT`) is invisible to this check. A future iteration can extend `ExpectedColumn` additively.

### Neutral

- The bridge from `model.ExtractMeta` to `[]ExpectedTable` is the caller's responsibility. It is a handful of lines (extract meta, map fields to `ExpectedColumn`, derive nullability from `IsRequired || IsPK`). The unit tests in `pkg/db/schema_drift_test.go` use directly-constructed `ExpectedTable` values; an integration test in `pkg/app` (live-DB AutoMigrate) demonstrates the model-driven path.

## Compliance

After this ADR is accepted:

1. `pkg/db/schema_drift.go` exists and exports `SchemaDrift`, `ExpectedTable`, `ExpectedColumn`, `SchemaDriftEntry`, `ErrSchemaDriftUnsupported`, and the four `DriftKindSchema*` constants.
2. The contract baseline (`contracts/baseline/api_exported_symbols.txt`) includes all of the above.
3. `docs/reference/API_CONTRACT_INVENTORY.md` lists the schema-level drift symbols in the `pkg/db` row.
4. `CHANGELOG.md` under `[Unreleased]` records the addition under `Added`.
5. `pkg/db` does **not** import `pkg/model` (the architectural rule that made `[]ExpectedTable` the chosen input).
6. SQLite / PostgreSQL / MySQL implementations are exercised by unit tests; MSSQL and Oracle return `ErrSchemaDriftUnsupported`.

## Related

- [`pkg/db/schema_drift.go`](../../pkg/db/schema_drift.go) — the implementation.
- [`pkg/db/migrate.go`](../../pkg/db/migrate.go) — file-level `Drift()`, the sibling check.
- `docs/audits/2026-05-14-post-sprint-readiness.md` §3 row 9, §7 task 8 — the audit recommending this work.
- ADR-001: stdlib-first runtime — `database/sql` plus `information_schema` is the stdlib path; no dependency added.
- SPEC.md §"SQL-first operations" — the framing this ADR makes concrete for runtime schema introspection.
