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

// originAllowed reports whether origin matches an allow-list entry. An entry
// is either an exact origin (scheme://host[:port], compared
// case-insensitively) or a subdomain wildcard, scheme://*.example.com[:port],
// which matches any origin whose host is a proper subdomain of example.com
// on the same scheme and port. The apex itself (https://example.com) does not
// match the wildcard entry: list it explicitly. A `*` anywhere else in an
// entry is not a pattern and never matches.
func originAllowed(allowed []string, origin string) bool {
	for _, o := range allowed {
		if strings.EqualFold(o, origin) {
			return true
		}
		if strings.Contains(o, "://*.") && wildcardOriginMatches(o, origin) {
			return true
		}
	}
	return false
}

func wildcardOriginMatches(pattern, origin string) bool {
	pi := strings.Index(pattern, "://*.")
	if pi < 0 {
		return false
	}
	scheme := pattern[:pi]
	oi := strings.Index(origin, "://")
	if oi < 0 || !strings.EqualFold(origin[:oi], scheme) {
		return false
	}
	pHost, pPort := splitOriginHostPort(pattern[pi+len("://*."):])
	oHost, oPort := splitOriginHostPort(origin[oi+3:])
	if pHost == "" || !strings.EqualFold(pPort, oPort) {
		return false
	}
	pHost, oHost = strings.ToLower(pHost), strings.ToLower(oHost)
	// A proper subdomain: at least one label before the suffix, and no path
	// or userinfo smuggled into the host part.
	if strings.ContainsAny(oHost, "/@?#") {
		return false
	}
	return len(oHost) > len(pHost)+1 && strings.HasSuffix(oHost, "."+pHost)
}

// splitOriginHostPort separates host[:port]; an IPv6 literal keeps its
// brackets and only a trailing :port outside them counts as the port.
func splitOriginHostPort(s string) (host, port string) {
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[i:], "]") {
		return s[:i], s[i+1:]
	}
	return s, ""
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

			allowed := allowAll || originAllowed(opts.AllowedOrigins, origin)

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
