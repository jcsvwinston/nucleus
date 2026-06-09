# Rate Limiting Guide

Reference date: 2026-06-09.
Status: Current.

This guide covers Nucleus's rate limiting system, including the token-bucket algorithm, per-route and per-role configuration, and production tuning.

## Table of Contents

- [Overview](#overview)
- [Configuration](#configuration)
- [Rate Limit Dimensions](#rate-limit-dimensions)
- [Token-Bucket Algorithm](#token-bucket-algorithm)
- [Per-Route Rate Limiting](#per-route-rate-limiting)
- [Per-Role Rate Limiting](#per-role-rate-limiting)
- [Rate Limit Response](#rate-limit-response)
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
rate_limit_requests: 100   # Sustained request budget (0 disables)
rate_limit_window: 1m      # Refill window (Go duration string)
```

### Advanced configuration

```yaml
# nucleus.yml
rate_limit_requests: 100
rate_limit_window: 1m
rate_limit_burst: 20       # Burst capacity above sustained budget
rate_limit_by_route: true  # Enable per-route partitioning
rate_limit_by_role: true   # Enable per-role partitioning
```

---

## Rate Limit Dimensions

### Global / per-IP rate limiting

Default behaviour when `rate_limit_requests` is configured. Each client is
keyed by user ID (when JWT middleware has set one in context) or client IP:

```yaml
rate_limit_requests: 100   # 100 requests per client
rate_limit_window: 1m      # Per 60 seconds
```

IP is resolved from `X-Forwarded-For` (leftmost entry), then `X-Real-IP`,
then `RemoteAddr`.

### Per-User rate limiting

When JWT middleware enriches the request context with a user ID, the limiter
automatically keys by user ID instead of IP:

```go
// The rate limiter reads the user ID from context (set by JWT middleware).
// user-123 gets an independent budget from user-456.
// No extra configuration is needed beyond rate_limit_requests.
```

---

## Token-Bucket Algorithm

Nucleus uses a token-bucket algorithm for all rate limiting.  When
`rate_limit_burst` is omitted or zero, the burst capacity equals
`rate_limit_requests` and the limiter behaves like a simple fixed-window
counter.  Setting a non-zero `rate_limit_burst` expands the bucket:

```yaml
rate_limit_requests: 100   # Refill rate: 100 tokens / window
rate_limit_window: 1m
rate_limit_burst: 20       # Bucket capacity: requests + burst = 120 tokens max
```

**How it works:**

- Bucket starts full (`requests + burst` tokens).
- Each request consumes 1 token.
- Tokens refill continuously at `requests / window` per second.
- A depleted bucket causes the next request to receive `429 Too Many Requests`.

**Characteristics:**

- Smooths out traffic spikes.
- Allows short controlled bursts when capacity has accumulated.
- Better user experience for legitimate clients that occasionally burst.

---

## Per-Route Rate Limiting

Enable with `rate_limit_by_route: true` in `nucleus.yml` to partition the
token bucket by normalised route path (numeric segments and UUIDs are
replaced with `:id` so `/users/123` and `/users/456` share one bucket):

```yaml
rate_limit_requests: 100
rate_limit_by_route: true
```

You can also apply `RateLimitMiddleware` directly when constructing the
router for fine-grained per-route limits:

```go
import (
    "time"
    "log/slog"

    "github.com/jcsvwinston/nucleus/pkg/router"
)

r := router.New(slog.Default())

// Apply a global limiter to all routes on this router.
r.Use(router.RateLimitMiddleware(router.RateLimitOptions{
    Requests: 100,
    Window:   time.Minute,
}))

// Stricter limit on the import endpoint via a sub-mux.
r.With(router.RateLimitMiddleware(router.RateLimitOptions{
    Requests: 10,
    Window:   time.Minute,
})).Post("/api/articles/import", importHandler)

// Relaxed limit on the health endpoint.
r.With(router.RateLimitMiddleware(router.RateLimitOptions{
    Requests: 1000,
    Window:   time.Minute,
})).Get("/api/health", healthHandler)
```

`RateLimitOptions` fields:

| Field | Type | Description |
|-------|------|-------------|
| `Requests` | `int` | Sustained request budget. `<= 0` disables the limiter. |
| `Window` | `time.Duration` | Refill window. Defaults to 1 minute when zero. |
| `Burst` | `int` | Extra burst capacity above `Requests`. |
| `ScopeByRoute` | `bool` | Partition bucket by normalised route path. |
| `ScopeByRole` | `bool` | Partition bucket by JWT role claim. |
| `KeyFunc` | `func(*http.Request) string` | Override the default client key function. |
| `RouteDimension` | `func(*http.Request) string` | Override route dimension extraction. |
| `RoleDimension` | `func(*http.Request) string` | Override role dimension extraction. |

---

## Per-Role Rate Limiting

Enable with `rate_limit_by_role: true`. When active, the token bucket is
partitioned by the role claim extracted from the JWT in the request context.
Unauthenticated requests are bucketed under the `anonymous` key.

```yaml
rate_limit_requests: 100
rate_limit_by_role: true
```

To apply different request budgets per role, use separate `RateLimitMiddleware`
instances on separate sub-routers or groups, one per role:

```go
// JWT role is read from auth.Claims.Role via the request context.
claims := auth.Claims{
    UserID: "user-123",
    Role:   "editor",
}

// The limiter reads the role from context automatically.
// Provide per-role budgets by applying different RateLimitOptions
// to sub-routers branched on RBAC outcome.
```

> Note: there is no `rate_limit_roles` config key.  Per-role budgets are
> expressed in code via multiple `RateLimitMiddleware` instances or via
> a custom `KeyFunc`.

---

## Rate Limit Response

When a client exceeds its budget, Nucleus returns:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 45
Content-Type: application/json; charset=utf-8

{"error":{"code":"RATE_LIMITED","message":"too many requests"}}
```

`Retry-After` is set to the number of seconds until the next token becomes
available (minimum 1).  No `X-RateLimit-*` headers are currently emitted.

---

## Production Tuning

### Recommended limits

| Endpoint Type | `rate_limit_requests` | `rate_limit_window` | Rationale |
|---------------|----------------------|---------------------|-----------|
| **Health checks** | 1000 | `1m` | Monitoring systems poll frequently |
| **Login** | 10 | `1m` | Prevent brute force |
| **Password reset** | 5 | `5m` | Prevent abuse |
| **API read** | 100-500 | `1m` | Normal usage patterns |
| **API write** | 50-100 | `1m` | More expensive operations |
| **File upload** | 10 | `1m` | Bandwidth protection |
| **Admin API** | 50 | `1m` | Bulk operations need throttling |

### Scaling considerations

#### In-memory limiters

The default rate limiter stores state in process memory.  In multi-replica
deployments:

- Each replica maintains an independent token bucket.
- A client hitting three replicas effectively receives three times the budget.
- This is acceptable for most use cases; it reduces coordination overhead.

#### Redis-backed rate limiting (future)

For strict global limits across replicas, a Redis-backed backend is planned
but not yet implemented:

```yaml
# Future configuration (not yet implemented)
# rate_limit_store: redis
# rate_limit_redis_url: redis://localhost:6379/0
```

### Handling legitimate spikes

```yaml
# Allow a short burst while keeping the sustained rate conservative.
rate_limit_requests: 100
rate_limit_burst: 50       # Absorbs up to 50 extra requests when tokens have accumulated
```

### Monitoring rate limits

```go
// Rate limit events are recorded via the Observability bus.
// Monitor in Grafana / Prometheus:
//   http_rate_limit_total        (counter)
//   http_rate_limit_errors       (counter)
//
// Alert on high rate-limit rates:
//   rate(http_rate_limit_errors[5m]) > 100  -- >100/min may indicate an attack
```

---

## Testing Rate Limits

### Unit testing

```go
func TestRateLimitMiddleware(t *testing.T) {
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    // Create rate limiter: 2 requests per second.
    limiter := router.RateLimitMiddleware(router.RateLimitOptions{
        Requests: 2,
        Window:   time.Second,
    })

    mw := limiter(handler)

    // First 2 requests should succeed.
    for i := 0; i < 2; i++ {
        req := httptest.NewRequest(http.MethodGet, "/", nil)
        rec := httptest.NewRecorder()
        mw.ServeHTTP(rec, req)

        if rec.Code != http.StatusOK {
            t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
        }
    }

    // Third request should be rate limited.
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

# Check application health
curl http://localhost:8080/api/health
```

---

## Quick Reference

```yaml
# Basic
rate_limit_requests: 100
rate_limit_window: 1m

# Advanced
rate_limit_requests: 100
rate_limit_window: 1m
rate_limit_burst: 20
rate_limit_by_route: true
rate_limit_by_role: true
```
