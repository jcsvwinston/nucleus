package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// repoRootForTest resolves the repository root (the directory containing the
// top-level go.mod for github.com/jcsvwinston/nucleus) from this test file's
// runtime path. The test file lives in internal/cli/, so the root is two
// directories up.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate repo root")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root go.mod not found at %s: %v", root, err)
	}
	return root
}

var nucleusRequireRE = regexp.MustCompile(`(?m)^require github\.com/jcsvwinston/nucleus .*$`)

// pinGoModToLocalNucleus rewrites the generated go.mod so the published
// nucleus dependency (pinned to "latest" or a release tag, neither resolvable
// offline) resolves to the local working copy. It replaces the require line
// with a syntactically valid placeholder version and appends a `replace`
// directive pointing at the repo root, putting the generated demo project back
// under the compiler without any network access.
func pinGoModToLocalNucleus(t *testing.T, projectDir, repoRoot string) {
	t.Helper()
	goModPath := filepath.Join(projectDir, "go.mod")
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read generated go.mod: %v", err)
	}

	updated := nucleusRequireRE.ReplaceAllString(
		string(raw),
		"require github.com/jcsvwinston/nucleus v0.0.0",
	)
	if updated == string(raw) {
		t.Fatalf("generated go.mod did not contain the expected nucleus require line:\n%s", raw)
	}
	// Quote the replace target so a repo path containing spaces stays a valid
	// go.mod directive.
	updated += "\nreplace github.com/jcsvwinston/nucleus => \"" + repoRoot + "\"\n"

	if err := os.WriteFile(goModPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write patched go.mod: %v", err)
	}
}

func runGoCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s (in %s) failed: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// TestRunNewGeneratesBuildableProject is the heart of the embedded-template
// refactor: it renders both starter templates and puts them back under the Go
// compiler. Because the templates are now real files (not string literals),
// a regression like the previously-shipped RBAC verb bug — or any malformed
// generated Go — would surface as a build failure here.
//
// The full `go build` is gated behind -short so the fast unit lane can skip it,
// but it runs by default.
func TestRunNewGeneratesBuildableProject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go build of generated project in -short mode")
	}

	repoRoot := repoRootForTest(t)

	for _, tmpl := range []string{"api", "mvc"} {
		tmpl := tmpl
		t.Run(tmpl, func(t *testing.T) {
			outDir := t.TempDir()
			projectName := "buildcheck"

			var stdout, stderr bytes.Buffer
			args := []string{projectName, "--out", outDir, "--template", tmpl, "--module", "example.com/buildcheck"}
			if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("runNew(%s) failed: %v\nstderr: %s", tmpl, err, stderr.String())
			}

			projectDir := filepath.Join(outDir, projectName)
			pinGoModToLocalNucleus(t, projectDir, repoRoot)

			runGoCommand(t, projectDir, "mod", "tidy")
			runGoCommand(t, projectDir, "build", "./...")
		})
	}
}

// TestRunNewGeneratesBuildableProject_PublishedModule is the network-gated
// sibling of TestRunNewGeneratesBuildableProject. It scaffolds both templates
// and builds them WITHOUT the local `replace` directive, so `go mod tidy`
// actually downloads the pinned `github.com/jcsvwinston/nucleus` release and
// the build exercises the published-module path a real user hits. This is the
// only test that would catch a broken pin (e.g. the floating "latest" that
// resolveFrameworkVersion no longer emits) or a published go.mod incompatible
// with the generated `go`/`toolchain` directives.
//
// It is gated behind NUCLEUS_NETWORK_TESTS=1 (and -short) so the default
// offline lane — TestRunNewGeneratesBuildableProject above, which it does not
// replace or weaken — stays hermetic.
func TestRunNewGeneratesBuildableProject_PublishedModule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping published-module go build in -short mode")
	}
	if os.Getenv("NUCLEUS_NETWORK_TESTS") != "1" {
		t.Skip("skipping published-module build; set NUCLEUS_NETWORK_TESTS=1 to run (requires network access to the module proxy)")
	}

	for _, tmpl := range []string{"api", "mvc"} {
		tmpl := tmpl
		t.Run(tmpl, func(t *testing.T) {
			outDir := t.TempDir()
			projectName := "buildcheck"

			var stdout, stderr bytes.Buffer
			args := []string{projectName, "--out", outDir, "--template", tmpl, "--module", "example.com/buildcheck"}
			if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("runNew(%s) failed: %v\nstderr: %s", tmpl, err, stderr.String())
			}

			projectDir := filepath.Join(outDir, projectName)

			// Guard the reproducibility contract: the generated require line
			// must pin a concrete version (what resolveFrameworkVersion
			// returns), never the floating "latest" pseudo-version.
			raw, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
			if err != nil {
				t.Fatalf("read generated go.mod: %v", err)
			}
			gomod := string(raw)
			if strings.Contains(gomod, "nucleus latest") {
				t.Fatalf("generated go.mod pins the floating \"latest\"; expected a concrete tag:\n%s", gomod)
			}
			wantRequire := "require github.com/jcsvwinston/nucleus " + resolveFrameworkVersion()
			if !strings.Contains(gomod, wantRequire) {
				t.Fatalf("generated go.mod missing expected pinned require %q:\n%s", wantRequire, gomod)
			}

			// No replace directive: resolve and build against the published
			// module exactly as a real user would.
			runGoCommand(t, projectDir, "mod", "tidy")
			runGoCommand(t, projectDir, "build", "./...")
		})
	}
}

// writeLocalNucleusGoMod writes a minimal go.mod into dir wiring the published
// nucleus dependency to the local working copy via a `replace`, so a generated
// tree compiles offline. It mirrors pinGoModToLocalNucleus but for a directory
// that has no scaffold-produced go.mod (the `nucleus generate` path used
// outside a project, where detectModulePath returns hasModule=false and the
// in-memory handler template is emitted).
func writeLocalNucleusGoMod(t *testing.T, dir, module, repoRoot string) {
	t.Helper()
	gomod := "module " + module + "\n\n" +
		"go " + scaffoldGoVersion + "\n\n" +
		"toolchain " + scaffoldToolchain + "\n\n" +
		"require github.com/jcsvwinston/nucleus v0.0.0\n\n" +
		// Quote the target so a repo path with spaces stays valid.
		"replace github.com/jcsvwinston/nucleus => \"" + repoRoot + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatalf("write local nucleus go.mod: %v", err)
	}
}

// TestRunGenerateResourceBuilds compile-checks the `nucleus generate resource`
// output for BOTH handler styles, against the local nucleus via `replace` (no
// network). It is the regression guard for the writeError-arity and
// router.ResourceHandlers-signature fixes in the in-memory handler template,
// and for the dead-helper removal in the with-service template — all three were
// latent because nothing built the generated controllers.
//
//   - "in_memory": generate into a bare directory (no pre-existing go.mod), so
//     detectModulePath reports hasModule=false and resourceHandlerTemplate (the
//     in-memory, writeJSON/writeError style) is emitted; then wrap a module and
//     build.
//   - "with_service": scaffold a project first (so a go.mod exists), then
//     generate inside it, exercising resourceHandlerWithServiceTemplate (the
//     router.Context style, from which the dead helpers were removed).
//
// Gated behind -short like TestRunNewGeneratesBuildableProject.
func TestRunGenerateResourceBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go build of generated resource in -short mode")
	}

	repoRoot := repoRootForTest(t)

	t.Run("in_memory", func(t *testing.T) {
		projectDir := t.TempDir()

		var stdout, stderr bytes.Buffer
		args := []string{"resource", "Widget", "--out", projectDir}
		if err := runGenerate(args, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runGenerate(resource) failed: %v\nstderr: %s", err, stderr.String())
		}

		// Sanity-check we exercised the in-memory template (no module ⇒
		// net/http-style handler), not the with-service one.
		handler, err := os.ReadFile(filepath.Join(projectDir, "internal", "controllers", "widget_handler.go"))
		if err != nil {
			t.Fatalf("read generated handler: %v", err)
		}
		if !strings.Contains(string(handler), "router.FromHTTP(") {
			t.Fatalf("expected in-memory handler to adapt handlers via router.FromHTTP; got:\n%s", handler)
		}

		writeLocalNucleusGoMod(t, projectDir, "example.com/genresource", repoRoot)
		runGoCommand(t, projectDir, "mod", "tidy")
		runGoCommand(t, projectDir, "build", "./...")
	})

	t.Run("with_service", func(t *testing.T) {
		outDir := t.TempDir()
		projectName := "genresource"

		var stdout, stderr bytes.Buffer
		newArgs := []string{projectName, "--out", outDir, "--template", "mvc", "--module", "example.com/genresource"}
		if err := runNew(newArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew failed: %v\nstderr: %s", err, stderr.String())
		}
		projectDir := filepath.Join(outDir, projectName)

		stdout.Reset()
		stderr.Reset()
		genArgs := []string{"resource", "Widget", "--out", projectDir}
		if err := runGenerate(genArgs, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runGenerate(resource) failed: %v\nstderr: %s", err, stderr.String())
		}

		// Sanity-check we exercised the with-service template (module present
		// ⇒ router.Context handlers returning error).
		handler, err := os.ReadFile(filepath.Join(projectDir, "internal", "controllers", "widget_handler.go"))
		if err != nil {
			t.Fatalf("read generated handler: %v", err)
		}
		if !strings.Contains(string(handler), "func (h *WidgetHandler) List(c *router.Context) error") {
			t.Fatalf("expected with-service handler with router.Context signature; got:\n%s", handler)
		}

		pinGoModToLocalNucleus(t, projectDir, repoRoot)
		runGoCommand(t, projectDir, "mod", "tidy")
		runGoCommand(t, projectDir, "build", "./...")
	})
}
