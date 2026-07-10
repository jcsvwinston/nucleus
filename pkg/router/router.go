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
// Access-Control-Allow-Credentials: true. It defaults to false (SEC-1,
// security-by-default): credentialed cross-origin responses are only emitted
// when the app explicitly opts in with WithCORSCredentials(true). Because the
// Fetch standard forbids combining credentials with the `*` wildcard — and
// reflecting every Origin with credentials is itself unsafe — credentials must
// be paired with an explicit origin allow-list (WithCORSOrigins). Enabling
// credentials without an allow-list is a misconfiguration.
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
		// Security-by-default (SPEC §2.4), completed at v1.0.0 per ADR-013
		// R4 / DEP-2026-007: an unconfigured router DENIES cross-origin
		// requests (no CORS headers are emitted). Opt in with an explicit
		// allow-list — WithCORSOrigins("https://app.example") — or restore
		// the historical allow-all with WithCORSOrigins() / a literal "*".
		corsAllowAll: false,
		// Security-by-default (SEC-1): never emit Access-Control-Allow-Credentials
		// unless the app explicitly opts in via WithCORSCredentials(true) paired
		// with an explicit origin allow-list. Reflecting every Origin WITH
		// credentials lets any site read authenticated cross-origin responses
		// (under SameSite=None cookies). SPEC §2.4.
		corsAllowCredentials: false,
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
