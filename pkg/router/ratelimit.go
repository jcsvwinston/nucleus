package router

import (
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// RateLimitOptions configures rate limiting middleware.
type RateLimitOptions struct {
	Requests       int
	Window         time.Duration
	Burst          int
	ScopeByRoute   bool
	ScopeByRole    bool
	KeyFunc        func(*http.Request) string
	RouteDimension func(*http.Request) string
	RoleDimension  func(*http.Request) string
}

type tokenBucketLimiter struct {
	mu      sync.Mutex
	limit   int
	burst   int
	window  time.Duration
	entries map[string]tokenBucketEntry
}

type tokenBucketEntry struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

var uuidLikeSegment = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[1-5][a-fA-F0-9]{3}-[89abAB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$`)

func newTokenBucketLimiter(limit, burst int, window time.Duration) *tokenBucketLimiter {
	if burst < 0 {
		burst = 0
	}
	return &tokenBucketLimiter{
		limit:   limit,
		burst:   burst,
		window:  window,
		entries: make(map[string]tokenBucketEntry),
	}
}

func (l *tokenBucketLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) > 10000 {
		l.prune(now)
	}

	capacity := float64(l.limit + l.burst)
	if capacity < 1 {
		capacity = 1
	}
	refillPerSecond := float64(l.limit) / l.window.Seconds()
	if refillPerSecond <= 0 {
		refillPerSecond = 1 / l.window.Seconds()
	}

	entry, ok := l.entries[key]
	if !ok {
		entry = tokenBucketEntry{
			tokens:     capacity,
			lastRefill: now,
			lastSeen:   now,
		}
	}

	elapsedSeconds := now.Sub(entry.lastRefill).Seconds()
	if elapsedSeconds > 0 {
		entry.tokens = math.Min(capacity, entry.tokens+(elapsedSeconds*refillPerSecond))
		entry.lastRefill = now
	}
	entry.lastSeen = now

	if entry.tokens >= 1 {
		entry.tokens -= 1
		l.entries[key] = entry
		return true, 0
	}

	missing := 1 - entry.tokens
	waitSeconds := missing / refillPerSecond
	if waitSeconds < 1 {
		waitSeconds = 1
	}
	l.entries[key] = entry
	return false, time.Duration(math.Ceil(waitSeconds)) * time.Second
}

func (l *tokenBucketLimiter) prune(now time.Time) {
	cutoff := now.Add(-5 * l.window)
	for key, entry := range l.entries {
		if entry.lastSeen.Before(cutoff) {
			delete(l.entries, key)
		}
	}
}

// RateLimitMiddleware enforces a fixed-window request limit.
func RateLimitMiddleware(opts RateLimitOptions) func(http.Handler) http.Handler {
	if opts.Requests <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	if opts.Window <= 0 {
		opts.Window = time.Minute
	}
	if opts.Burst < 0 {
		opts.Burst = 0
	}
	keyFn := opts.KeyFunc
	if keyFn == nil {
		keyFn = rateLimitKeyFromRequest
	}
	routeFn := opts.RouteDimension
	if routeFn == nil {
		routeFn = defaultRouteDimensionFromRequest
	}
	roleFn := opts.RoleDimension
	if roleFn == nil {
		roleFn = rateLimitRoleFromRequest
	}

	limiter := newTokenBucketLimiter(opts.Requests, opts.Burst, opts.Window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			baseKey := keyFn(r)
			if baseKey == "" {
				baseKey = "unknown"
			}

			key := baseKey
			if opts.ScopeByRoute {
				routeScope := routeFn(r)
				if routeScope == "" {
					routeScope = "unknown"
				}
				key += "|route:" + routeScope
			}
			if opts.ScopeByRole {
				roleScope := roleFn(r)
				if roleScope == "" {
					roleScope = "anonymous"
				}
				key += "|role:" + roleScope
			}

			allowed, retryAfter := limiter.allow(key, time.Now().UTC())
			if !allowed {
				afterSeconds := int(retryAfter.Seconds())
				if afterSeconds <= 0 {
					afterSeconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(afterSeconds))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMITED","message":"too many requests"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func rateLimitKeyFromRequest(r *http.Request) string {
	ctx := r.Context()
	var prefix string
	if tenant := observe.TenantIDFromCtx(ctx); tenant != "" {
		prefix = "tenant:" + tenant + "|"
	}
	if userID := observe.UserIDFromCtx(ctx); userID != "" {
		return prefix + "user:" + userID
	}
	return prefix + "ip:" + clientIP(r)
}

func rateLimitRoleFromRequest(r *http.Request) string {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || claims == nil {
		return "anonymous"
	}
	role := strings.TrimSpace(claims.Role)
	if role == "" {
		return "anonymous"
	}
	return strings.ToLower(role)
}

func defaultRouteDimensionFromRequest(r *http.Request) string {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	return method + ":" + normalizeRateLimitPath(r.URL.Path)
}

func normalizeRateLimitPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "/" {
		return "/"
	}

	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	for i, part := range parts {
		switch {
		case isNumericPathPart(part):
			parts[i] = ":id"
		case uuidLikeSegment.MatchString(part):
			parts[i] = ":id"
		case len(part) > 64:
			parts[i] = part[:64]
		}
	}

	return "/" + strings.Join(parts, "/")
}

func isNumericPathPart(part string) bool {
	if part == "" {
		return false
	}
	for _, r := range part {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func clientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
