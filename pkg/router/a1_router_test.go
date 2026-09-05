// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression tests for the A1 router findings of the 2026-09-03 maturity
// audit: NU-38 (CORS placement and subdomain wildcards), NU-36 (Compress
// decides on the first write), NU-6 (request timeout exemptions keep
// http.Flusher), NU-30 (http.route is the matched template, not the path),
// NU-31 (a mount's exact path serves the subtree root) and NU-32 (Walk
// hands back the registered handler).
package router

import (
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestCORS_SubdomainWildcard(t *testing.T) {
	mw := CORSMiddleware(CORSOptions{AllowedOrigins: []string{"https://*.example.com", "https://*.corp.example:8443"}})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	cases := []struct {
		origin  string
		allowed bool
	}{
		{"https://app.example.com", true},
		{"https://a.b.example.com", true},
		{"HTTPS://APP.EXAMPLE.COM", true},
		{"https://example.com", false},          // the apex is not a subdomain
		{"http://app.example.com", false},       // scheme must match
		{"https://app.example.com:8443", false}, // port must match the pattern (none)
		{"https://evil-example.com", false},     // suffix without the dot
		{"https://app.example.com.evil.net", false},
		{"https://x.corp.example:8443", true},
		{"https://x.corp.example", false}, // pattern carries a port
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", tc.origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Header().Get("Access-Control-Allow-Origin") != ""
		if got != tc.allowed {
			t.Errorf("origin %s: allowed=%v, want %v", tc.origin, got, tc.allowed)
		}
	}
}

func TestDefaultStack_CORSHeadersOnRateLimited(t *testing.T) {
	// NU-38: the 429 the limiter emits must still carry CORS headers, or the
	// browser reports a network failure instead of the error.
	r := New(quietLogger(), WithCORSOrigins("https://app.example.com"), WithRateLimit(1, time.Minute))
	r.Get("/ping", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"ok": "1"}) })
	var rec *httptest.ResponseRecorder
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("429 without CORS headers: %v", rec.Header())
	}
}

func TestCompress_DecidesOnFirstWrite(t *testing.T) {
	mw := Compress(5)
	serve := func(h http.HandlerFunc) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		mw(h).ServeHTTP(rec, req)
		return rec
	}
	// JSON is compressed and round-trips.
	rec := serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hello":"` + strings.Repeat("x", 200) + `"}`))
	})
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("json: Content-Encoding %q, want gzip", rec.Header().Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(body), `{"hello":"xxx`) {
		t.Fatalf("decompressed body mismatch: %.40s", body)
	}
	// A 204 has nothing to compress.
	rec = serve(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("204 with Content-Encoding %q", rec.Header().Get("Content-Encoding"))
	}
	// An image is already compressed: pass it through byte for byte.
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64))
	rec = serve(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != string(png) {
		t.Fatalf("image/png was rewritten: encoding %q, %d bytes", rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	// No Content-Type set: the first bytes are sniffed BEFORE compression.
	rec = serve(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	})
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("sniffed Content-Type %q, want text/html", ct)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("html: not compressed")
	}
	// Vary is set whether or not the body was compressed.
	rec = serve(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary missing on an uncompressed response")
	}
}

func TestTimeout_ExemptRoutesKeepFlusherAndNoDeadline(t *testing.T) {
	mw := TimeoutMiddlewareWithExemptions(30*time.Millisecond, []string{"/stream"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, canFlush := w.(http.Flusher)
		time.Sleep(80 * time.Millisecond)
		if r.Context().Err() != nil {
			return // the timeout handler already answered
		}
		if canFlush {
			w.Header().Set("X-Flusher", "yes")
		}
		w.WriteHeader(http.StatusOK)
	}))
	get := func(path, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := get("/slow", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/slow: status %d, want 503 (timeout)", rec.Code)
	}
	if rec := get("/stream/events", ""); rec.Code != http.StatusOK || rec.Header().Get("X-Flusher") != "yes" {
		t.Fatalf("/stream/events: status %d flusher=%q, want 200 with the raw writer", rec.Code, rec.Header().Get("X-Flusher"))
	}
	if rec := get("/slow", "text/event-stream"); rec.Code != http.StatusOK || rec.Header().Get("X-Flusher") != "yes" {
		t.Fatalf("SSE Accept on /slow: status %d flusher=%q, want exempt", rec.Code, rec.Header().Get("X-Flusher"))
	}
	// Zero disables the timeout for every route.
	off := TimeoutMiddlewareWithExemptions(0, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	off.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("timeout 0: status %d, want 200", rec.Code)
	}
}

func TestTelemetry_RouteIsMatchedTemplate(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev); _ = tp.Shutdown(context.Background()) })

	r := New(quietLogger())
	r.Get("/users/{id}", func(c *Context) error {
		return c.JSON(http.StatusOK, map[string]string{"route": RouteFromContext(c.Request.Context())})
	})
	r.Route("/admin", func(s *Mux) {
		s.Get("/reports/{name}", func(c *Context) error { return c.JSON(http.StatusOK, nil) })
	})

	spanFor := func(path string) (name, route string) {
		exp.Reset()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		for _, s := range exp.GetSpans() {
			for _, a := range s.Attributes {
				if string(a.Key) == "http.route" {
					return s.Name, a.Value.AsString()
				}
			}
		}
		t.Fatalf("no server span with http.route for %s", path)
		return "", ""
	}
	if name, route := spanFor("/users/42"); route != "/users/{id}" || name != "GET /users/{id}" {
		t.Fatalf("direct route: name=%q route=%q", name, route)
	}
	if _, route := spanFor("/admin/reports/q3"); route != "/admin/reports/{name}" {
		t.Fatalf("mounted route: route=%q, want /admin/reports/{name}", route)
	}
	if _, route := spanFor("/nowhere/1/2/3"); route != "unmatched" {
		t.Fatalf("404: route=%q, want unmatched", route)
	}
}

func TestMount_ExactPathServesSubtreeRoot(t *testing.T) {
	r := New(quietLogger())
	r.Route("/users", func(s *Mux) {
		s.Get("/", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"list": "yes"}) })
		s.Get("/{id}", func(c *Context) error {
			return c.JSON(http.StatusOK, map[string]string{"id": c.Request.PathValue("id")})
		})
	})
	for _, path := range []string{"/users", "/users/"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"list":"yes"`) {
			t.Fatalf("GET %s: status %d body %s (a 307 to /users/ is what NU-31 removed)", path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/7", nil))
	if !strings.Contains(rec.Body.String(), `"id":"7"`) {
		t.Fatalf("GET /users/7: %s", rec.Body.String())
	}
}

func TestWalk_HandsBackTheHandler(t *testing.T) {
	m := NewMux()
	m.Get("/a", func(c *Context) error { return nil })
	m.Mount("/static", http.NotFoundHandler())
	seen := 0
	_ = m.Walk(func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		seen++
		if h == nil {
			t.Fatalf("Walk: nil handler for %s %s", method, route)
		}
		return nil
	})
	if seen != 2 {
		t.Fatalf("Walk visited %d routes, want 2", seen)
	}
}
