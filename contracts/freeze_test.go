package contracts

import (
	"bufio"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/cli"
	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestContractFreeze_CLIPrimaryCommands_NoRemovals(t *testing.T) {
	baseline := readBaselineLines(t, "baseline", "cli_primary_commands.txt")
	current := toSet(cli.ContractPrimaryCommandNames())

	missing := make([]string, 0)
	for _, command := range baseline {
		if _, ok := current[command]; !ok {
			missing = append(missing, command)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("stable CLI contract regression: missing primary command(s): %s", strings.Join(missing, ", "))
	}
}

func TestContractFreeze_ConfigKeyPatterns_NoRemovals(t *testing.T) {
	baseline := readBaselineLines(t, "baseline", "config_key_patterns.txt")
	current := toSet(app.ContractConfigKeyPatterns())

	missing := make([]string, 0)
	for _, key := range baseline {
		if _, ok := current[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("stable config contract regression: missing key pattern(s): %s", strings.Join(missing, ", "))
	}
}

func TestContractFreeze_APIExportedSymbols_NoRemovals(t *testing.T) {
	currentLines := stableAPISymbolBaselineLines(t)
	if os.Getenv("NUCLEUS_UPDATE_CONTRACT_BASELINE") == "1" {
		writeBaselineLines(t, currentLines, "baseline", "api_exported_symbols.txt")
	}

	baseline := readBaselineLines(t, "baseline", "api_exported_symbols.txt")
	current := toSet(currentLines)

	missing := make([]string, 0)
	for _, symbol := range baseline {
		if _, ok := current[symbol]; !ok {
			missing = append(missing, symbol)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("stable API contract regression: missing exported symbol(s): %s", strings.Join(missing, ", "))
	}
}

func TestContractFreeze_BaselinesAreSortedUnique(t *testing.T) {
	checkSortedUnique(t, readBaselineLines(t, "baseline", "api_exported_symbols.txt"), "api_exported_symbols.txt")
	checkSortedUnique(t, readBaselineLines(t, "baseline", "cli_primary_commands.txt"), "cli_primary_commands.txt")
	checkSortedUnique(t, readBaselineLines(t, "baseline", "cli_json_status_keys.txt"), "cli_json_status_keys.txt")
	checkSortedUnique(t, readBaselineLines(t, "baseline", "config_key_patterns.txt"), "config_key_patterns.txt")
}

func checkSortedUnique(t *testing.T, lines []string, name string) {
	t.Helper()
	seen := map[string]struct{}{}
	for i, line := range lines {
		if i > 0 && lines[i-1] > line {
			t.Fatalf("%s must be sorted ascending; %q appears before %q", name, lines[i-1], line)
		}
		if _, exists := seen[line]; exists {
			t.Fatalf("%s contains duplicate entry %q", name, line)
		}
		seen[line] = struct{}{}
	}
}

func readBaselineLines(t *testing.T, rel ...string) []string {
	t.Helper()
	base := baselinePath(t, rel...)
	f, err := os.Open(base)
	if err != nil {
		t.Fatalf("open baseline %s: %v", base, err)
	}
	defer f.Close()

	out := make([]string, 0, 64)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan baseline %s: %v", base, err)
	}
	return out
}

func writeBaselineLines(t *testing.T, lines []string, rel ...string) {
	t.Helper()
	base := baselinePath(t, rel...)
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		t.Fatalf("create baseline dir for %s: %v", base, err)
	}
	data := strings.Join(lines, "\n")
	if data != "" {
		data += "\n"
	}
	if err := os.WriteFile(base, []byte(data), 0o644); err != nil {
		t.Fatalf("write baseline %s: %v", base, err)
	}
}

func baselinePath(t *testing.T, rel ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve current file path")
	}
	return filepath.Join(filepath.Dir(file), filepath.Join(rel...))
}

func stableAPISymbolBaselineLines(t *testing.T) []string {
	t.Helper()
	repoRoot := filepath.Dir(baselinePath(t))

	// The freeze baseline covers exactly the pkg/* packages that
	// docs/reference/API_CONTRACT_INVENTORY.md marks `stable` — the
	// `frozen` subset of the shared registry in packages_test.go. That
	// registry is the single source of truth for which scanners apply to
	// which package and documents every deliberate omission next to its
	// reasoning, so a `stable` promotion (flip `frozen` to true there) or a
	// brand-new pkg/* directory surfaces as a registry change rather than a
	// silent gap. Rebaseline with NUCLEUS_UPDATE_CONTRACT_BASELINE=1 after
	// any such change.
	lines := make([]string, 0, 512)
	for _, pkg := range frozenPackages() {
		pkgSymbols := exportedSymbolsForPackage(t, filepath.Join(repoRoot, pkg.relative))
		for _, symbol := range pkgSymbols {
			lines = append(lines, pkg.importPath()+" "+symbol)
		}
	}
	sort.Strings(lines)
	return dedupeSorted(lines)
}

func exportedSymbolsForPackage(t *testing.T, dir string) []string {
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
		t.Fatalf("no package files found in %s", dir)
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
		t.Fatalf("unable to resolve package in %s", dir)
	}

	docPkg := doc.New(target, "", doc.AllDecls)
	symbols := make([]string, 0, 128)
	for _, v := range docPkg.Vars {
		for _, name := range v.Names {
			if ast.IsExported(name) {
				symbols = append(symbols, "var:"+name)
			}
		}
	}
	for _, c := range docPkg.Consts {
		for _, name := range c.Names {
			if ast.IsExported(name) {
				symbols = append(symbols, "const:"+name)
			}
		}
	}
	for _, fn := range docPkg.Funcs {
		if ast.IsExported(fn.Name) {
			symbols = append(symbols, "func:"+fn.Name)
		}
	}
	for _, typ := range docPkg.Types {
		if !ast.IsExported(typ.Name) {
			continue
		}
		symbols = append(symbols, "type:"+typ.Name)
		symbols = append(symbols, exportedMembersFromTypeDecl(typ.Decl, typ.Name)...)
		// Constructor functions (those whose result is the type, e.g.
		// `NewMigrator() *Migrator`) are filed by go/doc under the
		// type's Funcs, not under the package-level docPkg.Funcs —
		// which holds ONLY functions go/doc could not associate with
		// any type. They are exported package symbols and part of the
		// stable contract, so emit them as `func:Name` (the same shape
		// the top-level loop above uses). Without this loop every
		// `NewXxx` constructor across pkg/* was invisible to the
		// freeze baseline — this was the source of the constructor-gap
		// bug closed on 2026-05-20.
		for _, fn := range typ.Funcs {
			if ast.IsExported(fn.Name) {
				symbols = append(symbols, "func:"+fn.Name)
			}
		}
		for _, method := range typ.Methods {
			if ast.IsExported(method.Name) {
				symbols = append(symbols, "method:"+typ.Name+"."+method.Name)
			}
		}
	}
	sort.Strings(symbols)
	return dedupeSorted(symbols)
}

func exportedMembersFromTypeDecl(decl *ast.GenDecl, owner string) []string {
	if decl == nil {
		return nil
	}
	out := make([]string, 0, 16)
	for _, spec := range decl.Specs {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != owner {
			continue
		}
		switch node := typeSpec.Type.(type) {
		case *ast.StructType:
			for _, field := range node.Fields.List {
				for _, name := range field.Names {
					if ast.IsExported(name.Name) {
						out = append(out, "field:"+owner+"."+name.Name)
					}
				}
			}
		case *ast.InterfaceType:
			for _, field := range node.Methods.List {
				for _, name := range field.Names {
					if ast.IsExported(name.Name) {
						out = append(out, "iface-method:"+owner+"."+name.Name)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return dedupeSorted(out)
}

func dedupeSorted(items []string) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items))
	prev := ""
	for i, item := range items {
		if i == 0 || item != prev {
			out = append(out, item)
		}
		prev = item
	}
	return out
}

func toSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}
