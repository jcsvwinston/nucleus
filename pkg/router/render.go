package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/validate"
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
func Error(w http.ResponseWriter, r *http.Request, err error, logger ...*slog.Logger) {
	var l *slog.Logger
	if len(logger) > 0 {
		l = logger[0]
	}
	gferrors.WriteError(w, r, err, l)
}

// NoContent writes a 204 No Content response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Created writes a 201 Created response with the given JSON data.
func Created(w http.ResponseWriter, data interface{}) {
	JSON(w, http.StatusCreated, data)
}

// maxJSONBodyBytes caps the request body Bind is willing to read. JSON
// bind targets are scalar-field structs, not bulk payloads, so the limit
// is deliberately tight — an unbounded json.Decoder would buffer an
// attacker-sized body into memory before validation ever runs. Callers
// with a legitimately larger payload use BindMax with an explicit limit.
const maxJSONBodyBytes = 1 << 20 // 1 MiB

// Bind decodes the request body as JSON into v, then validates it using
// struct validate tags. Returns a *DomainError if decoding or validation fails.
// Bodies are capped at 1 MiB (413 beyond it); use BindMax to raise the cap for
// endpoints that legitimately accept larger payloads.
//
// WARNING — unlike BindForm, Bind applies no mass-assignment guard: a client
// can set any json-exposed field, including server-owned ones such as
// model.BaseModel's id/created_at/updated_at. encoding/json offers no
// per-field skip-on-decode without also hiding the field from responses, so
// the guard cannot be applied transparently here. Bind JSON onto a dedicated
// input type that omits server-owned fields, or zero those fields after
// decoding, when the target embeds a persistence model.
func Bind(r *http.Request, v interface{}) error {
	return BindMax(r, v, maxJSONBodyBytes)
}

// BindMax is Bind with a caller-chosen body cap in bytes. maxBytes <= 0
// falls back to the default 1 MiB cap — an accidental zero must never
// mean "unlimited".
func BindMax(r *http.Request, v interface{}, maxBytes int64) error {
	if r == nil {
		return ErrNilContextRequest
	}
	if r.Body == nil {
		return gferrors.BadRequest("request body is empty")
	}
	if maxBytes <= 0 {
		maxBytes = maxJSONBodyBytes
	}
	// nil ResponseWriter: MaxBytesReader only uses it to flag the
	// connection for closure; the error return below is what matters here
	// (same pattern as BindForm).
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &gferrors.DomainError{
				Code:       "PAYLOAD_TOO_LARGE",
				Message:    fmt.Sprintf("request body exceeds %d bytes", maxErr.Limit),
				StatusCode: http.StatusRequestEntityTooLarge,
			}
		}
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
