// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-11 (external coverage demo, 2026-08-18): v1.9.0 shipped
// app.WithTemplateFuncs / app.WithTemplates — and the documented builder
// (nucleus.New().FromConfigFile(...).Mount(...).Start()) exposes NEITHER, so
// an application built the documented way still cannot register template
// functions: the QCD-FW-9 fix is only reachable by dropping down to
// app.New(cfg, opts...), which is exactly what the builder wraps.
//
// THIRD occurrence of the mirror-without-parity pattern (QCD-FW-4: a
// pkg/storage key the app config mirror did not expose; QCD-FW-11: app
// options the builder does not expose). Like TestStorageConfigMirrorParity
// before it, the parity guard below matters more than the point fix: every
// exported app.Option constructor must have an AppBuilder counterpart, or
// carry an explicit reasoned exclusion.
package nucleus

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"strings"
	"testing"
)

// builderParityExclusions lists exported app.Option constructors that
// deliberately have NO AppBuilder counterpart. Every entry needs a reason —
// an entry without one is QCD-FW-11 with permission.
var builderParityExclusions = map[string]string{}

// appOptionConstructors parses pkg/app and returns the names of exported
// functions whose result type is (or includes) app.Option.
func appOptionConstructors(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	appDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "app"))

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, appDir, nil, 0)
	if err != nil {
		t.Fatalf("parse pkg/app: %v", err)
	}

	var out []string
	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || !fn.Name.IsExported() {
					continue
				}
				if fn.Type.Results == nil {
					continue
				}
				for _, res := range fn.Type.Results.List {
					if ident, ok := res.Type.(*ast.Ident); ok && ident.Name == "Option" {
						out = append(out, fn.Name.Name)
					}
				}
			}
		}
	}
	return out
}

// Every exported app.Option constructor must be reachable from the
// documented builder: an AppBuilder method with the same name.
func TestAppBuilderMirrorsEveryAppOption(t *testing.T) {
	builderType := reflect.TypeOf(&AppBuilder{})
	options := appOptionConstructors(t)
	if len(options) == 0 {
		t.Fatal("found no exported app.Option constructors — the parser broke")
	}

	for _, name := range options {
		if _, ok := builderType.MethodByName(name); ok {
			continue
		}
		reason, excluded := builderParityExclusions[name]
		if !excluded || strings.TrimSpace(reason) == "" {
			t.Errorf("app.%s has no AppBuilder counterpart — an application built with nucleus.New() cannot reach it (QCD-FW-11). Add AppBuilder.%s, or an explicit exclusion WITH a reason.", name, name)
		}
	}

	// Exclusions must stay honest.
	for name, reason := range builderParityExclusions {
		if _, ok := builderType.MethodByName(name); ok {
			t.Errorf("exclusion for %q (%s) is stale: the builder method exists now — delete the entry", name, reason)
		}
	}
}

// The point symptoms, pinned via go doc so this compiles on v1.9.0.
func TestBuilderExposesTemplateOptions(t *testing.T) {
	for _, symbol := range []string{
		"github.com/jcsvwinston/nucleus/pkg/nucleus.AppBuilder.WithTemplateFuncs",
		"github.com/jcsvwinston/nucleus/pkg/nucleus.AppBuilder.WithTemplates",
		"github.com/jcsvwinston/nucleus/pkg/nucleus.AppBuilder.WithOpenAuthz",
	} {
		out, err := exec.Command("go", "doc", symbol).CombinedOutput()
		if err != nil {
			t.Errorf("builder method missing: %s (QCD-FW-11): %v\n%s", symbol, err, out)
		}
	}
}
