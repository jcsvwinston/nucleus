// Package observe provides observability utilities for the Nucleus framework,
// including structured logging via slog with context-aware field extraction.
package observe

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyUserID
	ctxKeyTraceID
	ctxKeyTenantID
	ctxKeyModelObserved
)

// NewLogger creates a *slog.Logger configured with the given level and format.
// Supported levels: "debug", "info", "warn" (alias "warning"), "error" (default: "info").
// Supported formats: "json", "text" (default: "json").
//
// Secret redaction is ON by default: attribute values whose key is in
// DefaultRedactedKeys (authorization, cookie, password, token, …) are
// replaced with RedactionPlaceholder before they reach the output. To
// extend the key set, change the placeholder, or disable redaction
// entirely, use NewLoggerWithRedaction. See ADR-007.
//
// Redaction is key-based and applies only to structured key-value
// attributes. It does NOT scan the message string — a secret
// interpolated into the msg (e.g. fmt.Sprintf("token=%s", t)) is logged
// verbatim. It also does not recurse into a struct logged via slog.Any
// under a non-secret key; only slog.Group attrs are expanded and
// matched. Always pass secrets as their own named attrs, and do not log
// secret material in the first place — redaction is defence-in-depth,
// not a license.
func NewLogger(level, format string) *slog.Logger {
	return NewLoggerWithRedaction(level, format, RedactionConfig{})
}

// NewLoggerWithRedaction is NewLogger with explicit control over secret
// redaction. The zero-value RedactionConfig is identical to NewLogger:
// redaction on, built-in key set, standard placeholder.
func NewLoggerWithRedaction(level, format string, cfg RedactionConfig) *slog.Logger {
	return newLogger(os.Stdout, level, format, cfg)
}

// newLogger is the shared constructor behind NewLogger and
// NewLoggerWithRedaction. The io.Writer is a parameter so tests can
// capture and assert on the actual rendered output; the exported
// constructors always pass os.Stdout.
func newLogger(w io.Writer, level, format string, cfg RedactionConfig) *slog.Logger {
	lvl := parseLevel(level)
	opts := &slog.HandlerOptions{
		Level: lvl,
		// newRedactor returns nil when cfg.Disabled is set, leaving
		// ReplaceAttr unset (zero overhead) in that case.
		ReplaceAttr: newRedactor(cfg),
	}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(handler)
}

// WithContext returns a logger enriched with fields extracted from the context
// (request_id, user_id, trace_id) if present.
func WithContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if id := RequestIDFromCtx(ctx); id != "" {
		logger = logger.With("request_id", id)
	}
	if id := UserIDFromCtx(ctx); id != "" {
		logger = logger.With("user_id", id)
	}
	if id := TenantIDFromCtx(ctx); id != "" {
		logger = logger.With("tenant_id", id)
	}
	if id := TraceIDFromCtx(ctx); id != "" {
		logger = logger.With("trace_id", id)
	}
	return logger
}

// CtxWithRequestID stores a request ID in the context.
func CtxWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestIDFromCtx extracts the request ID from the context, or returns "".
func RequestIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// CtxWithUserID stores a user ID in the context.
func CtxWithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, id)
}

// UserIDFromCtx extracts the user ID from the context, or returns "".
func UserIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyUserID).(string); ok {
		return v
	}
	return ""
}

// CtxWithTenantID stores a tenant ID in the context. Used by request-scope
// resolution in pkg/app and read by downstream middleware (logging,
// rate-limit per-tenant keying) that cannot import pkg/app without a cycle.
func CtxWithTenantID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyTenantID, id)
}

// TenantIDFromCtx extracts the tenant ID from the context, or returns "".
func TenantIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyTenantID).(string); ok {
		return v
	}
	return ""
}

// CtxWithTraceID stores a trace ID in the context.
func CtxWithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyTraceID, id)
}

// TraceIDFromCtx extracts the trace ID from the context, or returns "".
func TraceIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyTraceID).(string); ok {
		return v
	}
	return ""
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
