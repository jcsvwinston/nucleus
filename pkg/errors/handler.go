package errors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// ErrorResponse is the JSON envelope returned for all errors.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody holds the structured error fields.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// ErrorHandlerConfig configures the error handler behavior.
type ErrorHandlerConfig struct {
	// GlobalContext is a function that returns context data to include in all error logs.
	GlobalContext func(ctx context.Context) map[string]any

	// LogLevelMap allows configuring log levels for specific error types.
	LogLevelMap map[error]slog.Level

	// IgnoredErrors are error types that should not be reported (logged).
	IgnoredErrors []error

	// ThrottleConfig configures throttling of error reporting.
	ThrottleConfig *ThrottleConfig
}

// ThrottleConfig configures throttling of error reporting.
type ThrottleConfig struct {
	// SampleRate is the fraction of errors to log (0.0 to 1.0).
	// 0.1 means log 10% of errors.
	SampleRate float64

	// RateLimit is the maximum number of errors to log per duration.
	RateLimit int
	Duration  time.Duration

	// KeyFunc determines the throttling key for an error.
	// If nil, uses error type as key.
	KeyFunc func(error) string
}

// ErrorHandler handles error reporting and rendering with Laravel-style separation.
type ErrorHandler struct {
	config      *ErrorHandlerConfig
	logger      *slog.Logger
	throttleMap map[string]*throttleEntry
	throttleMu  sync.RWMutex
}

type throttleEntry struct {
	count      int
	resetTime  time.Time
	lastLogged time.Time
}

// NewErrorHandler creates a new ErrorHandler with the given configuration.
func NewErrorHandler(logger *slog.Logger, config *ErrorHandlerConfig) *ErrorHandler {
	if config == nil {
		config = &ErrorHandlerConfig{}
	}
	return &ErrorHandler{
		config:      config,
		logger:      logger,
		throttleMap: make(map[string]*throttleEntry),
	}
}

// Report handles error reporting (logging, sending to external services).
// It respects Reportable interface, log levels, ignored errors, and throttling.
func (h *ErrorHandler) Report(ctx context.Context, err error) {
	if h.logger == nil {
		return
	}

	// Check if error should be ignored
	if h.shouldIgnore(err) {
		return
	}

	// Check throttling
	if h.shouldThrottle(err) {
		return
	}

	// Use custom reporting if error implements Reportable
	if reportable, ok := err.(Reportable); ok {
		if reportable.Report(ctx, h.logger) {
			return // Custom reporting handled it
		}
	}

	// Default reporting
	h.defaultReport(ctx, err)
}

// Render handles error rendering to HTTP response.
// It respects Renderable interface.
func (h *ErrorHandler) Render(w http.ResponseWriter, r *http.Request, err error) {
	// Use custom rendering if error implements Renderable
	if renderable, ok := err.(Renderable); ok {
		if renderable.Render(w, r) {
			return // Custom rendering handled it
		}
	}

	// Default rendering
	h.defaultRender(w, r, err)
}

// shouldIgnore checks if an error type should be ignored.
func (h *ErrorHandler) shouldIgnore(err error) bool {
	for _, ignored := range h.config.IgnoredErrors {
		if errors.Is(err, ignored) {
			return true
		}
	}
	return false
}

// shouldThrottle checks if an error should be throttled.
func (h *ErrorHandler) shouldThrottle(err error) bool {
	if h.config.ThrottleConfig == nil {
		return false
	}

	cfg := h.config.ThrottleConfig

	// Sample rate throttling: log only that fraction of errors (NU-11 —
	// it used to be documented and inert).
	if cfg.SampleRate > 0 && cfg.SampleRate < 1.0 {
		if rand.Float64() >= cfg.SampleRate {
			return true
		}
	}

	// Rate limit throttling
	if cfg.RateLimit > 0 {
		key := h.getThrottleKey(err)
		h.throttleMu.Lock()
		defer h.throttleMu.Unlock()

		now := time.Now()
		// The map is keyed by error type, so it is bounded by the number of
		// error types — unless a KeyFunc derives keys from messages. Sweep
		// expired entries once it grows, so a chatty key set cannot make
		// it grow without bound.
		if len(h.throttleMap) > throttleSweepThreshold {
			for k, e := range h.throttleMap {
				if now.After(e.resetTime) {
					delete(h.throttleMap, k)
				}
			}
		}

		entry, exists := h.throttleMap[key]

		if !exists || now.After(entry.resetTime) {
			h.throttleMap[key] = &throttleEntry{
				count:     1,
				resetTime: now.Add(cfg.Duration),
			}
			return false
		}

		if entry.count >= cfg.RateLimit {
			return true // Throttled
		}

		entry.count++
		return false
	}

	return false
}

// getThrottleKey returns the throttling key for an error.
func (h *ErrorHandler) getThrottleKey(err error) string {
	if h.config.ThrottleConfig.KeyFunc != nil {
		return h.config.ThrottleConfig.KeyFunc(err)
	}
	// Default to the error TYPE — a DomainError by its code — not the
	// message: a message that embeds an id or a path made a new key per
	// occurrence, so nothing was ever throttled and the map only grew.
	if err == nil {
		return "nil"
	}
	var domErr *DomainError
	if errors.As(err, &domErr) && domErr.Code != "" {
		return "domain:" + domErr.Code
	}
	return fmt.Sprintf("%T", err)
}

// throttleSweepThreshold is the map size past which expired throttle
// entries are swept on the next report.
const throttleSweepThreshold = 256

// defaultReport performs default error logging.
func (h *ErrorHandler) defaultReport(ctx context.Context, err error) {
	var domErr *DomainError
	if !errors.As(err, &domErr) {
		domErr = &DomainError{
			Code:       "INTERNAL_ERROR",
			Message:    "an unexpected error occurred",
			StatusCode: http.StatusInternalServerError,
		}
	}

	// Determine log level
	level := h.getLogLevel(domErr)

	// Build log attributes
	attrs := []any{
		"code", domErr.Code,
		"message", domErr.Message,
		"status", domErr.StatusCode,
	}

	// Add global context
	if h.config.GlobalContext != nil {
		if ctxData := h.config.GlobalContext(ctx); len(ctxData) > 0 {
			for k, v := range ctxData {
				attrs = append(attrs, k, v)
			}
		}
	}

	// Add error-specific context
	if ctxProvider, ok := err.(ContextProvider); ok {
		if errCtx := ctxProvider.Context(); len(errCtx) > 0 {
			for k, v := range errCtx {
				attrs = append(attrs, k, v)
			}
		}
	}

	// Log at appropriate level
	switch level {
	case slog.LevelDebug:
		h.logger.Debug("client error", attrs...)
	case slog.LevelInfo:
		h.logger.Info("info", attrs...)
	case slog.LevelWarn:
		h.logger.Warn("warning", attrs...)
	case slog.LevelError:
		h.logger.Error("server error", attrs...)
	default:
		h.logger.Log(ctx, level, "error", attrs...)
	}
}

// defaultRender performs default JSON error rendering.
func (h *ErrorHandler) defaultRender(w http.ResponseWriter, _ *http.Request, err error) {
	var domErr *DomainError
	if !errors.As(err, &domErr) {
		domErr = &DomainError{
			Code:       "INTERNAL_ERROR",
			Message:    "an unexpected error occurred",
			StatusCode: http.StatusInternalServerError,
		}
	}

	resp := ErrorResponse{
		Error: ErrorBody{
			Code:    domErr.Code,
			Message: domErr.Message,
			Details: domErr.Details,
		},
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(domErr.StatusCode)
	json.NewEncoder(w).Encode(resp)
}

// getLogLevel returns the log level for an error.
func (h *ErrorHandler) getLogLevel(err error) slog.Level {
	// Check if error implements LogLevelProvider
	if levelProvider, ok := err.(LogLevelProvider); ok {
		return levelProvider.LogLevel()
	}

	// Check configured log level map
	if h.config.LogLevelMap != nil {
		if level, ok := h.config.LogLevelMap[err]; ok {
			return level
		}
	}

	// Default: 5xx = error, 4xx = debug
	if domErr, ok := err.(*DomainError); ok {
		if domErr.StatusCode >= 500 {
			return slog.LevelError
		}
		return slog.LevelDebug
	}

	return slog.LevelError
}

// WriteError writes an error as a JSON response using the default handler.
// This is a convenience function that creates a default handler and uses it.
// For advanced configuration, use ErrorHandler directly.
func WriteError(w http.ResponseWriter, r *http.Request, err error, logger *slog.Logger) {
	handler := NewErrorHandler(logger, nil)
	handler.Report(r.Context(), err)
	handler.Render(w, r, err)
}
