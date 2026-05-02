# Quark ORM Architecture

Quark is an enterprise-grade, generic-based Object-Relational Mapper for Go. It is designed around safety, immutability, and modularity.

## Core Design Principles

1. **Type Safety via Generics**: Quark leverages Go 1.18+ generics to provide a completely type-safe API. `For[T]` returns a `*Query[T]` specific to a struct model, eliminating the need for generic `interface{}` returns and assertions.
2. **Immutable Query Building**: Every method on `Query[T]` that modifies the query state (e.g., `.Where()`, `.Limit()`, `.Preload()`) returns a cloned instance of the builder. This prevents state contamination and ensures thread-safe query building.
3. **Database Independence**: The `Dialect` interface hides database-specific logic (like parameter placeholders or identifier quoting). Quark currently supports SQLite, PostgreSQL, and MySQL.
4. **Modularity and Injection**: Quark allows dependency injection of custom loggers, `QueryObserver` functions (for telemetry/metrics), and a modular Middleware chain for Hooks (`BeforeCreate`, `AfterDelete`, etc.) and Validation.

## Request Lifecycle

The standard lifecycle of a Quark database operation follows this path:

1. **Initialization (`quark.For[T]`)**: Parses struct metadata (cached via `sync.Map`), sets up the model details, and initializes an immutable `Query[T]` builder.
2. **State Construction**: The developer chains builder methods. Each method creates a clone of the `Query` state.
3. **Execution Endpoint**: A method like `.List()` or `.Create()` is invoked.
4. **Validation (Writes)**: Write endpoints intercept the model to validate using struct tags (`validate:"required"`) or a custom `Validatable` interface.
5. **Middleware & Hooks**: Operations pass through a middleware pipeline. Lifecycle hooks (`BeforeCreate`, `AfterUpdate`, etc.) are triggered before and after the core database execution.
6. **SQL Generation**: The query state is translated to dialect-specific SQL safely using `SQLGuard` to prevent SQL Injection on identifiers.
7. **Execution & Mapping**: The SQL is executed via `database/sql`, and results are reflected back into the generic struct models. Preloads are resolved efficiently using secondary `IN` queries.

## Multi-Tenant Architecture

Quark supports building multi-tenant applications efficiently without leaking connections or data, via the `TenantRouter`.

Quark abstracts three isolation strategies:
- **Database-per-Tenant**: Highest isolation. The router maintains an LRU cache of independent `*sql.DB` connection pools for each tenant to prevent connection exhaustion. Oldest connections are safely evicted via `db.Close()`.
- **Schema-per-Tenant**: Logical isolation on a single database. The router dynamically injects the `tenant_id` as a schema prefix in SQL generation (e.g., `SELECT * FROM tenant_acme.users`), heavily reducing infrastructure overhead.
- **Row-Level-Security (RLS)**: Simplest isolation. The router transparently injects a `WHERE tenant_id = ?` clause into every query via the `Query[T]` builder initialization.

## Native Execution (Procedures, Functions, and Events)

While `Query[T]` handles relational operations, Quark isolates the execution of raw database functions, stored procedures, and event streams.

1. **`Routine[T]`**: A builder similar to `Query[T]` designed to call Table-Valued Functions or scalar SQL functions. The `Dialect` implementation ensures proper translation (`SELECT * FROM func()` in Postgres vs `CALL proc()` in MySQL).
2. **`Call`**: For pure logic stored procedures (that mutate data or use `sql.Out` parameters), `quark.Call()` issues direct execution statements.
3. **`EventBus`**: An abstraction for database-native pub/sub events (e.g., PostgreSQL `LISTEN`/`NOTIFY`).

## Next Steps

See `ROADMAP.md` for future architectural extensions.
