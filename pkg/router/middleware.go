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
		RealIP,
		TelemetryMiddleware,
		rateLimitMiddleware(opts),
		RequestLogger(logger),
		Recoverer,
		TimeoutMiddleware(time.Duration(opts.timeoutSeconds) * time.Second),
		corsMiddleware(opts),
		Compress(5),
		SecurityHeaders,
	}

	if opts.enableCSRF {
		// Plumb the router's logger into the CSRF middleware so encrypt
		// failures and stale-token decrypts surface in the same handler
		// (redaction, attributes, sink) as the rest of the app. See
		// ADR-008.
		stack = append(stack, CSRFMiddleware(CSRFOptions{
			ExemptPaths:       opts.csrfExempt,
			EnableOriginCheck: true, // Enable Laravel-style origin verification by default
			Logger:            logger,
		}))
	}

	return stack
}

func rateLimitMiddleware(opts *routerOpts) func(http.Handler) http.Handler {
	if opts == nil || opts.rateLimitReqs <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	window := opts.rateLimitWin
	if window <= 0 {
		window = time.Minute
	}
	return RateLimitMiddleware(RateLimitOptions{
		Requests:       opts.rateLimitReqs,
		Window:         window,
		Burst:          opts.rateLimitBurst,
		ScopeByRoute:   opts.rateLimitRoute,
		ScopeByRole:    opts.rateLimitRole,
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

// SecurityHeaders sets standard security headers on every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; font-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(opts *routerOpts) func(http.Handler) http.Handler {
	allowedOrigins := opts.corsOrigins
	if opts.corsAllowAll || len(allowedOrigins) == 0 {
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
