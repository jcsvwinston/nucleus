package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunNewScaffold verifies that `nucleus new` produces a fluent-surface
// project for both templates: a thin root main.go built on pkg/nucleus (not
// pkg/app), a feature module package, and — for mvc — a scoped RBAC policy
// file wired through nucleus.yml.
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

			// The old composition root must be gone.
			if _, err := os.Stat(filepath.Join(projectDir, "cmd", "server", "main.go")); err == nil {
				t.Errorf("%s: legacy cmd/server/main.go should not be scaffolded", tc.template)
			}

			// Feature module package must exist.
			mustExist(t, filepath.Join(projectDir, "internal", "articles", "module.go"))

			if tc.mvc {
				// Web module replaces the old controllers/home_page.go.
				mustExist(t, filepath.Join(projectDir, "internal", "web", "module.go"))
				if _, err := os.Stat(filepath.Join(projectDir, "internal", "controllers", "home_page.go")); err == nil {
					t.Errorf("mvc: legacy internal/controllers/home_page.go should not be scaffolded")
				}

				// Scoped RBAC policy file, wired through config.
				mustExist(t, filepath.Join(projectDir, "rbac_policy.csv"))
				cfgBody := readFile(t, filepath.Join(projectDir, "nucleus.yml"))
				if !strings.Contains(cfgBody, "admin_rbac_policy_file") {
					t.Errorf("mvc: nucleus.yml missing admin_rbac_policy_file\n%s", cfgBody)
				}

				// The default authz middleware maps HTTP methods to CRUD action
				// verbs (GET->read, POST->create, ...). A policy written against
				// raw HTTP methods never matches, so every route 403s. Lock the
				// verb form so that runtime regression cannot reappear silently.
				policyBody := readFile(t, filepath.Join(projectDir, "rbac_policy.csv"))
				if !strings.Contains(policyBody, ", read, allow") {
					t.Errorf("mvc: rbac_policy.csv must grant the CRUD 'read' action:\n%s", policyBody)
				}
				if strings.Contains(policyBody, ", GET, ") || strings.Contains(policyBody, ", POST, ") {
					t.Errorf("mvc: rbac_policy.csv uses raw HTTP methods; must use CRUD verbs (read/create/update/delete):\n%s", policyBody)
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

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file %s to exist: %v", path, err)
	}
}
