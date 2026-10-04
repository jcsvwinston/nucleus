// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/httpneg"
)

// ProblemContentType is the media type of an RFC 9457 problem details
// document.
const ProblemContentType = httpneg.Problem

// Problem is an error as an RFC 9457 problem details document — the second
// error shape, beside the envelope {"error": {"code", "message",
// "details"}} (ErrorResponse) every error has answered in since v0.
//
// A client gets it when it asks for it — an Accept header that prefers
// application/problem+json to application/json — or for every error when
// the application opted in (app.WithProblemDetails, the builder's
// WithProblemDetails, or router.WithProblemDetails on a bare router).
// Otherwise the envelope answers, unchanged; it stays the default until the
// next major.
//
// The standard members carry the HTTP view of the error: Type is
// "about:blank" (RFC 9457 §4.2.1: the status code says all there is), Title
// the status phrase, Status the code, Detail the error's message and
// Instance the request path. Two extension members carry what the envelope
// carries and the standard members cannot: Code, the framework's
// machine-readable code ("NOT_FOUND", "VALIDATION_FAILED", …), and Details,
// the error's details — for a validation failure, the message per field.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code,omitempty"`
	Details  any    `json:"details,omitempty"`
}

// NewProblem builds the problem details document for err answered to r. A
// *DomainError anywhere in err's chain gives its status, code, message and
// details; any other error is a 500 whose detail is the generic message
// the envelope uses, never err.Error(). r may be nil (no Instance).
func NewProblem(r *http.Request, err error) Problem {
	var de *DomainError
	if !errors.As(err, &de) || de == nil {
		de = &DomainError{
			Code:       "INTERNAL_ERROR",
			Message:    "an unexpected error occurred",
			StatusCode: http.StatusInternalServerError,
		}
	}
	status := de.StatusCode
	if status < 100 || status > 999 {
		status = http.StatusInternalServerError
	}
	title := http.StatusText(status)
	if title == "" {
		title = "Error"
	}
	return Problem{
		Type:     "about:blank",
		Title:    title,
		Status:   status,
		Detail:   de.Message,
		Instance: requestPath(r),
		Code:     de.Code,
		Details:  de.Details,
	}
}

// WriteProblem writes err as a problem details document (see NewProblem),
// whatever the client's Accept header says; it does not log. The
// Content-Type is application/problem+json, or application/json for a
// client that accepts that and not the problem type.
//
// Most code never calls it: a handler returns its error and the router
// answers in the shape the client and the application chose, and
// WriteError — the writer middleware uses — makes the same choice.
func WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	p := NewProblem(r, err)
	w.Header().Set("Content-Type", httpneg.ProblemContentType(r)+"; charset=utf-8")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// requestPath is the path of the request as the client sent it: the
// request-target without its query. A sub-router mounted under a prefix
// sees its URL.Path stripped of that prefix; RequestURI keeps it.
func requestPath(r *http.Request) string {
	if r == nil {
		return ""
	}
	if uri := r.RequestURI; uri != "" && !strings.HasPrefix(uri, "*") {
		if i := strings.IndexAny(uri, "?#"); i >= 0 {
			uri = uri[:i]
		}
		// An absolute-form request-target (a proxy request) carries the
		// scheme and host; the path is what identifies the occurrence.
		if strings.Contains(uri, "://") {
			if r.URL != nil {
				return r.URL.Path
			}
			return ""
		}
		return uri
	}
	if r.URL != nil {
		return r.URL.Path
	}
	return ""
}
