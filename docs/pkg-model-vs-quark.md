# `pkg/model` and Quark: a symbol-by-symbol inventory

Nucleus has two data layers. `pkg/model` extracts metadata by reflection and
runs CRUD over `interface{}`; Quark does the same job with generics and a
query builder. This page measures the overlap, so the A4 arc can decide what
moves, what stays and what should never have been in a data layer at all.

**Measured on 2026-09-11** against nucleus v1.26.0 and quark v1.13.0.
Regenerate the two inventories with:

```bash
go doc -all ./pkg/model
cd ../quark && go doc -all .
```

`pkg/model` is 7,379 lines across nine non-test files and exports 30
top-level symbols. Quark's root package exports 527.

## 1. What `pkg/model` does that Quark does not

Seven things. Only three of them are data-layer work.

| # | Capability | Symbols | Verdict |
|---|---|---|---|
| 1 | **Presentation metadata** — label, HTML input type, choices, which fields are listed / searchable / filterable / excluded | `FieldMeta.Label`, `.HTMLType`, `.Choices`, `.IsList`, `.IsSearch`, `.IsFilter`, `.IsExcluded`, `Choice`, `parseAdminTag` | **Not data-layer work.** This is admin-panel vocabulary living in the model layer. Quark should never grow it. |
| 2 | **Runtime metadata mutation** — change a field's properties on a live registry | `Registry.UpdateFieldMeta`, `.BulkUpdateFieldMeta`, `FieldMetaUpdate` | Admin-panel need. Quark computes metadata once and caches it. |
| 3 | **CRUD without compile-time types** — operate on any registered model through `interface{}` | `CRUD`, `CRUDOperator`, `NewCRUD`, `QueryOpts`, `PaginatedResult` | **A real requirement, not an accident.** The admin panel serves models it cannot name at compile time, so `Query[T]` cannot replace it as-is. Any plan that assumes it can is wrong. |
| 4 | **Migration files generated per dialect** — deterministic CREATE/DROP DDL as text | `BuildSQLiteMigrationScaffold` and the MySQL / Postgres / MSSQL / Oracle variants, `BuildMigrationScaffoldForSystem` | Data-layer work Quark does differently: Quark plans and applies against a live database (`PlanMigration`, `Migrate`, `Sync`) rather than emitting files. This is the gap S4 has to close. |
| 5 | **`RejectClientPK`** — refuse an entity that arrives carrying its own primary key | `ModelConfig.RejectClientPK`, `ErrClientAssignedPK` | Data-layer work. A policy for handlers that decode a request body straight into an entity. Quark has no equivalent. |
| 6 | **`SanitizeOrderBy` as a shared barrier** — one order-by allow-list used by both CRUD and the admin API | `SanitizeOrderBy` | Quark validates identifiers internally but exports no equivalent, so a caller outside Quark cannot reuse the barrier. |
| 7 | **Primary key inferred from the field name** — a field called `ID` is the PK with no tag | `ExtractMeta` | Convenience Quark does not have: Quark requires `pk:"true"`. This is one half of the tag collision in §2. |

Everything else `pkg/model` exports, Quark covers and then some: hooks, query
observers, registries, foreign keys, indexes, tenant fields. And Quark carries
a large surface `pkg/model` has no answer for at all — transactions and
savepoints, CTEs, window functions, set operations, row locking, eager
loading, soft delete, caching, sharding, read replicas, native RLS, audit
logging, optimistic locking, batching, upsert, cursors, schema
introspection, backfill, per-column time zones, middleware and OTel.

**`pkg/model` has one primary key; Quark supports composite keys.**
`ModelMeta.PrimaryKey` is a single string. Quark carries `PK`, `CompositePK`
and `HasCompositePK`. A model with a two-column key cannot be expressed in
`pkg/model` at all.

## 2. The tag grammars contradict each other

Both layers read a tag called `db`. They do not agree on what it contains, and
neither notices.

| | Quark | `pkg/model` |
|---|---|---|
| separator inside `db` | comma | semicolon |
| first element of `db` | the **column name** | a **directive** |
| column name | `db:"email"` | `db:"column:email"` |
| primary key | separate tag: `pk:"true"` | `db:"pk"` |
| NOT NULL | `nullable:"false"` or `quark:"not_null"` | `db:"not null"` |
| unique | `quark:"unique"` | `db:"unique"` |
| index | — | `db:"index"` / `db:"index:name"` |
| default | `default:"…"` | — |
| rename | `quark:"rename:old"` | — |
| sizing | `db:"name,size=512"` | — |
| read-only | — | `db:"readonly"` |
| foreign key | `rel:` + `join:` | `db:"fk:table.column"` |

Quark reads eight tag keys (`db`, `pk`, `quark`, `nullable`, `default`, `rel`,
`join`, `polymorphic`). `pkg/model` reads four (`db`, `json`, `validate`,
`admin`). They intersect in exactly one — `db` — with incompatible grammars.

### What that does to a real struct

Both extractors, same struct, measured:

```
-- a struct written the Nucleus way: db:"pk", db:"column:email;unique;not null"
  quark    columns = [pk] [column:email;unique;not null] [readonly]   no PK detected
  nucleus  columns = [id(PK)] [email(NN)] [note(RO)]

-- a struct written the Quark way: db:"id" pk:"true", db:"email,size=255"
  quark    columns = [id(PK,NN)] [email(UNIQ,NN)] [note]
  nucleus  columns = [id(PK)] [email] [note]   every token UNKNOWN
```

Read the first row again: given a Nucleus-style model, **Quark believes the
table has a column literally named `column:email;unique;not null`**, and finds
no primary key. It reports nothing — no error, no warning.

In the other direction Nucleus recovers the column names by accident, because
it falls back to snake-casing the Go field name and the example's names happen
to match. The `size=255` and the `pk:"true"` are dropped; the only trace is a
boot-time WARN from `UnknownDBTokens`.

**So one struct cannot serve both layers today**, and both failure modes are
quiet. This is what S7's tag linter has to catch, and it is the strongest
single argument for the arc: the two layers are not merely redundant, they
disagree.

## 3. The two layers generate different schemas for the same model

Measured by calling each layer's type mapper directly, for PostgreSQL:

| Go type | `pkg/model` scaffold | Quark migrate | consequence |
|---|---|---|---|
| integer PK | `BIGSERIAL` | `SERIAL` | Quark's runs out at 2,147,483,647 rows |
| `int64` | `BIGINT` | `INTEGER` | values above 2³¹ do not fit |
| `float64` | `DOUBLE PRECISION` | `REAL` | `REAL` is single precision — about 6 significant digits |
| `time.Time` | `TIMESTAMPTZ` | `TIMESTAMP` | Quark's drops the time zone |
| `[]byte` | `BYTEA` | `BYTEA` | agree |

Quark's full Go-to-SQL matrix, measured across the six dialects, collapses
**every** integer width — `int8` through `int64`, `uint8` through `uint64` —
onto a single `INTEGER` (Oracle gets `NUMBER(19)`), and every float onto
`REAL` for PostgreSQL and SQLite. Arrays and maps (`[]string`, `[]int64`,
`map[string]any`) all become `TEXT` on every engine: there are no native
PostgreSQL arrays and no JSONB.

Two notes on how far this goes:

- **The `int64` → `INTEGER` mapping is a defect in its own right**, not an A4
  gap. What it does depends on the engine — PostgreSQL raises `integer out of
  range`, MySQL truncates or errors depending on its mode — and the exact
  behaviour has **not** been confirmed against live engines here, because this
  measurement had no container runtime available. Confirming it is the first
  thing S3's real-engine lane should do.
- **The divergence is the arc's whole subject.** A model migrated by
  `pkg/model` and then queried by Quark is reading columns Quark would have
  created with different widths. Whichever layer wins, the other one's
  existing schemas are the migration problem.

## 4. What this means for the plan

- **`pkg/model` cannot simply be deleted in favour of `Query[T]`** — item 3
  above. The admin panel needs CRUD over models it cannot name at compile
  time, and that is a capability, not legacy.
- **Items 1 and 2 should leave the data layer regardless of how A4 ends.**
  Presentation metadata in a model registry is the reason the two layers look
  more alike than they are.
- **Items 4, 5 and 6 are the real transfer list**: migration-file generation,
  the client-PK policy, and an exported order-by barrier.
- **The tag collision (§2) and the type divergence (§3) have to be settled
  before anything is generated with `--data quark`**, or the generator will
  emit models that one layer silently misreads.
