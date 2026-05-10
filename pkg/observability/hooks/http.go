package hooks

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// HTTPMiddlewareConfig configures NewHTTPMiddleware. Zero value is illegal:
// at minimum a *Bus must be set.
type HTTPMiddlewareConfig struct {
	// Bus is the observability bus events are emitted to. Required. If nil,
	// the returned middleware is a pass-through.
	Bus *observability.Bus

	// NodeID identifies this framework process. Empty during local dev is OK.
	NodeID string

	// ExcludePaths is a list of glob/prefix patterns that suppress
	// instrumentation. Patterns may end in "/*" for prefix match, contain
	// "*"/"?" for path.Match globs, or be plain prefixes. The default empty
	// list means everything is observed. Note: when no subscriber wants
	// HTTP events, the entire middleware is a single atomic load anyway —
	// ExcludePaths is for when you have observers but want to filter noise
	// (e.g. /healthz hits flooding the panel).
	ExcludePaths []string

	// MaxPayloadPreviewBytes caps the redacted body summary size. Default
	// 240 bytes when zero.
	MaxPayloadPreviewBytes int

	// MaxUserAgentBytes caps the User-Agent string. Default 320 bytes.
	MaxUserAgentBytes int

	// MaxPathBytes caps the URL path. Default 240 bytes.
	MaxPathBytes int
}

// NewHTTPMiddleware returns an http.Handler middleware that emits a
// HTTPRequestEvent for every request that passes the configured exclude
// list, but only when at least one subscriber wants HTTPRequest events.
//
// The middleware is a no-op (just `next.ServeHTTP`) when:
//   - cfg.Bus is nil
//   - cfg.Bus has no HTTPRequest subscribers
//   - the request path matches an ExcludePaths entry
//   - the request is a WebSocket upgrade (status code is meaningless and
//     mutating the request is intrusive)
//
// The "no subscribers" gate is the critical hot-path optimization. It
// resolves to a single atomic load on the read side; the event allocation,
// response-writer wrapping, and time-of-day call all happen lazily.
func NewHTTPMiddleware(cfg HTTPMiddlewareConfig) func(http.Handler) http.Handler {
	if cfg.Bus == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	maxPreview := cfg.MaxPayloadPreviewBytes
	if maxPreview <= 0 {
		maxPreview = 240
	}
	maxUA := cfg.MaxUserAgentBytes
	if maxUA <= 0 {
		maxUA = 320
	}
	maxPath := cfg.MaxPathBytes
	if maxPath <= 0 {
		maxPath = 240
	}

	excludes := append([]string(nil), cfg.ExcludePaths...)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Hot path gate. Single atomic load when nobody is watching.
			if !cfg.Bus.HasSubscribers(observability.KindHTTPRequest) {
				next.ServeHTTP(w, r)
				return
			}
			if shouldExcludePath(r.URL.Path, excludes) {
				next.ServeHTTP(w, r)
				return
			}
			if router.IsWebSocketUpgrade(r) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			ww := router.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			ctx := r.Context()

			ev := observability.AcquireHTTPRequestEvent(time.Now().UTC(), cfg.NodeID)
			ev.Method = r.Method
			ev.Path = truncate(r.URL.Path, maxPath)
			ev.Status = ww.Status()
			ev.Duration = time.Since(start)
			ev.RequestID = strings.TrimSpace(observe.RequestIDFromCtx(ctx))
			ev.TraceID = strings.TrimSpace(observe.TraceIDFromCtx(ctx))
			ev.UserID = strings.TrimSpace(observe.UserIDFromCtx(ctx))
			ev.RemoteIP = auth.ClientIPFromRequest(r)
			ev.UserAgent = truncate(strings.TrimSpace(r.UserAgent()), maxUA)
			ev.PayloadPreview = payloadPreview(r, maxPreview)

			cfg.Bus.Emit(ev)
		})
	}
}

func payloadPreview(r *http.Request, max int) string {
	if r == nil {
		return ""
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	switch method {
	case http.MethodGet, http.MethodDelete:
		q := redactSensitiveQuery(r.URL.Query())
		encoded := q.Encode()
		if encoded == "" {
			return ""
		}
		out := "query:" + encoded
		return truncate(out, max)
	default:
		if r.ContentLength > 0 {
			return truncate(fmt.Sprintf("body:redacted (%d bytes)", r.ContentLength), max)
		}
		ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if ct != "" {
			return truncate("body:redacted ("+ct+")", max)
		}
		return "body:redacted"
	}
}

func redactSensitiveQuery(values url.Values) url.Values {
	out := url.Values{}
	for k, items := range values {
		sensitive := isSensitiveKey(k)
		for _, item := range items {
			if sensitive {
				out.Add(k, "***")
				continue
			}
			out.Add(k, item)
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	return strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "TOKEN")
}

func shouldExcludePath(requestPath string, patterns []string) bool {
	value := strings.TrimSpace(requestPath)
	if value == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == "*" {
			return true
		}
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "/*")
			if prefix == "" || prefix == "/" {
				return true
			}
			if value == prefix || strings.HasPrefix(value, prefix+"/") {
				return true
			}
		}
		if strings.Contains(pattern, "*") || strings.Contains(pattern, "?") {
			if matched, _ := path.Match(pattern, value); matched {
				return true
			}
			continue
		}
		trimmed := strings.TrimRight(pattern, "/")
		if trimmed == "" {
			trimmed = pattern
		}
		if value == pattern || value == trimmed || strings.HasPrefix(value, trimmed+"/") {
			return true
		}
	}
	return false
}

func truncate(value string, max int) string {
	text := strings.TrimSpace(value)
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}
