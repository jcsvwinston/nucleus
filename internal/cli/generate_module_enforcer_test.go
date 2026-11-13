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

// projectWithChain writes a project — a go.mod and a package main whose
// nucleus.New() chain ends with the given calls — and returns its root.
// An empty chain and a false hasMain write no main.go at all: a project
// the generator cannot read a composition root from.
func projectWithChain(t *testing.T, chain string, hasMain bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasMain {
		return dir
	}
	writeGoFile(t, dir, "main.go", mainWithChain(chain))
	return dir
}

// mainWithChain is a package main whose nucleus.New() chain ends with the
// given calls, with a doc comment that quotes a chain nothing calls.
func mainWithChain(chain string) string {
	return "package main\n\nimport \"github.com/jcsvwinston/nucleus/pkg/nucleus\"\n\n" +
		"// nucleus.New().WithoutDefaults() — a doc comment does not count.\n" +
		"func main() {\n\t_ = nucleus.New().FromConfigFile(\"nucleus.yml\")" + chain + ".Start()\n}\n"
}

// secureHelper is a second file of package main that adds the enforcer to
// the chain main.go starts — an option the generator must see although it
// is not in main.go.
const secureHelper = "package main\n\nimport \"github.com/jcsvwinston/nucleus/pkg/nucleus\"\n\n" +
	"// secure adds default-deny authorization to the chain.\n" +
	"func secure(b *nucleus.AppBuilder) *nucleus.AppBuilder { return b.WithAuthz() }\n"

// writeGoFile writes src at dir/name, creating the directory.
func writeGoFile(t *testing.T, dir, name, src string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// generated is what `generate module notes` printed and wrote.
type generated struct {
	stdout, module, test, storage string
}

// generateNotes runs `generate module notes` on dir, offline and for
// sqlite, and returns what it printed with the module, test and storage
// sources.
func generateNotes(t *testing.T, dir string, extra ...string) generated {
	t.Helper()
	var out, errOut bytes.Buffer
	args := append([]string{"module", "notes", "--out", dir, "--dialect", "sqlite", "--offline"}, extra...)
	if err := runGenerate(args, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("runGenerate %v: %v\nstderr: %s", args, err, errOut.String())
	}
	return generated{
		stdout:  out.String(),
		module:  readFile(t, filepath.Join(dir, "internal", "notes", "module.go")),
		test:    readFile(t, filepath.Join(dir, "internal", "notes", "module_test.go")),
		storage: readFile(t, filepath.Join(dir, "internal", "notes", "notes.go")),
	}
}

// TestGenerateModule_RowsFollowTheCompositionRoot is D9 of the 2.0 contract
// in 1.x: on a root built WithoutDefaults() without WithAuthz() — the api
// starter's contract — the generator writes the module's policy rows
// commented out, says so, and the test it writes boots the same way and
// expects the anonymous write to land; on a root that builds an enforcer
// (the default stack, or WithoutDefaults() with WithAuthz(), in either
// spelling of the option, and wherever in the package the option is
// called) it writes what it always wrote. A root it cannot read is the
// default stack. The rows are decided by what the package main CALLS: the
// doc comment that quotes the chain does not count.
func TestGenerateModule_RowsFollowTheCompositionRoot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chain    string
		hasMain  bool
		enforced bool
	}{
		{name: "WithoutDefaults without WithAuthz", chain: ".WithoutDefaults().WithStorage().WithRateLimit()", hasMain: true},
		{name: "WithoutDefaults with WithAuthz", chain: ".WithoutDefaults().WithAuthz()", hasMain: true, enforced: true},
		{name: "the default stack", chain: "", hasMain: true, enforced: true},
		{name: "no composition root", hasMain: false, enforced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := projectWithChain(t, tc.chain, tc.hasMain)
			assertRowsFor(t, tc.enforced, false, generateNotes(t, dir))
		})
	}

	t.Run("the option spelling app.WithoutDefaults() counts", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeGoFile(t, dir, "main.go", "package main\n\nimport (\n\t\"github.com/jcsvwinston/nucleus/pkg/app\"\n)\n\n"+
			"func main() {\n\tcfg := app.DefaultConfig()\n\t_, _ = app.New(cfg, app.WithoutDefaults(), app.WithStorage())\n}\n")
		assertRowsFor(t, false, false, generateNotes(t, dir))
	})

	t.Run("WithAuthz() in a second file of package main counts", func(t *testing.T) {
		// The generator used to read one file — main.go — so an enforcer
		// added from a helper file was invisible: it commented the rows
		// out and said every route answers anyone on an application that
		// refuses anonymous callers on every route.
		dir := projectWithChain(t, ".WithoutDefaults()", true)
		writeGoFile(t, dir, "secure.go", secureHelper)
		assertRowsFor(t, true, false, generateNotes(t, dir))
	})

	t.Run("a build-tagged tools.go beside cmd/<app>/ does not hide the root", func(t *testing.T) {
		// `nucleus add` picks the first root .go file when none is package
		// main and never looks under cmd/; the generator's reader does,
		// so the layout with a tools.go at the root is read like the one
		// without it.
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeGoFile(t, dir, "tools.go", "//go:build tools\n\npackage tools\n\nimport _ \"golang.org/x/tools/cmd/stringer\"\n")
		writeGoFile(t, dir, filepath.Join("cmd", "svc", "main.go"), mainWithChain(".WithoutDefaults()"))
		assertRowsFor(t, false, false, generateNotes(t, dir))
	})

	t.Run("--with-policy on a root without an enforcer comments the open rows out", func(t *testing.T) {
		dir := projectWithChain(t, ".WithoutDefaults()", true)
		g := generateNotes(t, dir, "--with-policy")
		assertRowsFor(t, false, true, g)
		if !strings.Contains(g.module, "\t\t// \t{Subject: \"anonymous\", Object: \"/notes\", Action: \"*\"},") {
			t.Errorf("--with-policy must comment out the OPEN rows, so that what the file says is what the enforcer would load:\n%s", g.module)
		}
	})
}

// assertRowsFor checks what the generator says about the rows — in its
// output, module.go, module_test.go and the storage header — for a root
// that enforces them or not, with the read-only rows or the open ones.
func assertRowsFor(t *testing.T, enforced, open bool, g generated) {
	t.Helper()
	code := stripComments(g.module)
	testCode := stripComments(g.test)
	if enforced {
		if !strings.Contains(g.stdout, "  policy: anonymous read only") && !strings.Contains(g.stdout, "  policy: OPEN") {
			t.Errorf("on a root with an enforcer the generator reports the policy it wrote:\n%s", g.stdout)
		}
		for _, stale := range []string{"policy: NONE", "--with-policy: the rows commented out", "stay commented out"} {
			if strings.Contains(g.stdout, stale) {
				t.Errorf("on a root with an enforcer the generator must not say %q:\n%s", stale, g.stdout)
			}
		}
		if !strings.Contains(g.stdout, "Nothing else: the module carries its own policy rows, CSRF exemption and migrations") {
			t.Errorf("on a root with an enforcer the closing line says the module carries its rows:\n%s", g.stdout)
		}
		if !strings.Contains(code, "Policies: []nucleus.PolicyRule{") || strings.Contains(g.module, "\t\t// Policies:") {
			t.Errorf("on a root with an enforcer the rows are code:\n%s", g.module)
		}
		for name, src := range map[string]string{"module.go": g.module, "notes.go": g.storage, "module_test.go": g.test} {
			if strings.Contains(src, "commented out") {
				t.Errorf("on a root with an enforcer %s must not say the rows are commented out:\n%s", name, src)
			}
		}
		if strings.Contains(testCode, "WithoutDefaults()") {
			t.Errorf("the test of a module on a root with an enforcer boots the default stack:\n%s", g.test)
		}
		if !strings.Contains(g.test, "the module's own policy rows are in\n// force") {
			t.Errorf("the test's doc comment must say the rows are in force on its boot:\n%s", g.test)
		}
		want := "http.StatusForbidden"
		if open {
			want = "http.StatusCreated"
		}
		if !strings.Contains(testCode, "resp.Status != "+want) {
			t.Errorf("the test must expect %s for the anonymous POST:\n%s", want, g.test)
		}
		return
	}

	for _, want := range []string{"policy: NONE", "WithoutDefaults() without WithAuthz()", "commented out", "answers anyone", "DEP-2026-017",
		"Nothing else: the module carries its CSRF exemption and migrations (applied on start); its policy rows stay commented out until main.go carries WithAuthz()."} {
		if !strings.Contains(g.stdout, want) {
			t.Errorf("on a root without an enforcer the generator must say %q:\n%s", want, g.stdout)
		}
	}
	if strings.Contains(g.stdout, "carries its own policy rows") {
		t.Errorf("on a root without an enforcer the closing line must not say the module carries its rows:\n%s", g.stdout)
	}
	if !strings.Contains(g.module, "\t\t// Policies: []nucleus.PolicyRule{") || strings.Contains(code, "Policies:") {
		t.Errorf("on a root without an enforcer the rows are commented out, not code:\n%s", g.module)
	}
	for _, want := range []string{"WithAuthz()", "DEP-2026-017", "answers anyone", "policy rows (commented out: this application builds no enforcer)"} {
		if !strings.Contains(g.module, want) {
			t.Errorf("module.go must say %q:\n%s", want, g.module)
		}
	}
	// The storage header wraps "policy rows" across two lines in the Quark
	// variant, so the note alone is what the assertion looks for.
	if !strings.Contains(g.storage, "rows (commented out: this application builds no enforcer)") {
		t.Errorf("the storage header lists what module.go carries, so it says the rows are commented out:\n%s", g.storage)
	}
	// Uncommenting the rows — deleting the `// ` the generator put in front
	// of the literal — is the whole change, and leaves a gofmt-clean file
	// with the rows as code.
	uncommented := uncommentPolicyRows(g.module)
	if !strings.Contains(stripComments(uncommented), "Policies: []nucleus.PolicyRule{") {
		t.Errorf("uncommenting the rows did not produce a Policies field:\n%s", uncommented)
	}
	if formatted, err := format.Source([]byte(uncommented)); err != nil || string(formatted) != uncommented {
		t.Errorf("the module with its rows uncommented is not gofmt-clean (err=%v):\n%s", err, uncommented)
	}
	if !strings.Contains(testCode, "WithoutDefaults().") {
		t.Errorf("the test of a module on a root without an enforcer boots WithoutDefaults() as main.go does:\n%s", g.test)
	}
	if strings.Contains(testCode, "WithAuthz()") {
		t.Errorf("the test must not add the enforcer main.go lacks:\n%s", g.test)
	}
	if !strings.Contains(testCode, "resp.Status != http.StatusCreated") || !strings.Contains(testCode, "created.Data.ID == 0") {
		t.Errorf("the test must expect the anonymous POST to land and read the record back:\n%s", g.test)
	}
	for _, want := range []string{"WithoutDefaults() without", "WithAuthz()", "DEP-2026-017", "answers\n\t// anyone", "nothing is enforced on this boot (the policy rows are\n// commented out in module.go"} {
		if !strings.Contains(g.test, want) {
			t.Errorf("the test's comments must say %q, the same phrases as the generator:\n%s", want, g.test)
		}
	}
	if strings.Contains(g.test, "CSRF exemption is in") {
		t.Errorf("nucleus.New() without a configuration runs no CSRF middleware, so the test must not claim the exemption is in force:\n%s", g.test)
	}
	// The open rows keep their warning, in the file and in the output, and
	// the test's instructions for after WithAuthz() differ: uncommented, the
	// open rows still let the anonymous write land.
	openWarning := "--with-policy: the rows below open EVERY verb to anonymous callers"
	openStdout := "--with-policy: the rows commented out are the OPEN ones"
	openTest := "this write still lands"
	readOnlyTest := "change this expectation to http.StatusForbidden"
	if open {
		if !strings.Contains(g.module, openWarning) {
			t.Errorf("the open rows commented out must keep their scope-down warning:\n%s", g.module)
		}
		if !strings.Contains(g.stdout, openStdout) {
			t.Errorf("the output must say the rows commented out are the open ones:\n%s", g.stdout)
		}
		if !strings.Contains(g.test, openTest) || strings.Contains(g.test, readOnlyTest) {
			t.Errorf("the test of an open module must say the write still lands once the rows are uncommented:\n%s", g.test)
		}
		return
	}
	if strings.Contains(g.module, openWarning) || strings.Contains(g.stdout, openStdout) {
		t.Errorf("the read-only rows carry no open-rows warning:\n%s\n%s", g.module, g.stdout)
	}
	if !strings.Contains(g.test, readOnlyTest) || strings.Contains(g.test, openTest) {
		t.Errorf("the test of a read-only module must say to expect 403 once the rows are uncommented:\n%s", g.test)
	}
}

// uncommentPolicyRows is the edit the generated module's comment asks for:
// delete the `// ` the generator put in front of the Policies literal.
func uncommentPolicyRows(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		rest, ok := strings.CutPrefix(line, "\t\t// ")
		if !ok {
			continue
		}
		if strings.HasPrefix(rest, "Policies: []nucleus.PolicyRule{") || strings.HasPrefix(rest, "\t{Subject:") || rest == "}," {
			lines[i] = "\t\t" + rest
		}
	}
	return strings.Join(lines, "\n")
}

// TestCompositionRootEnforcesPolicies pins the reader the generator decides
// by: doctor's, so the two tools read one package main the same way — every
// file of it that the default build context compiles, at the root or under
// the first cmd/<name>/ that holds one.
func TestCompositionRootEnforcesPolicies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chain    string
		hasMain  bool
		files    map[string]string
		enforced bool
	}{
		{name: "WithoutDefaults", chain: ".WithoutDefaults()", hasMain: true},
		{name: "WithoutDefaults with WithAuthz", chain: ".WithoutDefaults().WithAuthz()", hasMain: true, enforced: true},
		{name: "WithAuthz alone", chain: ".WithAuthz()", hasMain: true, enforced: true},
		{name: "the default stack", chain: "", hasMain: true, enforced: true},
		{name: "no main.go", enforced: true},
		{name: "WithAuthz in a helper of main.go", chain: ".WithoutDefaults()", hasMain: true, enforced: true,
			files: map[string]string{"main.go": "package main\n\nimport \"github.com/jcsvwinston/nucleus/pkg/nucleus\"\n\n" +
				"func main() {\n\t_ = secure(nucleus.New().FromConfigFile(\"nucleus.yml\").WithoutDefaults()).Start()\n}\n\n" +
				"func secure(b *nucleus.AppBuilder) *nucleus.AppBuilder { return b.WithAuthz() }\n"}},
		{name: "WithAuthz in a second file of the package", chain: ".WithoutDefaults()", hasMain: true, enforced: true,
			files: map[string]string{"secure.go": secureHelper}},
		{name: "a second file that adds no enforcer", chain: ".WithoutDefaults()", hasMain: true,
			files: map[string]string{"routes.go": "package main\n\nfunc routes() {}\n"}},
		{name: "a file a build constraint excludes does not count", chain: ".WithoutDefaults()", hasMain: true,
			files: map[string]string{"secure.go": "//go:build ignore\n\n" + secureHelper}},
		{name: "a test file does not count", chain: ".WithoutDefaults()", hasMain: true,
			files: map[string]string{"secure_test.go": secureHelper}},
		{name: "cmd/<app>/ behind a build-tagged tools.go at the root", enforced: false,
			files: map[string]string{
				"tools.go":        "//go:build tools\n\npackage tools\n\nimport _ \"golang.org/x/tools/cmd/stringer\"\n",
				"cmd/svc/main.go": mainWithChain(".WithoutDefaults()"),
			}},
		{name: "cmd/<app>/ with the enforcer in a second file", enforced: true,
			files: map[string]string{
				"cmd/svc/main.go":   mainWithChain(".WithoutDefaults()"),
				"cmd/svc/secure.go": secureHelper,
			}},
		{name: "a library package at the root beside cmd/<app>/ on the default stack", enforced: true,
			files: map[string]string{
				"doc.go":          "// Package app is the library.\npackage app\n",
				"cmd/app/main.go": mainWithChain(""),
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := projectWithChain(t, tc.chain, tc.hasMain)
			for name, src := range tc.files {
				writeGoFile(t, dir, filepath.FromSlash(name), src)
			}
			if got := compositionRootEnforcesPolicies(dir); got != tc.enforced {
				t.Errorf("enforced = %v, want %v", got, tc.enforced)
			}
		})
	}
}

// TestGenerateResource_SentenceFollowsTheCompositionRoot: the sibling
// generator used to tell every project that, with the default-deny
// authorizer, its routes need rbac_policy.csv rows — on a WithoutDefaults()
// root without WithAuthz() there is no authorizer and nothing loads the
// file (DEP-2026-017), so the sentence follows the root as `generate
// module` does.
func TestGenerateResource_SentenceFollowsTheCompositionRoot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chain string
		want  string
		avoid string
	}{
		{name: "no enforcer", chain: ".WithoutDefaults()", want: "This application builds no RBAC enforcer (WithoutDefaults() without WithAuthz() in main.go): the routes above answer anyone", avoid: "With the default-deny authorizer"},
		{name: "default stack", chain: "", want: "With the default-deny authorizer, the routes above need rbac_policy.csv rows", avoid: "builds no RBAC enforcer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := projectWithChain(t, tc.chain, true)
			var out, errOut bytes.Buffer
			args := []string{"resource", "Widget", "--out", dir, "--dialect", "sqlite", "--offline"}
			if err := runGenerate(args, strings.NewReader(""), &out, &errOut); err != nil {
				t.Fatalf("runGenerate %v: %v\nstderr: %s", args, err, errOut.String())
			}
			if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), tc.avoid) {
				t.Errorf("generate resource must say %q and not %q on this root:\n%s", tc.want, tc.avoid, out.String())
			}
		})
	}
}
