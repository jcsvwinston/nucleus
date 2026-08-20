# ADR-022: Vertical-slice modules — carried policies, migrations and templates

Reference date: 2026-08-20.
Status: Accepted.
Related: [ADR-010](ADR-010-module-contract.md) (the module contract this
extends), [ADR-013](ADR-013-real-app-readiness.md) §R1 (which recorded the
module-migrations follow-up this ADR executes, with its invariant),
[ADR-015](ADR-015-authz-hardening.md) F-4 (the enforcer stays wrapped; no
Casbin types on the public surface).

## Context

`Module[C]` already lets a feature carry its routes, models, middleware,
jobs, webhooks, migrations and typed config — and its godoc promises that a
module "can be lifted into another application by adding it to that
application's `Mount(...)` list". Three gaps kept that promise half-true:

1. **A module could not be self-authorizing.** Under the default-deny
   enforcer (ADR-004) its routes answered a mute 403/419 until the operator
   hand-edited `rbac_policy_file` and `csrf_exempt_paths`. The DX audit
   measured this as the single costliest first-hour cliff, and the
   `generate resource` scaffold shipped resources that were dead on arrival.
2. **`Module.Migrations` was advisory.** The framework never applies it
   (SQL-first; ADR-013 R1) and `nucleus migrate` only reads disk
   directories, so a lifted module's embedded migrations were unreachable by
   any command — the consumer copied SQL files out by hand. ADR-013 R1
   recorded wiring this as future work, with the invariant that application
   boot never mutates the schema on its own.
3. **Templates did not travel.** Only `Migrations` is an `fs.FS`; a module's
   pages had to be copied into the host's `templates_dir`, and no extension
   point accepted an `fs.FS` at all.

## Decision

Four parts, landed as one arc:

**1. `Module[C].Policies []PolicyRule` and `Module[C].CSRFExempt []string`.**
Declarative fields, read through an unexported carrier interface (the
`moduleIntrospector` pattern — off the public `ModuleSpec` contract, so a
foreign spec degrades to contributing nothing). Objects and paths are
relative to the module's `Prefix`. Rules are validated at boot
(`ErrInvalidModulePolicy`; actions restricted to the CRUD verbs the authz
middleware can ever request — an unmatched row would be a silent lie).
CSRF exemptions join `cfg.CSRFExemptPaths` before `app.New` (the last
moment they can take effect); policy rows join the live enforcer right
after `app.New`, before module `OnStart`. Module rows are in-memory only
and the Casbin policy effect (`some(allow) && !some(deny)`) keeps
governance with the operator: a host CSV `deny` overrides any module
`allow`. The module proposes, the operator disposes.

**2. `Runtime.ApplyModuleMigrations(ctx)` + an `fs.FS`-capable module
migrator in `pkg/db`.** The module-scoped ledger namespacing
(`NewModuleMigrator`, ADR-010 §16) already existed; what was missing was an
`fs.FS` reader and a deliberate call site. The framework still never
applies migrations on its own — the invariant of ADR-013 R1 holds. What
changes is that the module author can now apply the module's embedded
migrations through the real migrator (ledger + checksums, unlike
`AutoMigrate`) with one explicit call, typically in `OnStart`. The boot
WARN for declared-but-unapplied migrations now names that call and is
suppressed when the module used it.

**3. `Module[C].Templates fs.FS` + `app.WithTemplatesFS(prefix, fsys)`.**
A new accumulating app option parses `.html` files from an `fs.FS` into the
template namespace under a prefix; `nucleus.Run` feeds each module's
`Templates` through it as `<module-name>/<relative-path>` before `app.New`
(templates must exist before the middleware stack and sub-mux derivation
freeze the engine). Precedence: `WithTemplates` base < module templates <
`templates_dir` — the host's files override a lifted module's, consistent
with part 1's governance rule.

**4. `nucleus generate module <name>`.** A generator for the
package-per-feature idiom (`internal/<name>/` with model, storage,
controller, module, embedded migrations, embedded page template) that
declares its `Policies`/`CSRFExempt` and applies its migrations via part 2
— so the scaffold boots to working 200s with zero manual edits. Covered by
the executable-scaffold guard: its E2E boots the generated project and
proves page + API answer without touching `rbac_policy.csv` or
`nucleus.yml`.

## Consequences

- The lift-a-module promise becomes literal for the surfaces a module can
  declare: mount, migrate (one explicit call), serve, authorized.
- The public API grows by the fields/types above; the frozen baseline is
  regenerated in the same changes (`freeze_additions_test.go` contract).
- `ModuleSpec` (the interface) is unchanged — new capabilities ride
  unexported carriers, so third-party `ModuleSpec` implementations keep
  compiling and simply do not contribute rows/templates.
- A module's allow rows widen reachability by default once mounted. This is
  the intended trade (mounting a module is already trusting its routes and
  handlers); the operator retains a one-row veto via CSV `deny`, and the
  boot log reports each module's loaded rule count.
- Rejected: auto-applying module migrations at boot (re-affirming ADR-013
  R1's rejected alternative); a `Policies()` method on `ModuleSpec`
  (breaks foreign implementations for no gain); module templates merged
  after mount (impossible: `html/template` forbids Parse after Execute and
  sub-muxes copy the engine at derivation).
