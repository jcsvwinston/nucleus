package app

import (
	"context"
)

// Extension is the interface that subsystems implement to register themselves
// with the App container during initialization. Extensions are attached after
// the core components (config, logger, router, DB, sessions, models) are
// initialized but before the HTTP server starts.
//
// The lifecycle is:
//   1. app.New(cfg) initializes core components.
//   2. Each Extension.Attach(a) is called in registration order.
//   3. app.Run(ctx) starts the HTTP server.
//   4. On shutdown, Extension.Shutdown(ctx) is called in reverse order.
type Extension interface {
	// Name returns a human-readable identifier for this extension (e.g. "admin", "storage").
	Name() string

	// Attach initializes the extension and wires it into the App container.
	// It receives the fully constructed core App (config, logger, router, DB, models, session).
	// Extensions may mount HTTP routes, register middleware, or set fields on App.
	Attach(a *App) error

	// Shutdown releases resources held by this extension.
	// Called in reverse registration order during App.Shutdown.
	Shutdown(ctx context.Context) error
}

// Option configures the App during construction via app.New(cfg, opts...).
type Option func(*appOptions)

// appOptions holds the optional configuration for App construction.
type appOptions struct {
	extensions   []Extension
	skipDefaults bool
	openAuthz    bool
}

// WithExtensions registers one or more extensions to be attached during app.New().
//
// Example:
//
//   a, err := app.New(cfg,
//       app.WithExtensions(
//           admin.Extension(),
//           storage.Extension(storageCfg),
//       ),
//   )
func WithExtensions(exts ...Extension) Option {
	return func(o *appOptions) {
		o.extensions = append(o.extensions, exts...)
	}
}

// WithoutDefaults disables automatic initialization of the default extensions
// (admin, storage, mail, authz). When used, only the core components are
// initialized and the caller must explicitly register desired extensions
// via WithExtensions.
//
// This is useful for lightweight API services that don't need the admin panel,
// file storage, or RBAC enforcement.
func WithoutDefaults() Option {
	return func(o *appOptions) {
		o.skipDefaults = true
	}
}

// WithOpenAuthz disables the default-deny RBAC middleware mounted by
// App.New (see ADR-004). Use only for early development, internal
// tooling, or demos where unauthenticated access is acceptable. The
// option emits a startup WARN log so the choice is visible in
// operational telemetry. There is no `Config.OpenAuthz` config key on
// purpose — opting out requires touching code and surfaces in PR
// review.
func WithOpenAuthz() Option {
	return func(o *appOptions) {
		o.openAuthz = true
	}
}
