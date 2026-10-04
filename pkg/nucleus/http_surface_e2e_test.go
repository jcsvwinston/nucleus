// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

func surfaceConfig(t *testing.T) app.Config {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.JWTSecret = strings.Repeat("http-surface-secret", 2)
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "surface.db")},
	}
	return cfg
}

func get(t *testing.T, srv *nucleustest.Server, method, path, accept string, body io.Reader) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL(path), body)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

// TestModuleVersion_MountsTheModuleUnderItsVersion pins Module.Version: the
// module serves under Prefix + "/" + Name, every response carries the
// version's headers, and the module's policy rows are relative to the
// versioned mount point — the default-deny authorizer grants the route the
// row names.
func TestModuleVersion_MountsTheModuleUnderItsVersion(t *testing.T) {
	sunset := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	v1 := nucleus.Module[struct{}]{
		Name:   "notes_v1",
		Prefix: "/api",
		Version: nucleus.APIVersion{
			Name:       "v1",
			Deprecated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			Sunset:     sunset,
			Successor:  "/api/v2/notes",
		},
		Policies: []nucleus.PolicyRule{{Subject: "anonymous", Object: "/notes", Action: "read"}},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/notes", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, []string{"v1"}) })
		},
	}.Build()
	v2 := nucleus.Module[struct{}]{
		Name:     "notes_v2",
		Prefix:   "/api",
		Version:  nucleus.APIVersion{Name: "v2"},
		Policies: []nucleus.PolicyRule{{Subject: "anonymous", Object: "/notes", Action: "read"}},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/notes", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, []string{"v2"}) })
		},
	}.Build()
	if v1.Prefix() != "/api/v1" {
		t.Fatalf("ModuleSpec.Prefix() = %q, want the full mount point /api/v1", v1.Prefix())
	}
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  surfaceConfig(t),
		Modules: map[string]nucleus.ModuleSpec{"notes_v1": v1, "notes_v2": v2},
	})
	t.Cleanup(srv.Stop)

	resp, body := get(t, srv, http.MethodGet, "/api/v1/notes", "application/json", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "v1") {
		t.Fatalf("/api/v1/notes: %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("Sunset") != sunset.Format(http.TimeFormat) || resp.Header.Get("Deprecation") == "" ||
		!strings.Contains(resp.Header.Get("Link"), `rel="successor-version"`) {
		t.Fatalf("version headers: %v", resp.Header)
	}
	resp, body = get(t, srv, http.MethodGet, "/api/v2/notes", "application/json", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "v2") || resp.Header.Get("Sunset") != "" {
		t.Fatalf("/api/v2/notes: %d %s %v", resp.StatusCode, body, resp.Header)
	}
	if resp, _ := get(t, srv, http.MethodGet, "/api/notes", "application/json", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/api/notes (no version) answered %d", resp.StatusCode)
	}
}

func TestModuleVersion_AMalformedVersionFailsBoot(t *testing.T) {
	bad := nucleus.Module[struct{}]{
		Name:    "bad",
		Prefix:  "/api",
		Version: nucleus.APIVersion{Name: "v1/beta"},
		Routes:  func(r nucleus.Router, _ struct{}) {},
	}.Build()
	cfg := surfaceConfig(t)
	cfg.Port = 0
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := nucleus.RunContext(ctx, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{"bad": bad},
	})
	if err == nil || !strings.Contains(err.Error(), `module "bad" Version`) {
		t.Fatalf("boot error = %v, want one naming the module's Version", err)
	}
}

type surfaceInput struct {
	ID    int64  `path:"id"`
	Page  int    `query:"page" validate:"omitempty,min=1"`
	Trace string `header:"X-Trace-Id"`
	Name  string `json:"name" validate:"required"`
}

// TestContextSurface_ThroughARealApplication drives the Context methods
// this surface adds through a booted application: typed binding of the
// whole request, negotiation, a route group under a version, a per-route
// timeout longer than both request_timeout and write_timeout, RawHTML, and
// the problem details opt-in on the builder.
func TestContextSurface_ThroughARealApplication(t *testing.T) {
	mod := nucleus.Module[struct{}]{
		Name:   "surface",
		Prefix: "/s",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/items/{id}", func(c *nucleus.Context) error {
				var in surfaceInput
				if err := c.BindRequest(&in); err != nil {
					return err
				}
				return c.Negotiate(http.StatusOK, in)
			})
			r.Get("/items/{id}", func(c *nucleus.Context) error {
				var in struct {
					ID int64 `path:"id"`
				}
				if err := c.BindPath(&in); err != nil {
					return err
				}
				var q struct {
					Page int `query:"page"`
				}
				if err := c.BindQuery(&q); err != nil {
					return err
				}
				var h struct {
					Trace string `header:"X-Trace-Id"`
				}
				if err := c.BindHeaders(&h); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, map[string]any{"id": in.ID, "page": q.Page, "trace": h.Trace})
			})
			r.Get("/page", func(c *nucleus.Context) error { return c.RawHTML(http.StatusOK, "<p>raw</p>") })
			r.Get("/legacy", func(c *nucleus.Context) error {
				return c.HTML(http.StatusOK, "<p>legacy</p>") //nolint:staticcheck // the deprecated form keeps working
			})
			r.Get("/missing", func(c *nucleus.Context) error { return gferrors.NotFound("item", "9") })
			r.With(nucleus.Timeout(4*time.Second)).Get("/export", func(c *nucleus.Context) error {
				time.Sleep(1500 * time.Millisecond)
				return c.String(http.StatusOK, "exported")
			})
			r.Get("/slow", func(c *nucleus.Context) error {
				select {
				case <-time.After(3 * time.Second):
				case <-c.Request.Context().Done():
				}
				return c.String(http.StatusOK, "too late")
			})
			nucleus.Versioned(r, nucleus.APIVersion{Name: "v0", Sunset: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}, func(g nucleus.Router) {
				g.Get("/ping", func(c *nucleus.Context) error { return c.String(http.StatusOK, "pong") })
			})
		},
	}.Build()

	b := nucleus.New().WithOpenAuthz().WithProblemDetails().Mount(mod)
	a, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Config = surfaceConfig(t)
	a.Config.RequestTimeout = time.Second
	// write_timeout a little over request_timeout, so the 503 of a route
	// without its own timeout still reaches the client; /s/export runs past
	// both.
	a.Config.WriteTimeout = 1300 * time.Millisecond
	srv := nucleustest.StartApp(t, a)
	t.Cleanup(srv.Stop)

	// BindRequest: the body cannot overwrite the path id; Negotiate picks XML.
	req, _ := http.NewRequest(http.MethodPost, srv.URL("/s/items/7?page=2"), strings.NewReader(`{"name":"n","ID":99}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/xml")
	req.Header.Set("X-Trace-Id", "tr")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/xml") ||
		!strings.Contains(string(raw), "<ID>7</ID>") || !strings.Contains(string(raw), "<Page>2</Page>") ||
		!strings.Contains(string(raw), "<Trace>tr</Trace>") {
		t.Fatalf("bind+negotiate: %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	}

	resp, body := get(t, srv, http.MethodGet, "/s/items/5?page=3", "application/json", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"id":5`) || !strings.Contains(body, `"page":3`) {
		t.Fatalf("single-source binders: %d %s", resp.StatusCode, body)
	}

	// A conversion failure under the opt-in: a problem naming the parameter.
	resp, body = get(t, srv, http.MethodGet, "/s/items/x", "application/problem+json", nil)
	var p gferrors.Problem
	_ = json.Unmarshal([]byte(body), &p)
	if resp.StatusCode != http.StatusBadRequest || p.Code != "BAD_REQUEST" || !strings.Contains(p.Detail, `"id"`) ||
		p.Instance != "/s/items/x" {
		t.Fatalf("bad path value: %d %s", resp.StatusCode, body)
	}

	// The opt-in: the domain error and the router's own 404 are problems.
	for _, path := range []string{"/s/missing", "/s/nope", "/nope"} {
		resp, body = get(t, srv, http.MethodGet, path, "application/problem+json", nil)
		if resp.StatusCode != http.StatusNotFound || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") ||
			!strings.Contains(body, `"status":404`) {
			t.Fatalf("GET %s: %d %q %s", path, resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
	}

	resp, body = get(t, srv, http.MethodGet, "/s/page", "", nil)
	if resp.StatusCode != http.StatusOK || body != "<p>raw</p>" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("RawHTML: %d %q %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if resp, body = get(t, srv, http.MethodGet, "/s/legacy", "", nil); resp.StatusCode != http.StatusOK || body != "<p>legacy</p>" {
		t.Fatalf("HTML: %d %s", resp.StatusCode, body)
	}

	resp, body = get(t, srv, http.MethodGet, "/s/v0/ping", "", nil)
	if resp.StatusCode != http.StatusOK || body != "pong" || resp.Header.Get("Sunset") == "" {
		t.Fatalf("Versioned group: %d %s %v", resp.StatusCode, body, resp.Header)
	}

	// Timeouts: the route's 4s outlives request_timeout (1s) and the
	// server's write_timeout (1.3s); the route without one is cut at 1s.
	resp, body = get(t, srv, http.MethodGet, "/s/export", "", nil)
	if resp.StatusCode != http.StatusOK || body != "exported" {
		t.Fatalf("/s/export with its own 4s: %d %s", resp.StatusCode, body)
	}
	resp, body = get(t, srv, http.MethodGet, "/s/slow", "application/json", nil)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, `"TIMEOUT"`) {
		t.Fatalf("/s/slow under the 1s request_timeout: %d %s", resp.StatusCode, body)
	}
}
