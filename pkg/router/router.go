// Package router provides an HTTP router for the GoFrame framework, built on
// top of chi/v5. It includes a default middleware stack, response helpers,
// CSRF protection, and request binding with validation.
package router

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
)

// Router wraps a chi.Router with GoFrame conventions and a default middleware stack.
type Router struct {
	chi.Router
	logger *slog.Logger
}

// Option configures a Router during creation.
type Option func(*routerOpts)

type routerOpts struct {
	enableCSRF     bool
	csrfExempt     []string
	corsAllowAll   bool
	corsOrigins    []string
	timeoutSeconds int
}

// WithCSRF enables CSRF protection middleware.
func WithCSRF(exemptPaths ...string) Option {
	return func(o *routerOpts) {
		o.enableCSRF = true
		o.csrfExempt = exemptPaths
	}
}

// WithCORSOrigins sets allowed CORS origins. An empty list allows all origins.
func WithCORSOrigins(origins ...string) Option {
	return func(o *routerOpts) {
		o.corsOrigins = origins
		o.corsAllowAll = len(origins) == 0
	}
}

// WithTimeout sets the request timeout in seconds.
func WithTimeout(seconds int) Option {
	return func(o *routerOpts) {
		o.timeoutSeconds = seconds
	}
}

// New creates a Router with the default middleware stack already applied.
func New(logger *slog.Logger, opts ...Option) *Router {
	o := &routerOpts{
		corsAllowAll:   true,
		timeoutSeconds: 30,
	}
	for _, fn := range opts {
		fn(o)
	}

	mux := chi.NewRouter()

	// Apply default middleware stack
	for _, mw := range DefaultStack(logger, o) {
		mux.Use(mw)
	}

	return &Router{
		Router: mux,
		logger: logger,
	}
}
