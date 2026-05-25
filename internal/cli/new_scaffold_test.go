package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunNewScaffold verifies that `nucleus new` produces an empty SKELETON on
// the fluent surface: a thin root main.go built on pkg/nucleus (not pkg/app)
// that mounts NO modules, plus config and an empty migrations/ dir. It must NOT
// bake in any demo feature code (that lives in examples/mvc_api, not the CLI).
func TestRunNewScaffold(t *testing.T) {
	cases := []struct {
		name     string
		template string
		mvc      bool
	}{
		{name: "api", template: "api"},
		{name: "mvc", template: "mvc", mvc: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			projectName := "scaffoldcheck"

			var stdout, stderr bytes.Buffer
			args := []string{projectName, "--out", outDir, "--template", tc.template}
			if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("runNew(%s) failed: %v\nstderr: %s", tc.template, err, stderr.String())
			}

			projectDir := filepath.Join(outDir, projectName)

			// Root main.go must exist, be on the fluent surface, and must
			// not reference the low-level app.New entry point.
			mainBody := readFile(t, filepath.Join(projectDir, "main.go"))
			if !strings.Contains(mainBody, "nucleus.New(") {
				t.Errorf("%s: root main.go does not use nucleus.New(\n%s", tc.template, mainBody)
			}
			if strings.Contains(mainBody, "app.New(") {
				t.Errorf("%s: root main.go still references app.New(\n%s", tc.template, mainBody)
			}

			// WithoutDefaults() distinguishes the lightweight api template
			// (core-only, no admin/authz) from the full mvc template. Lock the
			// distinction so an edit can't silently flip a template's surface.
			hasWithoutDefaults := strings.Contains(mainBody, "WithoutDefaults()")
			if tc.mvc && hasWithoutDefaults {
				t.Errorf("mvc: root main.go must NOT call WithoutDefaults() (full app)\n%s", mainBody)
			}
			if !tc.mvc && !hasWithoutDefaults {
				t.Errorf("api: root main.go must call WithoutDefaults() (core-only)\n%s", mainBody)
			}

			// Skeleton mounts NO modules — the demo CRUD is gone. Check the
			// code only: the doc comment legitimately shows a Mount(...) example
			// as guidance for adding the user's first module.
			if strings.Contains(stripComments(mainBody), "Mount(") {
				t.Errorf("%s: skeleton main.go must not Mount any module\n%s", tc.template, mainBody)
			}

			// No baked-in demo feature code anywhere in the core scaffold output.
			for _, demo := range []string{
				filepath.Join("cmd", "server", "main.go"),
				filepath.Join("internal", "articles", "module.go"),
				filepath.Join("internal", "models", "article.go"),
				filepath.Join("internal", "controllers", "article_api.go"),
				filepath.Join("internal", "web", "module.go"),
			} {
				if _, err := os.Stat(filepath.Join(projectDir, demo)); err == nil {
					t.Errorf("%s: skeleton must not scaffold demo file %s", tc.template, demo)
				}
			}

			// The skeleton has the config, README, and an empty migrations/ dir.
			mustExist(t, filepath.Join(projectDir, "nucleus.yml"))
			mustExist(t, filepath.Join(projectDir, "migrations"))
			readme := readFile(t, filepath.Join(projectDir, "README.md"))
			if !strings.Contains(readme, "examples/mvc_api") {
				t.Errorf("%s: README should point readers to examples/mvc_api\n%s", tc.template, readme)
			}

			if tc.mvc {
				// Full app: a minimal RBAC policy grants anonymous the built-in
				// health endpoint (default-deny Casbin otherwise). It uses the
				// CRUD action verb (read), NOT a raw HTTP method, and grants no
				// demo-resource writes.
				cfgBody := readFile(t, filepath.Join(projectDir, "nucleus.yml"))
				if !strings.Contains(cfgBody, "admin_rbac_policy_file") {
					t.Errorf("mvc: nucleus.yml missing admin_rbac_policy_file\n%s", cfgBody)
				}
				policyBody := readFile(t, filepath.Join(projectDir, "rbac_policy.csv"))
				if !strings.Contains(policyBody, ", read, allow") {
					t.Errorf("mvc: rbac_policy.csv must grant a CRUD 'read' action:\n%s", policyBody)
				}
				if strings.Contains(policyBody, ", GET, ") || strings.Contains(policyBody, ", POST, ") {
					t.Errorf("mvc: rbac_policy.csv uses raw HTTP methods; must use CRUD verbs:\n%s", policyBody)
				}
				if strings.Contains(policyBody, "/api/articles") {
					t.Errorf("mvc: skeleton rbac_policy.csv must not reference the demo /api/articles route:\n%s", policyBody)
				}
			} else {
				// api template is core-only: no admin RBAC file.
				if _, err := os.Stat(filepath.Join(projectDir, "rbac_policy.csv")); err == nil {
					t.Errorf("api: rbac_policy.csv should not be scaffolded for the core-only template")
				}
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// stripComments removes whole-line `//` comments so assertions on generated Go
// source inspect the actual code, not illustrative examples in doc comments.
func stripComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %s to exist: %v", path, err)
	}
}
