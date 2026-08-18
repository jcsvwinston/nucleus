// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-7 (external coverage demo, 2026-08-17): app.New loaded templates
// with a FLAT glob (TemplatesDir/*.html) while `nucleus startapp` scaffolds
// its template into a SUBDIRECTORY (internal/web/templates/<name>/index.html).
// On a freshly scaffolded project the glob matches nothing, a.Templates stays
// nil, SetHTMLTemplates is never called, and every c.HTML(...) returns
// ErrTemplateEngineNotSet — silently: no startup warning, just an error on
// the first request that tries to render. The framework's server-side render
// path did not work with the framework's own scaffold.
//
// Naming contract under test (documented in the render guide):
//   - every .html under TemplatesDir is registered under its path RELATIVE
//     to TemplatesDir with forward slashes ("fieldservice/index.html");
//   - files at the root keep their historical flat name ("base.html") —
//     the relative path of a root file IS its base name, so existing apps
//     keep working;
//   - {{define "..."}} blocks keep registering under their declared name.
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemplate(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The exact startapp shape: ONLY a nested template. On v1.8.1 the flat glob
// found nothing and the engine was never wired.
func TestNestedTemplatesLoadFromScaffoldLayout(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	tplDir := filepath.Join(dir, "templates")
	writeTemplate(t, tplDir, "fieldservice/index.html", "<h1>{{.title}}</h1>")

	cfg := DefaultConfig()
	cfg.TemplatesDir = tplDir

	a, err := New(&cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(nil) })

	if a.Templates == nil {
		t.Fatal("a.Templates is nil with a scaffolded (nested) TemplatesDir — the flat glob missed the subdirectory and c.HTML will return ErrTemplateEngineNotSet (QCD-FW-7)")
	}
	if a.Templates.Lookup("fieldservice/index.html") == nil {
		t.Fatalf("nested template not registered under its relative path %q; registered: %v", "fieldservice/index.html", a.Templates.DefinedTemplates())
	}

	var sb strings.Builder
	if err := a.Templates.ExecuteTemplate(&sb, "fieldservice/index.html", map[string]any{"title": "works"}); err != nil {
		t.Fatalf("render nested template: %v", err)
	}
	if !strings.Contains(sb.String(), "<h1>works</h1>") {
		t.Fatalf("unexpected render output: %q", sb.String())
	}
}

// Zero regression for flat layouts: root files keep their historical name
// and their {{define}} blocks keep working — the demo already shipped with
// this shape.
func TestFlatTemplatesKeepTheirNames(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	tplDir := filepath.Join(dir, "templates")
	writeTemplate(t, tplDir, "base.html", `{{define "footer"}}<footer>f</footer>{{end}}<main>{{.body}}</main>`)
	writeTemplate(t, tplDir, "nested/detail.html", "<p>{{.p}}</p>")

	cfg := DefaultConfig()
	cfg.TemplatesDir = tplDir

	a, err := New(&cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(nil) })

	if a.Templates == nil {
		t.Fatal("a.Templates is nil")
	}
	for _, name := range []string{"base.html", "footer", "nested/detail.html"} {
		if a.Templates.Lookup(name) == nil {
			t.Errorf("template %q not registered; registered: %v", name, a.Templates.DefinedTemplates())
		}
	}
}
