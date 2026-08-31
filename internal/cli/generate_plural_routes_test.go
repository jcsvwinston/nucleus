// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// NC-01/GF-02: `generate module` and `startapp` with an ALREADY-PLURAL name
// (products, todos, notes…) used to emit r.Get("/<name>") for the HTML page
// AND r.Resource("/<name>") for the JSON API — pluralizeResource returns a
// trailing-s name unchanged, so both registered GET /<name> and the app
// panicked at boot ("pattern GET /products conflicts with pattern GET
// /products"). These are the source-level pins; the boot-level proof lives
// in generate_module_slice_test.go.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModulePageRoute(t *testing.T) {
	cases := []struct{ snake, resource, want string }{
		{"widget", "widgets", "/widget"},
		{"products", "products", "/products/page"},
		{"todos", "todos", "/todos/page"},
		{"category", "categories", "/category"},
	}
	for _, tc := range cases {
		if got := modulePageRoute(tc.snake, tc.resource); got != tc.want {
			t.Errorf("modulePageRoute(%q, %q) = %q, want %q", tc.snake, tc.resource, got, tc.want)
		}
	}
}

func TestGenerateModulePluralNameDoesNotDuplicateGetRoute(t *testing.T) {
	dir := t.TempDir()
	result, err := generateModuleScaffold(dir, "products", "Products", "sqlite", false)
	if err != nil {
		t.Fatalf("generateModuleScaffold: %v", err)
	}
	src, err := os.ReadFile(result.ModulePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `r.Get("/products/page"`) {
		t.Fatalf("plural module must move the page off the resource Index path:\n%s", src)
	}
	if strings.Contains(string(src), `r.Get("/products",`) {
		t.Fatalf("plural module still registers the page on the resource path — this pair panics at boot:\n%s", src)
	}
	if !strings.Contains(string(src), `r.Resource("/products"`) {
		t.Fatalf("resource path must stay /products:\n%s", src)
	}
}

func TestGenerateModuleSingularNameKeepsPageRoute(t *testing.T) {
	dir := t.TempDir()
	result, err := generateModuleScaffold(dir, "widget", "Widget", "sqlite", false)
	if err != nil {
		t.Fatalf("generateModuleScaffold: %v", err)
	}
	src, err := os.ReadFile(result.ModulePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `r.Get("/widget"`) || !strings.Contains(string(src), `r.Resource("/widgets"`) {
		t.Fatalf("singular module must keep page /widget + resource /widgets:\n%s", src)
	}
}

func TestStartAppPluralNameDoesNotDuplicateGetRoute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/shop\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := runStartApp([]string{"products", "--out", dir, "--skip-migration"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runStartApp: %v\nstderr: %s", err, stderr.String())
	}

	src, err := os.ReadFile(filepath.Join(dir, "internal", "modules", "products_module.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `r.Get("/products/page"`) {
		t.Fatalf("plural startapp module must move the page off the resource Index path:\n%s", src)
	}
	if strings.Contains(string(src), `r.Get("/products",`) {
		t.Fatalf("plural startapp module still registers the page on the resource path — this pair panics at boot:\n%s", src)
	}
	if !strings.Contains(string(src), `r.Resource("/products"`) {
		t.Fatalf("resource path must stay /products:\n%s", src)
	}
}
