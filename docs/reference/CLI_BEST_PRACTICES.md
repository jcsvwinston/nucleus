# CLI Engineering Guidelines

Reference date: 2026-04-07.
Status: Current.

This document defines the CLI quality bar for Nucleus as an enterprise and long-lifecycle framework.

## CLI Design Principles

1. Predictability
- consistent command naming and argument structure
- stable output contracts for automation paths

2. Operational Safety
- explicit confirmation/override for destructive actions
- CI-safe non-interactive execution paths
- deterministic exit codes

3. Discoverability
- clear root help and per-command help
- command summaries focused on outcome, not implementation details

4. Maintainability
- command behavior backed by tests
- SQL/engine-specific behavior documented and validated

5. Upgrade Stability
- stable commands keep behavior across `v1.x`
- deprecations are additive first and include migration guidance

## Current Nucleus CLI Baseline

Implemented areas in `cmd/nucleus` + `internal/cli`:

- Runtime and diagnostics: `serve`, `routes`, `health`, `check --deploy`, `diffsettings`, `config print --effective`
  - `serve --without-defaults` (ADR-013 / R3) serves a core-only app — no admin/authz/mail/storage — matching an `api` scaffold's `go run .`; the flag is additive and optional, so the default `serve` stays full-stack
- Scaffolding and generation: `new`, `startapp`, `generate`
- SQL lifecycle and maintenance:
  - `migrate`, `sqlmigrate`, `sqlflush`, `sqlsequencereset`, `flush`
  - `optimizemigration`, `squashmigrations`
- Data and schema operations: `dumpdata`, `loaddata`, `inspectdb`, `ogrinspect`
- Security/admin operations: `createuser`, `changepassword`, `clearsessions`, `remove_stale_contenttypes`
  - these commands follow the global output contract (`--output plain|pretty|json`)
  - `json` mode is automation-safe and emits structured status payloads
- Asset and localization workflows: `collectstatic`, `findstatic`, `makemessages`, `compilemessages`
- Mail and provider diagnostics: `sendtestemail`, `mailproviders`
- Plugin diagnostics: `plugin list`, `plugin doctor`, `plugin test`
- Developer workflows: `shell`, `test`, `testserver`

## Compatibility Aliases

Aliases currently supported for developer ergonomics:

- `runserver`, `startproject`, `makemigrations`, `showmigrations`, `createsuperuser`, `dbshell`, `check`

Policy:

- aliases are convenience entrypoints
- canonical docs and behavior are defined by primary commands

## Enterprise CLI Guardrails

Required behavior for destructive/critical commands:

- `--force` + `--yes` patterns for production-risk operations
- clear dry-run support where feasible (`--dry-run`)
- explicit failure messages with actionable remediation

## Testing and Release Requirements

1. Unit and integration coverage
- command parser coverage
- command behavior coverage for critical paths

2. SQL engine coverage
- required lanes: SQLite/PostgreSQL/MySQL
- exploratory-to-required promotion for enterprise engines (MSSQL/Oracle) based on stability evidence

3. Release gate requirements
- no unresolved critical CLI regressions
- compatibility SLO gates satisfied
- changelog includes user-visible CLI behavior changes

## Known Improvement Areas

1. Advanced shell UX
- persistent history and multiline editing.

2. Explain/diagnostic mode expansion
- better visibility for generated SQL and execution plans in operational commands.

3. Contract metadata
- lifecycle tags (`stable/transitional/experimental`) per command and subcommand.

## v1.x CLI Contract Commitments

From `v1.0` onward:

- stable command semantics will not break in `v1.x`
- deprecations will include migration guidance before any major-version removal
- automation-facing outputs remain backward compatible for stable commands

See also:

- `docs/governance/ENTERPRISE_LONG_TERM_ROADMAP.md`
- `docs/governance/COMPATIBILITY_SLO.md`
- `docs/governance/RELEASE_CHECKLIST.md`
