// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Tests for WithTemplatesFS (ADR-022 §3): fs.FS template sources parse into
// the engine under a name prefix, accumulate across calls, see the
// registered template functions, and lose to templates_dir on a name
// collision (the host's on-disk files always win).
package app

import (
	"context"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func renderAppTemplate(t *testing.T, a *App, name string, data any) string {
	t.Helper()
	var sb strings.Builder
	if err := a.Templates.ExecuteTemplate(&sb, name, data); err != nil {
		t.Fatalf("ExecuteTemplate(%q): %v", name, err)
	}
	return sb.String()
}

func TestNew_WithTemplatesFS(t *testing.T) {
	t.Run("prefixed names, no templates_dir needed", func(t *testing.T) {
		cfg := testAppConfig()
		cfg.TemplatesDir = "" // no directory at all: the FS alone is the engine

		a, err := New(cfg, WithTemplatesFS("shop", fstest.MapFS{
			"index.html":         &fstest.MapFile{Data: []byte("<h1>{{.title}}</h1>")},
			"partials/row.html":  &fstest.MapFile{Data: []byte("<li>row</li>")},
			"assets/ignored.css": &fstest.MapFile{Data: []byte("body{}")},
		}))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer a.Shutdown(context.Background())
		if a.Templates == nil {
			t.Fatal("FS templates alone must configure the engine")
		}
		if got := renderAppTemplate(t, a, "shop/index.html", map[string]any{"title": "hi"}); !strings.Contains(got, "<h1>hi</h1>") {
			t.Errorf("shop/index.html: got %q", got)
		}
		if got := renderAppTemplate(t, a, "shop/partials/row.html", nil); !strings.Contains(got, "<li>row</li>") {
			t.Errorf("nested FS template: got %q", got)
		}
		if a.Templates.Lookup("shop/assets/ignored.css") != nil {
			t.Error("non-.html files must not register")
		}
	})

	t.Run("sources accumulate and see template funcs", func(t *testing.T) {
		cfg := testAppConfig()
		cfg.TemplatesDir = ""

		a, err := New(cfg,
			WithTemplateFuncs(template.FuncMap{"shout": strings.ToUpper}),
			WithTemplatesFS("a", fstest.MapFS{"x.html": &fstest.MapFile{Data: []byte(`{{shout "a"}}`)}}),
			WithTemplatesFS("b", fstest.MapFS{"x.html": &fstest.MapFile{Data: []byte(`{{shout "b"}}`)}}),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer a.Shutdown(context.Background())
		if got := renderAppTemplate(t, a, "a/x.html", nil); got != "A" {
			t.Errorf("a/x.html with funcs: got %q", got)
		}
		if got := renderAppTemplate(t, a, "b/x.html", nil); got != "B" {
			t.Errorf("second accumulated source: got %q", got)
		}
	})

	t.Run("templates_dir overrides an FS source on collision", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "shop"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "shop", "index.html"), []byte("host wins"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := testAppConfig()
		cfg.TemplatesDir = dir

		a, err := New(cfg, WithTemplatesFS("shop", fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("module loses")},
		}))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer a.Shutdown(context.Background())
		if got := renderAppTemplate(t, a, "shop/index.html", nil); got != "host wins" {
			t.Errorf("collision: templates_dir must win, got %q", got)
		}
	})

	t.Run("malformed FS template fails New, not panics", func(t *testing.T) {
		cfg := testAppConfig()
		cfg.TemplatesDir = ""
		a, err := New(cfg, WithTemplatesFS("shop", fstest.MapFS{
			"broken.html": &fstest.MapFile{Data: []byte("{{ .Unclosed ")},
		}))
		if err == nil {
			a.Shutdown(context.Background())
			t.Fatal("want a parse error from New")
		}
	})
}
