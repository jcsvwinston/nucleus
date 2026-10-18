// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// DEP-2026-022: since v2.0.0 every error of an application that chose
// nothing answers as RFC 9457 problem details; WithErrorEnvelope keeps the
// framework's envelope, {"error": {...}}, the v1 default, and
// WithProblemDetails states the default out loud. The first test pins the
// default, the others the two options and their contradiction.

// failingRoute registers GET /boom, whose handler returns a plain error,
// and answers one request to it: the framework renders the error in the
// application's shape. The applications under test are open (WithOpenAuthz)
// so that the request reaches the handler instead of the authorizer's 403.
func failingRoute(t *testing.T, a *App, accept string) *httptest.ResponseRecorder {
	t.Helper()
	a.Router.Get("/boom", func(c *router.Context) error { return errors.New("boom") })
	return callBoom(t, a, accept)
}

// callBoom answers one more request to the route failingRoute registered.
func callBoom(t *testing.T, a *App, accept string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /boom = %d, want 500", rec.Code)
	}
	return rec
}

func TestErrorShape_ProblemDetailsAreTheDefault(t *testing.T) {
	a, err := New(testAppConfig(), WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(t.Context()) })
	if !a.ProblemDetails() || a.ErrorEnvelope() {
		t.Fatalf("ProblemDetails=%v ErrorEnvelope=%v: an application that chose nothing answers problem details (DEP-2026-022)", a.ProblemDetails(), a.ErrorEnvelope())
	}
	rec := failingRoute(t, a, "")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json (the default since v2.0.0)", ct)
	}
	if body := rec.Body.String(); strings.HasPrefix(body, `{"error":`) || !strings.Contains(body, `"status":500`) {
		t.Fatalf("body = %s, want an RFC 9457 document", body)
	}
	// A client that names only application/json still gets the problem
	// document — labelled with the type it accepts (httpneg.ProblemContentType)
	// — because the default fixes the shape for every client.
	rec = callBoom(t, a, "application/json")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Accept: application/json → Content-Type = %q, want application/json (the type the client accepts)", ct)
	}
	if body := rec.Body.String(); strings.HasPrefix(body, `{"error":`) || !strings.Contains(body, `"status":500`) {
		t.Fatalf("Accept: application/json → body = %s, want an RFC 9457 document", body)
	}
}

func TestErrorShape_WithErrorEnvelopeKeepsTheEnvelope(t *testing.T) {
	a, err := New(testAppConfig(), WithOpenAuthz(), WithErrorEnvelope())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(t.Context()) })
	if !a.ErrorEnvelope() || a.ProblemDetails() {
		t.Fatalf("ErrorEnvelope=%v ProblemDetails=%v, want true/false", a.ErrorEnvelope(), a.ProblemDetails())
	}
	rec := failingRoute(t, a, "")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json (the envelope)", ct)
	}
	if body := rec.Body.String(); !strings.HasPrefix(body, `{"error":`) {
		t.Fatalf("body = %s, want the envelope", body)
	}
	// A client that negotiates problem details still gets them: the option
	// fixes the default shape, not the negotiation.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("Accept", "application/problem+json, application/json;q=0.5")
	a.Router.ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("negotiated Content-Type = %q, want application/problem+json", ct)
	}
}

func TestErrorShape_WithProblemDetailsFlipsIt(t *testing.T) {
	a, err := New(testAppConfig(), WithOpenAuthz(), WithProblemDetails())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(t.Context()) })
	if !a.ProblemDetails() || a.ErrorEnvelope() {
		t.Fatalf("ProblemDetails=%v ErrorEnvelope=%v, want true/false", a.ProblemDetails(), a.ErrorEnvelope())
	}
	rec := failingRoute(t, a, "")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
}

func TestErrorShape_BothOptionsAreRefused(t *testing.T) {
	_, err := New(testAppConfig(), WithProblemDetails(), WithErrorEnvelope())
	if err == nil {
		t.Fatal("New accepted WithProblemDetails and WithErrorEnvelope together; they contradict each other")
	}
	for _, want := range []string{"WithProblemDetails", "WithErrorEnvelope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}
