package router

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/validate"
)

// JSON writes a JSON response with the given status code and data.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

// Error writes an error as a structured JSON response. If the error is a
// *DomainError, its status code and details are used; otherwise a generic 500
// is returned.
func Error(w http.ResponseWriter, err error, logger ...*slog.Logger) {
	var l *slog.Logger
	if len(logger) > 0 {
		l = logger[0]
	}
	gferrors.WriteError(w, err, l)
}

// NoContent writes a 204 No Content response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Created writes a 201 Created response with the given JSON data.
func Created(w http.ResponseWriter, data interface{}) {
	JSON(w, http.StatusCreated, data)
}

// Bind decodes the request body as JSON into v, then validates it using
// struct validate tags. Returns a *DomainError if decoding or validation fails.
func Bind(r *http.Request, v interface{}) error {
	if r.Body == nil {
		return gferrors.BadRequest("request body is empty")
	}

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return gferrors.BadRequest("invalid JSON: " + err.Error())
	}

	if err := validate.Validate(v); err != nil {
		var domErr *gferrors.DomainError
		if errors.As(err, &domErr) {
			return domErr
		}
		return gferrors.BadRequest(err.Error())
	}

	return nil
}

// Paginate extracts page and page_size from query parameters with defaults
// and bounds. page defaults to 1, page_size to defaultSize (max 100).
func Paginate(r *http.Request, defaultSize int) (page, pageSize int) {
	page = 1
	pageSize = defaultSize

	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}

	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			pageSize = v
		}
	}

	if pageSize > 100 {
		pageSize = 100
	}

	return page, pageSize
}
