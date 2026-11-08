// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"

	// Blank-imported for its side effect, the way an application enables a
	// provider it wants.
	_ "github.com/jcsvwinston/nucleus/pkg/auth/federated/oidc"
)

// stubProvider is the smallest provider that satisfies the contract: it
// exists so FED-01 can exercise the seam end to end without an identity
// provider, the way a third party would.
type stubProvider struct{ name string }

func (p *stubProvider) Name() string { return p.name }
func (p *stubProvider) Begin(context.Context, federated.BeginRequest) (federated.Redirect, error) {
	return federated.Redirect{URL: "https://idp.example/authorize"}, nil
}
func (p *stubProvider) Complete(context.Context, federated.CompleteRequest) (*backend.User, error) {
	return &backend.User{ID: "1", Username: "ana"}, nil
}

// FED-01 — the browser-redirect contract is implementable by a third party,
// and the framework owns the anti-forgery state.
func probeFederatedContract(t *testing.T, _ *env) verdict {
	name := "authbench-stub"
	if err := federated.Register(name, func(backend.Config) (federated.Provider, error) {
		return &stubProvider{name: name}, nil
	}); err != nil {
		t.Logf("Register: %v", err)
		return absent
	}
	t.Cleanup(func() { federated.Unregister(name) })

	var found bool
	for _, n := range federated.Registered() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Log("a registered provider does not show up in the registry")
		return partial
	}
	return present
}

// FED-02 — an OIDC provider that ships with the framework.
func probeOIDCProvider(t *testing.T, _ *env) verdict {
	return shippedFederatedProvider(t, "oidc")
}

// FED-03 — a SAML provider that ships with the framework.
func probeSAMLProvider(t *testing.T, _ *env) verdict {
	if shippedFederatedProvider(t, "saml") == present {
		return present
	}
	return shippedFederatedProviderModule(t, "saml", filepath.Join("providers", "auth-saml"))
}

// shippedFederatedProvider asks the registry what the framework itself
// registered. The bench blank-imports the in-tree providers, which is
// exactly what an application does (`import _ ".../oidc"`, the same shape
// as a database driver); the registry is where the result lands.
func shippedFederatedProvider(t *testing.T, kind string) verdict {
	for _, n := range federated.Registered() {
		if strings.Contains(strings.ToLower(n), kind) {
			t.Logf("provider %q is registered", n)
			return present
		}
	}
	return absent
}

// shippedFederatedProviderModule measures a provider that ships as a module
// of its own under the repository (ADR-030/031: a provider that links a
// third-party library lives beside the framework, pins its last release and
// registers itself on import, like a database driver). The framework module
// cannot import it — the dependency points the other way — so the registry
// above never sees it; what ships is the module's source, and that is what
// the probe reads: the module's go.mod, and the `federated.Register(name,
// …)` call in its non-test files, with the registered name resolved through
// the file's constants. A module directory that is missing, does not
// compile as Go, or registers nothing under that kind measures absent. The
// comment in the module's README is not consulted: the call is.
func shippedFederatedProviderModule(t *testing.T, kind, moduleDir string) verdict {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the repository root")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", moduleDir)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Logf("no module at %s: %v", moduleDir, err)
		return absent
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Logf("%s does not parse as Go: %v", moduleDir, err)
		return absent
	}
	for _, pkg := range pkgs {
		consts := map[string]string{}
		var registered []string
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								if v, err := strconv.Unquote(lit.Value); err == nil {
									consts[name.Name] = v
								}
							}
						}
					}
				}
			}
		}
		for _, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Register" {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "federated" {
					return true
				}
				switch arg := call.Args[0].(type) {
				case *ast.BasicLit:
					if v, err := strconv.Unquote(arg.Value); err == nil {
						registered = append(registered, v)
					}
				case *ast.Ident:
					if v, ok := consts[arg.Name]; ok {
						registered = append(registered, v)
					}
				}
				return true
			})
		}
		for _, name := range registered {
			if strings.Contains(strings.ToLower(name), kind) {
				t.Logf("module %s registers federated provider %q", moduleDir, name)
				return present
			}
		}
	}
	t.Logf("module %s registers no %s provider", moduleDir, kind)
	return absent
}

// FED-04 — where the claims an identity provider returns land. The probe
// drives the seam with a provider that answers with THREE group
// memberships, the way a real one does, and checks they arrive.
func probeClaimMapping(t *testing.T, _ *env) verdict {
	user := (&multiRoleProvider{}).identity()
	roles := user.AllRoles()
	if len(roles) < 3 {
		t.Logf("the identity kept %v of three groups", roles)
		return absent
	}
	if !user.HasRole("billing") || !user.HasRole("SUPPORT") {
		t.Logf("the identity does not answer for its own roles: %v", roles)
		return partial
	}
	return present
}

// multiRoleProvider stands in for an identity provider that returns group
// memberships.
type multiRoleProvider struct{}

func (p *multiRoleProvider) identity() *backend.User {
	return &backend.User{
		ID: "1", Username: "ana",
		Role:  "editor",
		Roles: []string{"billing", "support"},
	}
}
