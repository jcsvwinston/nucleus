# pkg/model — contract

Lifecycle: `stable`. Split out of `API_CONTRACT_INVENTORY.md` (DX-12):
the machine-auditable contract prose lives here, one file per package, so
the inventory table stays readable for humans.

## Contract scope

`BaseModel`, metadata extraction, registry, CRUD interfaces and hooks; dialect-aware migration scaffolds (`BuildSQLiteMigrationScaffold`, `BuildPostgresMigrationScaffold`, `BuildMySQLMigrationScaffold`); `SanitizeOrderBy(meta *ModelMeta, raw string) (string, error)` — shared order-by allow-list used by both the CRUD layer and the admin API (ADR-011 injection barrier)

## Notes

Foundation for model/admin integration. Multi-driver `AutoMigrate` dispatches on `db.DB.System()`.
