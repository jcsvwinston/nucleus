package contracts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFirewall_NoThirdPartyTypesInStableAPIs ensures that third-party concrete types
// do not leak into stable public API surfaces. This is a critical Track C deliverable.
func TestFirewall_NoThirdPartyTypesInStableAPIs(t *testing.T) {
	repoRoot := filepath.Join(filepath.Dir(baselinePath(t)), "..")

	packages := []struct {
		importPath string
		relative   string
	}{
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/app", relative: "pkg/app"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/auth", relative: "pkg/auth"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/authz", relative: "pkg/authz"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/db", relative: "pkg/db"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/errors", relative: "pkg/errors"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/mail", relative: "pkg/mail"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/model", relative: "pkg/model"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/observe", relative: "pkg/observe"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/plugins", relative: "pkg/plugins"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/router", relative: "pkg/router"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/signals", relative: "pkg/signals"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/tasks", relative: "pkg/tasks"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/validate", relative: "pkg/validate"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/storage", relative: "pkg/storage"},
		{importPath: "github.com/jcsvwinston/GoFrame/pkg/outbox", relative: "pkg/outbox"},
	}

	// Third-party packages that should NOT appear in exported signatures
	forbiddenThirdParty := map[string]string{
		"github.com/alexedwards/scs/v2":                        "scs session types should be wrapped",
		"github.com/casbin/casbin/v2":                          "casbin enforcer should not leak to public API",
		"github.com/go-playground/validator/v10":               "validator types should be wrapped in ValidationError",
		"github.com/golang-jwt/jwt/v5":                         "jwt types should be internal only",
		"github.com/knadh/koanf/v2":                            "koanf types should be internal to config loading",
		"github.com/hibiken/asynq":                             "asynq types should be internal to tasks",
		"github.com/redis/go-redis/v9":                         "redis client should be wrapped",
		"github.com/jackc/pgx/v5":                              "pgx types should not leak (use *sql.DB)",
		"github.com/go-sql-driver/mysql":                       "mysql types should not leak (use *sql.DB)",
		"github.com/minio/minio-go/v7":                         "minio types should be wrapped in storage.Store",
		"cloud.google.com/go/storage":                          "GCS types should be wrapped in storage.Store",
		"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob": "Azure types should be wrapped in storage.Store",
		"go.opentelemetry.io/otel":                             "OTel types should be internal to observability",
	}

	violations := []string{}

	for _, pkg := range packages {
		pkgDir := filepath.Join(repoRoot, pkg.relative)
		pkgViolations := checkPackageForThirdPartyLeaks(t, pkgDir, pkg.importPath, forbiddenThirdParty)
		violations = append(violations, pkgViolations...)
	}

	if len(violations) > 0 {
		t.Fatalf("Third-party type leaks detected in stable APIs:\n%s", strings.Join(violations, "\n"))
	}
}

func checkPackageForThirdPartyLeaks(t *testing.T, dir, importPath string, forbidden map[string]string) []string {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		name := fi.Name()
		return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse package dir %s: %v", dir, err)
	}
	if len(pkgs) == 0 {
		return nil
	}

	var target *ast.Package
	for name, pkg := range pkgs {
		if name == "main" {
			continue
		}
		target = pkg
		break
	}
	if target == nil {
		for _, pkg := range pkgs {
			target = pkg
			break
		}
	}
	if target == nil {
		return nil
	}

	violations := []string{}
	imports := extractImports(target)

	for _, file := range target.Files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Type != nil {
				if fn.Name.IsExported() {
					checkFuncSignatureForLeaks(fn.Type, imports, forbidden, importPath, fn.Name.Name, &violations)
				}
			}
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
						checkTypeSpecForLeaks(ts, imports, forbidden, importPath, ts.Name.Name, &violations)
					}
				}
			}
		}
	}

	return violations
}

func extractImports(pkg *ast.Package) map[string]string {
	imports := make(map[string]string)
	for _, file := range pkg.Files {
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			name := ""
			if imp.Name != nil {
				name = imp.Name.Name
			}
			imports[path] = name
		}
	}
	return imports
}

func checkFuncSignatureForLeaks(ft *ast.FuncType, imports map[string]string, forbidden map[string]string, pkgPath, funcName string, violations *[]string) {
	if ft.Params != nil {
		for _, field := range ft.Params.List {
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, funcName, "parameter", violations)
		}
	}
	if ft.Results != nil {
		for _, field := range ft.Results.List {
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, funcName, "return", violations)
		}
	}
}

func checkTypeSpecForLeaks(ts *ast.TypeSpec, imports map[string]string, forbidden map[string]string, pkgPath, typeName string, violations *[]string) {
	switch node := ts.Type.(type) {
	case *ast.StructType:
		for _, field := range node.Fields.List {
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, typeName, "field", violations)
		}
	case *ast.InterfaceType:
		for _, field := range node.Methods.List {
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, typeName, "method", violations)
		}
	}
}

func checkFieldTypeForLeaks(expr ast.Expr, imports map[string]string, forbidden map[string]string, pkgPath, owner, kind string, violations *[]string) {
	// Check if the type references a forbidden third-party package
	if ident, ok := expr.(*ast.Ident); ok {
		// Simple identifier - check if it's an imported third-party type
		for impPath, impName := range imports {
			if reason, forbidden := forbidden[impPath]; forbidden {
				if impName != "" && ident.Name == impName {
					*violations = append(*violations, formatViolation(pkgPath, owner, kind, ident.Name, impPath, reason))
				}
			}
		}
	}

	if sel, ok := expr.(*ast.SelectorExpr); ok {
		// SelectorExpr like "casbin.Enforcer" or "validator.ValidationError"
		if ident, ok := sel.X.(*ast.Ident); ok {
			for impPath, impName := range imports {
				if reason, forbidden := forbidden[impPath]; forbidden {
					// Check if the package name matches
					if impName != "" && ident.Name == impName {
						*violations = append(*violations, formatViolation(pkgPath, owner, kind, sel.Sel.Name, impPath, reason))
					}
					// Also check if the import path is directly used
					if strings.HasSuffix(impPath, "/"+ident.Name) {
						*violations = append(*violations, formatViolation(pkgPath, owner, kind, sel.Sel.Name, impPath, reason))
					}
				}
			}
		}
	}

	// Recursively check composite types
	switch node := expr.(type) {
	case *ast.ArrayType:
		checkFieldTypeForLeaks(node.Elt, imports, forbidden, pkgPath, owner, kind, violations)
	case *ast.MapType:
		checkFieldTypeForLeaks(node.Key, imports, forbidden, pkgPath, owner, kind, violations)
		checkFieldTypeForLeaks(node.Value, imports, forbidden, pkgPath, owner, kind, violations)
	case *ast.StarExpr:
		checkFieldTypeForLeaks(node.X, imports, forbidden, pkgPath, owner, kind, violations)
	case *ast.ChanType:
		checkFieldTypeForLeaks(node.Value, imports, forbidden, pkgPath, owner, kind, violations)
	case *ast.FuncType:
		checkFuncSignatureForLeaks(node, imports, forbidden, pkgPath, owner, violations)
	case *ast.StructType:
		for _, field := range node.Fields.List {
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, owner, kind, violations)
		}
	case *ast.InterfaceType:
		for _, field := range node.Methods.List {
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, owner, kind, violations)
		}
	}
}

func formatViolation(pkgPath, owner, kind, typeName, thirdPartyPath, reason string) string {
	return fmt.Sprintf("  - %s: %s %s uses %s from %s (%s)", pkgPath, owner, kind, typeName, thirdPartyPath, reason)
}
