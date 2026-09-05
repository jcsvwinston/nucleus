package errors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotFound(t *testing.T) {
	err := NotFound("User", "123")
	if err.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %s", err.Code)
	}
	if err.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", err.StatusCode)
	}
	if err.Error() != "User '123' not found" {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestBadRequest(t *testing.T) {
	err := BadRequest("invalid input")
	if err.Code != "BAD_REQUEST" || err.StatusCode != 400 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("token expired")
	if err.Code != "UNAUTHORIZED" || err.StatusCode != 401 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestForbidden(t *testing.T) {
	err := Forbidden("access denied")
	if err.Code != "FORBIDDEN" || err.StatusCode != 403 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestConflict(t *testing.T) {
	err := Conflict("already exists")
	if err.Code != "CONFLICT" || err.StatusCode != 409 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestInternalError(t *testing.T) {
	err := InternalError("something broke")
	if err.Code != "INTERNAL_ERROR" || err.StatusCode != 500 {
		t.Errorf("unexpected: %+v", err)
	}
}

func TestValidationFailed(t *testing.T) {
	fields := map[string]string{"email": "required"}
	err := ValidationFailed(fields)
	if err.Code != "VALIDATION_FAILED" || err.StatusCode != 422 {
		t.Errorf("unexpected: %+v", err)
	}
	details, ok := err.Details.(map[string]string)
	if !ok || details["email"] != "required" {
		t.Errorf("unexpected details: %v", err.Details)
	}
}

func TestErrorsAs(t *testing.T) {
	err := NotFound("User", "1")
	var domErr *DomainError
	if !errors.As(err, &domErr) {
		t.Error("errors.As should match *DomainError")
	}
}

func TestWithDetails(t *testing.T) {
	err := BadRequest("bad").WithDetails(map[string]string{"key": "val"})
	if err.Details == nil {
		t.Error("expected details")
	}
}

func TestWriteError_DomainError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	err := NotFound("User", "42")
	WriteError(w, r, err, nil)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %s", resp.Error.Code)
	}
}

func TestWriteError_GenericError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)
	WriteError(w, r, errors.New("oops"), nil)

	if w.Code != 500 {
		t.Errorf("expected 500, got %d", w.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", resp.Error.Code)
	}
	// Should not leak internal error message
	if resp.Error.Message == "oops" {
		t.Error("should not leak internal error message")
	}
}

func TestDomainError_WithDetails(t *testing.T) {
	err := NotFound("User", "123")
	details := map[string]string{"field": "value"}

	errWithDetails := err.WithDetails(details)

	if errWithDetails.Code != "NOT_FOUND" {
		t.Errorf("expected code to remain NOT_FOUND, got %s", errWithDetails.Code)
	}
	if errWithDetails.Details == nil {
		t.Error("expected details to be set")
	}

	// Original error should not have details
	if err.Details != nil {
		t.Error("original error should not have details")
	}
}

func TestDomainError_Report(t *testing.T) {
	err := NotFound("User", "123")
	// By default, DomainError.Report returns false to use default logging
	if err.Report(nil, nil) {
		t.Error("expected Report to return false")
	}
}

func TestDomainError_Render(t *testing.T) {
	err := NotFound("User", "123")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	// By default, DomainError.Render returns false to use default rendering
	if err.Render(w, r) {
		t.Error("expected Render to return false")
	}
}

func TestDomainError_Context(t *testing.T) {
	t.Run("nil details", func(t *testing.T) {
		err := NotFound("User", "123")
		ctx := err.Context()
		if ctx != nil {
			t.Error("expected nil context when details is nil")
		}
	})

	t.Run("map details", func(t *testing.T) {
		details := map[string]any{"key": "value"}
		err := BadRequest("test").WithDetails(details)
		ctx := err.Context()
		if ctx == nil {
			t.Error("expected non-nil context")
		}
		if ctx["key"] != "value" {
			t.Errorf("expected key=value, got %v", ctx)
		}
	})

	t.Run("non-map details", func(t *testing.T) {
		details := "string details"
		err := BadRequest("test").WithDetails(details)
		ctx := err.Context()
		if ctx == nil {
			t.Error("expected non-nil context")
		}
		if ctx["details"] != "string details" {
			t.Errorf("expected details=string details, got %v", ctx)
		}
	})
}

func TestDomainError_LogLevel(t *testing.T) {
	t.Run("5xx status code", func(t *testing.T) {
		err := InternalError("server error")
		level := err.LogLevel()
		if level != slog.LevelError {
			t.Errorf("expected error level, got %v", level)
		}
	})

	t.Run("4xx status code", func(t *testing.T) {
		err := NotFound("User", "123")
		level := err.LogLevel()
		if level != slog.LevelDebug {
			t.Errorf("expected debug level, got %v", level)
		}
	})

	t.Run("422 status code", func(t *testing.T) {
		err := ValidationFailed(map[string]string{"field": "required"})
		level := err.LogLevel()
		if level != slog.LevelDebug {
			t.Errorf("expected debug level, got %v", level)
		}
	})
}

func TestNewErrorHandler(t *testing.T) {
	t.Run("with config", func(t *testing.T) {
		config := &ErrorHandlerConfig{
			GlobalContext: func(ctx context.Context) map[string]any {
				return map[string]any{"request_id": "123"}
			},
		}
		handler := NewErrorHandler(nil, config)
		if handler.config != config {
			t.Error("expected config to be set")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		handler := NewErrorHandler(nil, nil)
		if handler.config == nil {
			t.Error("expected default config to be created")
		}
	})
}

func TestErrorHandler_Report(t *testing.T) {
	t.Run("nil logger", func(t *testing.T) {
		handler := NewErrorHandler(nil, nil)
		handler.Report(context.Background(), errors.New("test"))
		// Should not panic
	})

	t.Run("ignored error", func(t *testing.T) {
		ignoredErr := errors.New("ignored")
		config := &ErrorHandlerConfig{
			IgnoredErrors: []error{ignoredErr},
		}
		handler := NewErrorHandler(nil, config)
		handler.Report(context.Background(), ignoredErr)
		// Should not panic
	})

	t.Run("custom reportable", func(t *testing.T) {
		customErr := &customReportableError{handled: true}
		handler := NewErrorHandler(nil, nil)
		handler.Report(context.Background(), customErr)
		// Should not panic
	})

	t.Run("default report", func(t *testing.T) {
		handler := NewErrorHandler(nil, nil)
		err := NotFound("User", "123")
		handler.Report(context.Background(), err)
		// Should not panic
	})
}

func TestErrorHandler_Render(t *testing.T) {
	t.Run("custom renderable", func(t *testing.T) {
		customErr := &customRenderableError{handled: true}
		handler := NewErrorHandler(nil, nil)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/test", nil)
		handler.Render(w, r, customErr)
		// Should not panic
	})

	t.Run("default render", func(t *testing.T) {
		handler := NewErrorHandler(nil, nil)
		err := NotFound("User", "123")
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/test", nil)
		handler.Render(w, r, err)

		if w.Code != 404 {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}

func TestErrorHandler_shouldIgnore(t *testing.T) {
	t.Run("matching ignored error", func(t *testing.T) {
		ignoredErr := errors.New("ignored")
		config := &ErrorHandlerConfig{
			IgnoredErrors: []error{ignoredErr},
		}
		handler := NewErrorHandler(nil, config)
		if !handler.shouldIgnore(ignoredErr) {
			t.Error("expected error to be ignored")
		}
	})

	t.Run("non-matching error", func(t *testing.T) {
		ignoredErr := errors.New("ignored")
		config := &ErrorHandlerConfig{
			IgnoredErrors: []error{ignoredErr},
		}
		handler := NewErrorHandler(nil, config)
		if handler.shouldIgnore(errors.New("other")) {
			t.Error("expected error to not be ignored")
		}
	})

	t.Run("no ignored errors", func(t *testing.T) {
		handler := NewErrorHandler(nil, nil)
		if handler.shouldIgnore(errors.New("test")) {
			t.Error("expected error to not be ignored")
		}
	})
}

func TestErrorHandler_shouldThrottle(t *testing.T) {
	t.Run("no throttle config", func(t *testing.T) {
		handler := NewErrorHandler(nil, nil)
		if handler.shouldThrottle(errors.New("test")) {
			t.Error("expected no throttling without config")
		}
	})

	t.Run("rate limit", func(t *testing.T) {
		config := &ErrorHandlerConfig{
			ThrottleConfig: &ThrottleConfig{
				RateLimit: 2,
				Duration:  time.Hour,
			},
		}
		handler := NewErrorHandler(nil, config)
		err := errors.New("test")

		// First two should not be throttled
		if handler.shouldThrottle(err) {
			t.Error("first error should not be throttled")
		}
		if handler.shouldThrottle(err) {
			t.Error("second error should not be throttled")
		}
		// Third should be throttled
		if !handler.shouldThrottle(err) {
			t.Error("third error should be throttled")
		}
	})

	t.Run("sample rate samples", func(t *testing.T) {
		// NU-11: SampleRate used to be documented and inert.
		config := &ErrorHandlerConfig{
			ThrottleConfig: &ThrottleConfig{
				SampleRate: 0.5,
			},
		}
		handler := NewErrorHandler(nil, config)
		throttled := 0
		for i := 0; i < 400; i++ {
			if handler.shouldThrottle(errors.New("test")) {
				throttled++
			}
		}
		if throttled == 0 || throttled == 400 {
			t.Errorf("sample rate 0.5 throttled %d of 400: not sampling", throttled)
		}
	})
}

func TestErrorHandler_getThrottleKey(t *testing.T) {
	t.Run("custom key func", func(t *testing.T) {
		config := &ErrorHandlerConfig{
			ThrottleConfig: &ThrottleConfig{
				KeyFunc: func(err error) string {
					return "custom-key"
				},
			},
		}
		handler := NewErrorHandler(nil, config)
		key := handler.getThrottleKey(errors.New("test"))
		if key != "custom-key" {
			t.Errorf("expected custom-key, got %s", key)
		}
	})

	t.Run("default key", func(t *testing.T) {
		config := &ErrorHandlerConfig{
			ThrottleConfig: &ThrottleConfig{},
		}
		handler := NewErrorHandler(nil, config)
		// NU-11: the default key is the error TYPE, not its message — a
		// message that carries an id made every occurrence a new key.
		err := errors.New("test error")
		key := handler.getThrottleKey(err)
		if key != "*errors.errorString" {
			t.Errorf("expected the error type as key, got %s", key)
		}
		dom := &DomainError{Code: "NOT_FOUND"}
		if key := handler.getThrottleKey(dom); key != "domain:NOT_FOUND" {
			t.Errorf("expected the domain code as key, got %s", key)
		}
	})
}

func TestErrorHandler_getLogLevel(t *testing.T) {
	t.Run("LogLevelProvider", func(t *testing.T) {
		customErr := &customLogLevelError{level: slog.LevelInfo}
		handler := NewErrorHandler(nil, nil)
		level := handler.getLogLevel(customErr)
		if level != slog.LevelInfo {
			t.Errorf("expected info level, got %v", level)
		}
	})

	t.Run("LogLevelMap", func(t *testing.T) {
		customErr := &customContextError{}
		config := &ErrorHandlerConfig{
			LogLevelMap: map[error]slog.Level{customErr: slog.LevelInfo},
		}
		handler := NewErrorHandler(nil, config)
		level := handler.getLogLevel(customErr)
		if level != slog.LevelInfo {
			t.Errorf("expected info level, got %v", level)
		}
	})

	t.Run("DomainError 5xx", func(t *testing.T) {
		err := InternalError("test")
		handler := NewErrorHandler(nil, nil)
		level := handler.getLogLevel(err)
		if level != slog.LevelError {
			t.Errorf("expected error level, got %v", level)
		}
	})

	t.Run("DomainError 4xx", func(t *testing.T) {
		err := NotFound("User", "123")
		handler := NewErrorHandler(nil, nil)
		level := handler.getLogLevel(err)
		if level != slog.LevelDebug {
			t.Errorf("expected debug level, got %v", level)
		}
	})

	t.Run("generic error", func(t *testing.T) {
		err := errors.New("generic")
		handler := NewErrorHandler(nil, nil)
		level := handler.getLogLevel(err)
		if level != slog.LevelError {
			t.Errorf("expected error level, got %v", level)
		}
	})
}

func TestErrorHandler_defaultReport(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("with global context", func(t *testing.T) {
		config := &ErrorHandlerConfig{
			GlobalContext: func(ctx context.Context) map[string]any {
				return map[string]any{"request_id": "123"}
			},
		}
		handler := NewErrorHandler(logger, config)
		handler.defaultReport(context.Background(), NotFound("User", "123"))
		// Should not panic
	})

	t.Run("with error context", func(t *testing.T) {
		handler := NewErrorHandler(logger, nil)
		err := &customContextError{context: map[string]any{"custom": "value"}}
		handler.defaultReport(context.Background(), err)
		// Should not panic
	})

	t.Run("generic error", func(t *testing.T) {
		handler := NewErrorHandler(logger, nil)
		handler.defaultReport(context.Background(), errors.New("generic"))
		// Should not panic
	})
}

func TestErrorHandler_defaultRender(t *testing.T) {
	t.Run("generic error", func(t *testing.T) {
		handler := NewErrorHandler(nil, nil)
		w := httptest.NewRecorder()
		handler.defaultRender(w, httptest.NewRequest("GET", "/test", nil), errors.New("generic"))

		if w.Code != 500 {
			t.Errorf("expected 500, got %d", w.Code)
		}

		var resp ErrorResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Error.Code != "INTERNAL_ERROR" {
			t.Errorf("expected INTERNAL_ERROR, got %s", resp.Error.Code)
		}
	})
}

// Custom error types for testing interfaces

type customReportableError struct {
	handled bool
}

func (e *customReportableError) Error() string {
	return "custom reportable"
}

func (e *customReportableError) Report(ctx context.Context, logger *slog.Logger) bool {
	return e.handled
}

type customRenderableError struct {
	handled bool
}

func (e *customRenderableError) Error() string {
	return "custom renderable"
}

func (e *customRenderableError) Render(w http.ResponseWriter, r *http.Request) bool {
	w.WriteHeader(http.StatusTeapot)
	w.Write([]byte("custom render"))
	return e.handled
}

type customLogLevelError struct {
	level slog.Level
}

func (e *customLogLevelError) Error() string {
	return "custom log level"
}

func (e *customLogLevelError) LogLevel() slog.Level {
	return e.level
}

type customContextError struct {
	context map[string]any
}

func (e *customContextError) Error() string {
	return "custom context"
}

func (e *customContextError) Context() map[string]any {
	return e.context
}
