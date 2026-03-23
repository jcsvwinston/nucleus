// Package errors provides domain-specific error types for the GoFrame framework.
// It defines a DomainError type with HTTP status codes and JSON serialization,
// along with convenience constructors for common error cases.
package errors

import (
	"fmt"
	"net/http"
)

// DomainError represents a structured application error with an HTTP status code,
// a machine-readable code, a human-readable message, and optional details.
type DomainError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	StatusCode int    `json:"-"`
	Details    any    `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *DomainError) Error() string {
	return e.Message
}

// WithDetails returns a copy of the error with the given details attached.
func (e *DomainError) WithDetails(details any) *DomainError {
	return &DomainError{
		Code:       e.Code,
		Message:    e.Message,
		StatusCode: e.StatusCode,
		Details:    details,
	}
}

// NotFound creates a 404 error indicating a resource was not found.
func NotFound(resource, id string) *DomainError {
	return &DomainError{
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("%s '%s' not found", resource, id),
		StatusCode: http.StatusNotFound,
	}
}

// BadRequest creates a 400 error for malformed or invalid requests.
func BadRequest(message string) *DomainError {
	return &DomainError{
		Code:       "BAD_REQUEST",
		Message:    message,
		StatusCode: http.StatusBadRequest,
	}
}

// Unauthorized creates a 401 error for unauthenticated requests.
func Unauthorized(message string) *DomainError {
	return &DomainError{
		Code:       "UNAUTHORIZED",
		Message:    message,
		StatusCode: http.StatusUnauthorized,
	}
}

// Forbidden creates a 403 error for unauthorized access attempts.
func Forbidden(message string) *DomainError {
	return &DomainError{
		Code:       "FORBIDDEN",
		Message:    message,
		StatusCode: http.StatusForbidden,
	}
}

// Conflict creates a 409 error for resource conflicts.
func Conflict(message string) *DomainError {
	return &DomainError{
		Code:       "CONFLICT",
		Message:    message,
		StatusCode: http.StatusConflict,
	}
}

// InternalError creates a 500 error for unexpected server errors.
func InternalError(message string) *DomainError {
	return &DomainError{
		Code:       "INTERNAL_ERROR",
		Message:    message,
		StatusCode: http.StatusInternalServerError,
	}
}

// ValidationFailed creates a 422 error with per-field validation details.
func ValidationFailed(fields map[string]string) *DomainError {
	return &DomainError{
		Code:       "VALIDATION_FAILED",
		Message:    "validation failed",
		StatusCode: http.StatusUnprocessableEntity,
		Details:    fields,
	}
}
