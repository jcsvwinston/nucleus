# Deprecation Notice: a storage block an application built WithoutDefaults() ignores → refused at v2.0.0

- ID: `DEP-2026-013`
- Status: `active`
- Announced in: `Unreleased` (the minor after `v1.30.1`; A11 N2, 2026-10-04)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-02 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`; the suite groups every breaking
  change into the one major at the close of its A12 arc (QADR-0010). This is
  a **behaviour change of a configuration combination**, not a symbol
  removal: no `// Deprecated:` marker carries it.
- Scope: `config`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

An application built `WithoutDefaults()` builds no storage. Until this
release, a storage block in its configuration — `storage.provider: s3`, any
`storage.*` key, or a `NUCLEUS_STORAGE__*` variable — was ignored without a
word: the application booted exactly as it would with no block at all, and
the first sign of it was a nil `Runtime.Storage()` or an upload that went
nowhere. That is what the api starter did with `nucleus add s3` (NU-99,
measured by the A11 catalog bench as `EN-10`…`EN-12`).

This release adds the way to build that storage — `WithStorage()` beside
`WithoutDefaults()`, which builds the storage the configuration declares and
none while it declares none — and the api starter carries it. An application
that declares storage and does not pass the option keeps starting with the
block ignored, as before, and now says so: one ERROR line at boot, and a
warning in `nucleus doctor`.

What changes and when:

- **This release:** the ignored block is reported, not refused. The boot log
  carries, once:

  ```
  level=ERROR msg="storage block IGNORED: the configuration declares storage and this application is built WithoutDefaults() without WithStorage(), so no store is built" provider=s3 fix="add WithStorage() beside WithoutDefaults() — …" install="the s3 provider is not linked into this binary: nucleus add s3" deprecation="DEP-2026-013: from v2.0.0 this configuration refuses to start"
  ```

  (`install` appears only when the selected provider is a module this
  project publishes and it is not linked.) `nucleus doctor` reports the
  same combination as a warning on its storage check, reading the
  composition root beside the configuration.
- **v2.0.0:** the same combination refuses to start, with the same message
  as the error.

## Affected Surfaces

- An application built with `app.WithoutDefaults()` /
  `nucleus.WithoutDefaults()` / `AppBuilder.WithoutDefaults()` whose
  configuration declares storage (`Config.StorageDeclared`) and that does
  not pass `WithStorage()`.
- Not affected: applications built with the defaults (storage is one of
  them), applications built `WithoutDefaults()` that declare no storage
  (nothing to build, nothing said), and applications that pass
  `WithStorage()`.
- No Go API is removed or changes shape. The additions are
  `app.WithStorage`, `nucleus.WithStorage`, `AppBuilder.WithStorage` and
  `app.Config.StorageDeclared`.

## Migration Path

- Replacement: either build the storage the configuration declares, or stop
  declaring it.
  - `nucleus.New().FromConfigFile("nucleus.yml").WithoutDefaults().WithStorage()…`
    (or `app.New(cfg, app.WithoutDefaults(), app.WithStorage())`);
  - or remove the `storage:` block and the `NUCLEUS_STORAGE__*` variables
    from that application's configuration.
- Behavior differences: with `WithStorage()` the application builds the
  declared store at boot, exactly as the default path does — tenant
  scoping, cleaner, public routes, shutdown — so a provider whose module is
  not linked, or whose configuration is incomplete, now fails the boot with
  the factory's own message (which names `nucleus add <provider>`).
- Required app changes: one builder call, or one deleted block. An
  application that shares one configuration file between a full application
  and a `WithoutDefaults()` worker that must not touch storage removes the
  block from the worker's configuration, or overrides it per process.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-013-ignored-storage-block-to-withstorage.md`
- Detection rule: the boot log line `storage block IGNORED` carrying
  `deprecation="DEP-2026-013…"`, or `nucleus doctor` reporting the storage
  block as IGNORED.
- Suggested rewrite: manual — add `.WithStorage()` after `.WithoutDefaults()`
  in the composition root, or delete the block.

## Validation

- Compatibility tests updated: `yes` — `pkg/app` pins that the combination
  still starts, builds no store and logs exactly one ERROR line naming
  `WithStorage()`, `nucleus add <provider>` when it is not linked, and this
  notice; `pkg/nucleus` pins the same through the builder; `internal/cli`
  pins the doctor warning. The additions are listed in
  `contracts/baseline/api_exported_symbols.txt` (additions only).
- Release note updated: through the squash commit, which feeds the
  release-please notes.
- Rollback plan documented: `yes` — before v2.0.0 nothing to roll back (the
  application behaves as before, plus the log line); after it, adding
  `WithStorage()` or removing the block restores a booting application.

## Timeline

- Announcement date: `2026-10-04`
- Review checkpoint: the set that certifies the release carrying this notice.
- Behaviour flip decision date: the `v2.0.0` major.

## Notes

The umbrella guard `scripts/check_deprecations.sh` reads `// Deprecated:`
markers on Go symbols. This notice deprecates a behaviour, not a symbol, so
the guard does not see it. Its version and date live in this notice, and in
the suite index of the umbrella's deprecation policy once a set publishes
it, until the guard learns to read notices that have no marker.
