# Packaging-Change Notice: the asynq jobs provider moves to `providers/tasks-asynq` at v2.0.0

- ID: `DEP-2026-020`
- Status: `active`
- Announced in: the next minor after `v1.32.0` (A12 `M0-N`, decided 2026-10-06 as decision 4 of the A12 arc)
- Behaviour flip: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  `docs/governance/DEPRECATION_TEMPLATE.md`. This is a **packaging change**:
  the symbols, their signatures and their behaviour do not change; the module
  that provides them does. The suite's deprecation policy (§3) lets packaging
  move in a minor with a guided compile error, as ADR-030/031 did; the owner
  chose to carry this one with the major so that the 2.0 is the only cut an
  application has to plan for (QADR-0010). No `// Deprecated:` marker carries
  it.
- Scope: `packaging`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

`pkg/tasks/providers/asynq` is the Redis-backed jobs provider, selected with
`jobs_provider: asynq`. It lives in the framework's root module, so
`github.com/hibiken/asynq` and its Redis client are in the `go.mod` of every
application, whether it runs jobs on Redis or not. The memory and SQL
providers need no broker; asynq is the one that brings a client library with
it, and the A12 `S0` measurement of the hello-world graph listed it among the
dependencies the core carries for a feature most applications do not turn on.

What changes and when:

- **This release:** nothing. The provider stays where it is; this notice says
  where it goes.
- **v2.0.0:** the provider lives in its own module,
  `github.com/jcsvwinston/nucleus/providers/tasks-asynq`, with its own tag,
  like the drivers, exporters and the other providers (ADR-030/031). The
  package path and every exported symbol keep their names inside the new
  module. An application that selects `jobs_provider: asynq` without the
  module linked fails at boot with the guided error the other optional
  modules already use, naming `nucleus add tasks-asynq`.

## Affected Surfaces

- `jobs_provider: asynq` (and `jobs_redis_url`) in the configuration of an
  application that does not import the provider's module.
- Direct imports of `github.com/jcsvwinston/nucleus/pkg/tasks/providers/asynq`.
- `nucleus doctor`'s tasks check, which inspects an asynq deployment: it keeps
  working, from the CLI's own module, which links the provider.
- Not affected: `jobs_provider: memory` (the default) and `jobs_provider: sql`.

## Migration Path

- Replacement: at the upgrade, `nucleus add tasks-asynq` (or a blank import of
  the new module in the composition root) and the import path
  `github.com/jcsvwinston/nucleus/providers/tasks-asynq` for code that names
  the provider's symbols.
- Behavior differences: none. Same provider, same configuration keys, same
  behaviour.
- Required app changes: one `go get`/`nucleus add` and, where the package is
  imported directly, the import path.

## Migration Assistant

- Assistant spec: `docs/migration_assistants/MA-2026-020-asynq-provider-module.md`
- Detection rule: `jobs_provider: asynq` in the configuration or
  `NUCLEUS_JOBS_PROVIDER=asynq`; a direct import of
  `pkg/tasks/providers/asynq`.
- Suggested rewrite: manual — `nucleus add tasks-asynq`; `sed` on the import
  path.

## Validation

- Compatibility tests updated: `yes` — the provider's own tests
  (`pkg/tasks/providers/asynq/run_stop_test.go`, `tasks_test.go`) and the
  framework's selection of it (`TestModuleJobsAsynqSchedulerLockWiring`,
  `pkg/nucleus/jobs_leader_test.go`) run from the root module today; the 2.0
  PR moves them with the package.
- Release note updated: through the squash commit.
- Rollback plan documented: `yes` — before v2.0.0 nothing changes; after it,
  linking the module restores the provider.

## Timeline

- Announcement date: `2026-10-16`
- Review checkpoint: `2027-01-17`, when the window opens.
- Packaging change date: the `v2.0.0` major.

## Notes

The umbrella guard reads this notice through its `Status` and «no earlier
than» lines and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 22).
