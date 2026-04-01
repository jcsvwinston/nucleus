package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

// DefaultStack returns the standard middleware chain for GoFrame applications.
func DefaultStack(logger *slog.Logger, opts *routerOpts) []func(http.Handler) http.Handler {
	stack := []func(http.Handler) http.Handler{
		middleware.RequestID,
		middleware.RealIP,
		TelemetryMiddleware,
		rateLimitMiddleware(opts),
		RequestLogger(logger),
		middleware.Recoverer,
		middleware.Timeout(time.Duration(opts.timeoutSeconds) * time.Second),
		corsMiddleware(opts),
		middleware.Compress(5),
		SecurityHeaders,
	}

	if opts.enableCSRF {
		stack = append(stack, CSRFMiddleware(CSRFOptions{ExemptPaths: opts.csrfExempt}))
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
		Requests: opts.rateLimitReqs,
		Window:   window,
	})
}

// RequestLogger returns middleware that logs each HTTP request with slog.
// It records method, path, status, duration, request_id, remote_addr, and user_agent.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Inject request ID into context for downstream use
			reqID := middleware.GetReqID(r.Context())
			ctx := observe.CtxWithRequestID(r.Context(), reqID)
			r = r.WithContext(ctx)

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(opts *routerOpts) func(http.Handler) http.Handler {
	allowedOrigins := opts.corsOrigins
	if opts.corsAllowAll || len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}

	return cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}
