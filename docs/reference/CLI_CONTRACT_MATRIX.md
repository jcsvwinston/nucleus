# CLI Contract Matrix

Reference date: 2026-06-21.
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
| `routes` | `stable` | Route introspection contract; `--json` is automation-safe on every path (the configuration-only listing boots its app at log level `error`, so no boot log line precedes the array on stdout). Since 2026-09 the command reads the compiled binary: inside a project (`--dir`, default `.`, is the directory of the main package; the project is the nearest `go.mod` at or above it, so `--dir ./cmd/<app>` reads that layout) it builds the main package and runs the binary from the module root with `NUCLEUS_PRINT_ROUTES=1`, which makes `nucleus.Run` print its route table and exit before listening, and lists every route with the module that registered it (`module` column/field, present on every JSON entry and empty for the framework's own; a subtree a module registers through `Router.Group` is one `<prefix>/*` entry, the same fidelity as the boot log). The run is bounded by `--timeout` (default 30s; the build is not counted): an application that serves instead of printing is stopped with its process group and reported as an error; the same kill happens when the command itself receives SIGINT or SIGTERM (a terminal Ctrl-C, a CI cancel), and the command then exits non-zero saying so and removes its build directory — nothing is left listening on any path. A `--dir` that holds no main package (the module root of a `cmd/<app>` layout, a library package) is an error (non-zero) naming `--dir`, the main packages the module holds and `--framework-only`, never the raw build output. A project whose nucleus requirement predates the variable is not built; the command lists the framework routes from the project's config file and says so. `--config` applies to the configuration-only listing only: inside a project it is refused (non-zero) unless `--framework-only` is given, never silently ignored. `--framework-only` (additive) keeps the configuration-only listing, which is also the fallback outside a project (no `go.mod` at or above `--dir`) — both declare themselves in a note that carries the sentence `listing framework-owned routes only.` (period included: the umbrella guard greps the phrase and its fixture asserts the sentence end). Plain output gained a LAST tab-separated column for the module: `METHOD\tPATTERN\tMODULE`, and with `--verbose` `METHOD\tPATTERN\tmiddleware=N\tMODULE` — every column that existed before keeps its position. |
| `health` | `stable` | Dependency health contract; `--json` output is automation-safe. |
| `config` | `transitional` | Effective-config inspection (ADR-010 Phase 3a). `config print --effective` merges the configured files (precedence `defaults < file[0] < … < file[N-1]`) and emits every effective key with its value and source `[kind:path]`, redacting secrets via the canonical `observe.DefaultRedactedKeys()`. `--config` is repeatable; `--json` is automation-safe. Phase 3.1 (2026-05-23) added the env layer and `file:line`: effective output now includes `NUCLEUS_`-prefixed env overrides as `[env:NUCLEUS_*]` sources, and YAML file sources carry their line (`[yaml:path:line]`; TOML/JSON report `[kind:path]`). The auth-gated `GET /_/config` runtime endpoint that shipped in Phase 3b was removed in ADR-019 (2026-06-21) when the admin subsystem was extracted — it was admin-session-gated and left with admin. `config print --effective` remains the canonical path. Since v1.12.0 it also reports, on **stderr**, when the merged configuration would not boot: it still renders the resolved view (that is what the command is for) and carries the loader's own rejection, so stdout — and therefore `--json` — stays machine-parseable. It was the last CLI surface that read a config file without judging it. Lifecycle remains `transitional` until the surface stabilises: `config schema` (ADR-010 §2) is not yet shipped, and the CLI-flags / programmatic-override layers of §4 are not attributed. |
| `doctor` | `transitional` | Diagnostic checks for framework subsystems. `--check <name>` scopes to one subsystem; `--json` output is automation-safe. The individual check set may evolve; status semantics and the success/failure exit codes are the stable surface: `pass`/`warning`/`error` count toward the overall verdict, and `info` (added 2026-08, with its own `info` counter in the JSON report) marks an optional subsystem that is simply not enabled — it never degrades the verdict, so a healthy minimal app reports HEALTHY. |
| `new` | `stable` | Project scaffold entrypoint contract. `--db <engine>` (additive, 2026-09) selects the driver module the generated `main.go` imports and the `databases.default.url` it starts on; the scaffold runs `go get <driver>` and `go mod tidy` so the project builds without a manual tidy, and `--offline` skips both (the default for the repository's own tests). |
| `startapp` | `stable` | In-project app scaffold contract. |
| `generate` | `stable` | Scaffold/generator command contract. `generate resource`/`generate module` print the exact route table the scaffolded module mounts. `generate resource --with-policy` (additive, 2026-08) seeds the anonymous `rbac_policy.csv` rows and the `csrf_exempt_paths` entry those routes need — development defaults only; it appends missing rows and never rewrites existing ones. `generate module` (additive, 2026-09) also emits `internal/<name>/module_test.go` (boots the slice through `pkg/nucleustest`), accepts `--mount` (edits the `nucleus.New()` chain in `main.go` by AST; refuses, with the lines to add, when the composition root has no such chain) and `--data sql\|quark`, and ends with `go mod tidy` (resolves `pkg/nucleustest` for the test and Quark for `--data quark`; `--offline` skips it and hands it back); the `internal/contracts` aggregator is created by `generate resource` only. |
| `openapi` | `experimental` | Exports the project OpenAPI JSON document from the `internal/contracts` package — the pre-check is on the package (at least one non-test `.go` file), not on the `contracts.go` file name the scaffold writes; when the project has no `internal/contracts` package yet, it fails before compiling the exporter and names the commands that create it: `generate resource`, `startapp`; generated runtime serving should use that same document builder and remains an explicit `MountOpenAPIHandler(...)` application decision, and the current subset includes scaffolded JSON request/response metadata, shared `data`/`count` response envelopes, shared JSON error/empty responses, and explicit path/query parameters where declared. |
| `migrate` | `stable` | Migration lifecycle command contract. Subcommands: `up`, `down`, `steps`, `status`, `drift`, `reset`, `refresh`, `create` — the action is required: since 2026-09 a bare `nucleus migrate` is a usage error naming the actions instead of an implicit `up` (a mutation nobody asked for by name). `migrate status` lists the directory plan and, after it, the ledger rows modules wrote under their `<module>/` namespace when they applied embedded migrations at start (`db.Migrator.Applied`). `migrate drift` reports applied migrations whose `.up.sql` file is missing on disk and **exits non-zero** when any drift is detected (CI-friendly). |
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
| `createuser` | `stable` | Admin user create/update contract. Manages rows in the `nucleus_admin_users` table. As of ADR-019 (2026-06-21) the admin subsystem — including ownership of the `nucleus_admin_users` schema — moved to the `orbit` module (`github.com/jcsvwinston/orbit`); the command stays in core but **no longer auto-creates the table**. It now requires orbit to have initialised the schema (a dialect-aware existence check on `nucleus_admin_users`) and fails fast with an actionable "orbit not installed" message when the table is absent, rather than planting an orphan table. Names, flags, and JSON/stdout output are unchanged — only the missing-schema failure mode changed. |
| `changepassword` | `stable` | Admin password rotation contract. Rotates the password for a row in the `nucleus_admin_users` table. The schema is owned by the `orbit` module (ADR-019, 2026-06-21); this command **no longer auto-creates the table** and applies the same dialect-aware existence guard as `createuser`, failing with a clear "orbit not installed" error instead of a raw SQL "no such table" when orbit has not initialised the database. Since v1.20.0 it also REFUSES, before writing anything, when `auth_backends` is configured without the local backend in it: the panel authenticates through that chain and never reads the local `password_hash`, so writing one and exiting 0 told the operator access was restored when it was not (QCD-FW-27). A chain that includes the local backend still proceeds — there the hash is the break-glass path. Names, flags and output on the paths that still write are unchanged. |
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
| `outbox` | `transitional` | Outbox maintenance. `requeue [id ...]` returns failed messages to `pending` with attempts reset to 0 (no ids = all failed; non-failed ids are untouched); `--json` output is automation-safe. The subcommand set may grow; requeue semantics are the surface to preserve. |
| `squashmigrations` | `transitional` | Workflow is supported; SQL synthesis details may evolve. |
| `dev` | `experimental` | Development loop (2026-09): builds the main package in `--dir` (the project is the nearest `go.mod` at or above it), runs the binary from the module root with `NUCLEUS_ENV=development` and, when `--port` is given, `NUCLEUS_PORT`; rebuilds on a debounced (`--debounce`, 300ms) change to `*.go`, `nucleus.yml`, `rbac_policy.csv` or any file under a `migrations/` or `templates/` directory (dot-directories, `node_modules` and `vendor` are not watched). A successful build stops the running application (SIGTERM to its process group, SIGKILL after 5s) and starts the new binary; a failed build prints the compiler output and keeps the last good binary serving. `--print-routes` runs each new binary with `NUCLEUS_PRINT_ROUTES=1` first and prints the table. `--proxy <url>` puts the command in front on the port (`--port`, else the configured one) and forwards `--proxy-paths` (default `/static,/assets`) to that URL, the rest to the application on a loopback port of its own (`NUCLEUS_HOST=127.0.0.1`); the front answers 503 while the application is down. Ctrl-C/SIGTERM kills the application with its process group, removes the build directory and exits 0. Output lines are prefixed `[dev]`; the application's output passes through. No compatibility guarantee: flags, watched set and output lines may change. |
| `completion` | `experimental` | Shell completion generator (2026-09): `completion <bash\|zsh\|fish>` prints a script generated from `commandSpecs`, `commandAliases`, each command's flags (read from its own `--help`) and its `usageSpec` grammar (the subcommands a first positional accepts; the values a flag accepts, from a usage section titled `... (--flag)`), so an offered word is one the binary accepts. Commands that print hand-written help (`add`, `generate`, `plugin`, `outbox`, `config`) complete their name only. Golden files under `internal/cli/testdata/completion/` pin the three scripts. The script is a snapshot of one binary; regenerate after upgrading. No compatibility guarantee on the script text. |
| `wizard` | `experimental` | Prompt-driven front-end stub for `--type inspectdb`/`new`/`startapp`: it walks the prompts, prints a summary, and ends in an explicit non-zero "experimental and did not execute changes" error naming the canonical command to run; it opens no database connection. Convenience surface with no compatibility guarantee; the canonical commands carry the contract. Removed from the public CLI overview until it executes something. |

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
