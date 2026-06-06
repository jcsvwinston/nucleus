// Package router provides an HTTP router for the Nucleus framework, built on
// top of Go's standard net/http.ServeMux (Go 1.22+). It includes a default
// middleware stack, response helpers, CSRF protection, and request binding
// with validation.
package router

import (
	"log/slog"
	"time"
)

// Router wraps a Mux with Nucleus conventions and a default middleware stack.
type Router struct {
	*Mux
	logger *slog.Logger
}

// Option configures a Router during creation.
type Option func(*routerOpts)

type routerOpts struct {
	enableCSRF           bool
	csrfExempt           []string
	corsAllowAll         bool
	corsOrigins          []string
	corsAllowCredentials bool
	timeoutSeconds       int
	rateLimitReqs        int
	rateLimitWin         time.Duration
	rateLimitBurst       int
	rateLimitRoute       bool
	rateLimitRole        bool
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

// WithCORSCredentials controls whether the CORS middleware emits
// Access-Control-Allow-Credentials: true. It defaults to true to preserve the
// historical behavior; pass false to forbid credentialed cross-origin
// requests. Per the Fetch standard, credentials are incompatible with the `*`
// wildcard, so callers that disable the origin allow-list (WithCORSOrigins
// with no arguments, or never calling it) should pair an explicit allow-list
// with credentials.
func WithCORSCredentials(allow bool) Option {
	return func(o *routerOpts) {
		o.corsAllowCredentials = allow
	}
}

// WithTimeout sets the request timeout in seconds.
func WithTimeout(seconds int) Option {
	return func(o *routerOpts) {
		o.timeoutSeconds = seconds
	}
}

// WithRateLimit enables in-process request rate limiting.
// Requests <= 0 disables the limiter.
func WithRateLimit(requests int, window time.Duration) Option {
	return func(o *routerOpts) {
		o.rateLimitReqs = requests
		o.rateLimitWin = window
	}
}

// RateLimitPolicy describes advanced limiter dimensions.
type RateLimitPolicy struct {
	Requests int
	Window   time.Duration
	Burst    int
	ByRoute  bool
	ByRole   bool
}

// WithRateLimitPolicy enables in-process request rate limiting with optional
// burst, route-level, and role-level dimensions.
func WithRateLimitPolicy(policy RateLimitPolicy) Option {
	return func(o *routerOpts) {
		o.rateLimitReqs = policy.Requests
		o.rateLimitWin = policy.Window
		o.rateLimitBurst = policy.Burst
		o.rateLimitRoute = policy.ByRoute
		o.rateLimitRole = policy.ByRole
	}
}

// New creates a Router with the default middleware stack already applied.
func New(logger *slog.Logger, opts ...Option) *Router {
	o := &routerOpts{
		corsAllowAll:         true,
		corsAllowCredentials: true,
		timeoutSeconds:       30,
	}
	for _, fn := range opts {
		fn(o)
	}

	mux := NewMux()

	// Apply default middleware stack
	for _, mw := range DefaultStack(logger, o) {
		mux.Use(mw)
	}

	return &Router{
		Mux:    mux,
		logger: logger,
	}
}
