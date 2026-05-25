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
