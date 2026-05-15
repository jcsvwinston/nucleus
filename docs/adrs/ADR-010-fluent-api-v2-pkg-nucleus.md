# ADR-010: Fluent API v2 for `pkg/nucleus` over `pkg/app`

**Status:** Proposed
**Date:** 2026-05-15
**Supersedes:** No

## Context

The framework ships two coexisting entry points for assembling a Nucleus application: `pkg/app.New(cfg, ...opts)` — the production-grade Extension surface documented in `SPEC.md` §3.1 and closed by the ADR-004 integration sprint — and `pkg/nucleus.New()...Run()` — a fluent quickstart wrapper over the same primitives, documented in `website/docs/getting-started/quickstart.md` and `project-structure.md`. The Extension surface is stable and mature. The fluent wrapper has not received the same investment: today (`pkg/nucleus/nucleus.go`) it offers a flat chain (`Port()`, `Host()`, `SQLite()`, `Postgres()`, `MySQL()`, `WithAdmin()`, `SPA()`, `Templates()`, `Static()`, `Cors()`, `Provide()`, `Model()`, `AutoMigrate()`, `Run()`), it `panic()`s on configuration errors in `Load(path)`, and it does not expose the production capabilities that `pkg/app` already provides (modules-as-units, alias-keyed multi-database, Casbin default-deny per ADR-004, JWKS auto-mount, circuit breakers, multi-tenant, etc.).

The result is a credibility gap. A developer arriving from the website sees an API that looks shallow and "starter-only" and either dismisses the framework as a tutorial toy or struggles to migrate from `pkg/nucleus` to `pkg/app` without rewriting `main`. The website itself amplifies this: `quickstart.md` shows `nucleus.New().Port(...).SQLite(...).Model(...).AutoMigrate().Get(...).Run()`, which scales to a single-file demo and breaks down at any non-trivial size.

This ADR redesigns `pkg/nucleus` as a deliberate fluent **façade** over `pkg/app` and the existing capability packages (`pkg/router`, `pkg/db`, `pkg/auth`, `pkg/authz`, `pkg/storage`, `pkg/mail`, `pkg/observe`, `pkg/signals`). It does not touch `pkg/app`. It does not reinvent the authorization, database, auth, storage, mail, or observability subsystems; those remain owned by their current packages and are composed by the façade. It does replace the legacy chain with a layered fluent API and introduces strict configuration-loading semantics that the legacy `Load(path)` never had.

This ADR also commits to the documentation-synchronisation discipline required to prevent the website from drifting silently again after the redesign lands.

**Pre-`v1.0` framing.** Nucleus has not been publicly released. `pkg/nucleus` has zero entries in `contracts/baseline/api_exported_symbols.txt` today (false-green in the freeze scanner). The only consumers are two example files (`examples/ecommerce_dashboard/backend/{main.go,handlers/handlers.go}`). Per `docs/governance/COMPATIBILITY_SLO.md` and the precedent established by ADR-006 and ADR-008, this iteration is a clean rewrite of an internal-only package — no deprecation cycle, no DEP/MA artefacts, no WARN-wrapped legacy methods. The CHANGELOG records the rewrite under `### Changed` with an inline `BREAKING (...)` label following the ADR-006/ADR-008 form.

## Decision

### 1. Canonical struct with three surfaces, one truth

`pkg/nucleus` exposes a canonical `App{}` struct that mirrors the configurable shape of `app.Config`, plus Go-only wiring fields tagged `yaml:"-"` so that they cannot be expressed in a configuration file. The package supports three coexisting surfaces, all of which produce the same `nucleus.App{}`:

```go
// 1. Fluent — sugar over the struct, ideal for demos and embedded use.
nucleus.New().
    FromConfigFile("config/nucleus.yaml").
    Use(middleware.Logger(), middleware.Recover()).
    Mount(articles.Module, users.Module).
    Start()

// 2. Direct struct — for tests and programmatic embedding.
nucleus.Run(nucleus.App{
    Port:      8080,
    Databases: map[string]nucleus.DatabaseConfig{"default": {URL: "sqlite://app.db"}},
    Modules:   map[string]nucleus.ModuleSpec{"articles": articles.Module},
})

// 3. Bootstrap pattern — the enterprise default.
// `bootstrap.New()` is a user-space convention, not a sub-package: any
// function in the application's own codebase (typically
// `internal/bootstrap/bootstrap.go`) that constructs and returns
// `nucleus.App`. The host application is responsible for the function;
// `pkg/nucleus` does not ship a `bootstrap` sub-package.
func main() { nucleus.Run(bootstrap.New()) }
```

**`App{}` field layout.** Concretely, the canonical struct embeds `app.Config` (so every yaml-bindable field of the existing production container is present unchanged) and adds four Go-only wiring fields:

```go
type App struct {
    app.Config         // all yaml-bindable fields inherited from pkg/app

    Modules    map[string]ModuleSpec `yaml:"-"` // keyed by module Name
    Middleware []Middleware          `yaml:"-"` // global middleware applied before module routes
    Services   []ServiceRegistration `yaml:"-"` // long-running goroutines registered with the lifecycle
    Lifecycle  LifecycleHooks        `yaml:"-"` // app-level OnStart/OnShutdown not owned by any module
}

type LifecycleHooks struct {
    OnStart    func(context.Context) error
    OnShutdown func(context.Context) error
}

type ServiceRegistration struct {
    Name   string
    Run    func(context.Context) error   // returns when ctx is cancelled
    Health func(context.Context) error   // optional, plumbed into /healthz
}
```

`Modules` is a `map[string]ModuleSpec` (not a slice) so configuration overlays can override individual modules by name — the same map-keyed principle Decision 3 establishes for `databases`. `Middleware` is a slice because order matters (router-level middleware is applied in registration order).

**`Run` verb collision avoided.** The builder-chain terminator is `Start()` (or `Serve()` — both shipped as synonyms for ergonomics; `Start()` is the canonical form). The package-level function is `nucleus.Run(App) error`. Go does not overload, so the two verbs must be distinct.

**Direct-struct validation.** `nucleus.Run(App{...})` invokes the same five-layer validator described in Decision 2 (layers 2–5: schema, semantic, referential, module-specific) before calling `app.New` internally. The direct-struct surface does not skip validation; only the file-loading layer (layer 1, syntactic) is bypassed since there is no file to parse.

**Equivalence test.** `pkg/nucleus/equivalence_test.go` verifies that the three surfaces, given equivalent inputs, produce equal `nucleus.App{}` values. Equality is compared field-by-field with the following normalisation rules:

- `Modules map[string]ModuleSpec`: sorted by key before comparison.
- `Databases map[string]DatabaseConfig` (inherited from `app.Config`): sorted by key.
- Any `time.Time` field set at construction time (none today; this is a forward-compat rule) is zeroed before comparison.
- `func` fields (`Lifecycle.OnStart`, `ServiceRegistration.Run`, `Module.Routes`, etc.) are compared by **reference identity** (pointer equality of the function header). The equivalence test invokes the three surfaces in the same scope so the same function literal is reused; any divergence in function identity is itself a bug worth catching.

### 2. `FromConfigFile` replaces `Load`

`nucleus.FromConfigFile(paths ...string)` replaces the existing `Load(path string)` (whose `panic()` on error is the most visible bug in the current package). Format inference by extension (`.yaml`, `.yml`, `.toml`, `.json`); explicit override via `nucleus.File(path, nucleus.AsYAML)`. Multiple files are accepted and merged left to right.

`FromConfigFile` performs **parse + validate + merge atomically** in five layers, before the application accepts traffic:

1. **Syntactic** — valid YAML/TOML/JSON with `file:line:column` errors. Each file is capped at **1 MB** before parsing; oversized files return a structured error rather than risking parser DoS via anchor-expansion or deep nesting (mitigates `gopkg.in/yaml.v3`-class issues).
2. **Schema** — fields match `nucleus.App{}` and `app.Config`; types correct; required fields present. Unknown fields fail (strict mode is the default; `nucleus.WithUnknownFields("warn")` degrades for development only — see the production-guard note in §Compliance). The validator emits "did you mean …?" hints for likely typos.
3. **Field semantics** — ranges, enums, parseable durations.
4. **Referential** — module `Requires` clauses point to configured database aliases; auth chain references valid providers; observability exporters resolve.
5. **Module-specific** — each module's `Config` struct (typed via `Module[C any]`, see Decision 6) is bound from `modules.<name>.*` and validated against its tags (`default:`, `validate:`).

`nucleus config schema` emits a JSON Schema describing the entire valid surface, suitable for IDE autocompletion (with `yaml-language-server`) and CI validation of `nucleus.yml` before merge.

**Relationship to `pkg/app.Bootstrap` / `app.QuickStart`.** Both already exist (`pkg/app/bootstrap.go`) and load a single config file plus call `app.New`. `FromConfigFile` is a richer surface: multi-file merge, five-layer validation, suffix-operator semantics. `app.Bootstrap` remains available for callers that want the minimal single-file path; the fluent surface adds capability on top, it does not replace `app.Bootstrap`.

### 3. Merge semantics across files

Last-file-wins, with per-type rules: scalars replace, maps deep-merge, lists replace by default. Two suffix-keyed operators on list/map keys provide additive and subtractive semantics:

- `<key>_append`: appends listed entries to the existing collection (`cors_origins_append: [https://staging.example.com]`).
- `<key>_remove`: removes listed entries from the existing collection.

The `_append`/`_remove` form is preferred over `<key>+`/`<key>-` because YAML/TOML/JSON parsers treat `+` and `-` as part of the key name with no special semantics; the underscore-suffixed form survives the parsing round-trip cleanly and is unambiguous in all three formats. `null` unsets and reverts to default. Incompatible types between files raise a boot error referencing both `file:line` locations.

**Design consequence**: any field that admits overrides is modelled as a **map keyed by name**, not a list of objects. This is already true for `databases` in `app.Config`; the principle extends to `auth.providers`, `modules.*`, observability exporters, tenants, and other override-friendly collections. Lists are reserved for collections where order matters and elements lack identity (CORS origins, OIDC scopes, IP allow-lists).

**Non-nullable security keys.** The merge engine refuses `null` for keys that designate security controls — `cors.origins`, `auth.providers`, `authz.policy_path`, `session.secret`, and any other key marked `lifecycle: security` in `docs/reference/CONFIG_KEY_REGISTRY.md`. A `null` on these keys is a boot error, not a silent revert-to-default. Reverting to a permissive framework default (e.g. `corsAllowAll: true`) on a config-level `null` would be a silent security degradation.

Mixed formats across files (one YAML and one TOML, for example) are technically supported and produce the same merge semantics. The framework emits a warning by default and rejects the mix when `nucleus.WithConfigStrict(true)` is in force.

### 4. Precedence chain

`defaults < config files (in order) < env vars < CLI flags < programmatic overrides`.

"Programmatic overrides" means Go code written **after** `FromConfigFile(...)` in the chain. The operator (env + flags) can always override what the developer wrote in the file; the developer can always pin a value in code for tests by placing the override after `FromConfigFile`. The chain is documented in `docs/reference/CONFIG_KEY_REGISTRY.md` and inspectable at runtime via the next section.

### 5. Effective-config inspection

`nucleus config print --effective` prints every active value with its source and, for files, line number:

```
port              = 80     [yaml:config/nucleus.prod.yaml:14]
logging.level     = warn   [yaml:config/nucleus.prod.yaml:8]
logging.format    = json   [toml:config/nucleus.toml:6]
databases.default.url = postgres://… [env:DATABASE_URL]
```

An authentication-gated `/_/config` endpoint exposes the same view at runtime. Both views redact sensitive values according to the framework's canonical redaction list — `observe.DefaultRedactedKeys()` from ADR-007, extended via the same `observe.RedactionConfig.ExtraKeys` mechanism. There is one canonical redaction surface; `/_/config` does not introduce a second list.

**Auth gate.** `/_/config` is gated by the same admin-panel authentication that guards `pkg/admin` routes (session-based, subject to the Casbin default-deny established by ADR-004). The endpoint is mounted only when `WithAdmin()` is in the application's composition. Deployments that opt out of the admin panel do not expose `/_/config`.

### 6. `nucleus.Module` contract

A module is the unit of feature organisation. It is self-contained and portable across applications.

```go
// ModuleSpec is the type-erased interface every module satisfies. It is the
// shape stored in App.Modules and consumed by Mount and Run.
type ModuleSpec interface {
    Name() string
    Prefix() string
    DefaultDB() string                  // logical DB alias used by default
    Requires() []string                 // logical DBs required; boot fails if missing
    Models() []any
    Middleware() []Middleware
    Routes(r Router)
    Jobs(j JobRegistry)
    Webhooks(w WebhookRegistry)
    Migrations() fs.FS                  // fs.FS (not embed.FS) so runtime-generated sources can satisfy the contract
    OnStart(ctx context.Context, a *App) error
    OnShutdown(ctx context.Context, a *App) error
    Config() any                        // typed via the generic constructor below
}

// Module is the generic constructor for typed module configs. Users
// instantiate it with their config type and the framework binds
// `modules.<Name>.*` directly into Module[C].Config without reflection
// at the call site.
type Module[C any] struct {
    Name       string
    Prefix     string
    DefaultDB  string
    Requires   []string
    Models     []any
    Middleware []Middleware
    Config     C
    Routes     func(r Router, cfg C)
    Jobs       func(j JobRegistry, cfg C)
    Webhooks   func(w WebhookRegistry, cfg C)
    Migrations fs.FS
    OnStart    func(ctx context.Context, a *App, cfg C) error
    OnShutdown func(ctx context.Context, a *App, cfg C) error
}

// Module[C] satisfies ModuleSpec via a wrapper produced by Build().
func (m Module[C]) Build() ModuleSpec { /* type-erased ModuleSpec wrapping m */ }
```

`Migrations` uses `fs.FS` (not `embed.FS`) so that modules can satisfy the contract with a runtime-generated migration source — `embed.FS` implements `fs.FS`, so any `//go:embed` usage works unchanged.

The framework binds `modules.<Name>.*` to `Module[C].Config` during configuration load (validation layer 5). The generic parameter preserves compile-time type safety for the module author; only the framework's internal storage uses the type-erased `ModuleSpec` interface.

Moving a module's package to another application brings its configuration shape, models, routes, jobs and migrations with it. The host application only has to add the module to its `Mount(...)` list.

`Requires` declares logical database aliases. If `app.Config.Databases` lacks a required entry, the framework fails at boot with `module "<name>" requires database "<alias>" which is not configured` — never a `nil pointer dereference` at runtime.

**Migration namespacing.** Module migration file checksums (per `pkg/db/migrate.go`'s `migrationsChecksumsTable`) are keyed as `<module_name>/<filename>` rather than just `<filename>`, preventing cross-module filename collisions when multiple modules share a database alias.

### 7. Router with three coexisting styles

`nucleus.Router` is an interface defined in `pkg/nucleus` (not an alias for `*router.Router`) so that modules do not take a hard import on `pkg/router`. The interface exposes `Get`, `Post`, `Put`, `Delete`, `Group`, and `Resource` with consistent signatures.

Within a module, the developer chooses **per section**, not per module:

- **Flat declarative** (`r.Get("/articles", List)`) for small or audit-sensitive modules.
- **`Resource(...)` REST** for CRUD modules. Methods to register are passed **explicitly** via a variadic argument: `r.Resource("/tickets", controller{}, nucleus.Methods(Index, Show, Create))`. The implementation does not use reflection-based method discovery — silently registering whatever methods happen to be present on the controller is a footgun (a developer adds `Patch` and wonders why 404s appear). Explicit registration is auditable.
- **`Group(...)` nested closures** (`r.Group("/admin", func(g Router) { ... })`) for areas with nested URL hierarchy and inherited middleware. Middleware composes additively at every group level; child groups can extend the parent stack or override entries.

Mixing the three styles within the same module is expected and supported.

Authorization policy is owned by `pkg/authz` (Casbin, ADR-004). `Resource` does not provide a `.Permissions(...)` attachment point in this iteration; route-level permission attachment, if ever needed, lands in a separate ADR after the authz surface for `pkg/nucleus` is designed.

### 8. Composition over reinvention

`pkg/nucleus` does not implement databases, auth, authz, storage, mail or observability. It composes the existing packages:

- `pkg/app` for the lifecycle, config plumbing, and the Casbin default-deny mount established by ADR-004.
- `pkg/router` for routing.
- `pkg/db` for SQL.
- `pkg/auth` and `pkg/authz` for authentication and authorization (Casbin enforcer mounted by default per ADR-004).
- `pkg/storage` for files.
- `pkg/mail` for email.
- `pkg/observe` for logging, tracing, metrics.
- `pkg/signals` for the in-process event bus.
- `pkg/tasks` for Asynq-based background work.

The fluent façade adds API ergonomics; it never duplicates capability.

**`WithoutDefaults` and `WithExtensions` seam.** Today's `pkg/app` exposes `app.WithoutDefaults()` and `app.WithExtensions(...)` for lightweight / customised composition. The fluent surface mirrors them as builder methods:

```go
nucleus.New().
    WithoutDefaults().
    WithExtensions(admin.Extension(), customAuth.Extension()).
    Mount(myModule).
    Start()
```

Direct-struct callers pass an `Options` field (`nucleus.App{ Options: []nucleus.Option{nucleus.WithoutDefaults()}, ... }`) that the framework forwards verbatim to `app.New(cfg, opts...)`. This closes the "missing seam" finding from the 2026-05-15 pkg/app+pkg/nucleus inventory pass.

### 9. Documentation-synchronisation discipline

Because this redesign will produce immediate drift between `pkg/nucleus` and the website if not actively prevented, this ADR commits to a docs-sync mechanism integrated into the iteration loop:

- **Manifest of coverage** in the frontmatter of each `website/docs/*.md(x)` page, declaring which symbols and config keys the page documents (`covers:` and `config_keys:` arrays). Enables cheap reverse lookup from changed symbols to affected pages.
- **`scripts/website/check-coverage.sh`** (introduced in a follow-up iteration) detects undocumented public symbols and dangling references.
- **`doc-updater` subagent extended** to read under `website/docs/`, propose edits to pages whose `covers:` list affected symbols. (Landed in the preceding iteration — PR #67, commit `af549bf`, 2026-05-15. The capability is real and available; this ADR commits to its use.)
- **Code blocks in website docs are imported from `examples/*`** via Docusaurus include syntax, so they cannot drift from real, compilable Go.
- **CI gate**: `.github/workflows/website-check.yml` (follow-up iteration) builds the website, runs `check-coverage.sh`, and verifies imported example files still build.
- **`contract-guardian` enforcement**: blocks iteration closure if `pkg/*` stable surface changed and the corresponding website page was not updated.

The mechanism's implementation (script, workflow, manifest backfill across existing pages) is its own iteration, scheduled immediately after the fluent v2 implementation lands. This ADR commits to the design only.

## Consequences

### Positive

- `pkg/nucleus` becomes the recommended entry point for **all** application sizes, not just demos. A reader of the README sees an API that survives contact with production.
- The `Load(path) panic()` bug is replaced by structured validation with actionable errors at boot. The five-layer validator catches an entire class of silent-misconfiguration bugs that affected earlier versions.
- Module portability and the alias-based `databases.<alias>` shape (already present in `app.Config`) become first-class in the fluent surface, eliminating the credibility gap with `pkg/app`.
- The website docs stay aligned with the implementation by construction (code imported from `examples/*`, manifest-tracked coverage, CI gate). The drift that produced today's `nucleus.New().Port().SQLite().AutoMigrate()` quickstart cannot recur silently.
- Composition over reinvention preserves every capability `pkg/app` already provides — Casbin default-deny posture, alias-based DB plumbing, JWT/JWKS mount, circuit breakers, multi-tenant routing.
- Operators gain `/_/config` and `nucleus config print --effective` — a definitive answer to "what is the running configuration?" without re-reading deployment manifests.

### Negative

- **Rewrite of `pkg/nucleus`.** The current `nucleus.New().Port().SQLite().Model().AutoMigrate().Run()` chain disappears. Pre-`v1.0`, with only two internal example consumers (`examples/ecommerce_dashboard/backend/{main.go,handlers/handlers.go}`), the rewrite is a clean break — those two files are rewritten in the same PR per CLAUDE.md §3. No DEP or MA artefacts are produced; the precedent from ADR-006 / ADR-008 governs.
- The website needs a substantial rewrite of `quickstart.md` and `project-structure.md`, plus introduction of the manifest pattern across pages. This is its own iteration, scheduled immediately after the fluent v2 implementation iteration.
- The new validator and merge engine add code to `pkg/nucleus`. Mitigated by the fact that they replace ad-hoc validation scattered through the old `Load` + builder flow and provide a single, testable surface.

### Neutral

- All three surfaces (fluent, direct-struct, bootstrap) converge on `app.New` as the implementation, so every guarantee SPEC.md §3.1 makes about the application container is inherited automatically.

## Compliance

After this ADR is Accepted (in the Phase 1 implementation PR — see §Implementation phases below):

1. `pkg/nucleus/nucleus.go` is rewritten to expose the `AppBuilder` chain with the new methods (`FromConfigFile`, `Use`, `Mount`, `WithoutDefaults`, `WithExtensions`, `Build`, `Start`/`Serve`). The legacy single-purpose methods are removed entirely; `examples/ecommerce_dashboard/backend/main.go` and `examples/ecommerce_dashboard/backend/handlers/handlers.go` are updated in the same PR to demonstrate the fluent surface.
2. `pkg/nucleus/router.go` (new) defines the `Router` **interface** and implements the three-style registration (flat, `Resource` with explicit `Methods(...)`, `Group`).
3. `pkg/nucleus/module.go` (new) defines `ModuleSpec` (type-erased) and `Module[C any]` (generic typed constructor).
4. `pkg/nucleus/config.go` (new) implements the five-layer validator and the merge engine with `_append`/`_remove` suffix operators.
5. `pkg/nucleus/equivalence_test.go` (new) verifies the three-surface equivalence test per the normalisation rules in §1.
6. `internal/cli/configcommands.go` (new — not `cmd/nucleus/cli_config.go`, which does not exist; correct target follows the existing `cachecommands.go` / `contenttypecommands.go` convention) implements `nucleus config print --effective` and `nucleus config schema`, wired in `cmd/nucleus/main.go`.
7. `CHANGELOG.md` `Unreleased` block records the rewrite under `### Changed` with an inline `BREAKING (pkg/nucleus rewrite): ...` label following the ADR-006 / ADR-008 form. No `DEP-` or `MA-` artefacts produced.
8. `.claude/agents/doc-updater.md` is extended to include `website/docs/*` in its scope and to use the coverage manifest. *Already landed in PR #67 (commit `af549bf`, 2026-05-15).*
9. `website/docs/getting-started/quickstart.md` and `project-structure.md` are rewritten in the immediately following iteration; the manifest pattern is introduced there.
10. `scripts/website/check-coverage.sh` and `.github/workflows/website-check.yml` are introduced in the docs-sync mechanism iteration.
11. **Freeze-scanner seed.** `pkg/nucleus` is already listed in `contracts/freeze_test.go:146-164`'s `stableAPISymbolBaselineLines` package list but its baseline entries are empty (the source of the false-green flagged in the 2026-05-15 inventory). At the end of Phase 1 (Foundation), `NUCLEUS_UPDATE_CONTRACT_BASELINE=1 go test ./contracts/...` is run to seed the baseline with the new `pkg/nucleus` exported symbols. From that point on, removals or renames in the new surface trip the freeze test as intended.
12. **`/_/config` auth gate.** Gated by the same admin-panel authentication that guards `pkg/admin` routes (session-based, subject to Casbin default-deny). The endpoint is mounted only when `WithAdmin()` is in the application's composition.
13. **Canonical redaction list.** `/_/config` and `nucleus config print --effective` both call `observe.DefaultRedactedKeys()` as the base redaction set, extended via the existing `observe.RedactionConfig.ExtraKeys` mechanism. No second redaction list is introduced.
14. **Non-nullable security keys.** `cors.origins`, `auth.providers`, `authz.policy_path`, `session.secret` (and any future keys tagged `lifecycle: security` in `docs/reference/CONFIG_KEY_REGISTRY.md`) are non-nullable: the merge engine treats `null` on these keys as a boot error rather than a silent revert-to-default.
15. **Strict-mode startup guard.** When `nucleus.WithUnknownFields("warn")` is active at startup, the framework emits a `WARN`-level `slog` event: `config: unknown-fields mode is WARN, not STRICT — do not deploy to production`. A `NUCLEUS_ENV=production` env var (introduced as part of this iteration) overrides to strict regardless of code-level setting.
16. **Migration namespacing.** Module migration file checksums in `pkg/db/migrate.go`'s `migrationsChecksumsTable` are keyed as `<module_name>/<filename>` to prevent cross-module collisions when multiple modules share a database alias.
17. **Config file size cap.** `FromConfigFile` imposes a **1 MB per-file** size cap before parsing. If exceeded, the loader returns a structured error referencing the file path; the parser is not invoked, eliminating anchor-expansion / deep-nesting DoS classes.

## Implementation phases

The compliance items above are landed across four iterations, each its own PR:

- **Phase 1 — Foundation.** Compliance items #1, #2, #3, #5, #11. Pins the new package shape: canonical `App{}` struct, `Module[C any]` + `ModuleSpec`, `Router` interface, three-surface equivalence test, freeze-scanner seed. Updates the two `examples/ecommerce_dashboard` consumers in the same PR. ADR `Status` flips from `Proposed` to `Accepted` in this PR's `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md` edit, following the ADR-006 / ADR-008 acceptance pattern.
- **Phase 2 — Config loading + merge engine.** Compliance items #4, #7, #14, #15, #16, #17.
- **Phase 3 — Effective-config inspection.** Compliance items #6, #12, #13.
- **Phase 4 — Docs-sync + website.** Compliance items #8, #9, #10. (#8 already landed; #9 and #10 are the new work.)

Each phase is independently mergeable after Phase 1 establishes the public shape.

## Related

- ADR-001: stdlib-first runtime — composition over reinvention respects this constraint.
- ADR-003: project identity Nucleus — the fluent surface continues to live in `pkg/nucleus`.
- ADR-004: Casbin enforcer mounted with default-deny — preserved unchanged; `pkg/nucleus.Run` delegates to `app.New` which performs the mount.
- ADR-006 / ADR-008: pre-`v1.0` clean-break precedents — CHANGELOG `### Changed` with inline `BREAKING (...)` label, no DEP/MA artefacts.
- ADR-007: `slog` secret redaction — the canonical redaction list `observe.DefaultRedactedKeys()` is reused by `/_/config` and `nucleus config print --effective`.
- `SPEC.md` §3.1 Application Container — describes the `pkg/app` surface this ADR composes. The 15 guarantees enumerated in `docs/iterations/2026-05-15-pkg-app-nucleus-inventory.md` §3 are preserved automatically because all three surfaces delegate to `app.New`.
- `SPEC.md` §2 Core Principles — explicit configuration & lifecycle, security by default, compatibility by contract.
- `docs/iterations/2026-05-15-pkg-app-nucleus-inventory.md` — Phase 1 inventory of the surface this ADR rewrites.
- `pkg/nucleus/nucleus.go` — current implementation being redesigned.
- `website/docs/getting-started/quickstart.md` and `website/docs/getting-started/project-structure.md` — current docs that will be rewritten in the follow-up iteration.
