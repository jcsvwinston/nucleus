package nucleus

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/knadh/koanf/v2"
)

// Middleware is a standard net/http middleware function. The framework's
// router applies middleware in registration order; the user-facing
// builder method `AppBuilder.Use(...)` and the per-module `Module[C].Middleware`
// field both accept values of this type.
type Middleware = func(http.Handler) http.Handler

// JobRegistry is the surface a module receives to register background
// jobs. The full implementation (backed by pkg/tasks / Asynq) lands in
// a later phase; this interface is intentionally empty in Phase 1 so
// the module contract is shape-complete without binding to a specific
// job-runtime API yet. Phase 2+ adds concrete Register methods.
type JobRegistry interface{}

// WebhookRegistry is the surface a module receives to register inbound
// webhook handlers. As with JobRegistry, the concrete Register surface
// is deferred to a later phase; Phase 1 establishes the shape only.
type WebhookRegistry interface{}

// ModuleSpec is the type-erased interface every module satisfies. It is
// the shape stored in `App.Modules` and consumed by `AppBuilder.Mount`
// and the framework's startup sequence.
//
// Modules are self-contained units of feature organisation: a module
// brings its own routes, models, migrations, jobs and webhooks, and
// can be lifted into another application by adding it to that
// application's `Mount(...)` list.
//
// Users do not implement `ModuleSpec` directly. They construct a
// `Module[C any]` with a typed configuration and call its `Build()`
// method, which returns a `ModuleSpec` wrapper.
type ModuleSpec interface {
	Name() string
	Prefix() string
	DefaultDB() string
	Requires() []string
	Models() []any
	Middleware() []Middleware
	Routes(r Router)
	Jobs(j JobRegistry)
	Webhooks(w WebhookRegistry)
	Migrations() fs.FS
	// OnStart runs before the module's Routes are registered (ADR-010
	// Phase 4, Gap 2), so a module initialises its managed resources here
	// — typically `rt.DB()` — and its Routes closure can then capture that
	// state directly. The `Runtime` handle replaces the former `*App`
	// config struct so modules reach the framework-managed connection pool
	// instead of opening their own.
	OnStart(ctx context.Context, rt Runtime) error
	OnShutdown(ctx context.Context, rt Runtime) error
	Config() any
}

// Module is the generic constructor for typed module configs. Users
// instantiate it with their config type. The framework binds
// `modules.<Name>.*` into the `Config` field during configuration load
// (Phase 2 — the validator landing point); Phase 1 establishes the
// shape so module authors can adopt the generic surface today.
//
// Call `Build()` to obtain the type-erased `ModuleSpec` that
// `AppBuilder.Mount` and `nucleus.App.Modules` expect.
type Module[C any] struct {
	Name       string
	Prefix     string
	DefaultDB  string
	Requires   []string
	Models     []any
	Middleware []Middleware
	// Config is the module's typed configuration. At Run time (ADR-010 §2
	// layer 5) the framework binds the `modules.<Name>.*` config subtree into
	// it, fills still-zero fields from `default:` struct tags, and validates it
	// against `validate:` tags. Precedence is: a value set here (the
	// programmatic baseline) < the config file < `default:` tags fill only what
	// remains zero. Because defaulting keys off the zero value, a field
	// deliberately left at its zero value cannot be distinguished from "unset"
	// and will receive its `default:` tag value if it has one.
	Config     C
	Routes     func(r Router, cfg C)
	Jobs       func(j JobRegistry, cfg C)
	Webhooks   func(w WebhookRegistry, cfg C)
	Migrations fs.FS
	OnStart    func(ctx context.Context, rt Runtime, cfg C) error
	OnShutdown func(ctx context.Context, rt Runtime, cfg C) error
}

// Build returns the type-erased `ModuleSpec` for this `Module[C]`,
// suitable for storage in `App.Modules` and `AppBuilder.Mount(...)`.
// The returned spec captures the module's typed `Config` by value so
// modifications to the Module after Build do not leak into the spec.
func (m Module[C]) Build() ModuleSpec {
	return moduleSpec[C]{m: m}
}

// moduleSpec is the unexported type-erased wrapper produced by
// `Module[C].Build()`. Function callbacks are invoked with the
// captured typed config so module authors keep compile-time type
// safety; only the framework's internal storage works through the
// `ModuleSpec` interface.
type moduleSpec[C any] struct {
	m Module[C]
}

func (s moduleSpec[C]) Name() string       { return s.m.Name }
func (s moduleSpec[C]) Prefix() string     { return s.m.Prefix }
func (s moduleSpec[C]) DefaultDB() string  { return s.m.DefaultDB }
func (s moduleSpec[C]) Requires() []string { return s.m.Requires }
func (s moduleSpec[C]) Models() []any      { return s.m.Models }
func (s moduleSpec[C]) Middleware() []Middleware {
	return s.m.Middleware
}
func (s moduleSpec[C]) Routes(r Router) {
	if s.m.Routes == nil {
		return
	}
	s.m.Routes(r, s.m.Config)
}
func (s moduleSpec[C]) Jobs(j JobRegistry) {
	if s.m.Jobs == nil {
		return
	}
	s.m.Jobs(j, s.m.Config)
}
func (s moduleSpec[C]) Webhooks(w WebhookRegistry) {
	if s.m.Webhooks == nil {
		return
	}
	s.m.Webhooks(w, s.m.Config)
}
func (s moduleSpec[C]) Migrations() fs.FS { return s.m.Migrations }
func (s moduleSpec[C]) OnStart(ctx context.Context, rt Runtime) error {
	if s.m.OnStart == nil {
		return nil
	}
	return s.m.OnStart(ctx, rt, s.m.Config)
}
func (s moduleSpec[C]) OnShutdown(ctx context.Context, rt Runtime) error {
	if s.m.OnShutdown == nil {
		return nil
	}
	return s.m.OnShutdown(ctx, rt, s.m.Config)
}
func (s moduleSpec[C]) Config() any { return s.m.Config }

// moduleConfigBinder is the unexported capability the framework type-asserts on
// a ModuleSpec to bind its typed config at Run time (ADR-010 §2 layer 5). Only
// the framework's own moduleSpec[C] wrapper implements it — users construct
// modules via Module[C].Build(), never by implementing ModuleSpec directly — so
// bindModuleConfigs can fall back gracefully for any foreign implementation.
type moduleConfigBinder interface {
	bindConfig(raw *koanf.Koanf) (ModuleSpec, error)
}

// bindConfig produces a new ModuleSpec whose typed Config has been bound for
// ADR-010 §2 layer 5. Starting from the author-supplied Config (the
// programmatic baseline), it overlays the module's `modules.<name>.*` file
// subtree (when present), fills still-zero fields from `default:` struct tags,
// then validates the result against its `validate:` tags. The Config is a value
// field, so binding returns a fresh moduleSpec[C] rather than mutating in place;
// the caller swaps it into App.Modules before Routes/OnStart run.
//
// A nil raw (the direct-struct Run surface, where there is no config file) skips
// the file overlay but still applies defaults and validation, mirroring how
// layers 3 and 4 run on both the FromConfigFile and direct-struct paths.
func (s moduleSpec[C]) bindConfig(raw *koanf.Koanf) (ModuleSpec, error) {
	cfg := s.m.Config
	if raw != nil {
		if err := raw.Unmarshal("", &cfg); err != nil {
			return nil, fmt.Errorf("%w: module %q: binding modules.%s.*: %w", ErrInvalidModuleConfig, s.m.Name, s.m.Name, err)
		}
	}
	if err := applyDefaults(&cfg); err != nil {
		return nil, fmt.Errorf("%w: module %q: applying defaults: %w", ErrInvalidModuleConfig, s.m.Name, err)
	}
	if err := validateModuleConfigValue(s.m.Name, cfg); err != nil {
		return nil, err
	}
	bound := s.m
	bound.Config = cfg
	return moduleSpec[C]{m: bound}, nil
}
