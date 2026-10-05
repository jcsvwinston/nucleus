// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewModuleTemplateWritesAModuleRepository: --template module writes a
// repository an application can `go get` — go.mod, the module, a test that
// calls nucleustest.CheckModule, a README and a CI workflow — and none of an
// application's files.
func TestNewModuleTemplateWritesAModuleRepository(t *testing.T) {
	stubScaffoldNetwork(t)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := runNew([]string{"Order-Notes", "--template", "module", "--module", "github.com/acme/ordernotes", "--out", out, "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v\n%s", err, stderr.String())
	}
	dir := filepath.Join(out, "Order-Notes")
	read := func(rel string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		return string(raw)
	}
	goMod := read("go.mod")
	for _, want := range []string{"module github.com/acme/ordernotes\n", "go " + scaffoldGoVersion + "\n", "require github.com/jcsvwinston/nucleus " + resolveFrameworkVersion() + "\n"} {
		if !strings.Contains(goMod, want) {
			t.Errorf("go.mod lacks %q:\n%s", want, goMod)
		}
	}
	module, test := read("module.go"), read("module_test.go")
	for name, src := range map[string]string{"module.go": module, "module_test.go": test} {
		formatted, err := format.Source([]byte(src))
		if err != nil {
			t.Errorf("%s does not parse: %v\n%s", name, err, src)
		} else if string(formatted) != src {
			t.Errorf("%s is not gofmt-clean:\n%s", name, src)
		}
	}
	for _, want := range []string{"package ordernotes\n", `const Name = "order_notes"`, "nucleus.Module[Config]{", "nucleus.Provide(rt, greeter)", `Prefix: "/" + Name`} {
		if !strings.Contains(module, want) {
			t.Errorf("module.go lacks %q", want)
		}
	}
	for _, want := range []string{"package ordernotes_test\n", "nucleustest.CheckModule(t, ordernotes.Module())", `"github.com/acme/ordernotes"`, `_ "github.com/jcsvwinston/nucleus/drivers/sqlite"`, "DependsOn: []string{ordernotes.Name}"} {
		if !strings.Contains(test, want) {
			t.Errorf("module_test.go lacks %q", want)
		}
	}
	if workflow := read(".github/workflows/test.yml"); !strings.Contains(workflow, "run: go test ./...") || !strings.Contains(workflow, "go-version-file: go.mod") {
		t.Errorf("the workflow does not run the tests:\n%s", workflow)
	}
	if !strings.Contains(read(".github/dependabot.yml"), "package-ecosystem: github-actions") {
		t.Error("dependabot.yml does not keep the pinned actions current")
	}
	if readme := read("README.md"); !strings.Contains(readme, "go get github.com/acme/ordernotes") || !strings.Contains(readme, "Mount(ordernotes.Module())") {
		t.Errorf("README:\n%s", readme)
	}
	for _, absent := range []string{"main.go", "nucleus.yml", "Dockerfile", "migrations"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("a module repository got the application file %s", absent)
		}
	}
	for _, want := range []string{"module name: order_notes, package: ordernotes", "go test ./...", "# skipped by --offline", "concepts/writing-a-module"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the post-scaffold text lacks %q:\n%s", want, stdout.String())
		}
	}
}

// Without --offline the scaffold fetches the SQLite driver its test opens a
// database with, at the version released with this CLI, and tidies.
func TestNewModuleTemplateResolvesTheTestDriver(t *testing.T) {
	calls := stubScaffoldNetwork(t)
	var stdout, stderr bytes.Buffer
	if err := runNew([]string{"greeter", "--template", "module", "--out", t.TempDir()}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	want := []string{"go get " + atRelease(t, "sqlite") + " in greeter", "go mod tidy in greeter"}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("network step:\n%s\nwant:\n%s", strings.Join(*calls, "\n"), strings.Join(want, "\n"))
	}
}

func TestNewModuleTemplateRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"greeter", "--template", "module", "--db", "postgres"}, "--db does not apply to --template module"},
		{[]string{"greeter", "--template", "module", "--with", "s3", "--port", "9090"}, "--port, --with does not apply"},
		{[]string{"1greeter", "--template", "module"}, "cannot name a module"},
		{[]string{"type", "--template", "module"}, "cannot name the module's Go package"},
		{[]string{"nucleus", "--template", "module"}, "cannot name the module's Go package"},
	} {
		stubScaffoldNetwork(t)
		err := runNew(append(tc.args, "--out", t.TempDir(), "--offline"), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("nucleus new %s: err = %v, want %q", strings.Join(tc.args, " "), err, tc.want)
		}
	}
}

// TestNewModuleTemplateTestsPassAgainstThisCheckout is the template's own
// proof: rendered, pointed at this working copy and run outside any
// workspace, its tests — CheckModule among them — pass.
func TestNewModuleTemplateTestsPassAgainstThisCheckout(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: builds and tests the scaffolded module")
	}
	stubScaffoldNetwork(t)
	out := t.TempDir()
	if err := runNew([]string{"greeter", "--template", "module", "--out", out, "--offline"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(out, "greeter")
	pinGoModToLocalNucleus(t, dir, repoRootForTest(t))
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	runGoCommand(t, dir, "mod", "tidy")
	runGoCommand(t, dir, "vet", "./...")
	runGoCommand(t, dir, "test", "./...")
}
