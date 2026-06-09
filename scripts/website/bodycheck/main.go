// Command bodycheck is the automated CI guard for the §9 anti-falsehood
// discipline (see CLAUDE.md §9). The drift guard in check-coverage.sh only
// inspects frontmatter; this tool inspects the BODY of every public docs page
// (website/docs/**) — the place where the three P0 falsehoods of 2026-05-24
// hid (a wrong Go version, a non-existent function name, an inexistent YAML
// key). It complements (does not replace) the docs-content-verifier subagent.
//
// Checks:
//  1. [hard] Go-version claims (`go 1.XX` / `Go 1.XX` / `1.XX+`) must match
//     the `go` directive in go.mod.
//  2. [hard] Go symbols of the form `pkg.Symbol` in fenced ```go blocks that
//     reference a Nucleus package must exist in the freeze baseline. Symbols
//     qualified by a block-local identifier (an alias or var defined in the
//     same example) are skipped, as are non-Nucleus packages (stdlib, etc.).
//  3. [advisory] YAML keys in nucleus-config blocks should exist in the
//     config-key registry. Reported but not failing, because distinguishing
//     a nucleus.yml block from an arbitrary YAML example is heuristic.
//
// Usage: bodycheck [-root .] [-docs website/docs] [-strict]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	root := flag.String("root", ".", "repo root")
	docs := flag.String("docs", "website/docs", "docs directory (relative to root)")
	strict := flag.Bool("strict", false, "exit 1 when a hard violation is found")
	flag.Parse()

	v := &verifier{root: *root}
	if err := v.load(); err != nil {
		fmt.Fprintln(os.Stderr, "bodycheck: load:", err)
		os.Exit(2)
	}

	pages, err := markdownPages(filepath.Join(*root, *docs))
	if err != nil {
		fmt.Fprintln(os.Stderr, "bodycheck:", err)
		os.Exit(2)
	}

	var hard, soft []string
	for _, p := range pages {
		h, s := v.checkFile(p)
		hard = append(hard, h...)
		soft = append(soft, s...)
	}

	fmt.Println("== body-content fact-check (§9) ==")
	fmt.Printf("  go.mod go directive: %s (minor %s)\n", v.goVersion, v.goMinor)
	report("hard violations (Go version / Go symbols)", hard)
	report("advisory (YAML keys vs config registry)", soft)

	if *strict && len(hard) > 0 {
		fmt.Printf("\nFAIL (--strict): %d body-content falsehood(s). Fix the doc, or route a rename via contract-guardian.\n", len(hard))
		os.Exit(1)
	}
	fmt.Println("\nOK (body-content).")
}

func report(title string, items []string) {
	fmt.Printf("  -- %s: %d\n", title, len(items))
	for _, it := range items {
		fmt.Printf("     %s\n", it)
	}
}

type verifier struct {
	root      string
	goVersion string                     // e.g. "1.26.4"
	goMinor   string                     // e.g. "1.26"
	pkgSyms   map[string]map[string]bool // pkg short name -> set of exported symbol names
	cfgKeys   map[string]bool            // config-key segments from the registry
	cfgTop    map[string]bool            // top-level config sections (anchor for yaml blocks)
}

var (
	reGoMod     = regexp.MustCompile(`^go\s+(\d+\.\d+)(?:\.\d+)?`)
	reBaseline  = regexp.MustCompile(`^github\.com/jcsvwinston/nucleus/pkg/([a-z0-9_]+)\b.*?[:](\w+)`)
	reGoVer     = regexp.MustCompile(`(?i)\bgo\s*(1\.\d+)`)
	reGoVerPlus = regexp.MustCompile(`\b(1\.\d+)\+`)
	rePkgSymbol = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.([A-Z][A-Za-z0-9]*)`)
	reLocalDef  = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\s*:=`)
	// A Nucleus import line — with or without the `import` keyword (single or
	// grouped form) and an optional alias — capturing the alias and pkg path.
	reNucleusImport = regexp.MustCompile(`^\s*(?:import\s+)?(?:([a-z][a-z0-9_]*)\s+)?"github\.com/jcsvwinston/nucleus/pkg/([a-z0-9_/]+)"`)
	reYamlKey       = regexp.MustCompile(`^(\s*)([a-z][a-z0-9_]*)\s*:`)
	reRegistryKey   = regexp.MustCompile("`([a-z][a-z0-9_]*(?:\\.[a-z0-9_<>]+)*)`")
)

func (v *verifier) load() error {
	// go.mod go directive
	gomod, err := os.ReadFile(filepath.Join(v.root, "go.mod"))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(gomod), "\n") {
		if m := reGoMod.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			v.goMinor = m[1]
			full := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "go"))
			v.goVersion = full
			break
		}
	}
	if v.goMinor == "" {
		return fmt.Errorf("no `go` directive in go.mod")
	}

	// freeze baseline -> pkg short name -> symbol set
	v.pkgSyms = map[string]map[string]bool{}
	base, err := os.ReadFile(filepath.Join(v.root, "contracts/baseline/api_exported_symbols.txt"))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(base), "\n") {
		m := reBaseline.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		pkg, sym := m[1], m[2]
		if v.pkgSyms[pkg] == nil {
			v.pkgSyms[pkg] = map[string]bool{}
		}
		v.pkgSyms[pkg][sym] = true
	}

	// config-key registry -> key segments + top-level sections
	v.cfgKeys = map[string]bool{}
	v.cfgTop = map[string]bool{}
	reg, err := os.ReadFile(filepath.Join(v.root, "docs/reference/CONFIG_KEY_REGISTRY.md"))
	if err != nil {
		return err
	}
	for _, m := range reRegistryKey.FindAllStringSubmatch(string(reg), -1) {
		key := m[1]
		segs := strings.Split(key, ".")
		for _, s := range segs {
			v.cfgKeys[s] = true
		}
		v.cfgTop[segs[0]] = true
	}
	return nil
}

func markdownPages(dir string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".mdx") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// checkFile returns (hardFindings, softFindings) for a single doc page.
func (v *verifier) checkFile(path string) (hard, soft []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: read error: %v", path, err)}, nil
	}
	rel := path
	lines := strings.Split(string(data), "\n")

	var fence string // "" outside a block; otherwise the block language
	var goBlock []string
	var goBlockStart int
	var yamlBlock []string

	flushGo := func() {
		hard = append(hard, v.checkGoBlock(rel, goBlock, goBlockStart)...)
		goBlock = nil
	}
	flushYaml := func() {
		soft = append(soft, v.checkYamlBlock(rel, yamlBlock)...)
		yamlBlock = nil
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			lang := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "```")))
			// Strip any MDX/Docusaurus meta-string (e.g. ```go file=<rootDir>/x)
			// so the language is still detected; otherwise the block would be
			// silently skipped and its symbols never verified.
			if sp := strings.IndexAny(lang, " \t"); sp != -1 {
				lang = lang[:sp]
			}
			if fence == "" { // opening
				fence = lang
				if lang == "go" {
					goBlockStart = i + 1
				}
			} else { // closing
				if fence == "go" {
					flushGo()
				} else if fence == "yaml" || fence == "yml" {
					flushYaml()
				}
				fence = ""
			}
			continue
		}
		switch fence {
		case "go":
			goBlock = append(goBlock, line)
		case "yaml", "yml":
			yamlBlock = append(yamlBlock, line)
		}
		// Go-version claims are checked everywhere (prose, headings, and code).
		hard = append(hard, v.checkGoVersionLine(rel, i+1, line)...)
	}
	// Flush a trailing unclosed block (malformed doc) rather than dropping it.
	if fence == "go" {
		flushGo()
	} else if fence == "yaml" || fence == "yml" {
		flushYaml()
	}
	return hard, soft
}

func (v *verifier) checkGoVersionLine(rel string, ln int, line string) (out []string) {
	seen := map[string]bool{}
	add := func(minor string) {
		if minor == "" || minor == v.goMinor || seen[minor] {
			return
		}
		seen[minor] = true
		out = append(out, fmt.Sprintf("%s:%d: Go version %q does not match go.mod (%s)", rel, ln, minor, v.goMinor))
	}
	for _, m := range reGoVer.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	for _, m := range reGoVerPlus.FindAllStringSubmatch(line, -1) {
		add(m[1])
	}
	return out
}

func (v *verifier) checkGoBlock(rel string, block []string, startLine int) (out []string) {
	// We validate a `pkg.Symbol` only when the block DEMONSTRABLY imports the
	// Nucleus package whose short name is `pkg`. This is deliberately
	// conservative: it eliminates false positives from stdlib short-name
	// collisions (e.g. stdlib `errors.Is` vs `pkg/errors`), third-party
	// packages, and import-less snippets — at the cost of not checking
	// snippets that omit imports. Consistent with the guard's "errs toward
	// false-negatives over noise" philosophy.
	// imported maps the qualifier USED IN CODE (alias, or the package's last
	// path segment) to the Nucleus package short name to look up in the
	// baseline. e.g. `import "…/pkg/auth"` → {"auth":"auth"};
	// `import a "…/pkg/auth"` → {"a":"auth"}.
	imported := map[string]string{}
	local := map[string]bool{} // := / var-declared identifiers in this block
	for _, line := range block {
		if m := reNucleusImport.FindStringSubmatch(line); m != nil {
			segs := strings.Split(strings.TrimRight(m[2], "/"), "/")
			realPkg := segs[len(segs)-1] // last path segment is the pkg name
			qualifier := m[1]            // explicit alias, if any
			if qualifier == "" {
				qualifier = realPkg
			}
			imported[qualifier] = realPkg
		}
		for _, m := range reLocalDef.FindAllStringSubmatch(line, -1) {
			local[m[1]] = true
		}
	}
	for idx, line := range block {
		for _, m := range rePkgSymbol.FindAllStringSubmatch(line, -1) {
			pkg, sym := m[1], m[2]
			realPkg, ok := imported[pkg]
			if !ok || local[pkg] {
				continue
			}
			syms, ok := v.pkgSyms[realPkg]
			if !ok {
				continue // not a frozen Nucleus package; nothing to verify against
			}
			if !syms[sym] {
				out = append(out, fmt.Sprintf("%s:%d: %s.%s not in freeze baseline for pkg/%s", rel, startLine+idx, pkg, sym, realPkg))
			}
		}
	}
	return out
}

func (v *verifier) checkYamlBlock(rel string, block []string) (out []string) {
	// Only inspect blocks anchored as Nucleus config: at least one top-level
	// (column-0) key must be a known config section. Otherwise it is an
	// arbitrary YAML example (CI workflow, etc.) and out of scope.
	anchored := false
	for _, line := range block {
		if m := reYamlKey.FindStringSubmatch(line); m != nil && m[1] == "" && v.cfgTop[m[2]] {
			anchored = true
			break
		}
	}
	if !anchored {
		return nil
	}
	for _, line := range block {
		m := reYamlKey.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[2]
		if !v.cfgKeys[key] {
			out = append(out, fmt.Sprintf("%s: yaml key %q not in CONFIG_KEY_REGISTRY", rel, key))
		}
	}
	return out
}
