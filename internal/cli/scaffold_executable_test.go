// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// The executable-scaffold guard (QCD-FW-7's systemic half). Twice now a
// generator and the runtime disagreed and nothing in CI noticed, because
// what the CLI generates was never RUN against the framework: QCD-CLI-4
// (generate resource emitted SQLite DDL against a PostgreSQL database) and
// QCD-FW-7 (startapp scaffolds its template into a subdirectory the flat
// template glob never loaded — every c.HTML answered
// ErrTemplateEngineNotSet on a fresh project).
//
// Contract: every artifact-producing command must have a test that
// scaffolds into a temp dir and proves the result is USABLE by the runtime
// — compiles, boots, and delivers what the artifact promises. Coverage map
// (one owner per command; heavy E2Es are not duplicated here):
//
//	new                → TestRunNewGeneratesBuildableProject (build) +
//	                     TestQuickstartExampleWorksOnScaffold (boot + HTTP)
//	startapp           → TestStartappScaffoldServesPageAndAPI (this file:
//	                     boot + framework-rendered page + REST API + probe
//	                     c.HTML against the generated template)
//	generate resource  → TestGenerateResourceProducesMountablePersistentModule
//	                     (boot + migrate + POST + process restart persistence)
//	generate model/handler/service/repository
//	                   → TestGenerateMultipleEntitiesBuilds (go vet over the
//	                     generated tree; these artifacts are inert code with
//	                     no runtime wiring of their own — the resource/startapp
//	                     E2Es exercise the wired variants)
//	generate migration → covered inside the resource E2E (migrate up applies
//	                     the generated pair against the configured dialect)
//	generate module    → TestGenerateModuleScaffoldIsSelfContained
//	                     (boot + page + API + embedded-migration persistence,
//	                     with rbac_policy.csv pinned untouched — the slice
//	                     carries its own policies/CSRF/migrations/templates)
//
// Exclusions: `wizard` (interactive front-end over the commands above — the
// canonical commands carry the contract), `inspectdb` (requires an external
// populated database; its output shape is pinned by TestRun_InspectDB-style
// unit tests).
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

// TestStartappScaffoldServesPageAndAPI boots a `nucleus new` + `nucleus
// startapp` project and demands, over real HTTP:
//
//  1. the scaffolded page route answers 200 text/html — rendered through the
//     framework's template engine;
//  2. a probe handler calling c.HTML with the GENERATED template's
//     documented name (its path relative to templates_dir) answers 200 —
//     this is the exact QCD-FW-7 repro: on v1.8.1 the engine is nil and the
//     probe answers 500 ErrTemplateEngineNotSet;
//  3. the scaffolded REST API answers 200.
func TestStartappScaffoldServesPageAndAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and boots a scaffolded app; skipped with -short")
	}
	repoRoot := repoRootForTest(t)

	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	newArgs := []string{"myapp", "--out", outDir, "--offline", "--template", "mvc", "--module", "example.com/myapp"}
	if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "myapp")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)

	stdout.Reset()
	stderr.Reset()
	if err := runStartApp([]string{"fieldservice", "--out", projectDir}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runStartApp: %v\nstderr: %s", err, stderr.String())
	}

	// Mount the generated module.
	mainPath := filepath.Join(projectDir, "main.go")
	mainSrc, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(mainSrc),
		"\"github.com/jcsvwinston/nucleus/pkg/nucleus\"",
		"\"example.com/myapp/internal/modules\"\n\n\t\"github.com/jcsvwinston/nucleus/pkg/nucleus\"", 1)
	patched = strings.Replace(patched, "if err := nucleus.New().",
		"if err := nucleus.New().\n\t\tMount(modules.FieldserviceModule()).\n\t\tMount(modules.TemplateProbeModule()).", 1)
	if !strings.Contains(patched, "Mount(modules.FieldserviceModule())") {
		t.Fatalf("could not add Mount lines to scaffold main.go:\n%s", mainSrc)
	}
	if err := os.WriteFile(mainPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	// The probe: a user handler rendering the GENERATED template through the
	// framework engine, by its documented name (path relative to
	// templates_dir). This is what the demo's first real MVC handler did.
	probe := `package modules

import (
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func TemplateProbeModule() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "template_probe",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/probe", func(c *nucleus.Context) error {
				// nucleus.Context.HTML(code, raw) shadows the template render;
				// the embedded router.Context carries the engine-backed variant.
				return c.Context.HTML(http.StatusOK, "fieldservice/index.html", map[string]interface{}{})
			})
		},
	}.Build()
}
`
	if err := os.WriteFile(filepath.Join(projectDir, "internal", "modules", "probe_module.go"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}

	// Default-deny RBAC: allow the three routes; CSRF exemption for the API.
	policyPath := filepath.Join(projectDir, "rbac_policy.csv")
	policy, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := `
p, anonymous, /fieldservice, read, allow
p, anonymous, /probe, read, allow
p, anonymous, /fieldservices, read, allow
p, anonymous, /fieldservices, create, allow
`
	if err := os.WriteFile(policyPath, append(policy, []byte(rows)...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Free port.
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

	// Migrate (the startapp migration pair applies against the configured
	// dialect) and boot.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	var migOut, migErr bytes.Buffer
	if err := runMigrate([]string{"up"}, strings.NewReader(""), &migOut, &migErr); err != nil {
		t.Fatalf("migrate up: %v\nstderr: %s", err, migErr.String())
	}

	build := exec.Command("go", "build", "-o", "app", ".")
	build.Dir = projectDir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the scaffold app: %v\n%s", err, out)
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	_ = bootScaffoldApp(t, projectDir, base)
	client := &http.Client{Timeout: 2 * time.Second}

	get := func(path string) (int, string, string) {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(body), resp.Header.Get("Content-Type")
	}

	// (2) The QCD-FW-7 repro: c.HTML against the generated template.
	if code, body, ct := get("/probe"); code != http.StatusOK || !strings.Contains(ct, "text/html") {
		t.Errorf("c.HTML with the scaffolded template's documented name: want 200 text/html, got %d (%s) body=%s — ErrTemplateEngineNotSet means the framework loader did not see the scaffold's subdirectory (QCD-FW-7)", code, ct, body)
	}

	// (1) The scaffolded page route itself.
	if code, body, ct := get("/fieldservice"); code != http.StatusOK || !strings.Contains(ct, "text/html") {
		t.Errorf("scaffolded page route: want 200 text/html, got %d (%s) body=%s", code, ct, body)
	}

	// (3) The scaffolded REST API.
	if code, body, _ := get("/fieldservices"); code != http.StatusOK {
		t.Errorf("scaffolded API route: want 200, got %d body=%s", code, body)
	}
}
