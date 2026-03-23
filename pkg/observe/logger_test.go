package observe

import (
	"context"
	"testing"
)

func TestNewLogger(t *testing.T) {
	// Should not panic for any level/format combination
	for _, level := range []string{"debug", "info", "warn", "error", "invalid"} {
		for _, format := range []string{"json", "text", "invalid"} {
			l := NewLogger(level, format)
			if l == nil {
				t.Errorf("NewLogger(%q, %q) returned nil", level, format)
			}
		}
	}
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	ctx = CtxWithRequestID(ctx, "req-123")
	ctx = CtxWithUserID(ctx, "user-456")
	ctx = CtxWithTraceID(ctx, "trace-789")

	if v := RequestIDFromCtx(ctx); v != "req-123" {
		t.Errorf("RequestID: expected req-123, got %s", v)
	}
	if v := UserIDFromCtx(ctx); v != "user-456" {
		t.Errorf("UserID: expected user-456, got %s", v)
	}
	if v := TraceIDFromCtx(ctx); v != "trace-789" {
		t.Errorf("TraceID: expected trace-789, got %s", v)
	}
}

func TestContextHelpers_Empty(t *testing.T) {
	ctx := context.Background()
	if v := RequestIDFromCtx(ctx); v != "" {
		t.Errorf("expected empty, got %s", v)
	}
	if v := UserIDFromCtx(ctx); v != "" {
		t.Errorf("expected empty, got %s", v)
	}
	if v := TraceIDFromCtx(ctx); v != "" {
		t.Errorf("expected empty, got %s", v)
	}
}

func TestWithContext(t *testing.T) {
	logger := NewLogger("info", "json")
	ctx := CtxWithRequestID(context.Background(), "req-1")
	enriched := WithContext(ctx, logger)
	if enriched == nil {
		t.Error("WithContext returned nil")
	}
}
