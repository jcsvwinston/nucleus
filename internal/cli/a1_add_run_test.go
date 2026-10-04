// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// NU-23: runAdd end to end — flag parsing, module lookup, the go get call
// and the import edit — with `go get` stubbed, so the test needs no proxy.
func TestRunAdd_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainSrc := "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotRoot, gotModule string
	calls := 0
	prev := goGet
	goGet = func(root, module string, _, _ io.Writer) error {
		calls++
		gotRoot, gotModule = root, module
		return nil
	}
	t.Cleanup(func() { goGet = prev })

	pg, ok := lookupAddable("postgres")
	if !ok {
		t.Fatal("postgres is not addable")
	}

	var out, errOut bytes.Buffer
	if err := runAdd([]string{"postgres", "--dir", dir}, nil, &out, &errOut); err != nil {
		t.Fatalf("runAdd: %v\n%s", err, errOut.String())
	}
	// The target is the module at the version released with this CLI, not
	// the bare module, which would resolve whatever the proxy calls latest.
	if calls != 1 || gotModule != pg.Target() || !strings.HasPrefix(gotModule, pg.Module+"@v") {
		t.Fatalf("go get called %d times with %q, want once with %q", calls, gotModule, pg.Target())
	}
	if abs, _ := filepath.Abs(dir); gotRoot != abs {
		t.Fatalf("go get ran in %q, want the module root %q", gotRoot, abs)
	}
	src, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(src), `_ "`+pg.Module+`"`) {
		t.Fatalf("blank import not written:\n%s", src)
	}
	if !strings.Contains(out.String(), "added  import _") {
		t.Fatalf("output does not report the edit:\n%s", out.String())
	}

	// Second run: idempotent, still one go get, no second import.
	out.Reset()
	if err := runAdd([]string{"postgres", "--dir", dir}, nil, &out, &errOut); err != nil {
		t.Fatalf("second runAdd: %v", err)
	}
	if !strings.Contains(out.String(), "already imported") {
		t.Fatalf("second run must report the import as present:\n%s", out.String())
	}
	src, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Count(string(src), pg.Module) != 1 {
		t.Fatalf("import duplicated:\n%s", src)
	}
}

func TestRunAdd_DryRunAndErrors(t *testing.T) {
	prev := goGet
	goGet = func(string, string, io.Writer, io.Writer) error {
		t.Fatal("go get must not run in --dry-run")
		return nil
	}
	t.Cleanup(func() { goGet = prev })

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	var out bytes.Buffer
	if err := runAdd([]string{"sqlite", "--dir", dir, "--dry-run"}, nil, &out, io.Discard); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out.String(), "would run: go get") || !strings.Contains(out.String(), "would add: import _") {
		t.Fatalf("dry-run output:\n%s", out.String())
	}
	if src, _ := os.ReadFile(filepath.Join(dir, "main.go")); strings.Contains(string(src), "import") {
		t.Fatalf("dry-run modified main.go")
	}

	if err := runAdd([]string{"no-such-module", "--dir", dir}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "is not in the catalog") {
		t.Fatalf("unknown module: err=%v", err)
	}
	empty := t.TempDir()
	if err := runAdd([]string{"postgres", "--dir", empty}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "no go.mod") {
		t.Fatalf("missing go.mod: err=%v", err)
	}

	// A failing go get is reported, not swallowed.
	goGet = func(string, string, io.Writer, io.Writer) error { return errors.New("proxy down") }
	if err := runAdd([]string{"postgres", "--dir", dir}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "proxy down") {
		t.Fatalf("go get failure: err=%v", err)
	}
}

// NU-14: the generated module opens reads to anonymous callers and nothing
// else; --with-policy is the explicit way to get the open rows.
func TestGenerateModule_PolicyDefaultsToReadOnly(t *testing.T) {
	dir := t.TempDir()
	res, err := generateModuleScaffold(dir, "note", "Note", "sqlite", false, false, moduleDataSQL)
	if err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(res.ModulePath)
	anonymousStar := regexp.MustCompile(`Subject: "anonymous",[^}]*Action: "\*"`)
	anonymousRead := regexp.MustCompile(`Subject: "anonymous",[^}]*Action: "read"`)
	if n := len(anonymousStar.FindAllString(string(src), -1)); n != 0 {
		t.Fatalf("default module opens %d verbs-wide rows to anonymous callers:\n%s", n, src)
	}
	if n := len(anonymousRead.FindAllString(string(src), -1)); n != 3 {
		t.Fatalf("default module must open exactly the three read rows, has %d:\n%s", n, src)
	}
	open := t.TempDir()
	res, err = generateModuleScaffold(open, "note", "Note", "sqlite", false, true, moduleDataSQL)
	if err != nil {
		t.Fatal(err)
	}
	src, _ = os.ReadFile(res.ModulePath)
	if n := len(anonymousStar.FindAllString(string(src), -1)); n != 2 {
		t.Fatalf("--with-policy must emit the two open rows, has %d:\n%s", n, src)
	}
}

// One catalog, three ways of shipping (ADR-034): a module is fetched pinned
// and imported, a core entry is imported and fetches nothing, a suite
// product is fetched — with its driver module for the project's engine —
// and not imported. Each ends by naming what selects or wires it.
func TestRunAdd_EveryWayOfShipping(t *testing.T) {
	var calls []string
	prev := goGet
	goGet = func(_, target string, _, _ io.Writer) error {
		calls = append(calls, target)
		return nil
	}
	t.Cleanup(func() { goGet = prev })

	project := func(t *testing.T) string {
		dir := t.TempDir()
		goMod := "module example.com/app\n\ngo 1.26\n\nrequire github.com/jcsvwinston/nucleus/drivers/postgres v0.1.7\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	for _, c := range []struct {
		name      string
		wantGets  []string
		wantMain  string // an import main.go must carry afterwards ("" = none written)
		wantLines []string
	}{
		{
			name:      "s3",
			wantGets:  []string{"github.com/jcsvwinston/nucleus/providers/storage-s3@"},
			wantMain:  "github.com/jcsvwinston/nucleus/providers/storage-s3",
			wantLines: []string{"select it in nucleus.yml: storage.provider: s3"},
		},
		{
			name:      "oidc",
			wantMain:  "github.com/jcsvwinston/nucleus/pkg/auth/federated/oidc",
			wantLines: []string{"part of the framework: nothing to fetch", "auth_federated"},
		},
		{
			name:      "quark",
			wantGets:  []string{"github.com/jcsvwinston/quark", "github.com/jcsvwinston/quark/drivers/postgres"},
			wantLines: []string{"not pinned", "wire it:", "--data quark"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls = nil
			dir := project(t)
			var out bytes.Buffer
			if err := runAdd([]string{c.name, "--dir", dir}, nil, &out, io.Discard); err != nil {
				t.Fatalf("nucleus add %s: %v", c.name, err)
			}
			if len(calls) != len(c.wantGets) {
				t.Fatalf("go get ran %q, want %d calls like %q", calls, len(c.wantGets), c.wantGets)
			}
			for i, want := range c.wantGets {
				if strings.HasSuffix(want, "@") {
					if !strings.HasPrefix(calls[i], want+"v") {
						t.Errorf("go get %q, want %s<released version>", calls[i], want)
					}
				} else if calls[i] != want {
					t.Errorf("go get %q, want %q", calls[i], want)
				}
			}
			src, _ := os.ReadFile(filepath.Join(dir, "main.go"))
			if c.wantMain != "" && !strings.Contains(string(src), `_ "`+c.wantMain+`"`) {
				t.Errorf("main.go must import %s:\n%s", c.wantMain, src)
			}
			if c.wantMain == "" && strings.Contains(string(src), "import") {
				t.Errorf("a suite product is not imported by nucleus add:\n%s", src)
			}
			for _, line := range c.wantLines {
				if !strings.Contains(out.String(), line) {
					t.Errorf("output must say %q:\n%s", line, out.String())
				}
			}
		})
	}
}
