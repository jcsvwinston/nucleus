// Package nucleus is the fluent façade over the production-grade
// application container in `pkg/app`. It is the recommended entry
// point for assembling Nucleus applications at any size — single-file
// demos, embedded services, and enterprise bootstrap patterns alike —
// and composes the existing capability packages (`pkg/router`,
// `pkg/db`, `pkg/auth`, `pkg/authz`, `pkg/storage`, `pkg/mail`,
// `pkg/observe`, `pkg/signals`, `pkg/tasks`) without duplicating any.
//
// Three coexisting surfaces produce the same `nucleus.App{}` value:
//
//   - Fluent: sugar over the struct, ideal for demos and embedded use.
//
//     nucleus.New().
//     FromConfigFile("config/nucleus.yaml").
//     Use(middleware.Logger(), middleware.Recover()).
//     Mount(articles.Module, users.Module).
//     Start()
//
//   - Direct struct: for tests and programmatic embedding.
//
//     nucleus.Run(nucleus.App{
//     Config:  app.Config{Port: 8080},
//     Modules: map[string]nucleus.ModuleSpec{
//     "articles": articles.Module,
//     },
//     })
//
//   - Bootstrap pattern: a user-space convention (no sub-package
//     ships with the framework). Define your own constructor — typically
//     `internal/bootstrap/bootstrap.go` — that returns `nucleus.App`,
//     then call `nucleus.Run(bootstrap.New())`.
//
// The package is the Phase 1 Foundation of ADR-010 (Fluent API v2 for
// pkg/nucleus): it pins the canonical struct shape, the `Module[C any]`
// generic constructor, the `Router` interface with three coexisting
// registration styles, and the three-surface equivalence guarantee.
// Configuration loading (ADR-010 Phase 2a–2d) is fully shipped:
// `FromConfigFile` accepts one or more paths and merges them
// left-to-right (last-file-wins for scalars, deep-merge for maps).
// Per-file size cap: 1 MiB (MaxConfigFileBytes). Supported formats:
// YAML (.yaml/.yml), TOML (.toml), JSON (.json). The `_append` and
// `_remove` suffix operators provide additive/subtractive list
// semantics. `null` reverts a key to its struct default — except for
// non-nullable security keys (e.g. `jwt_secret`) where `null` is a
// boot error (ErrSecurityKeyNotNullable). Mixed-format file lists
// emit a startup WARN by default; WithConfigStrict(true) upgrades
// the warning to ErrMixedConfigFormats. WithUnknownFields("warn")
// downgrades schema-validation failures to WARN-level slog events;
// NUCLEUS_ENV=production forces the mode back to strict regardless of
// the code-level setting.
package nucleus

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/knadh/koanf/v2"
)

// Option is the configuration-time option type accepted by `Run` and
// stored in `App.Options`. It is a re-export of `app.Option` so callers
// can pass `nucleus.WithoutDefaults()` / `nucleus.WithExtensions(...)`
// without taking an explicit dependency on `pkg/app`.
type Option = app.Option

// Extension is a re-export of `app.Extension`, the interface every
// production subsystem (storage, custom auth, the orbit admin module, …)
// implements to register itself with the application container. Pass values
// via `nucleus.WithExtensions(...)`.
type Extension = app.Extension

// WithoutDefaults disables the framework's default extensions (storage,
// mail, authz). Mirrors `app.WithoutDefaults`. Use for lightweight services
// that compose their own extension set.
func WithoutDefaults() Option { return app.WithoutDefaults() }

// WithExtensions registers one or more production extensions to be
// attached during application construction. Mirrors `app.WithExtensions`.
func WithExtensions(exts ...Extension) Option { return app.WithExtensions(exts...) }

// WithOpenAuthz is the explicit escape hatch from the default-deny
// Casbin enforcer mounted by `app.New` (ADR-004). Mirrors
// `app.WithOpenAuthz`. The framework logs a `WARN` at startup when this
// option is active so the choice is visible in operational telemetry.
func WithOpenAuthz() Option { return app.WithOpenAuthz() }

// LifecycleHooks holds app-level callbacks that fire before the
// HTTP listener starts and after the listener returns. Module-level
// `OnStart` / `OnShutdown` continue to live on `ModuleSpec`; the
// hooks here are reserved for cross-cutting concerns that no module
// owns (e.g. external readiness signalling).
type LifecycleHooks struct {
	OnStart    func(context.Context) error
	OnShutdown func(context.Context) error
}

// ServiceRegistration declares a long-running background goroutine
// the framework should manage alongside the HTTP listener. `Run`
// receives a context that the framework cancels at shutdown; the
// function must return when its context is cancelled.
//
// `Health` is optional. The full /healthz integration lands in a
// later phase; Phase 1 spawns `Run` but does not yet wire `Health`
// into the health endpoint.
type ServiceRegistration struct {
	Name   string
	Run    func(context.Context) error
	Health func(context.Context) error
}

// OpenAPISpec declares a JSON OpenAPI document endpoint for the application
// to mount at Run time (ADR-010 Phase 4, Slice 2). Pattern is passed verbatim
// to the underlying mount, which normalises an empty value to
// "/openapi.json"; the struct field itself stores whatever was supplied.
//
// Handler is any http.Handler that serves the document JSON — typically
// `openapi.Handler(provider)` for a generated document factory, but
// pre-rendered bytes, an embedded file, or a proxy work equally. The
// provider-typed `Provider` field was removed in v0.12.0 (DEP-2026-008);
// the stable surface is stdlib-only.
type OpenAPISpec struct {
	Pattern string
	Handler http.Handler
}

// App is the canonical struct that every entry point — fluent builder,
// direct-struct call, bootstrap function — produces. It embeds
// `app.Config` (so every yaml-bindable production-grade option is
// present unchanged) and adds four Go-only wiring fields tagged
// `yaml:"-"` so that they cannot be expressed in a configuration file.
//
// Modules is a map (not a slice) so configuration overlays can
// override individual modules by name in later phases. Middleware is a
// slice because registration order is significant: the router applies
// middleware in the order it was registered.
type App struct {
	app.Config `yaml:",inline"`

	Modules    map[string]ModuleSpec `yaml:"-"`
	Middleware []Middleware          `yaml:"-"`
	Services   []ServiceRegistration `yaml:"-"`
	Lifecycle  LifecycleHooks        `yaml:"-"`
	Options    []Option              `yaml:"-"`

	// OpenAPI, when non-nil, mounts a JSON OpenAPI document endpoint at
	// Run time via the underlying app container (ADR-010 Phase 4, Slice 2).
	// The fluent builder sets it through AppBuilder.WithOpenAPIHandler;
	// direct-struct callers populate it explicitly. Nil means no OpenAPI
	// endpoint.
	OpenAPI *OpenAPISpec `yaml:"-"`

	// moduleConfigsRaw holds the `modules.<name>.*` sub-koanf for each module
	// declared in the loaded config files (ADR-010 §2 layer 5), keyed by module
	// name. Set by FromConfigFile; nil for the direct-struct surface (no file to
	// parse). bindModuleConfigs consumes it at Run time to bind each module's
	// typed Config. Unexported so it stays off the public contract surface.
	moduleConfigsRaw map[string]*koanf.Koanf
}

// AppBuilder is the fluent surface returned by `New()`. Methods on
// `AppBuilder` are non-destructive against the caller and idempotent:
// `Use`, `Mount`, `WithoutDefaults`, and `WithExtensions` append to
// the underlying slices; `FromConfigFile` records intent. Errors
// accumulated during chaining (a duplicate module name, a malformed
// config file, …) are surfaced when the builder is realised via
// `Build`, `Start`, or `Serve`.
type AppBuilder struct {
	a                   App
	err                 error
	configStrict        bool   // ADR-010 §3 — reject mixed-format file lists in FromConfigFile.
	configUnknownFields string // ADR-010 §15 — "strict" (default) or "warn".
	configFileLoaded    bool   // set after FromConfigFile succeeds; gates misordered WithConfigStrict / WithUnknownFields.
}

// New returns an `AppBuilder` seeded with the framework's
// `app.DefaultConfig()`. The default config is the same value
// `pkg/app` produces — sensible production defaults for port, log
// level, observability bootstrap, etc. Override fields via the
// fluent methods or by reaching into the underlying struct through
// `Build`.
func New() *AppBuilder {
	return &AppBuilder{
		a: App{
			Config:  app.DefaultConfig(),
			Modules: make(map[string]ModuleSpec),
		},
	}
}

// FromConfigFile loads configuration from one or more files. Each
// file is read via the Phase 2b loader (`loadFromFiles` in config.go)
// which enforces, per file:
//
//   - 1 MiB per-file size cap (see MaxConfigFileBytes) — eliminates
//     parser-DoS classes against the underlying format parsers.
//   - YAML (`.yaml` / `.yml`), TOML (`.toml`), and JSON (`.json`)
//     formats. Any other extension surfaces `ErrUnsupportedConfigFormat`.
//   - Strict-unknown-fields schema validation against
//     `app.ContractConfigKeyPatterns()`. Unknown keys surface as
//     `ErrUnknownConfigKeys` with did-you-mean hints for likely
//     typos.
//
// Multi-file merge semantics (ADR-010 §3):
//
//   - Precedence is `struct defaults < file[0] < … < file[N-1]`.
//   - Scalars replace; maps deep-merge; lists replace by default.
//   - The suffix operators `<key>_append` and `<key>_remove` provide
//     additive and subtractive list semantics that survive every
//     parser the loader supports.
//   - `null` reverts the key to its struct default — except for keys
//     in the non-nullable security set (e.g. `jwt_secret`) where
//     `null` is a boot error (`ErrSecurityKeyNotNullable`).
//   - Mixed-format file lists emit a startup `WARN` by default;
//     `AppBuilder.WithConfigStrict(true)` upgrades the warning to a
//     hard `ErrMixedConfigFormats` error.
//
// Errors accumulate on the builder and surface at `Build` / `Start` /
// `Serve` — the bufio.Scanner pattern. `Err()` exposes the
// accumulator for callers that want to inspect chain status before
// realising.
//
// `WithConfigStrict(...)` must be called BEFORE `FromConfigFile` to
// affect the same load. The builder records the strict flag at call
// time; later flips do not retroactively re-evaluate a previously
// loaded file list.
func (b *AppBuilder) FromConfigFile(paths ...string) *AppBuilder {
	if b.err != nil {
		return b
	}
	if len(paths) == 0 {
		b.err = errors.New("nucleus: FromConfigFile requires at least one path")
		return b
	}
	opts := configLoadOptions{
		strict:        b.configStrict,
		unknownFields: b.configUnknownFields,
	}
	cfg, moduleConfigs, err := loadFromFilesWithModules(paths, opts)
	if err != nil {
		b.err = err
		return b
	}
	// Preserve fluent-chain Modules/Middleware/Services/etc. that the
	// caller registered before FromConfigFile — only the embedded
	// app.Config slot is replaced.
	b.a.Config = *cfg
	// ADR-010 §2 layer 5: retain the `modules.<name>.*` subtrees for binding at
	// Run time. FromConfigFile and Mount may be called in either order, so the
	// actual bind is deferred to Run, where both the config and the module set
	// are known. A later FromConfigFile replaces this wholesale (last-load-wins).
	b.a.moduleConfigsRaw = moduleConfigs
	b.configFileLoaded = true

	// ADR-010 §2 layer 3: field-semantic validation (ranges/enums/durations)
	// at load time, so a bad value surfaces at Build/Err/Start rather than
	// deep inside subsystem construction.
	if err := validateSemantics(cfg); err != nil {
		b.err = err
		return b
	}
	// ADR-010 §2 layer 4 (config half): cross-field referential checks
	// (smtp_host/smtp_port↔mail_driver; samesite/secure). The module
	// Requires()→Databases half runs in Run, where the module set is known.
	if err := validateReferential(cfg); err != nil {
		b.err = err
		return b
	}

	return b
}

// WithConfigStrict toggles the merge-engine's mixed-format guard for
// subsequent `FromConfigFile` calls on this builder. With strict
// mode on, a file list mixing two or more of YAML / TOML / JSON is
// rejected outright with `ErrMixedConfigFormats`; with strict mode
// off (the default), the loader emits a `WARN` slog event and
// proceeds with the merge. The toggle is per-builder and idempotent;
// re-calling with the same value is a no-op.
//
// Call this BEFORE `FromConfigFile` — the strict flag is read at
// load time, not retroactively. To prevent silent misuse, calling
// `WithConfigStrict` AFTER `FromConfigFile` records a deferred
// error on the builder so the misordered chain fails loud at
// `Build` / `Start` / `Serve` time. Most builders set strict mode
// once near the top of the chain and never touch it again, so this
// guard is invisible to correct usage.
func (b *AppBuilder) WithConfigStrict(strict bool) *AppBuilder {
	if b.err != nil {
		return b
	}
	if b.configFileLoaded {
		b.err = errors.New("nucleus: WithConfigStrict must be called before FromConfigFile (re-order the builder chain so the strict flag is set first)")
		return b
	}
	b.configStrict = strict
	return b
}

// WithUnknownFields configures how `FromConfigFile` reacts to keys
// present in a file but absent from `app.Config`'s schema. Two
// modes are accepted (see `UnknownFieldsStrict` / `UnknownFieldsWarn`
// constants):
//
//   - `"strict"` (default): unknown keys reject the load with
//     `ErrUnknownConfigKeys` and a did-you-mean hint.
//   - `"warn"`: unknown keys emit a `WARN`-level slog event listing
//     the offending keys; the load proceeds with the unknowns
//     stripped so they do not leak into the merged config.
//
// ADR-010 §15: when `WithUnknownFields("warn")` is active outside
// production, the loader additionally emits a "do not deploy to
// production" WARN at load time. The `NUCLEUS_ENV=production`
// environment variable is the operator escape hatch: when set, the
// loader forces the mode back to strict regardless of code-level
// configuration, and emits a WARN recording the override. A future
// build leaving `WithUnknownFields("warn")` in production code is
// therefore not silently exposed to typo'd config values.
//
// Any value other than the two accepted modes records
// `ErrInvalidUnknownFieldsMode` as a deferred builder error. Like
// `WithConfigStrict`, the call must happen BEFORE `FromConfigFile`;
// calling it after records the misorder error.
func (b *AppBuilder) WithUnknownFields(mode string) *AppBuilder {
	if b.err != nil {
		return b
	}
	if b.configFileLoaded {
		b.err = errors.New("nucleus: WithUnknownFields must be called before FromConfigFile (re-order the builder chain so the mode is set first)")
		return b
	}
	switch mode {
	case UnknownFieldsStrict, UnknownFieldsWarn:
		b.configUnknownFields = mode
		return b
	default:
		b.err = fmt.Errorf("%w: got %q", ErrInvalidUnknownFieldsMode, mode)
		return b
	}
}

// Use appends global middleware to be applied to the underlying
// router before any module routes. Registration order is preserved.
// To attach middleware to a specific subtree, declare it on the
// module's `Middleware` field or use `Router.Group` inside the
// module's `Routes` callback.
func (b *AppBuilder) Use(mws ...Middleware) *AppBuilder {
	if b.err != nil {
		return b
	}
	b.a.Middleware = append(b.a.Middleware, mws...)
	return b
}

// Mount registers one or more module specs. Each spec is stored in
// `App.Modules` keyed by `spec.Name()`. Two modules sharing a name is
// a configuration bug — the builder records the error and surfaces it
// when realised.
func (b *AppBuilder) Mount(specs ...ModuleSpec) *AppBuilder {
	if b.err != nil {
		return b
	}
	if b.a.Modules == nil {
		b.a.Modules = make(map[string]ModuleSpec, len(specs))
	}
	for _, s := range specs {
		name := s.Name()
		if name == "" {
			b.err = errors.New("nucleus: module Name must be non-empty")
			return b
		}
		if _, dup := b.a.Modules[name]; dup {
			b.err = fmt.Errorf("nucleus: duplicate module name %q in Mount", name)
			return b
		}
		b.a.Modules[name] = s
	}
	return b
}

// WithoutDefaults appends `app.WithoutDefaults()` to the option chain
// forwarded verbatim to `app.New`. Direct-struct callers achieve the
// same effect by setting `App.Options`.
func (b *AppBuilder) WithoutDefaults() *AppBuilder {
	if b.err != nil {
		return b
	}
	b.a.Options = append(b.a.Options, WithoutDefaults())
	return b
}

// WithExtensions appends `app.WithExtensions(exts...)` to the option
// chain forwarded verbatim to `app.New`.
func (b *AppBuilder) WithExtensions(exts ...Extension) *AppBuilder {
	if b.err != nil {
		return b
	}
	b.a.Options = append(b.a.Options, WithExtensions(exts...))
	return b
}

// WithOpenAPIHandler registers a JSON OpenAPI document endpoint to be
// mounted at Run time, served by any stdlib http.Handler — typically
// `openapi.Handler(provider)` for a generated document factory, but any
// handler that writes the document JSON works (pre-rendered bytes, an
// embedded file, a proxy). `pattern` is the route (defaulting to
// "/openapi.json" when empty). A nil handler records a deferred builder
// error. Calling it more than once replaces the previously recorded spec
// (last-wins), matching the other fluent setters.
//
// The stable builder is stdlib-only: the provider-typed WithOpenAPI was
// removed in v0.12.0 (DEP-2026-008).
func (b *AppBuilder) WithOpenAPIHandler(pattern string, handler http.Handler) *AppBuilder {
	if b.err != nil {
		return b
	}
	if handler == nil {
		b.err = errors.New("nucleus: WithOpenAPIHandler requires a non-nil handler")
		return b
	}
	b.a.OpenAPI = &OpenAPISpec{Pattern: pattern, Handler: handler}
	return b
}

// Build realises the builder into an `App` value plus any deferred
// error. The returned `App` is a copy of the builder's internal
// state: subsequent mutations on the builder do not affect a
// previously-built App. Used by `Start`, `Serve`, and the
// three-surface equivalence test.
func (b *AppBuilder) Build() (App, error) {
	if b.err != nil {
		return App{}, b.err
	}
	return cloneApp(b.a), nil
}

// Err exposes the builder's accumulated error without realising it.
// Useful in tests and in callers that want to inspect chain status
// before deciding to call Start. Returns `nil` if no error has been
// recorded.
func (b *AppBuilder) Err() error { return b.err }

// Start realises the builder and runs the resulting application until
// the process receives a shutdown signal or the context returned by
// `app.App.Run` is cancelled. Equivalent to `nucleus.Run(b.Build())`.
func (b *AppBuilder) Start() error {
	a, err := b.Build()
	if err != nil {
		return err
	}
	return Run(a)
}

// Serve is an alias for `Start`. ADR-010 lists `Start` as the
// canonical builder terminator; `Serve` is provided as an ergonomic
// synonym for callers who prefer the HTTP-server-flavoured name.
func (b *AppBuilder) Serve() error { return b.Start() }

// Run is the package-level direct-struct surface. It accepts a fully
// populated `App` and runs the same startup sequence the fluent
// builder uses. Direct-struct callers — typically tests or the
// bootstrap pattern — invoke this function with their own constructed
// value.
//
// Startup sequence (ADR-010 Phase 4 ordering):
//
//  1. Construct `*app.App` via `app.New(&a.Config, a.Options...)`.
//  2. Apply `a.Middleware` globally to the application router.
//  3. Build a per-module `Runtime` handle bound to each module's
//     `DefaultDB` alias.
//  4. Run app-level `Lifecycle.OnStart`.
//  5. For each module (sorted order): run `OnStart(ctx, rt)` — BEFORE
//     route registration, so a module initialises managed resources its
//     Routes closure can then capture (Gap 2) — and register its
//     `OnShutdown` only after `OnStart` succeeds.
//  6. Collect each module's `spec.Jobs` / `spec.Webhooks` registrations
//     against the real registries (a broken registration fails boot here).
//  7. For each module: route its `spec.Routes(Router)` under
//     `spec.Prefix()`, applying per-module middleware first; then mount
//     webhook routes under the `webhooks_prefix`.
//  8. Start the module jobs runtime (pkg/tasks provider per
//     `jobs_provider`) and spawn each `ServiceRegistration` Run in a
//     goroutine; the framework cancels their shared context at shutdown.
//  9. Block on `app.App.Run`.
//  10. After Run returns: cancel services and the jobs worker, stop the
//     jobs scheduler, run app-level `Lifecycle.OnShutdown` (module
//     `OnShutdown` hooks fire inside `app.App.Run`'s shutdown path).
func Run(a App) error {
	cfg := a.Config

	// Normalise before validating: app.New normalises internally (multi-tenant
	// / alias canonicalisation, synthesising the "default" database alias when
	// Databases is empty), but layer-4 validateModuleRequires below checks
	// Requires() against cfg.Databases and must see the synthesised aliases.
	// The FromConfigFile path is already normalised by loadFromFiles; this makes
	// the direct-struct Run(App{...}) path consistent (NormalizeRuntimeConfig is
	// idempotent, so the second call inside app.New is a no-op).
	app.NormalizeRuntimeConfig(&cfg)

	// ADR-010 §2 layer 3: field-semantic validation. Covers the direct-struct
	// surface (no FromConfigFile load); for the builder path FromConfigFile
	// already validated at load, so this is an idempotent re-check — kept
	// (not skipped) because a caller can mutate App.Config programmatically
	// after Build/FromConfigFile, and that override must not bypass layer 3.
	if err := validateSemantics(&cfg); err != nil {
		return err
	}
	// ADR-010 §2 layer 4: config cross-field checks, then the module
	// Requires()→configured-database-alias check (ADR-010 §6). The latter
	// can only run here — modules are Mount-ed on the builder, not present in
	// the loaded config — so it fails fast before app.New does any work.
	if err := validateReferential(&cfg); err != nil {
		return err
	}
	if err := validateModuleRequires(&cfg, a.Modules); err != nil {
		return err
	}

	// ADR-010 §2 layer 5: bind each module's `modules.<name>.*` subtree into its
	// typed Config, apply `default:` tags, and validate `validate:` tags. Runs
	// before app.New so a bad module config fails fast — no DB pool or telemetry
	// is set up for a misconfigured app — and so the bound specs are in place
	// before registerModuleModels and the module OnStart/Routes sequence below.
	if err := bindModuleConfigs(&a); err != nil {
		return err
	}

	// Webhooks authenticate by signature (WebhookSpec.Secret), not by CSRF
	// token, so when CSRF protection is on and at least one module declares
	// webhooks the webhook prefix is exempted here — before app.New builds
	// the middleware stack, which is the last moment the exemption can take
	// effect. The trailing "/" keeps the prefix match exact ("/webhooks/…"
	// without also exempting a hypothetical "/webhooksfoo").
	if cfg.CSRFEnabled && anyModuleDeclaresWebhooks(a.Modules) {
		cfg.CSRFExemptPaths = append(cfg.CSRFExemptPaths, webhookPathPrefix(&cfg)+"/")
	}

	core, err := app.New(&cfg, a.Options...)
	if err != nil {
		return fmt.Errorf("nucleus: app.New: %w", err)
	}

	if core.Router != nil && len(a.Middleware) > 0 {
		core.Router.Use(a.Middleware...)
	}

	// Module names are sorted once to give a deterministic order across
	// runs — important for the equivalence test and for predictable
	// startup logs. The sorted slice is reused for every subsequent
	// module-iteration so the ordering rationale is declared in one place.
	sortedSpecs := sortedModuleSpecs(a.Modules)

	// ADR-010 Phase 4, Gap 1: each module receives a `Runtime` handle bound
	// to its declared `DefaultDB` alias (empty == application default), so a
	// module reaches the framework-managed `*sql.DB`/`AutoMigrate` instead
	// of opening its own connection. Built once per module and shared
	// between that module's OnStart and OnShutdown hooks.
	runtimes := make(map[string]Runtime, len(sortedSpecs))
	for _, spec := range sortedSpecs {
		runtimes[spec.Name()] = newRuntime(core, spec.DefaultDB())
	}

	// ADR-010 Phase 4 (Slice 2): catalogue each module's declared Models in the
	// application's model registry — before module OnStart, so a module may rely
	// on its models being registered.
	if err := registerModuleModels(core, sortedSpecs); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// App-level Lifecycle.OnStart runs before any module starts.
	if a.Lifecycle.OnStart != nil {
		if err := a.Lifecycle.OnStart(ctx); err != nil {
			return fmt.Errorf("nucleus: Lifecycle.OnStart: %w", err)
		}
	}

	// ADR-010 Phase 4, Gap 2: module OnStart runs BEFORE route registration.
	// A module initialises its managed resources here (e.g. `m.db = rt.DB()`)
	// so its Routes closure can capture that state directly — the Phase 1
	// order (Routes before OnStart) made the closure observe not-yet-
	// initialised state, forcing a lazy-accessor workaround in modules.
	//
	// A module's OnShutdown is registered only AFTER its OnStart succeeds, in
	// sorted module order. So a mid-sequence OnStart failure (Run returns the
	// error and never reaches core.Run) leaves no shutdown hook registered for
	// a module that never started — closing a correctness edge flagged in
	// review. NOTE: this does not roll back the OnShutdown of modules that DID
	// start earlier in the sequence (Run returns before core.Run, whose
	// shutdown path would invoke them); a partially-initialised startup that
	// leaks earlier modules' resources remains a tracked follow-up.
	for _, spec := range sortedSpecs {
		s := spec
		rt := runtimes[s.Name()]
		if err := s.OnStart(ctx, rt); err != nil {
			return fmt.Errorf("nucleus: module %q OnStart: %w", s.Name(), err)
		}
		core.OnShutdown(func(ctx context.Context) error {
			return s.OnShutdown(ctx, rt)
		})
	}

	// Readiness diagnostics: emit exactly one boot-time WARN per module for
	// any surface the contract advertises but the runtime does not honour
	// (embedded Migrations under the SQL-first policy). Done in its own loop —
	// outside the core.Router guard below and separate from mountModule's
	// multiple return paths — so the warnings fire once and regardless of
	// routing.
	for _, spec := range sortedSpecs {
		warnModuleReadiness(core, spec)
	}

	// ADR-010 Phase 2: collect every module's Jobs/Webhooks registrations
	// against the real registries. After OnStart — the closures may capture
	// state a module initialised there — and before mounting, so a broken
	// registration fails boot before any route is live. The jobs runtime
	// itself starts further down, next to the user services, once the router
	// is fully assembled.
	moduleJobsRuntime := newModuleJobs(moduleLogger(core))
	moduleWebhooksRuntime := newModuleWebhooks(moduleLogger(core))
	for _, spec := range sortedSpecs {
		if err := moduleJobsRuntime.collect(spec); err != nil {
			return err
		}
		if err := moduleWebhooksRuntime.collect(spec); err != nil {
			return err
		}
	}

	// Module mount: per-module middleware, then routes — after OnStart.
	// Webhook routes mount on the same router, under the webhooks prefix.
	// (With a nil router, webhook routes are skipped exactly like Routes.)
	if core.Router != nil {
		for _, spec := range sortedSpecs {
			mountModule(core, spec)
		}
		moduleWebhooksRuntime.mount(core, webhookPathPrefix(core.Config))
	}

	// ADR-010 Phase 4, Slice 2: mount the OpenAPI document endpoint if the
	// builder/struct declared one. The core mount owns the nil-handler and
	// empty-pattern guards, so a misconfigured direct-struct App.OpenAPI
	// fails loud here rather than being silently skipped.
	if a.OpenAPI != nil {
		if err := core.MountOpenAPIHandler(a.OpenAPI.Pattern, a.OpenAPI.Handler); err != nil {
			return fmt.Errorf("nucleus: MountOpenAPIHandler: %w", err)
		}
	}

	servicesCtx, cancelServices := context.WithCancel(ctx)
	var wg sync.WaitGroup

	// Start the module jobs runtime (a no-op when no module registered jobs):
	// the pkg/tasks worker runs on servicesCtx via wg — the same lifecycle as
	// user services — and the scheduler starts ticking here, before core.Run.
	if err := moduleJobsRuntime.start(servicesCtx, &wg, core.Config); err != nil {
		cancelServices()
		wg.Wait()
		return err
	}

	for _, svc := range a.Services {
		if svc.Run == nil {
			continue
		}
		wg.Add(1)
		s := svc
		go func() {
			defer wg.Done()
			if err := s.Run(servicesCtx); err != nil && !errors.Is(err, context.Canceled) {
				// Surface service failures through the framework's
				// structured logger so a misbehaving worker (cert
				// rotation, key re-key, session invalidation, …) is
				// visible in operational telemetry rather than
				// silently dying. context.Canceled is the normal
				// signal-driven exit path and is filtered out.
				core.Logger.Error("nucleus: service terminated with error",
					"service", s.Name, "error", err)
			}
		}()
	}

	runErr := core.Run(ctx)

	cancelServices()
	wg.Wait()
	// Scheduler first (no new ticks), then the worker — after wg.Wait() the
	// ctx-driven worker exit has already happened, so this is a final,
	// idempotent cleanup of provider resources.
	moduleJobsRuntime.close()

	if a.Lifecycle.OnShutdown != nil {
		// FW-2: bound the app-level shutdown hook with the same budget the
		// rest of the framework uses for graceful shutdown (pkg/app derives
		// it from write_timeout, falling back to 10s). Previously this used
		// context.WithCancel(context.Background()) — no deadline — so a hook
		// that blocked forever would hang process shutdown indefinitely.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), lifecycleShutdownTimeout(core))
		defer shutdownCancel()
		if err := a.Lifecycle.OnShutdown(shutdownCtx); err != nil && runErr == nil {
			runErr = fmt.Errorf("nucleus: Lifecycle.OnShutdown: %w", err)
		}
	}

	return runErr
}

// lifecycleShutdownTimeout returns the deadline budget for the app-level
// Lifecycle.OnShutdown hook (FW-2). It mirrors pkg/app's internal
// withTimeoutFromConfig: prefer the configured write_timeout, fall back to
// 10s when it is unset or non-positive. The duration-only shape keeps this
// reachable from pkg/nucleus without exporting pkg/app's helper. core and
// core.Config are non-nil on the success path of app.New, but both are
// guarded defensively so a future caller cannot trip a nil dereference.
func lifecycleShutdownTimeout(core *app.App) time.Duration {
	const fallback = 10 * time.Second
	if core == nil || core.Config == nil {
		return fallback
	}
	if core.Config.WriteTimeout > 0 {
		return core.Config.WriteTimeout
	}
	return fallback
}

// mountModule registers a module's routes on the application router.
// Per-module middleware is scoped to the module's prefix via the underlying
// Mux's `Route` helper so it does not leak into sibling modules. Jobs and
// webhooks are not mounted here: their registrations are collected against
// the real registries in Run's collection loop, webhook routes mount under
// the webhooks prefix (outside any module prefix), and the jobs runtime
// starts alongside the user services.
func mountModule(core *app.App, spec ModuleSpec) {
	prefix := spec.Prefix()
	mws := spec.Middleware()

	if prefix == "" && len(mws) == 0 {
		spec.Routes(newRouterAdapter(core.Router, ""))
		return
	}

	if prefix == "" {
		// Middleware-only, no prefix scoping needed.
		core.Router.Mux.Group(func(sub *routerpkg.Mux) {
			for _, mw := range mws {
				sub.Use(mw)
			}
			spec.Routes(newRouterAdapterFromMux(sub, ""))
		})
		return
	}

	core.Router.Mux.Route(prefix, func(sub *routerpkg.Mux) {
		for _, mw := range mws {
			sub.Use(mw)
		}
		spec.Routes(newRouterAdapterFromMux(sub, ""))
	})
}

// warnModuleReadiness emits at most one boot-time WARN per inert surface a
// module advertises: embedded Migrations (Nucleus is SQL-first and never auto-
// applies them — ADR-006). It changes no behaviour; it only makes the gap loud
// so a real app-builder is not surprised by a silent no-op. Jobs/Webhooks are
// no longer warned about here — since ADR-010 Phase 2 they are executed for
// real (see jobs.go / webhooks.go).
func warnModuleReadiness(core *app.App, spec ModuleSpec) {
	name := spec.Name()

	// Embedded migrations: a non-nil FS that actually contains at least one
	// entry. A read error or an empty/nil FS is treated as "no migrations
	// declared" so we never warn on the common no-op case.
	if fsys := spec.Migrations(); fsys != nil {
		if entries, err := fs.ReadDir(fsys, "."); err == nil && len(entries) > 0 {
			moduleLogger(core).Warn(
				"nucleus: module declares embedded migrations but Nucleus does not auto-apply them (SQL-first); run `nucleus migrate up`",
				"module", name,
			)
		}
	}

}

// moduleLogger returns the framework logger to use for module diagnostics,
// falling back to slog.Default() if the application container has no logger
// configured. core.Logger is non-nil on the success path of app.New (the
// existing service-error logging in Run relies on it), but the fallback keeps
// these defensive boot-time diagnostics panic-free for any future caller.
func moduleLogger(core *app.App) *slog.Logger {
	if core != nil && core.Logger != nil {
		return core.Logger
	}
	return slog.Default()
}

// registerModuleModels catalogues every module's declared Models() in the
// application's model registry. Precondition: the registry is always
// initialised (even under WithoutDefaults). Postcondition: every module model
// is registered before module OnStart runs, so generic CRUD, AutoMigrate
// metadata, and any model-registry consumer can all see it. Per-field display
// metadata is parsed from each model's `admin:` struct tags into the registry;
// the core itself does not consume it — model-registry readers do (e.g. the
// orbit admin module). See ModuleSpec.Models.
func registerModuleModels(core *app.App, specs []ModuleSpec) error {
	for _, spec := range specs {
		for _, m := range spec.Models() {
			if err := core.RegisterModel(m); err != nil {
				return fmt.Errorf("nucleus: module %q RegisterModel %T: %w", spec.Name(), m, err)
			}
		}
	}
	return nil
}

// sortedModuleSpecs returns the modules in deterministic name order.
// Used by the equivalence test and by the startup sequence so route
// registration order is stable across processes.
func sortedModuleSpecs(modules map[string]ModuleSpec) []ModuleSpec {
	if len(modules) == 0 {
		return nil
	}
	names := make([]string, 0, len(modules))
	for n := range modules {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ModuleSpec, 0, len(names))
	for _, n := range names {
		out = append(out, modules[n])
	}
	return out
}

// cloneApp returns a copy of an App where the slices and maps are
// shallow-copied so mutations on the builder after Build do not leak
// into the realised App. Function values, embedded `app.Config`
// scalars, and ServiceRegistration value semantics are preserved.
func cloneApp(a App) App {
	out := a
	if a.Modules != nil {
		out.Modules = make(map[string]ModuleSpec, len(a.Modules))
		for k, v := range a.Modules {
			out.Modules[k] = v
		}
	}
	if a.Middleware != nil {
		out.Middleware = make([]Middleware, len(a.Middleware))
		copy(out.Middleware, a.Middleware)
	}
	if a.Services != nil {
		out.Services = make([]ServiceRegistration, len(a.Services))
		copy(out.Services, a.Services)
	}
	if a.Options != nil {
		out.Options = make([]Option, len(a.Options))
		copy(out.Options, a.Options)
	}
	if a.moduleConfigsRaw != nil {
		out.moduleConfigsRaw = make(map[string]*koanf.Koanf, len(a.moduleConfigsRaw))
		for k, v := range a.moduleConfigsRaw {
			out.moduleConfigsRaw[k] = v
		}
	}
	return out
}
