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
	if calls != 1 || gotModule != pg.Module {
		t.Fatalf("go get called %d times with %q, want once with %q", calls, gotModule, pg.Module)
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

	if err := runAdd([]string{"no-such-module", "--dir", dir}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "unknown module") {
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
