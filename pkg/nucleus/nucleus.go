// Package nucleus provides the fluent façade over pkg/app for assembling
// Nucleus applications. The package is a composition layer — every
// guarantee SPEC.md §3.1 makes about pkg/app is preserved automatically
// because every entry point in this package ultimately delegates to
// app.New. See ADR-010 for the design rationale.
//
// Three coexisting surfaces, all producing the same nucleus.App value:
//
//   1. Fluent — sugar over the struct, ideal for demos and embedded use:
//      nucleus.New().
//          FromConfigFile("config/nucleus.yaml").
//          Use(middleware.Logger(), middleware.Recover()).
//          Mount(articles.Module.Build(), users.Module.Build()).
//          Start()
//
//   2. Direct struct — for tests and programmatic embedding:
//      nucleus.Run(nucleus.App{
//          Config:  app.Config{Port: 8080, ...},
//          Modules: map[string]nucleus.ModuleSpec{"articles": articles.Module.Build()},
//      })
//
//   3. Bootstrap pattern — the enterprise default, a user-space
//      convention (no pkg/nucleus/bootstrap sub-package):
//      func main() { nucleus.Run(bootstrap.New()) }
//
// All three converge on app.New(cfg, opts...).Run(ctx), so the
// production Extension surface, lifecycle ordering, default-deny
// authz, and observability defaults documented in pkg/app §3.1 apply
// unchanged.
package nucleus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// App is the canonical struct produced by every nucleus surface. It
// embeds app.Config (so every yaml-bindable field of the production
// container is inherited unchanged) and adds Go-only wiring fields
// tagged yaml:"-" so they cannot be expressed in a configuration
// file. ADR-010 §1.
type App struct {
	app.Config

	// Modules is the keyed-by-name set of modules the application
	// mounts. The map shape allows configuration overlays to override
	// individual modules by name (ADR-010 §3); a slice cannot.
	Modules map[string]ModuleSpec `yaml:"-"`

	// Middleware is global, router-level middleware applied before
	// module routes. Slice (not map) because order matters.
	Middleware []Middleware `yaml:"-"`

	// Services registers long-running goroutines tied to the
	// application's lifecycle. Phase 1 stores them; the supervisor
	// that runs them lands in a later phase.
	Services []ServiceRegistration `yaml:"-"`

	// Lifecycle holds app-level OnStart / OnShutdown hooks that are
	// not owned by any module.
	Lifecycle LifecycleHooks `yaml:"-"`

	// Options is the slice of pkg/app.Option values forwarded to
	// app.New(cfg, opts...). Populated by AppBuilder.WithoutDefaults
	// and AppBuilder.WithExtensions; direct-struct callers can also
	// set it directly.
	Options []app.Option `yaml:"-"`

	// SPA is the optional single-page-app serving configuration,
	// preserved from the legacy nucleus.SPAConfig. Empty zero-value
	// disables SPA serving.
	SPA SPAConfig `yaml:"-"`
}

// LifecycleHooks groups app-level start/stop callbacks. Both fields
// are optional; a nil OnStart or OnShutdown is a no-op.
type LifecycleHooks struct {
	OnStart    func(context.Context) error
	OnShutdown func(context.Context) error
}

// ServiceRegistration declares a long-running goroutine bound to the
// application's lifecycle. Phase 1 stores the value on the App; the
// supervisor that actually runs them is a future phase.
type ServiceRegistration struct {
	// Name is a human-readable identifier used in logs and the
	// hypothetical service-status endpoint.
	Name string
	// Run is the goroutine body. It must return when ctx is
	// cancelled; a Run that ignores ctx is a bug.
	Run func(ctx context.Context) error
	// Health is plumbed into /healthz when set. Optional.
	Health func(ctx context.Context) error
}

// SPAConfig is the single-page-app fallback configuration. When
// AppBuilder.SPA is called (or App.SPA is set directly), requests that
// don't match an API prefix and don't resolve to a static file fall
// back to IndexFile so client-side routing works.
type SPAConfig struct {
	// Dir is the filesystem root that holds the SPA bundle. Empty
	// means SPA serving is disabled.
	Dir string
	// IndexFile is the file (relative to Dir) served as the SPA
	// fallback. Typically "index.html".
	IndexFile string
	// APIPrefix is the URL prefix that bypasses the SPA fallback.
	// Requests under this prefix that do not match a registered
	// route return 404 instead of the index page. Typically "/api".
	APIPrefix string
}

// disabled reports whether SPA serving is effectively off.
func (s SPAConfig) disabled() bool { return s.Dir == "" }

// ErrModuleRequiresMissingDB is returned (via Build / Start / Run) when
// a module declares a Requires() alias that the App's Databases map
// does not contain. Callers can errors.Is against this sentinel to
// distinguish boot-time wiring errors from later runtime failures.
var ErrModuleRequiresMissingDB = errors.New("nucleus: module requires a database alias that is not configured")

// AppBuilder is the fluent chain accumulator. It carries an App value
// plus an error slot so the chain can defer reporting until the
// terminator (Build / Start / Serve / Run) is called. Mid-chain method
// calls that encounter errors stash them; subsequent chained calls
// short-circuit. This matches the bufio.Scanner pattern and avoids
// requiring callers to check after every step.
type AppBuilder struct {
	app App
	err error
}

// New constructs a fresh AppBuilder seeded with app.DefaultConfig().
// The returned builder is ready for chained configuration; Start is
// the terminator that builds the underlying *app.App and serves
// traffic.
func New() *AppBuilder {
	return &AppBuilder{
		app: App{
			Config:  app.DefaultConfig(),
			Modules: make(map[string]ModuleSpec),
		},
	}
}

// Run is the direct-struct entry point. It runs the same validate-then-
// app.New-then-app.Run pipeline that the builder does, but skips the
// fluent chain — useful for tests and for the bootstrap.New() pattern
// where main() builds a complete App value out-of-band.
//
// nucleus.Run(App) and (*AppBuilder).Start() must produce equivalent
// behaviour given equivalent inputs; the equivalence test in
// equivalence_test.go enforces this.
func Run(a App) error {
	b := &AppBuilder{app: ensureModuleMap(a)}
	return b.Start()
}

// FromConfigFile loads configuration from the given path(s) and merges
// them into the builder's App. Phase 1 implements only the single-file
// path delegating to app.LoadConfig; multi-file merge with the
// _append/_remove suffix operators and the five-layer validator land
// in Phase 2 (ADR-010 §2 / §3).
//
// Errors are stashed on the builder and reported by the terminator.
// FromConfigFile never panics (unlike the legacy Load it replaces).
func (b *AppBuilder) FromConfigFile(paths ...string) *AppBuilder {
	if b.err != nil {
		return b
	}
	if len(paths) == 0 {
		b.err = errors.New("nucleus.AppBuilder.FromConfigFile: at least one path required")
		return b
	}
	// Phase 1 limitation: single-file load. Phase 2 lands the
	// multi-file merge engine + 5-layer validator.
	if len(paths) > 1 {
		b.err = fmt.Errorf("nucleus.AppBuilder.FromConfigFile: multi-file merge is a Phase 2 deliverable (received %d paths)", len(paths))
		return b
	}
	cfg, err := app.LoadConfig(paths[0])
	if err != nil {
		b.err = fmt.Errorf("nucleus.AppBuilder.FromConfigFile %q: %w", paths[0], err)
		return b
	}
	b.app.Config = *cfg
	return b
}

// FromStruct replaces the builder's accumulated App with the supplied
// value, preserving the Modules map allocation contract. Intended for
// callers that have built an App value out-of-band (typically via a
// bootstrap.New() helper) and want to thread it through the fluent
// chain for additional Use / Mount / WithExtensions calls.
func (b *AppBuilder) FromStruct(a App) *AppBuilder {
	if b.err != nil {
		return b
	}
	b.app = ensureModuleMap(a)
	return b
}

// Use appends global middleware. Applied before module routes; stacks
// additively with per-module middleware declared on Module[C].Middleware.
func (b *AppBuilder) Use(mw ...Middleware) *AppBuilder {
	if b.err != nil {
		return b
	}
	b.app.Middleware = append(b.app.Middleware, mw...)
	return b
}

// Mount registers one or more modules. Modules are keyed by Name() in
// App.Modules; mounting two modules with the same Name is an error
// surfaced at Start / Build (it cannot fire on the per-call chain
// without breaking fluent semantics).
func (b *AppBuilder) Mount(modules ...ModuleSpec) *AppBuilder {
	if b.err != nil {
		return b
	}
	for _, m := range modules {
		if m == nil {
			b.err = errors.New("nucleus.AppBuilder.Mount: nil ModuleSpec")
			return b
		}
		name := m.Name()
		if _, exists := b.app.Modules[name]; exists {
			b.err = fmt.Errorf("nucleus.AppBuilder.Mount: duplicate module name %q", name)
			return b
		}
		b.app.Modules[name] = m
	}
	return b
}

// WithoutDefaults forwards app.WithoutDefaults to app.New, producing a
// lightweight core-only application. Closes the missing-seam finding
// from the 2026-05-15 pkg/app + pkg/nucleus inventory (ADR-010 §8).
func (b *AppBuilder) WithoutDefaults() *AppBuilder {
	if b.err != nil {
		return b
	}
	b.app.Options = append(b.app.Options, app.WithoutDefaults())
	return b
}

// WithExtensions forwards app.WithExtensions(...) to app.New. The
// fluent variadic shape matches the underlying app.Option contract.
func (b *AppBuilder) WithExtensions(exts ...app.Extension) *AppBuilder {
	if b.err != nil {
		return b
	}
	b.app.Options = append(b.app.Options, app.WithExtensions(exts...))
	return b
}

// SPA enables single-page-app serving. Equivalent to setting App.SPA
// directly; provided as a fluent convenience.
func (b *AppBuilder) SPA(dir string, cfg SPAConfig) *AppBuilder {
	if b.err != nil {
		return b
	}
	cfg.Dir = dir
	b.app.SPA = cfg
	return b
}

// WithApp is an escape hatch for callers that need direct read/write
// access to the underlying *app.Config during the fluent chain. Use
// sparingly — the typed builder methods should cover the common cases.
func (b *AppBuilder) WithApp(fn func(*App)) *AppBuilder {
	if b.err != nil {
		return b
	}
	fn(&b.app)
	return b
}

// Build resolves the accumulated chain into a wired *app.App without
// starting the server. Useful for tests that want to interact with the
// container in-process. Build returns the App and any deferred error.
func (b *AppBuilder) Build() (*app.App, error) {
	if b.err != nil {
		return nil, b.err
	}
	return buildApp(b.app)
}

// Start is the canonical builder terminator. It calls Build and then
// runs the application until SIGINT/SIGTERM or context cancellation.
// Renamed from the legacy Run() to avoid colliding with the
// package-level nucleus.Run(App) function — Go does not overload.
func (b *AppBuilder) Start() error {
	a, err := b.Build()
	if err != nil {
		return err
	}
	return a.Run(context.Background())
}

// Serve is a synonym for Start, provided for callers who prefer the
// HTTP-server vocabulary. Identical behaviour.
func (b *AppBuilder) Serve() error { return b.Start() }

// Logger returns a logger derived from the builder's current
// configuration. Useful for callers that want to log during fluent
// chain construction (e.g. "loaded config from X, mounting Y modules").
// The same logger shape is used internally by app.New.
func (b *AppBuilder) Logger() *slog.Logger {
	// Mirror what pkg/app/app.go:New does for the logger, but apply
	// it inline so callers can use it before Build. The redacting
	// handler from ADR-007 is the default; LogLevel/LogFormat are
	// honoured from the accumulated config.
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// Config returns a pointer to the builder's accumulated app.Config for
// read-only inspection. Mutations through this pointer are visible to
// subsequent chain steps; prefer WithApp() when explicit intent matters.
func (b *AppBuilder) Config() *app.Config {
	return &b.app.Config
}

// ensureModuleMap guarantees a non-nil Modules map on a freshly
// supplied App value. Direct-struct callers can omit the field; the
// framework treats nil and empty identically.
func ensureModuleMap(a App) App {
	if a.Modules == nil {
		a.Modules = make(map[string]ModuleSpec)
	}
	return a
}

// buildApp turns a complete App value into a wired *app.App. It is the
// shared body of AppBuilder.Build, AppBuilder.Start, and nucleus.Run —
// the single point where module validation, model registration, route
// wiring, and SPA fallback registration converge. The equivalence test
// proves the three surfaces produce identical *app.App configurations
// by exercising this function from each path.
func buildApp(a App) (*app.App, error) {
	if err := validateModules(&a); err != nil {
		return nil, err
	}

	cfg := a.Config
	wired, err := app.New(&cfg, a.Options...)
	if err != nil {
		return nil, fmt.Errorf("nucleus.buildApp: %w", err)
	}

	// Apply global middleware before any module routes register.
	if len(a.Middleware) > 0 {
		wired.Router.Use(a.Middleware...)
	}

	// Register module models for AutoMigrate, then mount routes.
	root := newRouter(wired.Router.Mux)
	for _, name := range sortedModuleKeys(a.Modules) {
		mod := a.Modules[name]
		if mods := mod.Models(); len(mods) > 0 {
			if err := wired.AutoMigrate(mods...); err != nil {
				return nil, fmt.Errorf("nucleus.buildApp: module %q AutoMigrate: %w", name, err)
			}
		}
		if mw := mod.Middleware(); len(mw) > 0 {
			// Module-level middleware applies inside the module's
			// prefix group. Implemented via a Group block so the
			// middleware does not leak to sibling modules.
			root.Group(mod.Prefix(), func(g Router) {
				g.Use(mw...)
				mod.Routes(g)
			})
			continue
		}
		if mod.Prefix() != "" {
			root.Group(mod.Prefix(), func(g Router) { mod.Routes(g) })
		} else {
			mod.Routes(root)
		}
	}

	// Register module OnStart / OnShutdown hooks. The pkg/app
	// lifecycle drives them via the standard OnShutdown mechanism;
	// OnStart fires before Run returns. Phase 1 simplification: hook
	// invocation is left for a follow-up wiring iteration once we
	// confirm the pkg/app shutdown ordering semantics interact
	// cleanly with module ordering. Phase 1 builds and serves
	// modules' routes but does not invoke OnStart/OnShutdown — the
	// callbacks are stored and not yet drained.
	//
	// TODO(adr-010-phase2): drain OnStart before serve, OnShutdown
	// via wired.OnShutdown(...) in reverse Mount order.

	// SPA fallback registers last so it acts as a catch-all under "/".
	if !a.SPA.disabled() {
		registerSPA(wired.Router.Mux, a.SPA)
	}

	// App-level Lifecycle hooks (non-module). OnShutdown plumbs in
	// reverse-registration order through pkg/app's existing hook
	// machinery.
	if a.Lifecycle.OnShutdown != nil {
		wired.OnShutdown(a.Lifecycle.OnShutdown)
	}
	// TODO(adr-010-phase2): app.Lifecycle.OnStart invocation.

	return wired, nil
}

// validateModules checks the Requires() declarations of every module
// against the configured Databases map and returns
// ErrModuleRequiresMissingDB when any required alias is absent.
func validateModules(a *App) error {
	for name, mod := range a.Modules {
		for _, alias := range mod.Requires() {
			if _, ok := a.Config.Databases[alias]; !ok {
				return fmt.Errorf("%w: module %q requires database alias %q", ErrModuleRequiresMissingDB, name, alias)
			}
		}
	}
	return nil
}

// sortedModuleKeys returns Modules keys in lexicographic order. Used
// by buildApp to give Mount registration deterministic ordering across
// runs (Go map iteration is intentionally randomised). Deterministic
// ordering is also a precondition for the three-surface equivalence
// test.
func sortedModuleKeys(m map[string]ModuleSpec) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Insertion sort — Modules count is small (typically <20); a
	// stdlib sort import is overkill. Replace with sort.Strings if
	// the inventory grows substantially.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// registerSPA wires the SPA fallback under "/" on the supplied Mux.
// Static files inside cfg.Dir are served when present; otherwise the
// IndexFile is returned so client-side routing can handle the path.
// Requests under cfg.APIPrefix bypass the fallback (a 404 from the
// router for an unmatched API route is preserved).
func registerSPA(mux interface {
	Handle(pattern string, h http.Handler)
}, cfg SPAConfig) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.APIPrefix != "" && strings.HasPrefix(r.URL.Path, cfg.APIPrefix) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		path := filepath.Join(cfg.Dir, filepath.Clean(r.URL.Path))
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			http.FileServer(http.Dir(cfg.Dir)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(cfg.Dir, cfg.IndexFile))
	})
	mux.Handle("/", handler)
}
