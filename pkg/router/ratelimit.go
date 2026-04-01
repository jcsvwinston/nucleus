package router

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

// RateLimitOptions configures rate limiting middleware.
type RateLimitOptions struct {
	Requests int
	Window   time.Duration
	KeyFunc  func(*http.Request) string
}

type fixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]fixedWindowEntry
}

type fixedWindowEntry struct {
	windowStart time.Time
	count       int
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		limit:   limit,
		window:  window,
		entries: make(map[string]fixedWindowEntry),
	}
}

func (l *fixedWindowLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) > 10000 {
		l.prune(now)
	}

	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.windowStart) >= l.window {
		l.entries[key] = fixedWindowEntry{
			windowStart: now,
			count:       1,
		}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func (l *fixedWindowLimiter) prune(now time.Time) {
	cutoff := now.Add(-5 * l.window)
	for key, entry := range l.entries {
		if entry.windowStart.Before(cutoff) {
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
	keyFn := opts.KeyFunc
	if keyFn == nil {
		keyFn = rateLimitKeyFromRequest
	}

	limiter := newFixedWindowLimiter(opts.Requests, opts.Window)
	retryAfter := strconv.Itoa(int(opts.Window.Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if key == "" {
				key = "unknown"
			}
			if !limiter.allow(key, time.Now().UTC()) {
				w.Header().Set("Retry-After", retryAfter)
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
	if userID := observe.UserIDFromCtx(r.Context()); userID != "" {
		return "user:" + userID
	}
	return "ip:" + clientIP(r)
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
