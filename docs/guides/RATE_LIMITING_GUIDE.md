# Rate Limiting Guide

Reference date: 2026-05-29.
Status: Current.

This guide covers Nucleus's rate limiting system, including fixed-window and token-bucket algorithms, per-route and per-role configuration, and production tuning.

## Table of Contents

- [Overview](#overview)
- [Configuration](#configuration)
- [Rate Limit Dimensions](#rate-limit-dimensions)
- [Fixed-Window Algorithm](#fixed-window-algorithm)
- [Token-Bucket Algorithm](#token-bucket-algorithm)
- [Per-Route Rate Limiting](#per-route-rate-limiting)
- [Per-Role Rate Limiting](#per-role-rate-limiting)
- [Rate Limit Headers](#rate-limit-headers)
- [Production Tuning](#production-tuning)
- [Testing Rate Limits](#testing-rate-limits)

---

## Overview

Nucleus provides multi-dimensional rate limiting via middleware:

| Dimension | Scope | Use Case |
|-----------|-------|----------|
| **Global** | All requests | Protect entire application |
| **Per-IP** | Client IP address | Prevent abuse from single source |
| **Per-User** | Authenticated user ID | Fair usage per account |
| **Per-Route** | Specific endpoint | Protect expensive operations |
| **Per-Role** | JWT role claim | Different limits for user types |

---

## Configuration

### Basic configuration

```yaml
# nucleus.yml
rate_limit: 100           # Requests per window (global default)
rate_limit_window: 60     # Window size in seconds
```

### Advanced configuration

```yaml
# nucleus.yml
rate_limit: 100
rate_limit_window: 60
rate_limit_burst: 20      # Token-bucket burst capacity
rate_limit_by_route: true # Enable per-route limits
rate_limit_by_role: true  # Enable per-role limits
```

---

## Rate Limit Dimensions

### Global rate limiting

Applies to all requests across all clients:

```yaml
rate_limit: 1000      # 1000 requests total
rate_limit_window: 60 # Per 60 seconds
```

### Per-IP rate limiting

Default behavior when `rate_limit` is configured:

```yaml
rate_limit: 100       # 100 requests per IP
rate_limit_window: 60 # Per 60 seconds
```

Keyed by `X-Real-IP` header or `RemoteAddr`.

### Per-User rate limiting

When JWT middleware enriches context with user ID:

```go
// Automatically uses user ID from context (set by JWT middleware)
// user-123 gets independent budget from user-456
```

---

## Fixed-Window Algorithm

The default rate limiter uses fixed windows:

```
Window: 60 seconds
Limit: 100 requests

Timeline:
[0s -------------------- 60s] [60s ------------------- 120s]
Requests: 0-99 allowed        Requests: 0-99 allowed (reset)
Request 100 -> 429 Too Many Requests
```

**Characteristics:**
- Simple and fast (in-memory counter)
- Boundary spike: up to 2x requests at window edges
- No state between windows

---

## Token-Bucket Algorithm

When `rate_limit_burst` is configured, Nucleus uses token-bucket for smoother limiting:

```yaml
rate_limit: 100           # Refill rate: 100 tokens/minute
rate_limit_window: 60
rate_limit_burst: 20      # Bucket capacity: can absorb 20 extra requests
```

**How it works:**
- Bucket starts full (100 tokens)
- Each request consumes 1 token
- Bucket refills at 100 tokens/minute
- Can burst up to 20 extra requests if tokens accumulated

**Characteristics:**
- Smooths out traffic
- Allows controlled bursting
- Better UX for legitimate users

---

## Per-Route Rate Limiting

Enable with `rate_limit_by_route: true`:

```yaml
rate_limit: 100
rate_limit_by_route: true
```

Define route-specific limits in code:

```go
import (
    "time"

    "github.com/jcsvwinston/nucleus/pkg/router"
)

r := router.New(logger)

// Default rate limit middleware (100 requests / 60s, keyed per client)
r.Use(router.RateLimitMiddleware(router.RateLimitOptions{
    Requests: 100,
    Window:   60 * time.Second,
}))

// Override for specific routes by wrapping a stricter or looser limiter
// onto a sub-router with .With(...).
r.Post("/api/articles", createHandler) // Uses the global limit above

r.With(router.RateLimitMiddleware(router.RateLimitOptions{
    Requests: 10,
    Window:   60 * time.Second,
})).Post("/api/articles/import", importHandler) // Stricter limit

r.With(router.RateLimitMiddleware(router.RateLimitOptions{
    Requests: 1000,
    Window:   60 * time.Second,
})).Get("/api/health", healthHandler) // Relaxed limit
```

---

## Per-Role Rate Limiting

Enable with `rate_limit_by_role: true`:

```yaml
rate_limit: 100
rate_limit_by_role: true
```

Define role-specific limits:

```yaml
rate_limit_roles:
  admin: 10000     # Admins: 10k requests/window
  editor: 1000     # Editors: 1k requests/window
  user: 100        # Regular users: 100 requests/window
  anonymous: 50    # Unauthenticated: 50 requests/window
```

Roles are extracted from JWT claims by the JWT middleware:

```go
// JWT token with role claim
claims := auth.Claims{
    UserID: "user-123",
    Role:   "editor",
}

// Rate limiter extracts role from context
// Editor gets 1000 requests/window instead of default 100
```

---

## Rate Limit Headers

Nucleus returns standard rate limit headers:

```http
HTTP/1.1 200 OK
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 87
X-RateLimit-Reset: 1712764860
```

When rate limited:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 45
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1712764860

{
    "error": {
        "code": "RATE_LIMITED",
        "message": "rate limit exceeded, try again in 45 seconds",
        "status": 429
    }
}
```

---

## Production Tuning

### Recommended limits

| Endpoint Type | Limit | Window | Rationale |
|---------------|-------|--------|-----------|
| **Health checks** | 1000 | 60s | Monitoring systems poll frequently |
| **Login** | 10 | 60s | Prevent brute force |
| **Password reset** | 5 | 300s | Prevent abuse |
| **API read** | 100-500 | 60s | Normal usage patterns |
| **API write** | 50-100 | 60s | More expensive operations |
| **File upload** | 10 | 60s | Bandwidth protection |
| **Admin panel** | 200 | 60s | Internal users need more capacity |
| **Admin API** | 50 | 60s | Bulk operations need throttling |

### Scaling considerations

#### In-memory limiters

The default rate limiter stores state in process memory. In multi-replica deployments:

- Each replica has independent limits
- Client hitting 3 replicas gets 3x the limit
- Acceptable for most use cases

#### Redis-backed rate limiting (future)

For strict global limits across replicas, use Redis:

```yaml
# Future configuration (not yet implemented)
rate_limit_store: redis
rate_limit_redis_url: redis://localhost:6379/0
```

### Handling legitimate spikes

```yaml
# Use burst capacity for controlled spikes
rate_limit: 100
rate_limit_burst: 50  # Allow 50% extra for short bursts
```

### Monitoring rate limits

```go
// Rate limit events are emitted via metrics
// Monitor in Grafana/Prometheus:
// - http_rate_limit_total (counter)
// - http_rate_limit_errors (counter)

// Alert on high rate limit rates:
// rate(http_rate_limit_errors[5m]) > 100  # More than 100/min = potential attack
```

---

## Testing Rate Limits

### Unit testing

```go
func TestRateLimitMiddleware(t *testing.T) {
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    // Create rate limiter: 2 requests per second
    limiter := router.RateLimitMiddleware(router.RateLimitOptions{
        Requests: 2,
        Window:   time.Second,
    })

    mw := limiter(handler)

    // First 2 requests should succeed
    for i := 0; i < 2; i++ {
        req := httptest.NewRequest(http.MethodGet, "/", nil)
        rec := httptest.NewRecorder()
        mw.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
            t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
        }
    }

    // Third request should be rate limited
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    rec := httptest.NewRecorder()
    mw.ServeHTTP(rec, req)

    if rec.Code != http.StatusTooManyRequests {
        t.Errorf("expected 429, got %d", rec.Code)
    }
}
```

### Integration testing

```bash
# Test with curl
for i in {1..110}; do
    curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/api/articles
done

# Should see 100x 200, then 10x 429
```

### Load testing

```bash
# Using hey
hey -n 1000 -c 10 http://localhost:8080/api/articles

# Check rate limit metrics
curl http://localhost:8080/api/health
```

---

## Quick Reference

```yaml
# Basic
rate_limit: 100
rate_limit_window: 60

# Advanced
rate_limit: 100
rate_limit_window: 60
rate_limit_burst: 20
rate_limit_by_route: true
rate_limit_by_role: true
rate_limit_roles:
  admin: 10000
  editor: 1000
  user: 100
```
