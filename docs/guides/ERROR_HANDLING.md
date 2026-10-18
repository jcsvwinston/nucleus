# Error Handling Guide

Reference date: 2026-05-07.
Status: Current.

This guide covers Nucleus's error handling system (`pkg/errors`), including Laravel-style features like report/render separation, configurable log levels, global context, and exception throttling.

## Table of Contents

- [Overview](#overview)
- [Laravel-Style Features](#laravel-style-features)
- [Domain Error Types](#domain-error-types)
- [Creating Errors](#creating-errors)
- [Reportable Exceptions](#reportable-exceptions)
- [Renderable Exceptions](#renderable-exceptions)
- [HTTP Error Responses](#http-error-responses)
- [Error Handler Configuration](#error-handler-configuration)
- [Exception Log Levels](#exception-log-levels)
- [Global Log Context](#global-log-context)
- [Exception Throttling](#exception-throttling)
- [Ignoring Exceptions](#ignoring-exceptions)
- [Error Wrapping and Unwrapping](#error-wrapping-and-unwrapping)

---

## Overview

Nucleus provides a structured error system in `pkg/errors` with Laravel-style features:

1. **Report/Render Separation**: Separate logging (report) from HTTP responses (render)
2. **Reportable Exceptions**: Custom reporting logic for external services (Sentry, Flare)
3. **Renderable Exceptions**: Custom HTTP responses per error type
4. **Configurable Log Levels**: Set log levels per error type
5. **Global Context**: Automatic context in all error logs
6. **Exception Throttling**: Prevent log spam with rate limiting

---

## Laravel-Style Features

### Report/Render Separation

Nucleus separates error handling into two distinct concerns:

- **Report**: Logging, sending to external services (Sentry, Flare)
- **Render**: HTTP response to the user

```go
import "github.com/jcsvwinston/nucleus/pkg/errors"

handler := errors.NewErrorHandler(logger, nil)

// Report (logging, external services)
handler.Report(ctx, err)

// Render (HTTP response)
handler.Render(w, r, err)
```

### Interfaces

```go
// Reportable - custom reporting logic
type Reportable interface {
    Report(ctx context.Context, logger *slog.Logger) bool
}

// Renderable - custom HTTP rendering
type Renderable interface {
    Render(w http.ResponseWriter, r *http.Request) bool
}

// ContextProvider - additional logging context
type ContextProvider interface {
    Context() map[string]any
}

// LogLevelProvider - custom log level
type LogLevelProvider interface {
    LogLevel() slog.Level
}
```

---

## Domain Error Types

Nucleus defines standard domain errors:

| Error Code | HTTP Status | Description |
|------------|-------------|-------------|
| `NOT_FOUND` | 404 | Resource not found |
| `UNAUTHORIZED` | 401 | Authentication required |
| `FORBIDDEN` | 403 | Insufficient permissions |
| `VALIDATION_FAILED` | 422 | Request validation failed |
| `CONFLICT` | 409 | Resource conflict |
| `INTERNAL_ERROR` | 500 | Internal server error |
| `BAD_REQUEST` | 400 | Malformed request |

---

## Creating Errors

### Using error constructors

```go
import "github.com/jcsvwinston/nucleus/pkg/errors"

// Simple domain error
err := errors.NotFound("article", "42")

// Error with details
err := errors.ValidationFailed(map[string]string{
    "email": "invalid format",
})

// Unauthorized
err := errors.Unauthorized("authentication required")

// Forbidden
err := errors.Forbidden("admin access required")

// Conflict
err := errors.Conflict("email already exists")

// Internal error
err := errors.InternalError("database connection failed")
```

---

## Reportable Exceptions

Implement `Reportable` for custom reporting logic (e.g., send to Sentry):

```go
type PaymentError struct {
    *errors.DomainError
    OrderID string
}

func (e *PaymentError) Report(ctx context.Context, logger *slog.Logger) bool {
    // Send to Sentry/Flare
    sentry.CaptureException(e)
    return true // Handled, don't use default logging
}
```

Usage:

```go
err := &PaymentError{
    DomainError: errors.Conflict("payment failed"),
    OrderID:     "12345",
}

handler.Report(ctx, err) // Uses custom Report method
```

---

## Renderable Exceptions

Implement `Renderable` for custom HTTP responses:

```go
type MaintenanceError struct {
    *errors.DomainError
}

func (e *MaintenanceError) Render(w http.ResponseWriter, r *http.Request) bool {
    // Return custom HTML page
    w.Header().Set("Content-Type", "text/html")
    w.WriteHeader(503)
    w.Write([]byte(`<html><body>Maintenance mode</body></html>`))
    return true // Handled, don't use default rendering
}
```

Usage:

```go
err := &MaintenanceError{
    DomainError: errors.InternalError("system maintenance"),
}

handler.Render(w, r, err) // Uses custom Render method
```

---

## HTTP Error Responses

### Basic usage (convenience function)

```go
import "github.com/jcsvwinston/nucleus/pkg/errors"

func GetArticle(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")

    article, err := articleService.Find(id)
    if err != nil {
        errors.WriteError(w, r, err, logger)
        return
    }

    json.NewEncoder(w).Encode(article)
}
```

### Advanced usage (custom handler)

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    GlobalContext: func(ctx context.Context) map[string]any {
        return map[string]any{
            "user_id": userIDFromCtx(ctx),
            "trace_id": traceIDFromCtx(ctx),
        }
    },
    ThrottleConfig: &errors.ThrottleConfig{
        RateLimit: 100,
        Duration:  time.Minute,
    },
})

handler.Report(ctx, err)
handler.Render(w, r, err)
```

### Error response format

By default an application answers every error as RFC 9457 problem details
(`application/problem+json`; DEP-2026-022), with the framework's code and
the validation details as extension members:

```json
{
    "type": "about:blank",
    "title": "Not Found",
    "status": 404,
    "detail": "article '42' not found",
    "instance": "/api/articles/42",
    "code": "NOT_FOUND"
}
```

An application built with `WithErrorEnvelope()` answers the framework's
envelope instead (a client that prefers `application/problem+json` in its
`Accept` header still gets problem details):

```json
{
    "error": {
        "code": "NOT_FOUND",
        "message": "article '42' not found",
        "details": null
    }
}
```

---

## Error Handler Configuration

### Global Context

Add automatic context to all error logs:

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    GlobalContext: func(ctx context.Context) map[string]any {
        return map[string]any{
            "user_id": getUserID(ctx),
            "request_id": getRequestID(ctx),
        }
    },
})
```

### Configurable Log Levels

Set log levels for specific error types:

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    LogLevelMap: map[error]slog.Level{
        &DatabaseError{}, slog.LevelCritical,
        &ValidationError{}, slog.LevelInfo,
    },
})
```

### Ignoring Exceptions

Ignore certain error types from logging:

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    IgnoredErrors: []error{
        &WebhookTimeoutError{},
        &HealthCheckError{},
    },
})
```

---

## Exception Log Levels

### Default behavior

DomainErrors automatically use:
- `slog.LevelError` for 5xx errors
- `slog.LevelDebug` for 4xx errors

### Custom log levels via interface

```go
type CriticalError struct {
    *errors.DomainError
}

func (e *CriticalError) LogLevel() slog.Level {
    return slog.LevelCritical
}
```

### Custom log levels via config

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    LogLevelMap: map[error]slog.Level{
        &DatabaseError{}, slog.LevelCritical,
    },
})
```

---

## Global Log Context

### Error-specific context

Implement `ContextProvider` on your errors:

```go
type OrderError struct {
    *errors.DomainError
    OrderID string
    UserID  string
}

func (e *OrderError) Context() map[string]any {
    return map[string]any{
        "order_id": e.OrderID,
        "user_id":  e.UserID,
    }
}
```

### Global context via handler

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    GlobalContext: func(ctx context.Context) map[string]any {
        return map[string]any{
            "request_id": getRequestID(ctx),
            "user_id":    getUserID(ctx),
            "trace_id":   getTraceID(ctx),
        }
    },
})
```

Log output includes both global and error-specific context:

```
ERROR server error code=ORDER_FAILED message=order processing failed status=500 request_id=abc123 user_id=456 order_id=789
```

---

## Exception Throttling

### Rate limiting

Prevent log spam during error bursts:

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    ThrottleConfig: &errors.ThrottleConfig{
        RateLimit: 100, // Max 100 errors
        Duration:  time.Minute, // Per minute
    },
})
```

### Custom throttling key

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    ThrottleConfig: &errors.ThrottleConfig{
        RateLimit: 10,
        Duration:  time.Minute,
        KeyFunc: func(err error) string {
            // Throttle by user ID
            if e, ok := err.(*UserError); ok {
                return e.UserID
            }
            return err.Error()
        },
    },
})
```

---

## Ignoring Exceptions

### Ignore by type

```go
handler := errors.NewErrorHandler(logger, &errors.ErrorHandlerConfig{
    IgnoredErrors: []error{
        &WebhookTimeoutError{},
        &HealthCheckError{},
    },
})
```

### Conditional ignoring

```go
type IgnorableError struct {
    *errors.DomainError
    ShouldIgnore bool
}

func (e *IgnorableError) Report(ctx context.Context, logger *slog.Logger) bool {
    if e.ShouldIgnore {
        return true // Don't log
    }
    return false // Use default logging
}
```

---

## Error Wrapping and Unwrapping

Nucleus errors support Go 1.13+ error wrapping:

```go
import (
    "errors"
    "fmt"

    gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// Wrap domain error with context
err := gferrors.NotFound("article", "42")
wrapped := fmt.Errorf("get article: %w", err)

// Unwrap
var notFound *gferrors.DomainError
if errors.As(wrapped, &notFound) {
    // Handle not found
}
```

---

## Best Practices

1. **Use Report/Render separation**: Separate logging from HTTP responses
2. **Implement Reportable for external services**: Send critical errors to Sentry/Flare
3. **Implement Renderable for custom responses**: Use HTML pages for user-facing errors
4. **Use ContextProvider**: Add business context to error logs
5. **Configure log levels appropriately**: Critical errors at CRITICAL, validation at DEBUG
6. **Use throttling**: Prevent log spam during error bursts
7. **Ignore benign errors**: Don't log webhook timeouts, health checks, etc.
8. **Wrap, don't swallow**: Always wrap underlying errors with context

```go
// Good - report/render separation
handler.Report(ctx, err)
handler.Render(w, r, err)

// Good - custom reporting
type PaymentError struct {
    *errors.DomainError
}
func (e *PaymentError) Report(ctx context.Context, logger *slog.Logger) bool {
    sentry.CaptureException(e)
    return true
}

// Good - custom rendering
type MaintenanceError struct {
    *errors.DomainError
}
func (e *MaintenanceError) Render(w http.ResponseWriter, r *http.Request) bool {
    renderMaintenancePage(w)
    return true
}
```
