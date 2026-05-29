package router

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSOptions configures the CORS middleware.
type CORSOptions struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int // seconds
}

// CORSMiddleware returns middleware that handles Cross-Origin Resource Sharing.
// It processes preflight OPTIONS requests and sets the appropriate CORS headers
// on all responses.
func CORSMiddleware(opts CORSOptions) func(http.Handler) http.Handler {
	if len(opts.AllowedMethods) == 0 {
		opts.AllowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(opts.AllowedHeaders) == 0 {
		opts.AllowedHeaders = []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"}
	}
	if opts.MaxAge <= 0 {
		opts.MaxAge = 300
	}

	allowAll := len(opts.AllowedOrigins) == 1 && opts.AllowedOrigins[0] == "*"

	methodsStr := strings.Join(opts.AllowedMethods, ", ")
	headersStr := strings.Join(opts.AllowedHeaders, ", ")
	exposedStr := strings.Join(opts.ExposedHeaders, ", ")
	maxAgeStr := strconv.Itoa(opts.MaxAge)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := allowAll
			if !allowed {
				for _, o := range opts.AllowedOrigins {
					if strings.EqualFold(o, origin) {
						allowed = true
						break
					}
				}
			}

			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			// FW-6: `Access-Control-Allow-Origin: *` together with
			// `Access-Control-Allow-Credentials: true` is an invalid
			// combination — the browser rejects the response and the
			// credentialed request fails. When credentials are enabled we
			// must reflect the specific request Origin (which already passed
			// the allow-list / allow-all check above) and add `Vary: Origin`
			// so shared caches do not serve one origin's response to another.
			// The literal `*` is only emitted for credential-less requests.
			if allowAll && !opts.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

			if opts.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if exposedStr != "" {
				w.Header().Set("Access-Control-Expose-Headers", exposedStr)
			}

			// Preflight
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", methodsStr)
				w.Header().Set("Access-Control-Allow-Headers", headersStr)
				w.Header().Set("Access-Control-Max-Age", maxAgeStr)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
