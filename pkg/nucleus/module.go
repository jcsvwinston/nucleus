package nucleus

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// ModuleSpec is the type-erased interface every module satisfies. It is
// the shape stored in App.Modules and consumed by AppBuilder.Mount and
// nucleus.Run. ModuleSpec exists so that maps of modules can be held
// without a generic type parameter at the storage site; the generic
// Module[C] type produces ModuleSpec values via Build() (ADR-010 §6).
//
// Phase 1 surface: the methods enumerated below. Jobs(...) and
// Webhooks(...) hooks are part of the ADR §6 design but are deferred
// to a later phase pending integration with pkg/tasks. Adding them
// later is additive on ModuleSpec — existing implementations satisfy
// the wider interface trivially via the Build() adapter.
type ModuleSpec interface {
	// Name is the module identifier. Used as the key in App.Modules
	// and as the prefix for module-scoped configuration
	// (modules.<Name>.*).
	Name() string

	// Prefix is the URL prefix the module's routes mount under.
	// Empty string means the module owns root-level paths.
	Prefix() string

	// DefaultDB is the logical database alias the module uses by
	// default for ORM-aware operations. Empty string defaults to
	// "default".
	DefaultDB() string

	// Requires lists logical database aliases the module needs to
	// boot. If any required alias is missing from the application's
	// configured Databases map, the framework returns an error at
	// Build time rather than failing later with a nil pointer.
	Requires() []string

	// Models returns the registered model values for the module.
	// Used by AutoMigrate and by the admin panel (if enabled).
	Models() []any

	// Middleware returns module-scoped middleware that runs before
	// any route registered by Routes(). Stacks additively with
	// app-level middleware.
	Middleware() []Middleware

	// Routes is called once at Mount time with a Router scoped to
	// the module's Prefix. The module registers its routes inside
	// the callback.
	Routes(r Router)

	// Migrations returns the module's migration filesystem. The
	// fs.FS interface (rather than embed.FS) lets modules satisfy
	// the contract with runtime-generated sources — embed.FS
	// already implements fs.FS, so //go:embed directives work
	// unchanged.
	Migrations() fs.FS

	// OnStart is called after the application has finished wiring
	// and just before serving traffic. Errors abort startup.
	OnStart(ctx context.Context, a *app.App) error

	// OnShutdown is called during graceful shutdown, in reverse
	// registration order.
	OnShutdown(ctx context.Context, a *app.App) error

	// Config returns the module's configuration value as `any`.
	// Concretely, the underlying value is a pointer to the typed
	// `C` from Module[C], so the configuration-binding layer can
	// reflectively populate it. Callers should not depend on the
	// nil-ness of the returned value in Phase 1 — Phase 2 (config
	// loading + 5-layer validator) settles that contract.
	Config() any
}

// Module is the generic typed constructor for a module. Users
// instantiate it with their config type and the framework binds
// `modules.<Name>.*` from the configuration files directly into
// `Module[C].Config` without exposing reflection at the call site
// (Phase 2 lands the actual binding logic; Phase 1 just establishes
// the type-safe shape).
//
// Example:
//
//	type ArticlesConfig struct {
//	    DefaultLocale string `yaml:"default_locale" default:"en"`
//	    PageSize      int    `yaml:"page_size"      default:"20"`
//	}
//
//	var Module = nucleus.Module[ArticlesConfig]{
//	    Name:      "articles",
//	    Prefix:    "/articles",
//	    DefaultDB: "default",
//	    Models:    []any{&Article{}, &Comment{}},
//	    Routes: func(r nucleus.Router, cfg ArticlesConfig) {
//	        r.Get("/", List)
//	        r.Get("/{id}", Show)
//	    },
//	}
//
// The host application calls `articles.Module.Build()` and passes the
// resulting ModuleSpec to AppBuilder.Mount.
type Module[C any] struct {
	Name       string
	Prefix     string
	DefaultDB  string
	Requires   []string
	Models     []any
	Middleware []Middleware
	Config     C

	// Routes runs once at Mount time with a Router scoped to Prefix
	// and the module's typed configuration. nil is allowed (the
	// module registers no routes).
	Routes func(r Router, cfg C)

	// Migrations is the fs.FS source for the module's SQL files.
	// Phase 1 stores the value; the Phase 2 / future iteration that
	// integrates Migrator-driven application reads it. nil is
	// allowed (module ships no migrations).
	Migrations fs.FS

	// OnStart is invoked after wiring, before serving traffic. nil
	// is allowed.
	OnStart func(ctx context.Context, a *app.App, cfg C) error

	// OnShutdown is invoked during graceful shutdown in reverse
	// registration order. nil is allowed.
	OnShutdown func(ctx context.Context, a *app.App, cfg C) error
}

// Build produces the type-erased ModuleSpec that AppBuilder.Mount
// accepts. The returned value captures the Module[C] by value, so
// post-Build mutations of the original struct are not observed by the
// framework.
func (m Module[C]) Build() ModuleSpec {
	if m.Name == "" {
		panic(fmt.Sprintf("nucleus.Module[%T].Build: Name is required", m.Config))
	}
	return &moduleAdapter[C]{m: m}
}

// moduleAdapter type-erases Module[C] into ModuleSpec. Stored by
// pointer so Config() can return a stable address (Phase 2's
// configuration-binding layer reflects on the returned `any` and
// writes through it).
type moduleAdapter[C any] struct {
	m Module[C]
}

func (a *moduleAdapter[C]) Name() string         { return a.m.Name }
func (a *moduleAdapter[C]) Prefix() string       { return a.m.Prefix }
func (a *moduleAdapter[C]) DefaultDB() string    { return a.m.DefaultDB }
func (a *moduleAdapter[C]) Requires() []string   { return append([]string(nil), a.m.Requires...) }
func (a *moduleAdapter[C]) Models() []any        { return append([]any(nil), a.m.Models...) }
func (a *moduleAdapter[C]) Middleware() []Middleware {
	return append([]Middleware(nil), a.m.Middleware...)
}
func (a *moduleAdapter[C]) Migrations() fs.FS { return a.m.Migrations }
func (a *moduleAdapter[C]) Config() any       { return &a.m.Config }

func (a *moduleAdapter[C]) Routes(r Router) {
	if a.m.Routes == nil {
		return
	}
	a.m.Routes(r, a.m.Config)
}

func (a *moduleAdapter[C]) OnStart(ctx context.Context, app *app.App) error {
	if a.m.OnStart == nil {
		return nil
	}
	return a.m.OnStart(ctx, app, a.m.Config)
}

func (a *moduleAdapter[C]) OnShutdown(ctx context.Context, app *app.App) error {
	if a.m.OnShutdown == nil {
		return nil
	}
	return a.m.OnShutdown(ctx, app, a.m.Config)
}
