# CLI Contract Matrix

Reference date: 2026-04-07.
Status: Current.

This file defines lifecycle tags for Nucleus CLI command contracts.

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
| `serve` | `stable` | HTTP server bootstrap command contract. Flags: `--config`, `--host`, `--port`, and `--without-defaults` (ADR-013 / R3, added 2026-05-31). `--without-defaults` is an additive, optional bool that serves a core-only app via `app.New(cfg, app.WithoutDefaults())` — no admin/authz/mail/storage — matching an `api` scaffold's `go run .`; omitting it preserves the full-stack default, so the contract is unchanged. |
| `routes` | `stable` | Route introspection contract; use `--json` for automation when available. |
| `health` | `stable` | Dependency health contract; `--json` output is automation-safe. |
| `config` | `transitional` | Effective-config inspection (ADR-010 Phase 3a). `config print --effective` merges the configured files (precedence `defaults < file[0] < … < file[N-1]`) and emits every effective key with its value and source `[kind:path]`, redacting secrets via the canonical `observe.DefaultRedactedKeys()`. `--config` is repeatable; `--json` is automation-safe. The auth-gated `GET /_/config` runtime endpoint mirror shipped in Phase 3b (2026-05-23). Phase 3.1 (2026-05-23) added the env layer and `file:line`: effective output now includes `NUCLEUS_`-prefixed env overrides as `[env:NUCLEUS_*]` sources, and YAML file sources carry their line (`[yaml:path:line]`; TOML/JSON report `[kind:path]`). Lifecycle remains `transitional` until the surface stabilises: `config schema` (ADR-010 §2) is not yet shipped, and the CLI-flags / programmatic-override layers of §4 are not attributed. |
| `doctor` | `transitional` | Diagnostic checks for framework subsystems. `--check <name>` scopes to one subsystem; `--json` output is automation-safe. The individual check set may evolve; pass/warn/fail status semantics and the success/failure exit codes are the stable surface. |
| `new` | `stable` | Project scaffold entrypoint contract. |
| `startapp` | `stable` | In-project app scaffold contract. |
| `generate` | `stable` | Scaffold/generator command contract. |
| `openapi` | `experimental` | Exports the project OpenAPI JSON document from `internal/contracts`; generated runtime serving should use that same document builder and remains an explicit `MountOpenAPI(...)` application decision, and the current subset includes scaffolded JSON request/response metadata, shared `data`/`count` response envelopes, shared JSON error/empty responses, and explicit path/query parameters where declared. |
| `migrate` | `stable` | Migration lifecycle command contract. Subcommands: `up`, `down`, `steps`, `status`, `drift`, `reset`, `refresh`, `create`. `migrate drift` reports applied migrations whose `.up.sql` file is missing on disk and **exits non-zero** when any drift is detected (CI-friendly). |
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
| `wizard` | `experimental` | Interactive prompt-driven front-end for complex commands (`--type inspectdb`/`new`/`startapp`). Convenience surface with no compatibility guarantee; the underlying canonical commands it drives carry the contract. |

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
