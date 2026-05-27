# ADR-010: Fluent API v2 for `pkg/nucleus` over `pkg/app`

**Status:** Accepted (Phase 1 landed 2026-05-16; Phase 2a landed 2026-05-16; Phase 2b landed 2026-05-17; Phase 2c landed 2026-05-17; Phase 2d landed 2026-05-17; Phase 3a landed 2026-05-22; Phase 3b landed 2026-05-23; Phase 3.1 landed 2026-05-23; §2 layer-3 field-semantic validation landed 2026-05-24; §2 layer-4 referential validation landed 2026-05-26)
**Date:** 2026-05-15
**Accepted:** 2026-05-16
**Reference date:** 2026-05-17
**Supersedes:** No

## Context

The framework ships two coexisting entry points for assembling a Nucleus application: `pkg/app.New(cfg, ...opts)` — the production-grade Extension surface documented in `SPEC.md` §3.1 and closed by the ADR-004 integration sprint — and `pkg/nucleus.New()...Run()` — a fluent quickstart wrapper over the same primitives, documented in `website/docs/getting-started/quickstart.md` and `project-structure.md`. The Extension surface is stable and mature. The fluent wrapper has not received the same investment: today (`pkg/nucleus/nucleus.go`) it offers a flat chain (`Port()`, `Host()`, `SQLite()`, `Postgres()`, `MySQL()`, `WithAdmin()`, `SPA()`, `Templates()`, `Static()`, `Cors()`, `Provide()`, `Model()`, `AutoMigrate()`, `Run()`), it `panic()`s on configuration errors in `Load(path)`, and it does not expose the production capabilities that `pkg/app` already provides (modules-as-units, alias-keyed multi-database, Casbin default-deny per ADR-004, JWKS auto-mount, circuit breakers, multi-tenant, etc.).

The result is a credibility gap. A developer arriving from the website sees an API that looks shallow and "starter-only" and either dismisses the framework as a tutorial toy or struggles to migrate from `pkg/nucleus` to `pkg/app` without rewriting `main`. The website itself amplifies this: `quickstart.md` shows `nucleus.New().Port(...).SQLite(...).Model(...).AutoMigrate().Get(...).Run()`, which scales to a single-file demo and breaks down at any non-trivial size.

This ADR redesigns `pkg/nucleus` as a deliberate fluent **façade** over `pkg/app` and the existing capability packages (`pkg/router`, `pkg/db`, `pkg/auth`, `pkg/authz`, `pkg/storage`, `pkg/mail`, `pkg/observe`, `pkg/signals`). It does not touch `pkg/app`. It does not reinvent the authorization, database, auth, storage, mail, or observability subsystems; those remain owned by their current packages and are composed by the façade. It does replace the legacy chain with a layered fluent API and introduces strict configuration-loading semantics that the legacy `Load(path)` never had.

This ADR also commits to the documentation-synchronisation discipline required to prevent the website from drifting silently again after the redesign lands.

**Pre-`v1.0` framing.** Nucleus has not been publicly released. `pkg/nucleus` had zero entries in `contracts/baseline/api_exported_symbols.txt` at the time this ADR was proposed (false-green in the freeze scanner). The only consumers were two example files (`examples/ecommerce_dashboard/backend/{main.go,handlers/handlers.go}`). Per `docs/governance/COMPATIBILITY_SLO.md` and the precedent established by ADR-006 and ADR-008, this iteration is a clean rewrite of an internal-only package — no deprecation cycle, no DEP/MA artefacts, no WARN-wrapped legacy methods. The CHANGELOG records the rewrite under `### Changed` with an inline `BREAKING (...)` label following the ADR-006/ADR-008 form.

**Owner decision overriding the original consumer-rewrite plan (2026-05-16).** The original Negative-consequences and Implementation phases sections stipulated rewriting the two `examples/ecommerce_dashboard/backend/*` consumers in the same Phase 1 PR. The owner replaced that path with **wholesale deletion of every `examples/*` tree** in the same Phase 1 PR. The previous reference applications were obsolete relative to the framework's current shape; rebuilding them mid-rewrite would have added noise without validating the new surface. New, post-Phase 1 reference applications are authored as part of ADR-010 Phase 4 / docs-sync (target: v0.9.X). The text below — Negative §[examples consumer rewrite], Compliance #1, §9 #4 (Documentation-synchronisation) — reflects this revised plan.

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
    OnStart(ctx context.Context, rt Runtime) error    // Phase 4 Gap 1: Runtime, not *App (see amendment 2026-05-25)
    OnShutdown(ctx context.Context, rt Runtime) error
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
    OnStart    func(ctx context.Context, rt Runtime, cfg C) error  // Phase 4 Gap 1 (amendment 2026-05-25)
    OnShutdown func(ctx context.Context, rt Runtime, cfg C) error
}

// Module[C] satisfies ModuleSpec via a wrapper produced by Build().
func (m Module[C]) Build() ModuleSpec { /* type-erased ModuleSpec wrapping m */ }
```

`Migrations` uses `fs.FS` (not `embed.FS`) so that modules can satisfy the contract with a runtime-generated migration source — `embed.FS` implements `fs.FS`, so any `//go:embed` usage works unchanged.

The framework binds `modules.<Name>.*` to `Module[C].Config` during configuration load (validation layer 5). The generic parameter preserves compile-time type safety for the module author; only the framework's internal storage uses the type-erased `ModuleSpec` interface.

Moving a module's package to another application brings its configuration shape, models, routes, jobs and migrations with it. The host application only has to add the module to its `Mount(...)` list.

> **Amendment (2026-05-25) — Phase 4 Slice "Gap 1"/"Gap 2": `nucleus.Runtime` in the module lifecycle.** Authoring the first reference app (`examples/mvc_api`, Slice 1) surfaced that `OnStart`/`OnShutdown` received `*nucleus.App` — the *config* struct — not the running container, so a module could not reach the framework-managed `*sql.DB`/`AutoMigrate` and had to open its own connection (the wrong pattern to teach, and a BLOCKER for the website include-from-source slice). The hooks now receive a `nucleus.Runtime` handle instead — a thin, stable façade over the running `*app.App` exposing only what a module needs:
>
> ```go
> type Runtime interface {
>     DB() *sql.DB                      // managed pool for the module's DefaultDB alias (app default when unset); nil if unconfigured
>     AutoMigrate(models ...any) error  // dev convenience; SQL-first migrations remain the production path
>     Logger() *slog.Logger             // never nil
> }
> ```
>
> The framework builds one `Runtime` per module, bound to that module's declared `DefaultDB` alias, and the module uses `rt.DB()` — the framework owns that handle's lifecycle (it opens it from `databases.*.url` and closes it at shutdown; a module must NOT close it). Returning only stdlib types (`*sql.DB`, `*slog.Logger`) keeps `Runtime` clear of the API firewall. This is a pre-`v1.0` `Module[C]`/`ModuleSpec` signature change with no external consumers, so no DEP/MA cycle is required (ADR-006/ADR-008 precedent). **Gap 2:** `Run` now invokes a module's `OnStart` *before* registering its `Routes`, so a module initialises its resources (`m.db = rt.DB()`) and the `Routes` closure captures them directly — eliminating the lazy-accessor workaround Slice 1 needed. `Runtime` is implemented by the framework, not users, so future minor versions may add methods without breaking module authors.

> **Amendment (2026-05-25) — Phase 4 Slice 2 framework enablement: `Module.Models` auto-registration + builder OpenAPI mount.** Migrating the `nucleus new` scaffolder onto the fluent surface (so scaffold, `examples/mvc_api`, and the website docs all share one idiom) required two capabilities the fluent surface lacked:
>
> - **`Module.Models` → model registry.** `Run` now catalogues each mounted module's `Models()` in the application model registry (a small `registerModuleModels` helper, before module `OnStart`). It registers **unconditionally**, not gated on the admin subsystem: the registry is always allocated (even under `WithoutDefaults`) and backs generic CRUD and `AutoMigrate` metadata in addition to the admin panel, so a module's models should be catalogued regardless. When admin *is* mounted, each model surfaces in the panel; per-model display (list/search/filter columns) is driven by the model's `admin:` struct tags, so `Models: []any{T{}}` yields a working admin with no per-model config on the module surface. No exported symbol added (behavioural).
> - **Builder-level OpenAPI mount.** New `AppBuilder.WithOpenAPI(pattern, provider)` + exported `OpenAPISpec{Pattern, Provider}` + `App.OpenAPI *OpenAPISpec` (Go-only wiring field, `yaml:"-"`); `Run` delegates to `app.App.MountOpenAPI`. Additive contract (freeze rebaselined; no removals). **Stable↔experimental coupling, acknowledged:** `OpenAPISpec.Provider` carries `openapi.DocumentProvider` from `pkg/openapi`, which is classified `experimental`. This anchors a `stable` surface to an experimental type — accepted here because `pkg/app.App.MountOpenAPI` (in the frozen baseline) **already** exposes the identical type, so the coupling pre-exists and the raw type keeps the two surfaces consistent (rather than introducing a divergent `nucleus`-local alias). The consequence is recorded explicitly: a pre-`v1.0` signature change to `openapi.DocumentProvider` would ripple into both stable surfaces, and any promotion of `pkg/openapi` to `stable` is coupled to this surface. The firewall test guards only third-party leaks, not first-party experimental→stable coupling, so this is a documented architectural decision rather than a test-enforced one. Three-surface equivalence covers `App.OpenAPI` (pointer identity; the spec is immutable post-construction and `cloneApp` shares the pointer, mirroring the `effective` snapshot).

`Requires` declares logical database aliases. If `app.Config.Databases` lacks a required entry, the framework fails at boot with `module "<name>" requires database "<alias>" which is not configured` — never a `nil pointer dereference` at runtime.

**Migration namespacing.** Module migration file checksums (per `pkg/db/migrate.go`'s `migrationsChecksumsTable`) are keyed as `<module_name>/<filename>` rather than just `<filename>`, preventing cross-module filename collisions when multiple modules share a database alias. The same namespacing applies to the applied-migrations tracking table (`migrationsTable`) — a half-namespaced design would still fail on the primary-key insert into `nucleus_schema_migrations` when two modules ship the same filename. Phase 2d implements the namespacing on both tracking tables.

### 7. Router with three coexisting styles

`nucleus.Router` is an interface defined in `pkg/nucleus` (not an alias for `*router.Router`) so that modules do not take a hard import on `pkg/router`. The interface exposes `Get`, `Post`, `Put`, `Patch`, `Delete`, `Group`, and `Resource` with consistent signatures. `Patch` is included as a flat-declarative verb (mirrors `pkg/router.Mux.Patch`) so a controller that exposes a partial-update endpoint via the flat style does not have to fall through to `Resource(...)` just to obtain a `PATCH` route; `pkg/router` already plumbs `PATCH` in its default middleware stack and ServeMux registration, so the addition is a one-line surface change with no underlying-runtime cost.

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
- **Code blocks in website docs are imported from `examples/*`** via Docusaurus include syntax, so they cannot drift from real, compilable Go. *(Deferred to Phase 4 / v0.9.X: the original `examples/*` tree was removed on 2026-05-16 per the owner decision; new reference applications shipping in Phase 4 reintroduce the include-from-source pattern.)*
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

- **Rewrite of `pkg/nucleus`.** The current `nucleus.New().Port().SQLite().Model().AutoMigrate().Run()` chain disappears. Pre-`v1.0`, with only two internal example consumers (`examples/ecommerce_dashboard/backend/{main.go,handlers/handlers.go}`), the rewrite is a clean break. **Per the 2026-05-16 owner decision, those two files were not rewritten in the Phase 1 PR; the full `examples/*` tree was removed instead** and new reference applications are authored in v0.9.X as part of Phase 4 / docs-sync. No DEP or MA artefacts are produced; the precedent from ADR-006 / ADR-008 governs.
- The website needs a substantial rewrite of `quickstart.md` and `project-structure.md`, plus introduction of the manifest pattern across pages. This is its own iteration, scheduled as Phase 4 (docs-sync) and **now bundled with the authoring of new reference applications under a freshly-scoped `examples/`** per the 2026-05-16 owner decision. Target: v0.9.X.
- The new validator and merge engine add code to `pkg/nucleus`. Mitigated by the fact that they replace ad-hoc validation scattered through the old `Load` + builder flow and provide a single, testable surface.

### Neutral

- All three surfaces (fluent, direct-struct, bootstrap) converge on `app.New` as the implementation, so every guarantee SPEC.md §3.1 makes about the application container is inherited automatically.

## Compliance

After this ADR is Accepted (in the Phase 1 implementation PR — see §Implementation phases below):

1. `pkg/nucleus/nucleus.go` is rewritten to expose the `AppBuilder` chain with the new methods (`FromConfigFile`, `Use`, `Mount`, `WithoutDefaults`, `WithExtensions`, `Build`, `Start`/`Serve`). The legacy single-purpose methods are removed entirely. **Per the 2026-05-16 owner decision the previous `examples/ecommerce_dashboard/backend/*` consumers are not rewritten in this PR; the full `examples/*` tree is removed instead, and new reference applications demonstrating the fluent surface land in Phase 4 / v0.9.X.**
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

- **Phase 1 — Foundation.** Compliance items #1, #2, #3, #5, #11. Pins the new package shape: canonical `App{}` struct, `Module[C any]` + `ModuleSpec`, `Router` interface, three-surface equivalence test, freeze-scanner seed. **Per the 2026-05-16 owner decision the entire `examples/*` tree was removed in this PR** (replacing the original "update the two `examples/ecommerce_dashboard` consumers" path); new reference applications are authored in Phase 4. ADR `Status` flipped from `Proposed` to `Accepted` in this PR's `docs/adrs/ADR-010-fluent-api-v2-pkg-nucleus.md` edit, following the ADR-006 / ADR-008 acceptance pattern.
- **Phase 2 — Config loading + merge engine.** Compliance items #4, #7, #14, #15, #16, #17.
  - **Phase 2a (landed 2026-05-16):** Single-file YAML/TOML/JSON loader; 1 MiB size cap (#17); wildcard-matcher `keyMatchesAny` with `<alias>`/`<site>`/`<tenant>` placeholder support.
  - **Phase 2b (landed 2026-05-17):** Multi-file merge engine with `_append`/`_remove` suffix operators; null-revert semantics with non-nullable security keys (`ErrSecurityKeyNotNullable`, #14); mixed-format `WARN`/hard-reject via `WithConfigStrict` (`ErrMixedConfigFormats`, #7 partial); `pkg/app.NormalizeRuntimeConfig` public wrapper. Five-layer validator: layers 1 (syntactic) and 2 (schema) fully landed; layer 3 range/enum validation deferred (out of the four-phase slicing — follow-up). Items #15 (strict-mode startup guard) and #16 (migration namespacing) remain for Phase 2c/2d.
  - **Phase 2c (landed 2026-05-17):** Strict-mode startup guard (#15). `AppBuilder.WithUnknownFields("strict"|"warn")` toggles strict schema validation per builder; warn mode emits a `WARN`-level slog event listing the offending keys and proceeds with the load (the unknowns are stripped). `NUCLEUS_ENV=production` (case-insensitive, whitespace-trimmed; constant `EnvProduction`) forces the mode back to strict regardless of code-level configuration and emits a `WARN` recording the override. Misuse paths covered: invalid mode value records `ErrInvalidUnknownFieldsMode`; calling after `FromConfigFile` records a misorder deferred error analogous to `WithConfigStrict`. Compliance item #16 (migration namespacing) remains for Phase 2d.
  - **Phase 2d (landed 2026-05-17):** Module migration namespacing (#16). `pkg/db.NewModuleMigrator(db, path, moduleName, logger)` creates a `*Migrator` whose applied-migration and checksum rows are stored under `<moduleName>/<file-id>` storage keys, preventing cross-module filename collisions when several modules share a database alias. The legacy `NewMigrator` constructor is unchanged. `Migrator.Drift` is ownership-aware: an unscoped Migrator ignores foreign-module rows, a module-scoped Migrator only reports drift for its own rows. `Status` / `Drift` continue to return raw file IDs (operators see filenames, not storage keys). The four-phase slicing of ADR-010 §2 is now complete.
  - **§2 layer 3 — field-semantic validation (landed 2026-05-24):** the deferred range/enum/duration layer. A hand-written `validateSemantics(*app.Config)` (new `ErrInvalidConfigValue` sentinel) runs in the nucleus layer — `AppBuilder.FromConfigFile` (fail-fast at load) and the package-level `Run` (direct-struct surface), per §2's both-paths requirement — rejecting values that bind cleanly but are out of range, not a recognised enum member, or a negative duration. Validated: enums `session_store`/`log_level`/`log_format`/`session_cookie_samesite` (empty → default), ranges `port`/`smtp_port` ∈ [0,65535] (0 = OS-assigned) and non-negative rate-limit counts, and non-negative server/session/jwt/rate-limit durations. Out of scope (deliberate): `mail_driver`/`storage.provider` (plugin-extensible / validated downstream), `env` (freeform), `multitenant.resolver` (already auto-normalised). Not hooked in `app.New` — layer 3 is a nucleus-layer guarantee, leaving the lower-level `pkg/app` entry point unchanged.
  - **§2 layer 4 — referential validation (landed 2026-05-26):** cross-key consistency checks, in two halves with different timings (a deliberate deviation from §2's "all five layers in `FromConfigFile`" framing, recorded here). **Config half** — `validateReferential(*app.Config)` (new `ErrInvalidConfigReference` sentinel) runs immediately after `validateSemantics` in BOTH `FromConfigFile` and `Run`: `mail_driver=smtp` requires `smtp_host` set + `smtp_port>0`; `session_cookie_samesite=none` requires `session_cookie_secure=true` (browsers drop a non-Secure SameSite=None cookie; with `session_cookie_secure` now defaulting true, hitting this needs a deliberate double opt-out). **Module half** — `validateModuleRequires(*app.Config, map[string]ModuleSpec)` fulfils the long-documented §6 guarantee (every alias a module declares in `Requires()` must be a configured database, else boot fails with `module "<name>" requires database "<alias>" which is not configured`). This half runs ONLY in `Run`: modules are registered via `Mount` on the builder, not present in the loaded config file, so it cannot fold into `FromConfigFile`. `Run` calls `app.NormalizeRuntimeConfig` before validating so the synthesised `default` alias is visible (the `FromConfigFile` path is already normalised by `loadFromFiles`). Out of scope (deliberate, deferred): the §2 "auth chain references valid providers" and "observability exporters resolve" checks — auth providers and exporters are plugin-extensible collections with no closed, config-resolvable member set today. Contract: `ErrInvalidConfigReference` rebaselined additively (+1); no removed/renamed symbol.
- **Phase 3 — Effective-config inspection.** Compliance items #6, #12, #13.
  - **Phase 3a (landed 2026-05-22):** the inspection *tooling* — `pkg/nucleus.LoadEffective(paths, extraKeys...)` (new stable API: `ConfigSource`, `EffectiveValue`, `EffectiveConfig`) merges the configured files with per-key provenance, and `nucleus config print --effective` (compliance #6, CLI half) prints every effective key with its value and source. `loadFromFiles` was refactored into a thin wrapper over a new `loadMerged` that tracks provenance by snapshot-and-diff. Redaction (compliance #13) reuses the canonical `observe.DefaultRedactedKeys()` — extended in this PR with the framework's compound secret keys (`jwt_secret`, `admin_bootstrap_password`, `admin_cluster_token`, `session_redis_url`, `admin_cluster_redis_url`, `secret_access_key`, `account_key`) so both log and config-print redaction cover them — plus a parent-aware rule mapping `databases.<alias>.url`/`.dsn` onto the canonical entries (no second list). **As-built scope decisions (owner-confirmed, diverging from the literal §5):** provenance is *source-kind + path* only — the env / flag / programmatic value layers of §4 are **not** applied in the `FromConfigFile` path and `file:line` numbers are **not** emitted; both are deferred to **Phase 3.1**.
  - **Phase 3b (landed 2026-05-23):** the auth-gated `GET /_/config` endpoint (compliance #12), the runtime mirror of `config print --effective`. As-built: §5 / #12 assumed a `WithAdmin()` composition toggle that does not exist, so the endpoint is mounted from the nucleus layer (`pkg/nucleus.Run`) and gated on the admin subsystem being active (`core.Admin != nil`) — a `WithoutDefaults()` app does not expose it. Three defence-in-depth layers: (1) the mount gate; (2) the app-wide ADR-004 Casbin default-deny — since that enforcer reads JWT claims only, a session-admin resolves to the `anonymous` subject there, so `Run` adds a bootstrap-subject allow policy for the fixed `/_/config` path via `core.Authorizer.AddPolicy(authz.BootstrapSubject, "/_/config", "*")` (the same precedent as `/admin/*` in `authz.BootstrapAllowList`, but added from the nucleus layer that owns the route rather than by editing the stable `pkg/authz` package); (3) `admin.NewDatabaseAdminAuth(core.DefaultDB(), core.Session, core.Config.AdminPrefix)` validates the admin session — anon → 403, admin session → 200 JSON (`Cache-Control: no-store`). The effective snapshot is threaded builder→`Run` via an unexported `App.effective *EffectiveConfig` captured in `FromConfigFile` (with the same load options + the app's `LogRedactExtraKeys`, so the endpoint's redaction is app-aware where the CLI is canonical-only). The direct-struct `Run(App{})` path (no file paths) falls back to a snapshot flattened from the live `core.Config` with a new `ConfigSource.Kind` value `"runtime"` (alongside the file kinds `"default"`/`"yaml"`/`"toml"`/`"json"`). Redaction reuses the canonical `observe.DefaultRedactedKeys()`, extended in this PR with the AWS access-key-ID pair (`access_key_id`, `aws_access_key_id`) so the S3 credential is fully covered; public identifiers (`account_name`, `smtp_user`, admin bootstrap username/email) are deliberately left in cleartext.
  - **Phase 3.1 (landed 2026-05-23):** the env-value layer + `file:line` provenance deferred from 3a. **Env layer:** `loadMerged` now applies the `NUCLEUS_`-prefixed environment layer (same koanf `env.Provider` and `__`→`.` transform as `app.LoadConfig`) AFTER the file loop, honouring the §4 precedence `defaults < files < env`. This also closes a latent gap — the fluent `FromConfigFile`→`Run` path previously ignored env entirely (`app.New` only `mergeDefaults`), so env overrides now take effect via the builder, not just via `app.LoadConfig`. Env-sourced keys carry `ConfigSource{Kind:"env", Path:<VAR_NAME>}`. Only schema-recognised keys are applied (env is an ambient namespace — an unrecognised `NUCLEUS_`-prefixed var is ignored, unlike strictly-validated files); empty values on non-nullable security keys (e.g. `NUCLEUS_JWT_SECRET=`) are a boot error mirroring the file `null` guard, so env cannot silently disable signing. **`file:line`:** new additive `ConfigSource.Line int` (`omitempty`); YAML files report the 1-based key line via a `go.yaml.in/yaml/v3` `yaml.Node` walk (the same module koanf's parser uses — promoted from indirect to direct, confined to unexported helpers). TOML reports kind+path only (positions exist solely behind go-toml's *unstable* API — out of scope) and JSON has no standard line API. Known limitations: keys produced by `_append`/`_remove` operators, and keys reached only through YAML anchors/merge keys, carry no line. The CLI renders `kind:path:line` (e.g. `yaml:nucleus.yaml:12`). Owner decision (2026-05-23): YAML-only `file:line`, no dependency on go-toml's unstable API. Contract: `ConfigSource.Line` rebaselined additively (+1); no removed/renamed symbol. The CLI-flags and programmatic-override layers of §4 remain unimplemented.
- **Phase 4 — Docs-sync + website + new reference applications.** Compliance items #8, #9, #10. (#8 already landed; #9 and #10 are the new work.) **Per the 2026-05-16 owner decision the new `examples/*` reference applications (replacing the tree removed in Phase 1) land in this phase**, alongside the website rewrite and the docs-sync mechanism. Target window: v0.9.X. Sliced:
  - **Slice 1 (landed 2026-05-24):** first reference app `examples/mvc_api` — a minimal MVC+REST app (one `notes` resource) on the fluent surface, in the root module (compiles against local `pkg/`, build/test-checked by CI), schema via `nucleus migrate up`. Authoring it **validated the new surface and surfaced two framework gaps** (the ADR's intent): **(Gap 1)** `ModuleSpec.OnStart` receives `*nucleus.App` (config), not the runtime `*app.App` — modules cannot reach the managed `*sql.DB`/`AutoMigrate`, so the example opens its own connection. **This is a BLOCKER for the website include-from-source slice** (it would teach the wrong pattern); resolution is the **next Slice — pass a `nucleus.Runtime` handle into `OnStart`/`OnShutdown`** (a pre-`v1.0` `Module[C]`/`ModuleSpec` signature change; no external consumers ⇒ no deprecation cycle), then rework the example to `rt.DB()`. **(Gap 2)** `Run` calls `Routes` before `OnStart`, so capturing lifecycle-initialised state in the `Routes` closure silently captures nil — documented; the example uses a lazy accessor; a regression test locks it out. The ordering should be documented in `ModuleSpec` godoc (+ optionally reversed) in the Gap-1 slice.
  - **Slice "Gap 1"/"Gap 2" (landed 2026-05-25):** introduced the `nucleus.Runtime` handle and passed it into `OnStart`/`OnShutdown` in place of `*nucleus.App` (Gap 1), and reversed the startup order so module `OnStart` runs before `Routes` registration (Gap 2). See the amendment under §Module spec above. `examples/mvc_api` reworked to `rt.DB()` — no own connection, no lazy accessor. Additive freeze rebaseline (`type:Runtime` + its three `iface-method`s); the `OnStart`/`OnShutdown` signature change carries the same baseline entry names so it surfaces as an intentional pre-`v1.0` break for the contract-guardian, not a removal. **Unblocks Slice 2.**
  - **Slice 2 (pending, now unblocked):** website include-from-source pattern + `quickstart.md`/`project-structure.md` rewrite, importing real code from `examples/mvc_api`.
  - **Slice 3 (pending):** more reference apps + the `website-check.yml` CI gate (#10 completion).

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
