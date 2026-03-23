package errors

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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

// WriteError writes an error as a JSON response. If the error is a *DomainError,
// its status code and fields are used. Otherwise a generic 500 is returned.
// Server errors (5xx) are logged at error level; client errors (4xx) at debug.
func WriteError(w http.ResponseWriter, err error, logger *slog.Logger) {
	var domErr *DomainError
	if !errors.As(err, &domErr) {
		domErr = &DomainError{
			Code:       "INTERNAL_ERROR",
			Message:    "an unexpected error occurred",
			StatusCode: http.StatusInternalServerError,
		}
	}

	if logger != nil {
		if domErr.StatusCode >= 500 {
			logger.Error("server error", "code", domErr.Code, "message", domErr.Message, "status", domErr.StatusCode)
		} else {
			logger.Debug("client error", "code", domErr.Code, "message", domErr.Message, "status", domErr.StatusCode)
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
