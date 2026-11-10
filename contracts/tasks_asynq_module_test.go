// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package contracts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// providers/tasks-asynq is the import path the asynq jobs provider has from
// v2.0.0 on (DEP-2026-020). Until the major the provider's code stays in
// pkg/tasks/providers/asynq, in the root module, and the sibling module
// FORWARDS: every exported constant, variable, type (as an alias) and
// function of the in-tree package has a namesake there. An application that
// writes the module's import today must find at 2.0 exactly what it finds
// now, so the two surfaces must not drift apart in silence: a symbol added
// in-tree and not forwarded, or forwarded and gone in-tree, fails here by
// name.
//
// The root module does not import the sibling (that is the point of the
// split), so both packages are parsed by path, not loaded.
func TestTasksAsynqModuleForwardsTheWholeInTreeSurface(t *testing.T) {
	root := repoRootFromContracts(t)
	inTree := exportedTopLevelIdents(t, filepath.Join(root, "pkg", "tasks", "providers", "asynq"))
	module := exportedTopLevelIdents(t, filepath.Join(root, "providers", "tasks-asynq"))

	var missing, extra []string
	for name := range inTree {
		if _, ok := module[name]; !ok {
			missing = append(missing, name+" ("+inTree[name]+")")
		}
	}
	for name := range module {
		if _, ok := inTree[name]; !ok {
			extra = append(extra, name+" ("+module[name]+")")
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("pkg/tasks/providers/asynq exports symbols that providers/tasks-asynq does not forward (DEP-2026-020: the module must offer at 2.0 what the in-tree package offers now):\n  %s",
			strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("providers/tasks-asynq exports symbols that pkg/tasks/providers/asynq does not have (the module forwards until v2.0.0; new API belongs in-tree first):\n  %s",
			strings.Join(extra, "\n  "))
	}
	if len(inTree) == 0 {
		t.Fatal("no exported identifiers found in pkg/tasks/providers/asynq; the parse found nothing")
	}
}

// exportedTopLevelIdents parses the non-test .go files of one directory and
// returns every exported package-level identifier with its kind (const, var,
// type, func). Methods are not listed: a type alias carries them.
func exportedTopLevelIdents(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Join(dir, name), err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					out[d.Name.Name] = "func"
				}
			case *ast.GenDecl:
				kind := d.Tok.String()
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							out[s.Name.Name] = kind
						}
					case *ast.ValueSpec:
						for _, id := range s.Names {
							if id.IsExported() {
								out[id.Name] = kind
							}
						}
					}
				}
			}
		}
	}
	return out
}

// repoRootFromContracts walks up from the test's directory to the go.mod
// that declares the framework module.
func repoRootFromContracts(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && strings.HasPrefix(string(b), "module "+modulePath+"\n") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}
