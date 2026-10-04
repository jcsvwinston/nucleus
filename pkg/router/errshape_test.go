// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
)

func shapeRouter(opts ...Option) *Router {
	r := New(quietLogger(), opts...)
	r.Get("/things", func(c *Context) error { return c.JSON(http.StatusOK, []string{"one"}) })
	r.Get("/missing", func(c *Context) error { return gferrors.NotFound("thing", "1") })
	r.Get("/teapot", func(c *Context) error { return NewHTTPError(http.StatusTeapot, "short and stout") })
	r.Get("/boom", func(c *Context) error { return errors.New("driver said: table secrets is locked") })
	r.Route("/api", func(sub *Mux) {
		sub.Get("/items", func(c *Context) error { return c.NoContent() })
	})
	// An opaque mounted handler answers its own 404s.
	r.Mount("/files", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "opaque says no", http.StatusNotFound)
	}))
	return r
}

func doShape(t *testing.T, h http.Handler, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type envelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type %q, want application/json; body %s", ct, rec.Body)
	}
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Error.Code == "" {
		t.Fatalf("not the envelope (%v): %s", err, rec.Body)
	}
	return env
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) gferrors.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type %q, want application/problem+json; body %s", ct, rec.Body)
	}
	var p gferrors.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Status == 0 {
		t.Fatalf("not a problem document (%v): %s", err, rec.Body)
	}
	return p
}

func TestRouterOwnMiss_AnswersInTheEnvelopeForAJSONClient(t *testing.T) {
	h := shapeRouter()
	for _, path := range []string{"/nope", "/api/nope", "/api/items/1/typo", "/api"} {
		rec := doShape(t, h, http.MethodGet, path, "application/json")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s → %d", path, rec.Code)
		}
		env := decodeEnvelope(t, rec)
		if env.Error.Code != "NOT_FOUND" || !strings.Contains(env.Error.Message, path) {
			t.Fatalf("GET %s → %+v", path, env.Error)
		}
	}
	// The domain error and the router's 404 share the shape.
	dom := decodeEnvelope(t, doShape(t, h, http.MethodGet, "/missing", "application/json"))
	if dom.Error.Code != "NOT_FOUND" {
		t.Fatalf("domain error: %+v", dom.Error)
	}

	// 405 keeps its Allow header and names the methods.
	rec := doShape(t, h, http.MethodDelete, "/api/items", "application/json")
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Header().Get("Allow"), "GET") {
		t.Fatalf("DELETE /api/items → %d Allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	env := decodeEnvelope(t, rec)
	if env.Error.Code != "METHOD_NOT_ALLOWED" || env.Error.Details["allow"] == nil {
		t.Fatalf("405 envelope: %+v", env.Error)
	}
}

func TestRouterOwnMiss_KeepsPlainTextForBrowsersAndWildcards(t *testing.T) {
	h := shapeRouter()
	for _, accept := range []string{"", "*/*", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"} {
		rec := doShape(t, h, http.MethodGet, "/api/nope", accept)
		if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") ||
			strings.TrimSpace(rec.Body.String()) != "404 page not found" {
			t.Fatalf("Accept %q → %d %q %q", accept, rec.Code, rec.Header().Get("Content-Type"), rec.Body)
		}
	}
}

func TestRouterOwnMiss_AnOpaqueMountAnswersItsOwn404(t *testing.T) {
	rec := doShape(t, shapeRouter(), http.MethodGet, "/files/nope", "application/json")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "opaque says no") {
		t.Fatalf("opaque mount → %d %s", rec.Code, rec.Body)
	}
}

func TestProblemDetails_WhenTheClientPrefersThem(t *testing.T) {
	h := shapeRouter()
	p := decodeProblem(t, doShape(t, h, http.MethodGet, "/api/nope", "application/problem+json"))
	if p.Status != http.StatusNotFound || p.Code != "NOT_FOUND" || p.Type != "about:blank" ||
		p.Title != "Not Found" || p.Instance != "/api/nope" {
		t.Fatalf("router 404 as a problem: %+v", p)
	}
	p = decodeProblem(t, doShape(t, h, http.MethodGet, "/missing", "application/problem+json"))
	if p.Status != http.StatusNotFound || p.Code != "NOT_FOUND" || !strings.Contains(p.Detail, "not found") {
		t.Fatalf("domain error as a problem: %+v", p)
	}
	p = decodeProblem(t, doShape(t, h, http.MethodGet, "/teapot", "application/problem+json"))
	if p.Status != http.StatusTeapot || p.Code != "IM_A_TEAPOT" || p.Detail != "short and stout" {
		t.Fatalf("HTTPError as a problem: %+v", p)
	}
	p = decodeProblem(t, doShape(t, h, http.MethodGet, "/boom", "application/problem+json"))
	if p.Status != http.StatusInternalServerError || strings.Contains(p.Detail, "secrets") {
		t.Fatalf("an unclassified error must not leak its text: %+v", p)
	}

	// Without the preference the envelope answers, byte for byte as before.
	rec := doShape(t, h, http.MethodGet, "/missing", "application/json")
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":{"code":"NOT_FOUND","message":"thing '1' not found"}}` {
		t.Fatalf("envelope changed: %s", got)
	}
	rec = doShape(t, h, http.MethodGet, "/teapot", "")
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"short and stout"}` {
		t.Fatalf("HTTPError envelope changed: %s", got)
	}
}

func TestProblemDetails_WhenTheApplicationOptsIn(t *testing.T) {
	h := shapeRouter(WithProblemDetails(true))
	// Every error, whatever JSON type the client names.
	for _, path := range []string{"/missing", "/teapot", "/api/nope"} {
		rec := doShape(t, h, http.MethodGet, path, "application/problem+json")
		decodeProblem(t, rec)
	}
	// A client that named only application/json gets the problem body under
	// the type it named.
	rec := doShape(t, h, http.MethodGet, "/missing", "application/json")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type %q", ct)
	}
	var p gferrors.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Status != http.StatusNotFound || p.Code != "NOT_FOUND" {
		t.Fatalf("opt-in body for an application/json client: %s", rec.Body)
	}
	// A browser still gets the router's own plain-text 404.
	rec = doShape(t, h, http.MethodGet, "/api/nope", "text/html")
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("browser 404 under the opt-in: %q", rec.Header().Get("Content-Type"))
	}
}

func TestProblemDetails_ValidationDetailsTravelAsAnExtension(t *testing.T) {
	r := New(quietLogger())
	r.Get("/list", func(c *Context) error {
		var in struct {
			Page int `query:"page" validate:"min=1"`
		}
		return BindQuery(c.Request, &in)
	})
	p := decodeProblem(t, doShape(t, r, http.MethodGet, "/list?page=0", "application/problem+json"))
	details, _ := p.Details.(map[string]any)
	if p.Status != http.StatusUnprocessableEntity || p.Code != "VALIDATION_FAILED" || details["page"] == nil {
		t.Fatalf("validation as a problem: %+v", p)
	}
}

func TestErrorsWriteError_NegotiatesTheSameWay(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gferrors.WriteError(w, r, gferrors.Forbidden("no"), nil)
	})
	decodeProblem(t, doShape(t, h, http.MethodGet, "/x", "application/problem+json"))
	decodeEnvelope(t, doShape(t, h, http.MethodGet, "/x", "application/json"))
	// Under a router that opted in, middleware errors are problems too.
	r := New(quietLogger(), WithProblemDetails(true))
	r.Use(func(http.Handler) http.Handler { return h })
	r.Get("/y", func(c *Context) error { return c.NoContent() })
	decodeProblem(t, doShape(t, r, http.MethodGet, "/y", "*/*"))
}

func TestFrameworkRefusalsShareTheShape(t *testing.T) {
	r := New(quietLogger(), WithCSRF(), WithRateLimit(2, time.Minute))
	r.Post("/form", func(c *Context) error { return c.NoContent() })

	// A state-changing request without a CSRF token.
	rec := doShape(t, r, http.MethodPost, "/form", "application/json")
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("POST /form without a token → %d", rec.Code)
	}
	if env := decodeEnvelope(t, rec); env.Error.Code != "CSRF_FAILED" {
		t.Fatalf("CSRF refusal: %+v", env.Error)
	}

	// The limiter's refusal, asked for as a problem; Retry-After survives.
	var last *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		last = doShape(t, r, http.MethodPost, "/form", "application/problem+json")
	}
	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") == "" {
		t.Fatalf("third request → %d Retry-After %q", last.Code, last.Header().Get("Retry-After"))
	}
	if p := decodeProblem(t, last); p.Code != "RATE_LIMITED" {
		t.Fatalf("rate limit as a problem: %+v", p)
	}
}
