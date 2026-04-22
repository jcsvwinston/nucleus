# CLI Contract Matrix

Reference date: 2026-04-07.
Status: Current.

This file defines lifecycle tags for GoFrame CLI command contracts.

Command source of truth:

- primary commands: `internal/cli/root.go` (`commandSpecs`)
- compatibility aliases: `internal/cli/aliases.go` (`commandAliases`)

## Lifecycle Tags

- `stable`: command name and core behavior are treated as compatibility contract.
- `transitional`: supported command path, but details may evolve faster while keeping migration notes.
- `experimental`: command or behavior has no compatibility guarantee.

## Primary Command Matrix

| Command | Lifecycle | Contract Notes |
| --- | --- | --- |
| `serve` | `stable` | HTTP server bootstrap command contract. |
| `routes` | `stable` | Route introspection contract; use `--json` for automation when available. |
| `health` | `stable` | Dependency health contract; `--json` output is automation-safe. |
| `new` | `stable` | Project scaffold entrypoint contract. |
| `startapp` | `stable` | In-project app scaffold contract. |
| `generate` | `stable` | Scaffold/generator command contract. |
| `openapi` | `experimental` | Exports the project OpenAPI JSON document from `internal/contracts`; generated runtime serving should use that same document builder, and the current subset includes scaffolded JSON request/response metadata, shared `data`/`count` response envelopes, shared JSON error/empty responses, and explicit path/query parameters where declared. |
| `migrate` | `stable` | Migration lifecycle command contract. |
| `sqlmigrate` | `stable` | SQL preview for migration files. |
| `sqlflush` | `stable` | SQL preview for flush behavior. |
| `sqlsequencereset` | `transitional` | Stable for primary engines; enterprise-engine SQL edge cases still maturing. |
| `flush` | `stable` | Destructive operation guardrails (`--force`/`--yes`) are contract behavior. |
| `seed` | `stable` | SQL seed execution contract. |
| `dumpdata` | `stable` | Fixture export contract. |
| `loaddata` | `stable` | Fixture import contract with safety flags for destructive modes. |
| `inspectdb` | `transitional` | Command contract is stable; generated code shape may evolve by dialect improvements. |
| `ogrinspect` | `transitional` | Geospatial introspection is supported but still maturing across engines. |
| `createcachetable` | `stable` | Cache table provisioning contract. |
| `clearsessions` | `stable` | Session cleanup contract (`expired`/`all`). |
| `createuser` | `stable` | Admin user create/update contract. |
| `changepassword` | `stable` | Admin password rotation contract. |
| `remove_stale_contenttypes` | `stable` | Content-type cleanup contract with guardrails. |
| `collectstatic` | `stable` | Static collection contract. |
| `findstatic` | `stable` | Static asset discovery contract. |
| `makemessages` | `stable` | i18n extraction contract. |
| `compilemessages` | `stable` | i18n compilation contract. |
| `mailproviders` | `transitional` | Provider discovery output may evolve as plugin ecosystem matures. |
| `sendtestemail` | `stable` | Mail delivery verification contract. |
| `plugin list` | `stable` | Plugin inventory contract; `--json` preferred for automation. |
| `plugin doctor` | `transitional` | Diagnostic check set can evolve; status semantics remain stable. |
| `plugin test` | `stable` | Capability smoke execution contract; supports JSON reports. |
| `shell` | `stable` | Interactive/query execution contract (`-c`, `--sandbox`). |
| `test` | `stable` | Go test wrapper contract with framework-focused flags. |
| `testserver` | `transitional` | Fixture+serve workflow is supported; ergonomics may evolve. |
| `diffsettings` | `stable` | Config diff contract; `--json` output is automation-safe. |
| `optimizemigration` | `transitional` | SQL rewrite heuristics may evolve while command path remains supported. |
| `squashmigrations` | `transitional` | Workflow is supported; SQL synthesis details may evolve. |

## Compatibility Alias Matrix

Aliases are intentionally convenience-first and not the canonical product surface.

| Alias | Canonical Command | Lifecycle | Notes |
| --- | --- | --- | --- |
| `runserver` | `serve` | `transitional` | Convenience entrypoint; canonical docs target `serve`. |
| `startproject` | `new` | `transitional` | Convenience alias for project creation. |
| `makemigrations` | `migrate create` | `transitional` | Argument rewriting shim kept for ergonomics. |
| `showmigrations` | `migrate status` | `transitional` | Status alias path. |
| `createsuperuser` | `createuser` | `transitional` | Convenience alias. |
| `dbshell` | `shell` | `transitional` | Convenience alias. |
| `check` | `health` | `transitional` | Includes `check --deploy` compatibility path. |

## Output and Automation Contract Rules

- For automation, prefer command paths that provide `--json`.
- Human-readable plain text output may improve over time without being treated as breaking.
- Exit codes for success/failure are treated as stable behavior for primary commands.

## Review Rule

When adding or changing a command/alias:

1. update this matrix with lifecycle tag and rationale,
2. add command tests in `internal/cli/*_test.go`,
3. include user-visible behavior changes in `CHANGELOG.md`.
