# pkg/db — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`db.New`, `db.DB` (incl. `Health`, `System`), `db.Config` (incl. `StatementObserver` — opt-in driver-level SQL instrumentation, ADR-021), `db.StatementObserver`/`db.StatementInfo`, migrator APIs (`NewMigrator`, `NewModuleMigrator`, migration lifecycle methods, file-level `Drift`/`DriftEntry`/`DriftKindMissingUpFile`/`DriftKindChecksumMismatch`, schema-level `SchemaDrift`/`SchemaDriftEntry`/`ExpectedTable`/`ExpectedColumn`/`DriftKindSchemaMissingTable`/`DriftKindSchemaMissingColumn`/`DriftKindSchemaExtraColumn`/`DriftKindSchemaColumnNullability`/`ErrSchemaDriftUnsupported`), `ExecScript` (dialect-aware migration-script executor), SQL URL support

## Notes

URL schemes `sqlite://`, `postgres://`/`postgresql://`, `mysql://`, `sqlserver://`/`mssql://`, `oracle://` are all `stable` (MSSQL/Oracle promoted to required CI gate 2026-05-12). `ExecScript(execer, system, script)` splits an Oracle script on `/`-terminator lines (stripping them — go-ora runs one PL/SQL block per Exec) and Execs each block; non-Oracle dialects pass through to a single Exec. Used by `App.AutoMigrate` and the file Migrator. The `execer` parameter is an unexported interface satisfied by `*sql.DB`/`*sql.Tx`. `SchemaDrift` introspects the live schema for all five supported engines (SQLite, PostgreSQL, MySQL, MSSQL, Oracle — ADR-009 addendum 2026-05-15). `ErrSchemaDriftUnsupported` fires only for engines outside the supported set. `NewModuleMigrator` (ADR-010 Phase 2d) namespaces a Migrator's applied / checksum bookkeeping under `<moduleName>/<file-id>` so several modules can share a database alias without colliding on identical migration filenames. `Migrator.Drift` is ownership-aware: only reports drift for the rows the current Migrator owns.
