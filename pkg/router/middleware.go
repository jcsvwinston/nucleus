package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// DefaultStack returns the standard middleware chain for Nucleus applications.
func DefaultStack(logger *slog.Logger, opts *routerOpts) []func(http.Handler) http.Handler {
	stack := []func(http.Handler) http.Handler{
		RequestID,
		// CORS sits right after RequestID so every response that leaves the
		// stack — a 429 from the limiter, a 503 from the timeout, a 500 from
		// the recoverer — carries the CORS headers a browser needs to hand
		// the error to the page instead of reporting a blank network
		// failure (NU-38). A preflight is answered here, before telemetry
		// and the limiter, which is what a preflight is for.
		corsMiddleware(opts),
		realIPMiddleware(newTrustedProxyMatcher(opts.trustedProxies)),
		TelemetryMiddleware,
		// The limiter here keys by client IP: a standalone router has no
		// identity middleware. pkg/app does not configure it through the
		// options and mounts RateLimitFromPolicy after the bearer is
		// decoded instead, so its keys carry user and tenant (NU-4).
		rateLimitMiddleware(opts),
		RequestLogger(logger),
		RecovererWithLogger(logger),
		TimeoutMiddlewareWithExemptions(time.Duration(opts.timeoutSeconds)*time.Second, opts.timeoutExempt),
		Compress(5),
		securityHeaders(opts.hsts),
	}

	if opts.enableCSRF {
		// Plumb the router's logger into the CSRF middleware so encrypt
		// failures and stale-token decrypts surface in the same handler
		// (redaction, attributes, sink) as the rest of the app. See
		// ADR-008.
		stack = append(stack, CSRFMiddleware(CSRFOptions{
			ExemptPaths:       opts.csrfExempt,
			EnableOriginCheck: true, // Enable Laravel-style origin verification by default
			InsecureCookie:    opts.csrfInsecureCookie,
			Logger:            logger,
		}))
	}

	return stack
}

func rateLimitMiddleware(opts *routerOpts) func(http.Handler) http.Handler {
	if opts == nil || opts.rateLimitReqs <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return RateLimitFromPolicy(RateLimitPolicy{
		Requests: opts.rateLimitReqs,
		Window:   opts.rateLimitWin,
		Burst:    opts.rateLimitBurst,
		ByRoute:  opts.rateLimitRoute,
		ByRole:   opts.rateLimitRole,
	})
}

// RateLimitFromPolicy builds the request limiter DefaultStack would mount
// for policy, with the default key (tenant and user from the context, else
// client IP) and the default route and role dimensions. It exists so an
// application can mount the limiter AFTER its identity middleware: the key
// reads the user id, the claims and the tenant from the request context, and
// a limiter mounted before the bearer is decoded only ever sees an IP. A
// policy with Requests <= 0 yields a pass-through.
func RateLimitFromPolicy(policy RateLimitPolicy) func(http.Handler) http.Handler {
	if policy.Requests <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	window := policy.Window
	if window <= 0 {
		window = time.Minute
	}
	return RateLimitMiddleware(RateLimitOptions{
		Requests:       policy.Requests,
		Window:         window,
		Burst:          policy.Burst,
		ScopeByRoute:   policy.ByRoute,
		ScopeByRole:    policy.ByRole,
		RouteDimension: defaultRouteDimensionFromRequest,
		RoleDimension:  rateLimitRoleFromRequest,
	})
}

// RequestLogger returns middleware that logs each HTTP request with slog.
// It records method, path, status, duration, request_id, remote_addr, and user_agent.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Inject request ID into context for downstream use
			reqID := GetReqID(r.Context())
			ctx := observe.CtxWithRequestID(r.Context(), reqID)
			r = r.WithContext(ctx)

			ww := NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration_ms", float64(time.Since(start).Nanoseconds())/1e6,
				"request_id", reqID,
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
				"bytes_written", ww.BytesWritten(),
			)
		})
	}
}

// hstsHeaderValue is the Strict-Transport-Security policy Nucleus emits: one
// year, covering subdomains. It is sent only over TLS (or when HSTS is forced
// for production) so a plain-HTTP dev run is never pinned to HTTPS.
const hstsHeaderValue = "max-age=31536000; includeSubDomains"

// SecurityHeaders sets standard security headers on every response. HSTS is
// emitted when the request arrives over a direct TLS connection. Behind a
// TLS-terminating proxy (r.TLS == nil) use the router's WithHSTS option / an
// `env: production` app so the header is still sent.
func SecurityHeaders(next http.Handler) http.Handler {
	return securityHeaders(false)(next)
}

// securityHeaders returns the security-headers middleware. When alwaysHSTS is
// true, Strict-Transport-Security is emitted regardless of r.TLS (production
// behind a proxy); otherwise it is emitted only over a direct TLS connection.
func securityHeaders(alwaysHSTS bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "0")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// style-src keeps 'unsafe-inline': server-rendered app templates
			// routinely carry style="" attributes and framework-agnostic nonce
			// plumbing does not exist yet. script-src has no such allowance.
			w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self' data:")
			// Opt out of the powerful-feature APIs a server-rendered app has
			// no business using by default; apps that need one override the
			// header downstream (handlers run after this middleware sets it).
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			// Cut cross-origin window handles (Spectre-class isolation) and
			// block other origins from embedding responses as subresources.
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
			if alwaysHSTS || r.TLS != nil {
				w.Header().Set("Strict-Transport-Security", hstsHeaderValue)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func corsMiddleware(opts *routerOpts) func(http.Handler) http.Handler {
	allowedOrigins := opts.corsOrigins
	// corsAllowAll is only set by an explicit WithCORSOrigins() call with no
	// arguments ("empty list allows all origins", the documented semantics).
	// An unconfigured router leaves it false, and an empty allow-list reaches
	// CORSMiddleware as-is — which emits no CORS headers: deny cross-origin,
	// the v1.0.0 default (ADR-013 R4 / DEP-2026-007).
	if opts.corsAllowAll {
		allowedOrigins = []string{"*"}
	}

	return CORSMiddleware(CORSOptions{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: opts.corsAllowCredentials,
		MaxAge:           300,
	})
}
