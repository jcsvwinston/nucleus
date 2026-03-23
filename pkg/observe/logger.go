// Package observe provides observability utilities for the GoFrame framework,
// including structured logging via slog with context-aware field extraction.
package observe

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyUserID
	ctxKeyTraceID
)

// NewLogger creates a *slog.Logger configured with the given level and format.
// Supported levels: "debug", "info", "warn", "error" (default: "info").
// Supported formats: "json", "text" (default: "json").
func NewLogger(level, format string) *slog.Logger {
	lvl := parseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// WithContext returns a logger enriched with fields extracted from the context
// (request_id, user_id, trace_id) if present.
func WithContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if id := RequestIDFromCtx(ctx); id != "" {
		logger = logger.With("request_id", id)
	}
	if id := UserIDFromCtx(ctx); id != "" {
		logger = logger.With("user_id", id)
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
