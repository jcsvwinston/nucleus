// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/httpneg"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

// One shape for errors. Everything the router answers as an error — a
// handler's DomainError, a binding failure, the router's own 404 and 405,
// the request timeout, the CSRF middleware's and the rate limiter's
// refusals — is written by writeDomainError, so a client reads every error
// by the same names:
//
//   - the envelope, {"error": {"code", "message", "details"}}, by default;
//   - an RFC 9457 problem details document (gferrors.Problem) when the
//     client prefers application/problem+json, or for every request when
//     the application opted in (WithProblemDetails).
//
// The envelope stays the default until the next major: changing what an
// existing client parses is a breaking change, so problem details arrive
// beside it instead of in its place.

// WithProblemDetails makes problem details (RFC 9457,
// application/problem+json) the shape of every error the router answers —
// handler errors, binding failures, the router's own 404 and 405, the
// timeout, the CSRF and rate-limit refusals — and of every error written
// through errors.WriteError on its requests (the authorizer's 403, the
// bearer middleware's 401). Without it the envelope answers unless the
// client prefers application/problem+json.
// app.New sets it from app.WithProblemDetails.
func WithProblemDetails(enabled bool) Option {
	return func(o *routerOpts) {
		o.problemDetails = enabled
	}
}

// writeDomainError answers de in the shape the client and the application
// chose (see the comment at the top of this file). It does not log.
func writeDomainError(w http.ResponseWriter, r *http.Request, de *gferrors.DomainError) {
	if de.StatusCode < 100 || de.StatusCode > 999 {
		cp := *de
		cp.StatusCode = http.StatusInternalServerError
		de = &cp
	}
	gferrors.NewErrorHandler(nil, nil).Render(w, r, de)
}

// codeForStatus is the machine-readable code an error that carries only a
// status (an *HTTPError) gets in a problem details document: the status
// phrase in upper snake case, "BAD_REQUEST" for 400.
func codeForStatus(status int) string {
	text := http.StatusText(status)
	if text == "" {
		return fmt.Sprintf("HTTP_%d", status)
	}
	var b strings.Builder
	for _, r := range strings.ToUpper(text) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteByte('_')
		}
	}
	return b.String()
}

// missRecorder captures what http.ServeMux answers for a request no pattern
// serves — the status (404, or 405 for a path registered under other
// methods) and the Allow header — without letting its plain-text body reach
// the client.
type missRecorder struct {
	header http.Header
	status int
}

func (m *missRecorder) Header() http.Header { return m.header }
func (m *missRecorder) WriteHeader(status int) {
	if m.status == 0 {
		m.status = status
	}
}
func (m *missRecorder) Write(b []byte) (int, error) {
	if m.status == 0 {
		m.status = http.StatusOK
	}
	return len(b), nil
}

// maxEchoedPath bounds how much of the request path a router error repeats
// back: it is the client's own input.
const maxEchoedPath = 200

// serveMiss answers a request the ServeMux has no pattern for, given the
// handler the mux itself would run (its 404, or its 405). A client that
// prefers JSON gets the framework's error shape; anybody else — a browser,
// a client that sends */* or no Accept at all — gets exactly what the mux
// answers, Go's plain text, as before.
func serveMiss(w http.ResponseWriter, r *http.Request, miss http.Handler) {
	if !httpneg.WantsJSONError(r) {
		miss.ServeHTTP(w, r)
		return
	}
	rec := &missRecorder{header: http.Header{}}
	miss.ServeHTTP(rec, r)
	path := r.URL.Path
	if p := mountPrefix(r); p != "" {
		path = p + path
	}
	if len(path) > maxEchoedPath {
		path = path[:maxEchoedPath] + "…"
	}
	var de *gferrors.DomainError
	switch rec.status {
	case http.StatusNotFound:
		de = &gferrors.DomainError{
			Code:       "NOT_FOUND",
			Message:    fmt.Sprintf("no route serves %s %s", r.Method, path),
			StatusCode: http.StatusNotFound,
		}
	case http.StatusMethodNotAllowed:
		allow := rec.header.Values("Allow")
		for _, v := range allow {
			w.Header().Add("Allow", v)
		}
		de = &gferrors.DomainError{
			Code:       "METHOD_NOT_ALLOWED",
			Message:    fmt.Sprintf("%s is not allowed on %s", r.Method, path),
			StatusCode: http.StatusMethodNotAllowed,
		}
		if methods := splitAllow(allow); len(methods) > 0 {
			de.Details = map[string]any{"allow": methods}
		}
	default:
		// Not a miss the mux reports in a way this function knows: let the
		// mux answer it as it always has.
		miss.ServeHTTP(w, r)
		return
	}
	writeDomainError(w, r, de)
}

func splitAllow(values []string) []string {
	var out []string
	for _, v := range values {
		for _, m := range strings.Split(v, ",") {
			if m = strings.TrimSpace(m); m != "" {
				out = append(out, m)
			}
		}
	}
	return out
}
