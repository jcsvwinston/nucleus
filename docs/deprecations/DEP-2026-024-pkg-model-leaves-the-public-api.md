# Deprecation Notice: `pkg/model` leaves the public API at v2.0.0

- ID: `DEP-2026-024`
- Status: `removed` (applied on `dry-run/2.0`, 2026-10-23; the `v2.0.0` tag publishes it)
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`; decision D5 of the owner, 2026-10-16)
- Earliest removal: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md` and the suite's deprecation policy.
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`pkg/model` is the framework's own data layer: a registry of models
(`Registry`, `ModelMeta`, `FieldMeta`), reflection-based metadata, a generic
`CRUD` over `database/sql`, migration scaffolds per engine and lifecycle
signals. It predates Quark. Quark is the data layer of the suite (A4, A8),
and keeping a second one in the framework means two tag vocabularies that
disagree about what `db` means (the umbrella's finding «las dos capas de
datos se contradicen»), two ways to write a model in the documentation, and
a starter that teaches the one the suite does not recommend.

The owner decided on 2026-10-16 that `pkg/model` leaves the public API with
the major — not that it is demoted and kept — and that the Orbit adapter
built on it (`datasource/nucleus`) leaves with it, so that the admin panel
reads Quark models only (`quarkdatasource`). The 48 exported symbols of the
package are marked `// Deprecated:` in this release; nothing is removed in
v1.x.

What changes and when:

- **This release:** the package, `BaseModel`, `NewRegistry` and `NewCRUD`
  carry the marker; everything keeps working. The starter (`nucleus new`),
  `nucleus generate model` and `nucleus generate resource` still produce
  `pkg/model`, and `nucleustest`'s factories use it; `nucleus generate
  module` already builds on Quark. Moving the rest to Quark is the «DX al 5»
  work of the suite's 2.0 contract (§2.1), additive, in 1.x minors before the
  major.
- **v2.0.0:** `pkg/model` is no longer part of the public API. What the
  framework still needs internally — the migration scaffolds the generators
  render, the SQL observer — moves under `internal/`. `Runtime.Models()` and
  `App.Models` go with the registry. Applications define their models with
  Quark and register them with the application the way the starter of that
  release does.

## Affected Surfaces

- `github.com/jcsvwinston/nucleus/pkg/model`: every exported symbol
  (`BaseModel`, `Registry`, `NewRegistry`, `ModelMeta`, `FieldMeta`,
  `ExtractMeta`, `CRUD`, `NewCRUD`, `CRUDOperator`, `Filter`, `QueryOpts`,
  `PaginatedResult`, the `Build*MigrationScaffold` functions, the SQL
  observer…).
- `nucleus.Runtime.Models()` and `app.App.Models`, which expose the registry.
- The lifecycle signals `pkg/signals` emits for it (DEP-2026-025).
- Orbit's `datasource/nucleus` adapter (its own notice, in Orbit).
- Not affected: Quark models and `quarkdatasource`; the SQL-first migrations
  of `pkg/db`, which are not tied to `pkg/model`.

## Migration Path

- Replacement: a Quark model (`quark.For[T]`, `quark:"..."` tags) and Quark's
  hooks for what the lifecycle signals did. The suite documents the pairing
  in Nucleus' «Using Quark with Nucleus» and in Quark's `init --with nucleus`.
- Behavior differences: Quark checks versions, scopes tenants and escapes
  `LIKE` by its own rules; the migration is a rewrite of the model layer, not
  a rename. The generators of a later 1.x minor produce the Quark form.
- Required app changes: model definitions, the handlers that call
  `model.CRUD`, and tests that use `nucleustest`'s factories over
  `pkg/model`.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-024-pkg-model-to-quark.md`
- Detection rule: `grep -rln 'nucleus/pkg/model"' --include='*.go'`;
  staticcheck SA1019 on the package.
- Suggested rewrite: manual, with the generators of the minor that moves
  them as the reference for the target form.

## Validation

- Compatibility tests updated: `yes` — the package's own tests
  (`pkg/model/*_test.go`, from `TestInferHTMLType` on) pin that everything
  keeps working in v1; the 2.0 PR moves or deletes them with the package.
- Release note updated: through the squash commit.
- Rollback plan documented: `yes` — nothing changes in v1.x.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Removal decision date: the `v2.0.0` major.
- Applied: `dry-run/2.0`, 2026-10-23 — `pkg/model` moved to `internal/model`; `App.Models`, `App.RegisterModel`, `ErrModelsRegistryNotInitialized`, `Runtime.Models`, `Module.Models` and `ModuleSpec.Models` removed; `CheckModule` loses its "models" verdict. The generators and the public site still teach `pkg/model` until §2.1 of the suite contract lands, and the tests that build a generated project are red on the branch by design (see Notes).

## Notes

The umbrella guard reads the markers (they cite this notice, the version and
the date) and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 26). The decision went
against the recommendation of the measurement session (demote in 2.0, remove
in 3.0); the owner took it knowing that it retires the Orbit adapter.

Applied on `dry-run/2.0` (2026-10-23), with what the notice did not name:
the registry's other public faces went with it (`App.RegisterModel`,
`ErrModelsRegistryNotInitialized`, `Module.Models`, `ModuleSpec.Models` —
an interface method, so a hand-rolled `ModuleSpec` loses it — and the
"models" verdict of `CheckModule`); `hooks.NewSQLObserver` keeps a public
signature because `pkg/observability/hooks` now owns `SQLQueryEvent` and
`SQLQueryObserver`. `App.AutoMigrate(models ...any)` and
`nucleustest.Make` still read the `db:` tag grammar through
`internal/model.ExtractMeta`, and the boot-time WARN for an unrecognised
directive runs in `AutoMigrate`, per model, since there is no registry to
sweep. What waits on §2.1 of the suite contract (generators, starter,
`nucleustest` factories and the public pages on Quark, in a 1.x minor):
`nucleus generate model|resource` and `startapp` still write `pkg/model`
into user code, the quickstart listing still embeds `model.BaseModel`, and
the tests that `go mod tidy` a generated project
(`TestGenerateMultipleEntitiesBuilds`, `TestRunGenerateResourceBuilds`,
`TestQuickstartSliceServesOnTheScaffold`, `TestRun_GenerateResource`,
`TestRun_StartAppScaffold`, `TestRun_OpenAPIExport`) are red on the branch
by design, as is the suite-starter lane through Orbit's retired adapter.
