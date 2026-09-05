package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordMatched is a middleware that copies the routing decision into a
// response header so a test can read it without a handler — an unmatched
// request has none.
func recordMatched(header string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if Matched(r) {
				w.Header().Set(header, "yes")
			} else {
				w.Header().Set(header, "no")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TestMatched_ReportsTheRoutingDecisionBeforeMiddlewareRuns is the
// contract behind the 404-for-unknown-paths change: a middleware mounted
// with Use can tell whether a registered route serves the request, at the
// parent and inside a mounted sub-router, before anything answers.
func TestMatched_ReportsTheRoutingDecisionBeforeMiddlewareRuns(t *testing.T) {
	m := NewMux()
	m.Use(recordMatched("X-Matched"))
	m.Get("/users", func(c *Context) error { return c.NoContent() })
	m.Route("/api", func(sub *Mux) {
		sub.Use(recordMatched("X-Sub-Matched"))
		sub.Get("/items", func(c *Context) error { return c.NoContent() })
	})

	cases := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantMatched string
		wantSub     string // "" when the sub-router is never reached
	}{
		{"registered route", http.MethodGet, "/users", http.StatusNoContent, "yes", ""},
		{"unregistered path", http.MethodGet, "/nope", http.StatusNotFound, "no", ""},
		{"method mismatch on a registered path", http.MethodPost, "/users", http.StatusMethodNotAllowed, "no", ""},
		{"registered route under a mount", http.MethodGet, "/api/items", http.StatusNoContent, "yes", "yes"},
		{"unregistered path under a mount", http.MethodGet, "/api/nope", http.StatusNotFound, "yes", "no"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("X-Matched"); got != tc.wantMatched {
				t.Fatalf("%s %s: Matched at the parent = %q, want %q", tc.method, tc.path, got, tc.wantMatched)
			}
			if got := rec.Header().Get("X-Sub-Matched"); got != tc.wantSub {
				t.Fatalf("%s %s: Matched inside the mount = %q, want %q", tc.method, tc.path, got, tc.wantSub)
			}
		})
	}
}

// TestMatched_WithoutAMuxReportsTrue: a middleware wrapped around a plain
// handler has no routing decision to consult and must keep enforcing, so
// the conservative answer is "matched".
func TestMatched_WithoutAMuxReportsTrue(t *testing.T) {
	if !Matched(httptest.NewRequest(http.MethodGet, "/anything", nil)) {
		t.Fatal("Matched must report true when no Mux dispatched the request")
	}
	if !Matched(nil) {
		t.Fatal("Matched(nil) must report true")
	}
}

// TestCSRF_UnregisteredPathFallsThroughToThe404 pins the CSRF half of the
// change: a POST to a path nobody serves answers the mux's 404, not a 419
// vouching for a form that does not exist — while a registered route
// without a token is still refused.
func TestCSRF_UnregisteredPathFallsThroughToThe404(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := New(logger, WithCSRF())
	r.Post("/form", func(c *Context) error { return c.NoContent() })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /nope with CSRF on: status = %d, want 404 (the mux answers, not the CSRF gate); body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/form", nil))
	if rec.Code != 419 {
		t.Fatalf("POST /form without a token: status = %d, want 419 (registered routes stay CSRF-protected)", rec.Code)
	}
}
