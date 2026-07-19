---
sidebar_position: 2
title: Support & compatibility
covers: []
config_keys: []
---

# Support & compatibility policy

This page tells you what you can rely on when you build on Nucleus, what
counts as a breaking change, and how you will hear about one before it
reaches you.

## Versions

Nucleus follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The project is on the stable `v1.x` line (`v1.0.0` was tagged 2026-07-10).

Support is on the current `v1.x` line: fixes land in new `v1.x` releases,
and upgrading from one `v1.x` release to a later one is meant to be a
drop-in change for code that only uses stable surfaces. There is no
separate long-term-support branch — staying current on `v1.x` *is* the
supported path.

Every release is described in
[`CHANGELOG.md`](https://github.com/jcsvwinston/nucleus/blob/main/CHANGELOG.md).
Read it between upgrades; it is the one place where behaviour changes are
recorded.

Nucleus builds with the Go toolchain declared in its `go.mod` (currently Go
1.26). Raising the minimum Go version is treated as a notable change and is
called out in the changelog.

## What is covered

Three surfaces carry the compatibility promise:

1. **Public Go API** — the exported symbols in `pkg/*`.
2. **CLI** — the registered commands, their flags, and the JSON shape of
   their structured output.
3. **Configuration** — the registered keys in `nucleus.yml` and their
   documented defaults.

Everything else is an implementation detail: packages under `internal/`, the
concrete third-party types behind an interface, log wording, the internals of
generated files. Those can change in any release.

### Maturity levels

Not every public package is at the same stage. Each one carries a level:

| Level | What it means |
| --- | --- |
| `stable` | Covered by the promise below. Breaking changes only in a new major version, and only after a deprecation cycle. |
| `transitional` | Public and supported, but still maturing. Breaking adjustments may land in a minor release, always with release notes. |
| `experimental` | No compatibility guarantee yet. Published for early use and feedback; expect it to move. |

Most of the framework — `pkg/app`, `pkg/router`, `pkg/db`, `pkg/model`,
`pkg/auth`, `pkg/authz`, `pkg/mail`, `pkg/storage`, `pkg/tasks`,
`pkg/observe`, `pkg/observability`, `pkg/circuit` and the rest — is `stable`.

Two areas were deliberately left outside the `v1.0` promise so they can keep
improving without forcing a new major:

- **`pkg/openapi`** is `experimental`. The document model and helpers are
  usable today, and the CLI scaffolding builds on them, but the surface may
  still grow or change shape.
- **`pkg/outbox`** is `transitional`. The store and dispatcher work and are
  supported, but some ergonomics may still tighten. One nuance worth knowing:
  the outbox's *configuration* keys live on `pkg/app.Config`, so the config
  shape is frozen with `pkg/app` even though the Go surface is not.
- The **external CLI command bridge** (`nucleus-<name>` binaries found on
  `PATH`) is `transitional` and intentionally minimal.

The authoritative, per-symbol lists are the
[API contract inventory](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/API_CONTRACT_INVENTORY.md),
the
[CLI contract matrix](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CLI_CONTRACT_MATRIX.md)
and the
[config key registry](https://github.com/jcsvwinston/nucleus/blob/main/docs/reference/CONFIG_KEY_REGISTRY.md).
If a symbol, command or key does not appear in one of those, do not build on
it.

## What counts as a breaking change

On a `stable` surface, all of these are breaking:

- removing or renaming an exported symbol, a CLI command, a flag, or a
  config key;
- changing the signature of an exported function or method, or the shape of
  an exported type;
- changing a key in the JSON a CLI command emits;
- changing a documented default in a way that alters the behaviour of an app
  that never touched that setting.

None of them will happen inside `v1.x`. They are reserved for a new major
version, and they are preceded by the deprecation cycle below.

*Adding* a symbol, command, flag or config key is not breaking, and happens
in ordinary minor releases. Correcting behaviour that contradicts the
documentation is treated as a bug fix; the fix is called out in the
changelog.

## How deprecation works

Nothing on a stable surface disappears without warning. A deprecation runs
through three stages:

1. **Marked.** The symbol, command or key is documented as deprecated and
   the intended removal release is stated. It keeps working exactly as
   before.
2. **Warned.** Using it emits a structured warning at runtime or from the
   CLI, so the deprecation shows up in your logs and not only in release
   notes.
3. **Removed.** At the announced release — never earlier, and never inside
   `v1.x` for a stable surface. The removal is recorded in the changelog
   under that version.

Where a deprecation requires changes in your code, migration notes ship with
it; where the change is mechanical, a migration assistant performs the edit
for you.

## How the promise is enforced

The policy is checked by tests, not by good intentions.

- **Contract freeze tests.** The exported symbols of the stable packages, the
  list of CLI commands, the JSON keys those commands return, and the valid
  `nucleus.yml` key paths are each pinned to a recorded baseline. Any net
  removal fails the build. The baselines are never edited just to make a test
  pass — they are regenerated only as part of a deliberate, documented change.
- **Type firewall.** A test parses every stable package and fails if a
  third-party type appears in an exported signature. That is what keeps
  third-party libraries replaceable, and it is why moving between SQLite,
  PostgreSQL and MySQL is a configuration change rather than a code change.
- **Release validation.** Before a release is tagged, the framework is run
  against fixture applications and against every supported database engine. A
  regression on a stable contract blocks the release.

## Databases

SQLite, PostgreSQL and MySQL are supported out of the box. MSSQL and Oracle
are supported behind the `mssql` and `oracle` build tags. What "supported"
means differs by subsystem, and the honest map is:

| Subsystem | SQLite | PostgreSQL | MySQL | MSSQL | Oracle |
|---|---|---|---|---|---|
| Model CRUD, migrations, cache | ✓ | ✓ | ✓ | ✓ | ✓ |
| SQL session store | ✓ | ✓ | ✓ | — | — |
| Outbox | ✓ | ✓ | ✓ | — | — |

The CRUD/migrate/cache row is exercised against real engines in release
validation. The SQL session store and the outbox only speak
sqlite/postgres/mysql — configuring them on MSSQL or Oracle fails fast at
startup with a clear error instead of emitting SQL the engine rejects at
runtime.

Engine-specific limitations of the generic CRUD `Create`, declared rather
than papered over:

- **Oracle: no primary-key back-fill.** After `Create`, the entity's ID field
  stays at its zero value on Oracle (the driver needs a `RETURNING … INTO`
  output binding the generic CRUD does not use). Inserting works; if your code
  needs the generated key immediately, query it back explicitly.
- **MSSQL: `OUTPUT` vs triggers.** The primary-key back-fill uses
  `OUTPUT INSERTED`, which SQL Server rejects on tables that have triggers
  (error 334). On such tables, inserts through CRUD fail — either drop the
  trigger or perform that insert with plain SQL.
- **MSSQL: explicit integer keys vs `IDENTITY`.** A pre-assigned integer key
  is passed through to the engine (see the Create semantics on the models
  page). SQL Server rejects explicit values for `IDENTITY` columns with its
  own clear error (544, `SET IDENTITY_INSERT`); SQLite, PostgreSQL and MySQL
  accept them. Use DB-generated keys on identity columns, or a non-identity
  column for natural keys.

## Reporting a compatibility problem

If an upgrade within `v1.x` breaks code that only uses stable surfaces, that
is a bug in Nucleus, not in your application. Open an issue on the
[repository](https://github.com/jcsvwinston/nucleus/issues) with the two
versions and a minimal reproduction.
