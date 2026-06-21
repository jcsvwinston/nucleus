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
	repoRoot := filepath.Dir(baselinePath(t))

	// Public pkg/* surfaces guarded against third-party type leaks — the
	// `firewalled` subset of the shared registry in packages_test.go. This
	// set is intentionally NOT the same as the freeze baseline: freeze
	// covers `stable` packages, whereas the firewall guards every public
	// surface that imports (or could plausibly import) a forbidden library,
	// including the transitional pkg/outbox. pkg/circuit is
	// excluded on purpose (pure stdlib, nothing to leak). The registry
	// records each inclusion/exclusion reason next to the data and a guard
	// test fails if a new pkg/* directory is added without a posture
	// decision.
	packages := firewalledPackages()

	// Third-party packages that should NOT appear in exported signatures
	forbiddenThirdParty := map[string]string{
		"github.com/alexedwards/scs/v2":                           "scs session types should be wrapped",
		"github.com/casbin/casbin/v2":                             "casbin enforcer should not leak to public API",
		"github.com/go-playground/validator/v10":                  "validator types should be wrapped in ValidationError",
		"github.com/golang-jwt/jwt/v5":                            "jwt types should be internal only",
		"github.com/knadh/koanf/v2":                               "koanf types should be internal to config loading",
		"github.com/hibiken/asynq":                                "asynq types should be internal to tasks",
		"github.com/redis/go-redis/v9":                            "redis client should be wrapped",
		"github.com/jackc/pgx/v5":                                 "pgx types should not leak (use *sql.DB)",
		"github.com/go-sql-driver/mysql":                          "mysql types should not leak (use *sql.DB)",
		"github.com/minio/minio-go/v7":                            "minio types should be wrapped in storage.Store",
		"cloud.google.com/go/storage":                             "GCS types should be wrapped in storage.Store",
		"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob":    "Azure types should be wrapped in storage.Store",
		"go.opentelemetry.io/otel":                                "OTel types should be internal to observability",
		"go.opentelemetry.io/otel/exporters/prometheus":           "Prometheus exporter must stay behind the http.Handler returned by observe.SetupOpenTelemetry (ADR-012)",
		"github.com/prometheus/client_golang/prometheus":          "prometheus registry must stay internal to pkg/observe; expose only via http.Handler (ADR-012)",
		"github.com/prometheus/client_golang/prometheus/promhttp": "promhttp handler must stay behind the http.Handler returned by observe.SetupOpenTelemetry (ADR-012)",
		"github.com/aws/aws-sdk-go-v2/config":                     "AWS SDK config should be confined to NewAWSSecretsManagerResolver (ADR-005)",
		"github.com/aws/aws-sdk-go-v2/service/secretsmanager":     "AWS Secrets Manager types should stay behind the internal secretsManagerAPI interface (ADR-005)",
	}

	violations := []string{}

	for _, pkg := range packages {
		pkgDir := filepath.Join(repoRoot, pkg.relative)
		pkgViolations := checkPackageForThirdPartyLeaks(t, pkgDir, pkg.importPath(), forbiddenThirdParty)
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
			// An explicit alias (including `_` and `.`) wins. Otherwise the
			// identifier the import binds to in code is the package's own
			// name — which for a well-behaved module is the last path
			// segment, EXCEPT that Semantic Import Versioning appends a
			// `/vN` element that is NOT part of the identifier. Without
			// stripping it, a normal `github.com/casbin/casbin/v2` import
			// resolved to the segment `v2` (or to ""), so every forbidden
			// `/vN` package was invisible to the leak scan (audit F-4).
			name := ""
			if imp.Name != nil {
				name = imp.Name.Name
			} else {
				name = localPackageName(path)
			}
			imports[path] = name
		}
	}
	return imports
}

// localPackageName returns the identifier an import path binds to in code
// when no explicit alias is present: the last path segment that is not a
// Semantic Import Versioning major-version element (`v2`, `v5`, `v10`, …).
// A small override table covers the minority of modules whose package
// name diverges from that segment; keep it in sync with the divergent
// entries of forbiddenThirdParty.
func localPackageName(path string) string {
	if override, ok := pkgNameOverrides[path]; ok {
		return override
	}
	segs := strings.Split(path, "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if isMajorVersionSegment(segs[i]) {
			continue
		}
		return segs[i]
	}
	// Every segment was a version element (only reachable for a degenerate
	// path like "v2"). Return "" so the empty-name guard in the matcher
	// skips it rather than treating the raw path as an identifier.
	return ""
}

// isMajorVersionSegment reports whether a path segment is a Go Semantic
// Import Versioning major-version element: a `v` followed by one or more
// digits (`v2`, `v10`). `v1` never appears in import paths, but matching
// it here is harmless.
func isMajorVersionSegment(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// pkgNameOverrides maps an import path to the identifier it binds to in
// code, for modules whose package name differs from the last non-version
// path segment (so the localPackageName heuristic alone would miss them).
// Every forbidden package with a divergent name MUST appear here or the
// firewall goes blind to it again — the exact class of bug F-4 fixed.
var pkgNameOverrides = map[string]string{
	"github.com/redis/go-redis/v9": "redis",
	"github.com/minio/minio-go/v7": "minio",
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
			// Unexported, named fields are not part of the public API
			// surface, so a forbidden type held there cannot be reached by
			// an importer and is not a leak. Embedded fields (no names) DO
			// promote their type's exported methods to the struct's public
			// surface, so they are always checked.
			if len(field.Names) > 0 && !anyExported(field.Names) {
				continue
			}
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, typeName, "field", violations)
		}
	case *ast.InterfaceType:
		for _, field := range node.Methods.List {
			// Same rule as struct fields. Embedded interfaces (e.g.
			// `pkg.SomeInterface`) land here as len(Names)==0 fields and are
			// always checked; an unexported method name is package-private,
			// so its signature is not part of the importer-visible surface.
			if len(field.Names) > 0 && !anyExported(field.Names) {
				continue
			}
			checkFieldTypeForLeaks(field.Type, imports, forbidden, pkgPath, typeName, "method", violations)
		}
	}
}

func anyExported(names []*ast.Ident) bool {
	for _, n := range names {
		if n.IsExported() {
			return true
		}
	}
	return false
}

func checkFieldTypeForLeaks(expr ast.Expr, imports map[string]string, forbidden map[string]string, pkgPath, owner, kind string, violations *[]string) {
	// Check if the type references a forbidden third-party package. With
	// extractImports now resolving every import to the identifier it binds
	// to in code (alias > override > last non-`vN` segment), a single
	// name-match per import is exact — the old `HasSuffix(impPath, ident)`
	// fallback is gone (it both missed `/vN` paths and double-counted the
	// ones it did catch).
	if ident, ok := expr.(*ast.Ident); ok {
		// Bare identifier — only a dot-imported forbidden type lands here.
		for impPath, impName := range imports {
			if reason, isForbidden := forbidden[impPath]; isForbidden {
				if impName != "" && ident.Name == impName {
					addViolation(violations, pkgPath, owner, kind, ident.Name, impPath, reason)
				}
			}
		}
	}

	if sel, ok := expr.(*ast.SelectorExpr); ok {
		// SelectorExpr like "casbin.Enforcer" or "validator.ValidationError".
		if ident, ok := sel.X.(*ast.Ident); ok {
			for impPath, impName := range imports {
				if reason, isForbidden := forbidden[impPath]; isForbidden {
					if impName != "" && ident.Name == impName {
						addViolation(violations, pkgPath, owner, kind, sel.Sel.Name, impPath, reason)
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

// blessedLeaks records third-party type exposures on a stable public
// surface that are deliberately allowed, each justified by an ADR. They
// are NOT firewall failures. Keys are "<pkgImportPath> <ownerSymbol>
// <leakedType> <thirdPartyImportPath>"; the owner is the exported type or
// func that carries the exposure and leakedType is the specific
// third-party type blessed. Including the leaked type means a *different*
// type from the same third-party package surfacing on the same owner is
// NOT silently blessed — it surfaces as a fresh violation. Keep this list
// minimal and narrow — every entry widens the public dependency surface of
// a frozen package, so each must cite the ADR that blessed it and name the
// exact symbol, never a whole package.
//
// All entries below were surfaced by the F-4 resolver fix (the firewall
// was previously blind to these `/vN` imports) and adjudicated in
// ADR-015. Exactly one revealed leak — casbin embedded in authz.Enforcer
// — was NOT blessed; it was wrapped behind an unexported field instead.
var blessedLeaks = map[string]string{
	// auth.Claims must embed jwt.RegisteredClaims to satisfy the jwt.Claims
	// interface required by jwt.ParseWithClaims (pkg/auth/jwt.go). Wrapping
	// would only move the leak to the interface methods' return types
	// (*jwt.NumericDate, jwt.ClaimStrings). The embed is the structurally
	// minimal form of a mandatory dependency. (ADR-015 §3a)
	"github.com/jcsvwinston/nucleus/pkg/auth Claims RegisteredClaims github.com/golang-jwt/jwt/v5": "ADR-015: structural — jwt.RegisteredClaims embed required by jwt.Claims interface",

	// auth.SessionManager.SCS / SetStore are deliberate escape hatches for
	// advanced SCS configuration and pluggable session stores (Redis/SQL/
	// custom). Their whole purpose is to hand the caller the underlying scs
	// types; wrapping would defeat them. (ADR-015 §3b)
	"github.com/jcsvwinston/nucleus/pkg/auth SCS SessionManager github.com/alexedwards/scs/v2": "ADR-015: intentional escape hatch exposing *scs.SessionManager for advanced configuration",
	"github.com/jcsvwinston/nucleus/pkg/auth SetStore Store github.com/alexedwards/scs/v2":     "ADR-015: intentional extension point accepting a pluggable scs.Store",

	// auth.NewRedisSessionStore / NewRedisSessionStoreFromURL are integration
	// constructors: callers share a configured redis client (redis is itself
	// an optional, plugin-style backend). (ADR-015 §3c)
	"github.com/jcsvwinston/nucleus/pkg/auth NewRedisSessionStore UniversalClient github.com/redis/go-redis/v9": "ADR-015: integration constructor accepting a caller-owned redis.UniversalClient",
	"github.com/jcsvwinston/nucleus/pkg/auth NewRedisSessionStoreFromURL Client github.com/redis/go-redis/v9":   "ADR-015: integration constructor returning the created *redis.Client so the caller can Close it",

	// validate.RegisterRule is the documented custom-rule extension point.
	// validator.Func's parameter (validator.FieldLevel) is a fat interface;
	// re-exposing it under a Nucleus name would only relocate the leak.
	// (ADR-015 §3d)
	"github.com/jcsvwinston/nucleus/pkg/validate RegisterRule Func github.com/go-playground/validator/v10": "ADR-015: documented custom-rule extension point; validator.Func is the rule signature",
}

// addViolation records a leak unless it is explicitly blessed.
func addViolation(violations *[]string, pkgPath, owner, kind, typeName, thirdPartyPath, reason string) {
	key := pkgPath + " " + owner + " " + typeName + " " + thirdPartyPath
	if _, blessed := blessedLeaks[key]; blessed {
		return
	}
	*violations = append(*violations, formatViolation(pkgPath, owner, kind, typeName, thirdPartyPath, reason))
}

func formatViolation(pkgPath, owner, kind, typeName, thirdPartyPath, reason string) string {
	return fmt.Sprintf("  - %s: %s %s uses %s from %s (%s)", pkgPath, owner, kind, typeName, thirdPartyPath, reason)
}
