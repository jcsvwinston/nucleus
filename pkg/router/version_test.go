// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package router

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestVersion_MountsUnderItsSegmentAndStampsTheHeaders(t *testing.T) {
	deprecated := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	sunset := time.Date(2027, 4, 1, 12, 0, 0, 0, time.UTC)
	r := New(quietLogger())
	var apiMux *Mux
	r.Route("/api", func(api *Mux) {
		apiMux = api
		api.Version(APIVersion{Name: "v1", Deprecated: deprecated, Sunset: sunset,
			Successor: "/api/v2/notes", Policy: "https://example.com/deprecations/v1"}, func(v1 *Mux) {
			v1.Get("/notes", func(c *Context) error { return c.JSON(http.StatusOK, []string{"v1"}) })
		})
		api.Version(APIVersion{Name: "v2"}, func(v2 *Mux) {
			v2.Get("/notes", func(c *Context) error { return c.JSON(http.StatusOK, []string{"v2"}) })
		})
	})

	rec := doShape(t, r, http.MethodGet, "/api/v1/notes", "application/json")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "v1") {
		t.Fatalf("/api/v1/notes: %d %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Deprecation"); got != "@1790812800" {
		t.Fatalf("Deprecation %q", got)
	}
	if got := rec.Header().Get("Sunset"); got != "Thu, 01 Apr 2027 12:00:00 GMT" {
		t.Fatalf("Sunset %q", got)
	}
	links := strings.Join(rec.Header().Values("Link"), ", ")
	if !strings.Contains(links, `</api/v2/notes>; rel="successor-version"`) ||
		!strings.Contains(links, `<https://example.com/deprecations/v1>; rel="deprecation"`) {
		t.Fatalf("Link %q", links)
	}

	// The version's own 404 carries the headers too.
	rec = doShape(t, r, http.MethodGet, "/api/v1/nope", "application/json")
	if rec.Code != http.StatusNotFound || rec.Header().Get("Sunset") == "" {
		t.Fatalf("/api/v1/nope: %d Sunset %q", rec.Code, rec.Header().Get("Sunset"))
	}

	// A current version adds no header.
	rec = doShape(t, r, http.MethodGet, "/api/v2/notes", "application/json")
	if rec.Code != http.StatusOK || rec.Header().Get("Deprecation") != "" || rec.Header().Get("Sunset") != "" || len(rec.Header().Values("Link")) != 0 {
		t.Fatalf("/api/v2/notes: %d %v", rec.Code, rec.Header())
	}

	// The version mounts appear in the listing of the router they are on.
	var patterns []string
	_ = apiMux.Walk(func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		patterns = append(patterns, route)
		return nil
	})
	if !strings.Contains(strings.Join(patterns, " "), "/v1/") {
		t.Fatalf("Walk does not list the version mounts: %v", patterns)
	}
}

func TestVersion_ValidateRejectsWhatCannotBeMounted(t *testing.T) {
	for _, v := range []APIVersion{
		{},
		{Name: "v1/beta"},
		{Name: "v 1"},
		{Name: "{v}"},
		{Name: ".."},
		{Name: "v1", Successor: "/v2>; rel=evil"},
	} {
		if err := v.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil", v)
		}
	}
	for _, v := range []APIVersion{{Name: "v1"}, {Name: "2026-10-01"}, {Name: "v1.2_beta~x"}} {
		if err := v.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", v, err)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Mux.Version accepted an invalid version")
		}
	}()
	NewMux().Version(APIVersion{Name: "a/b"}, func(*Mux) {})
}
