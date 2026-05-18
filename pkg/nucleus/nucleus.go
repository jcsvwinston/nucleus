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
// Configuration loading lands progressively: Phase 2a (this state)
// ships `FromConfigFile` against a single YAML file with the 1 MiB
// size cap (MaxConfigFileBytes), strict-unknown-fields schema
// validation, and did-you-mean hints for likely typos. Multi-file
// merge with the `_append` / `_remove` suffix operators is the
// Phase 2b deliverable — passing more than one path today returns a
// targeted error referencing that phase. TOML / JSON parsers and
// the deeper semantic / referential / module-specific validator
// layers follow in subsequent Phase 2 sub-iterations.
package nucleus

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/jcsvwinston/nucleus/pkg/app"
	routerpkg "github.com/jcsvwinston/nucleus/pkg/router"
)

// Option is the configuration-time option type accepted by `Run` and
// stored in `App.Options`. It is a re-export of `app.Option` so callers
// can pass `nucleus.WithoutDefaults()` / `nucleus.WithExtensions(...)`
// without taking an explicit dependency on `pkg/app`.
type Option = app.Option

// Extension is a re-export of `app.Extension`, the interface every
// production subsystem (admin, storage, custom auth, …) implements to
// register itself with the application container. Pass values via
// `nucleus.WithExtensions(...)`.
type Extension = app.Extension

// WithoutDefaults disables the framework's default extensions (admin,
// storage, mail, authz). Mirrors `app.WithoutDefaults`. Use for
// lightweight services that compose their own extension set.
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
}

// AppBuilder is the fluent surface returned by `New()`. Methods on
// `AppBuilder` are non-destructive against the caller and idempotent:
// `Use`, `Mount`, `WithoutDefaults`, and `WithExtensions` append to
// the underlying slices; `FromConfigFile` records intent. Errors
// accumulated during chaining (a duplicate module name, a malformed
// config file, …) are surfaced when the builder is realised via
// `Build`, `Start`, or `Serve`.
type AppBuilder struct {
	a                App
	err              error
	configStrict     bool // ADR-010 §3 — reject mixed-format file lists in FromConfigFile.
	configFileLoaded bool // set after FromConfigFile succeeds; gates misordered WithConfigStrict.
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
	cfg, err := loadFromFiles(paths, configLoadOptions{strict: b.configStrict})
	if err != nil {
		b.err = err
		return b
	}
	// Preserve fluent-chain Modules/Middleware/Services/etc. that the
	// caller registered before FromConfigFile — only the embedded
	// app.Config slot is replaced.
	b.a.Config = *cfg
	b.configFileLoaded = true
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
// Phase 1 startup sequence:
//
//  1. Construct `*app.App` via `app.New(&a.Config, a.Options...)`.
//  2. Apply `a.Middleware` globally to the application router.
//  3. For each module: route its `spec.Routes(Router)` under
//     `spec.Prefix()`, applying per-module middleware first, then
//     invoke shape-only `spec.Jobs(nil)` / `spec.Webhooks(nil)`.
//  4. Register module `OnShutdown` hooks with the application.
//  5. Run app-level `Lifecycle.OnStart`.
//  6. Spawn each `ServiceRegistration` Run in a goroutine; the
//     framework cancels their context at shutdown.
//  7. Block on `app.App.Run`.
//  8. After Run returns: cancel services, run app-level
//     `Lifecycle.OnShutdown`.
//
// The five-layer config validator (Phase 2), `/_/config` endpoint
// (Phase 3) and reference-application integrations (Phase 4) layer on
// top of this minimal core in subsequent iterations.
func Run(a App) error {
	cfg := a.Config
	core, err := app.New(&cfg, a.Options...)
	if err != nil {
		return fmt.Errorf("nucleus: app.New: %w", err)
	}

	if core.Router != nil && len(a.Middleware) > 0 {
		core.Router.Use(a.Middleware...)
	}

	// Module mount: per-module middleware, then routes. Module names
	// are sorted once to give a deterministic registration order
	// across runs — important for the equivalence test and for
	// predictable startup logs. The sorted slice is reused for every
	// subsequent module-iteration so the ordering rationale is
	// declared in one place.
	sortedSpecs := sortedModuleSpecs(a.Modules)

	if core.Router != nil {
		for _, spec := range sortedSpecs {
			mountModule(core, spec)
		}
	}

	for _, spec := range sortedSpecs {
		s := spec
		core.OnShutdown(func(ctx context.Context) error {
			return s.OnShutdown(ctx, &a)
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if a.Lifecycle.OnStart != nil {
		if err := a.Lifecycle.OnStart(ctx); err != nil {
			return fmt.Errorf("nucleus: Lifecycle.OnStart: %w", err)
		}
	}

	for _, spec := range sortedSpecs {
		if err := spec.OnStart(ctx, &a); err != nil {
			return fmt.Errorf("nucleus: module %q OnStart: %w", spec.Name(), err)
		}
	}

	servicesCtx, cancelServices := context.WithCancel(ctx)
	var wg sync.WaitGroup
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

	if a.Lifecycle.OnShutdown != nil {
		shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
		defer shutdownCancel()
		if err := a.Lifecycle.OnShutdown(shutdownCtx); err != nil && runErr == nil {
			runErr = fmt.Errorf("nucleus: Lifecycle.OnShutdown: %w", err)
		}
	}

	return runErr
}

// mountModule registers a module's routes (and shape-only jobs /
// webhooks) on the application router. Per-module middleware is
// scoped to the module's prefix via the underlying Mux's `Route`
// helper so it does not leak into sibling modules.
func mountModule(core *app.App, spec ModuleSpec) {
	prefix := spec.Prefix()
	mws := spec.Middleware()

	if prefix == "" && len(mws) == 0 {
		spec.Routes(newRouterAdapter(core.Router, ""))
		spec.Jobs(nil)
		spec.Webhooks(nil)
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
		spec.Jobs(nil)
		spec.Webhooks(nil)
		return
	}

	core.Router.Mux.Route(prefix, func(sub *routerpkg.Mux) {
		for _, mw := range mws {
			sub.Use(mw)
		}
		spec.Routes(newRouterAdapterFromMux(sub, ""))
	})
	spec.Jobs(nil)
	spec.Webhooks(nil)
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
	return out
}
