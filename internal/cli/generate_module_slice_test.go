// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Executable-scaffold guard for `nucleus generate module` (ADR-022 §4).
// The slice's whole promise is "Mount() is the entire integration", so the
// E2E proves it by execution AND by omission: after scaffolding, the ONLY
// edit is the Mount line in main.go — no rbac_policy.csv rows, no
// csrf_exempt_paths, no `nucleus migrate` run — and the page, the JSON API
// and the embedded-migration-backed persistence must all answer over real
// HTTP. The test pins the omission too: the policy file's bytes are
// asserted unchanged.
package cli

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateModuleScaffoldIsSelfContained(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots a scaffolded app; skipped with -short")
	}
	repoRoot := repoRootForTest(t)

	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	newArgs := []string{"myapp", "--out", outDir, "--template", "mvc", "--module", "example.com/myapp"}
	if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "myapp")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)

	stdout.Reset()
	stderr.Reset()
	if err := runGenerate([]string{"module", "widget", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate module: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "widget.Module()") {
		t.Fatalf("generate module must print the Mount line, got:\n%s", stdout.String())
	}

	// Snapshot the policy file BEFORE booting: the module must not need it.
	policyPath := filepath.Join(projectDir, "rbac_policy.csv")
	policyBefore, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}

	// The single edit the contract allows: Mount the module in main.go.
	mainPath := filepath.Join(projectDir, "main.go")
	mainSrc, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(mainSrc),
		"\"github.com/jcsvwinston/nucleus/pkg/nucleus\"",
		"\"example.com/myapp/internal/widget\"\n\n\t\"github.com/jcsvwinston/nucleus/pkg/nucleus\"", 1)
	patched = strings.Replace(patched, "if err := nucleus.New().",
		"if err := nucleus.New().\n\t\tMount(widget.Module()).", 1)
	if !strings.Contains(patched, "Mount(widget.Module())") {
		t.Fatalf("could not add the Mount line to scaffold main.go:\n%s", mainSrc)
	}
	if err := os.WriteFile(mainPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	// Free port into nucleus.yml.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cfgPath := filepath.Join(projectDir, "nucleus.yml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := strings.Replace(string(cfg), "port: 8080", fmt.Sprintf("port: %d", port), 1)
	if err := os.WriteFile(cfgPath, []byte(cfgStr), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build and boot. Deliberately NO `nucleus migrate` run: the module
	// applies its embedded migrations itself on start.
	build := exec.Command("go", "build", "-o", "app", ".")
	build.Dir = projectDir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the scaffold+module app: %v\n%s", err, out)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	_ = bootScaffoldApp(t, projectDir, base)
	client := &http.Client{Timeout: 2 * time.Second}

	// (1) The embedded page template renders through the framework engine.
	resp, err := client.Get(base + "/widget")
	if err != nil {
		t.Fatalf("GET /widget: %v", err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("module page: want 200 text/html, got %d (%s) body=%s", resp.StatusCode, resp.Header.Get("Content-Type"), page)
	}

	// (2) Cookie-less JSON POST persists through the embedded-migration
	// schema: module policy row + module CSRF exemption + applied SQL, all
	// carried by the slice.
	resp, err = client.Post(base+"/widgets", "application/json", strings.NewReader(`{"name":"vertical"}`))
	if err != nil {
		t.Fatalf("POST /widgets: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /widgets: want 201, got %d body=%s", resp.StatusCode, body)
	}

	// (3) The API reads back what it wrote.
	resp, err = client.Get(base + "/widgets")
	if err != nil {
		t.Fatalf("GET /widgets: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"vertical"`) {
		t.Errorf("GET /widgets: want 200 with the created record, got %d body=%s", resp.StatusCode, body)
	}

	// (4) The omission, pinned: the host policy file was never touched.
	policyAfter, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(policyBefore, policyAfter) {
		t.Error("rbac_policy.csv changed — the slice's promise is that it never needs host policy edits")
	}
}

// NC-01/GF-02 (P0): an ALREADY-PLURAL module name used to generate a page
// route and a resource Index on the SAME "GET /<name>" pattern —
// pluralizeResource returns trailing-s names unchanged — and the app
// panicked at boot ("pattern GET /products conflicts with pattern GET
// /products"). The generator printed "Nothing else" anyway. This test is
// the executable form of the audit repro: generate module products, Mount
// it, and the app must BOOT and serve — page at /products/page, JSON API
// at /products.
func TestGenerateModulePluralNameBootsWithoutRouteConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots a scaffolded app; skipped with -short")
	}
	repoRoot := repoRootForTest(t)

	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	newArgs := []string{"shop", "--out", outDir, "--template", "mvc", "--module", "example.com/shop"}
	if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "shop")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)

	stdout.Reset()
	stderr.Reset()
	if err := runGenerate([]string{"module", "products", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runGenerate module: %v\nstderr: %s", err, stderr.String())
	}
	// The generator must say where the page moved, so the printed route
	// table matches what actually mounts.
	if !strings.Contains(stdout.String(), "/products/page") {
		t.Fatalf("generate module must print the real page route for plural names, got:\n%s", stdout.String())
	}

	// The single edit the contract allows: Mount the module in main.go.
	mainPath := filepath.Join(projectDir, "main.go")
	mainSrc, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(mainSrc),
		"\"github.com/jcsvwinston/nucleus/pkg/nucleus\"",
		"\"example.com/shop/internal/products\"\n\n\t\"github.com/jcsvwinston/nucleus/pkg/nucleus\"", 1)
	patched = strings.Replace(patched, "if err := nucleus.New().",
		"if err := nucleus.New().\n\t\tMount(products.Module()).", 1)
	if !strings.Contains(patched, "Mount(products.Module())") {
		t.Fatalf("could not add the Mount line to scaffold main.go:\n%s", mainSrc)
	}
	if err := os.WriteFile(mainPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	// Free port into nucleus.yml.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	cfgPath := filepath.Join(projectDir, "nucleus.yml")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := strings.Replace(string(cfg), "port: 8080", fmt.Sprintf("port: %d", port), 1)
	if err := os.WriteFile(cfgPath, []byte(cfgStr), 0o644); err != nil {
		t.Fatal(err)
	}

	build := exec.Command("go", "build", "-o", "app", ".")
	build.Dir = projectDir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the scaffold+module app: %v\n%s", err, out)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// THE regression: before the fix the process panicked here and never
	// answered /healthz.
	_ = bootScaffoldApp(t, projectDir, base)
	client := &http.Client{Timeout: 2 * time.Second}

	// (1) JSON API on /products (resource Index, not the HTML page).
	resp, err := client.Get(base + "/products")
	if err != nil {
		t.Fatalf("GET /products: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("GET /products: want 200 application/json, got %d (%s) body=%s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}

	// (2) The page still exists, on its non-conflicting path.
	resp, err = client.Get(base + "/products/page")
	if err != nil {
		t.Fatalf("GET /products/page: %v", err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("module page: want 200 text/html, got %d (%s) body=%s", resp.StatusCode, resp.Header.Get("Content-Type"), page)
	}

	// (3) Writes persist through the slice's own policy/CSRF/migrations.
	resp, err = client.Post(base+"/products", "application/json", strings.NewReader(`{"name":"tornillo"}`))
	if err != nil {
		t.Fatalf("POST /products: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /products: want 201, got %d body=%s", resp.StatusCode, body)
	}
}
