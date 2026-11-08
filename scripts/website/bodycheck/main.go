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
//  4. [hard] `db:"…"` struct tags in ```go blocks must parse cleanly against
//     the REAL parser (pkg/model.ExtractMeta over a synthetic field): any
//     directive the parser would silently ignore fails the check. This is the
//     guard for NU5-1 — the website taught `db:"id,primary"` / `fk=…` for
//     four audit rounds while parseDBTag never recognized any of it.
//
// Usage: bodycheck [-root .] [-docs website/docs] [-strict]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/model"
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
	root           string
	goVersion      string                     // e.g. "1.26.4"
	goMinor        string                     // e.g. "1.26"
	pkgSyms        map[string]map[string]bool // pkg short name -> set of exported symbol names
	cfgKeys        map[string]bool            // config-key segments from the registry
	cfgOpenSubtree map[string]bool            // sections whose user-keyed child owns an opaque subtree (`auth.<backend>.*`)
	cfgTop         map[string]bool            // top-level config sections (anchor for yaml blocks)
	cfgUserKeyed   map[string]bool            // sections whose CHILDREN are names the user picks
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
	// A registry key: dotted segments, a `<placeholder>` for a name the
	// operator chooses, and `*` as the last segment for a subtree the
	// framework does not own (`auth.<backend>.*`, `interceptors.<name>.*`,
	// `storage.<provider>.*`: the section belongs to a REGISTERED third
	// party, which validates its own keys).
	reRegistryKey = regexp.MustCompile("`([a-z][a-z0-9_]*(?:\\.(?:[a-z0-9_<>]+|\\*))*)`")
	reDBTag       = regexp.MustCompile(`\bdb:"([^"]*)"`)
	// A Quark model block: a pk:"…" or quark:"…" tag, or an import of the
	// Quark module. In such a block `db:"…"` is Quark's grammar (the column
	// name, then options after a comma), not pkg/model's directives.
	reQuarkBlock  = regexp.MustCompile(`\b(?:pk|quark):"|"github\.com/jcsvwinston/quark`)
	reQuarkColumn = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
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
	// Sections whose children are USER-CHOSEN names are declared as such by
	// the registry itself: it writes them with a placeholder segment
	// (`databases.<alias>.url`, `multitenant.tenants.<tenant>.site`). The
	// segment right before a placeholder is therefore a section whose child
	// level is a name the operator invents — never a registry key. Deriving
	// the list from the registry keeps it correct when a new map-typed
	// section is added; a hardcoded list would rot.
	//
	// A pattern row ending in `*` right after a placeholder
	// (`auth.<backend>.*`) says more: everything BELOW the operator-chosen
	// name is a third party's configuration struct — a registered backend,
	// interceptor or storage provider validates its own keys, and the
	// framework only checks that the section belongs to a registered name.
	// The registry cannot list those keys, so the checker must not judge
	// them: before this rule the pages teaching LDAP, OIDC, SAML, Sentry
	// and a third-party storage provider carried 39 permanent advisories.
	v.indexRegistry(string(reg))
	return nil
}

// indexRegistry reads the keys CONFIG_KEY_REGISTRY.md documents into the
// three sets checkYamlBlock consults: every literal segment is a key, the
// segment before a `<placeholder>` is user-keyed, and the segment before a
// `<placeholder>.*` pair owns an open subtree.
func (v *verifier) indexRegistry(reg string) {
	v.cfgUserKeyed = map[string]bool{}
	v.cfgOpenSubtree = map[string]bool{}
	for _, m := range reRegistryKey.FindAllStringSubmatch(reg, -1) {
		key := m[1]
		segs := strings.Split(key, ".")
		for i, seg := range segs {
			if seg == "*" {
				if i >= 2 && strings.HasPrefix(segs[i-1], "<") {
					v.cfgOpenSubtree[segs[i-2]] = true
				}
				continue
			}
			if strings.HasPrefix(seg, "<") {
				if i > 0 {
					v.cfgUserKeyed[segs[i-1]] = true
				}
				continue
			}
			v.cfgKeys[seg] = true
		}
		if !strings.HasPrefix(segs[0], "<") {
			v.cfgTop[segs[0]] = true
		}
	}
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
	quarkModel := false
	for _, line := range block {
		if reQuarkBlock.MatchString(line) {
			quarkModel = true
		}
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
		for _, m := range reDBTag.FindAllStringSubmatch(line, -1) {
			if quarkModel {
				out = append(out, checkQuarkDBTag(rel, startLine+idx, m[1])...)
				continue
			}
			out = append(out, checkDBTag(rel, startLine+idx, m[1])...)
		}
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

// checkDBTag validates one documented db-tag against the real parser by
// running model.ExtractMeta over a synthetic one-field struct carrying the
// tag. Two failure modes: the parser rejects the tag outright (error), or it
// records directives it does not recognize — which at runtime are applied as
// nothing. Docs must never teach either.
func checkDBTag(rel string, ln int, tag string) (out []string) {
	if tag == "" || tag == "-" { // "-" is the exclusion convention — valid
		return nil
	}
	probe := reflect.StructOf([]reflect.StructField{{
		Name: "Probe",
		Type: reflect.TypeOf(""),
		Tag:  reflect.StructTag(fmt.Sprintf(`db:%q`, tag)),
	}})
	meta, err := model.ExtractMeta(reflect.New(probe).Elem().Interface())
	if err != nil {
		return []string{fmt.Sprintf("%s:%d: db tag %q rejected by the parser: %v", rel, ln, tag, err)}
	}
	for _, f := range meta.Fields {
		for _, tok := range f.UnknownDBTokens {
			out = append(out, fmt.Sprintf("%s:%d: db tag %q: directive %q is not recognized by the parser (at runtime it is silently ignored)", rel, ln, tag, tok))
		}
	}
	return out
}

// checkQuarkDBTag validates one db-tag of a Quark model block (NU-137: the
// generators write Quark models, and the website teaches them). Quark reads
// the text before the first comma as the column name and the rest as
// options (size=, precision=, scale=); the column must be a plain
// identifier or "-". pkg/model's directive grammar (`column:name;required`,
// a bare `pk`) in a Quark block is the falsehood this catches: Quark would
// take `column:name` as the column's name.
func checkQuarkDBTag(rel string, ln int, tag string) (out []string) {
	column := strings.TrimSpace(tag)
	if i := strings.IndexByte(column, ','); i >= 0 {
		column = strings.TrimSpace(column[:i])
	}
	if column == "" || column == "-" {
		return nil
	}
	if !reQuarkColumn.MatchString(column) {
		return []string{fmt.Sprintf("%s:%d: db tag %q in a Quark model: %q is not a column name (Quark reads db:\"<column>[,option…]\"; pkg/model directives do not apply)", rel, ln, tag, column)}
	}
	return nil
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
	// Two kinds of key are not the framework's to judge, and judging them
	// made the page that TEACHES configuration permanently red — an
	// advisory that is always wrong is one people learn to skip.
	//
	//   1. Everything under `modules:` belongs to the MODULE's own config
	//      struct, declared in a Go type the framework cannot enumerate.
	//   2. The child of a user-keyed section is a name the operator picks
	//      (`databases: primary:`, `tenants: acme:`). One level only — the
	//      keys BELOW it are the framework's again and stay checked.
	//   3. Unless the registry declares that subtree open (`auth.<backend>.*`,
	//      `interceptors.<name>.*`, `storage.<provider>.*`): below a name
	//      the registry does not know (`auth: ldap:`, `interceptors:
	//      sentry:`, `storage: ceph:`) the keys belong to the registered
	//      third party's own struct. A typed section the registry DOES know
	//      (`storage: s3:`) keeps its keys checked.
	type frame struct {
		indent int
		key    string
	}
	var stack []frame

	for _, line := range block {
		m := reYamlKey.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent, key := len(m[1]), m[2]

		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}

		underModules := false
		for _, f := range stack {
			if f.key == "modules" {
				underModules = true
				break
			}
		}
		parentUserKeyed := len(stack) > 0 && v.cfgUserKeyed[stack[len(stack)-1].key]
		underOpenSubtree := false
		for i := 0; i+1 < len(stack); i++ {
			if v.cfgOpenSubtree[stack[i].key] && !v.cfgKeys[stack[i+1].key] {
				underOpenSubtree = true
				break
			}
		}

		stack = append(stack, frame{indent: indent, key: key})

		if underModules || parentUserKeyed || underOpenSubtree {
			continue
		}
		if !v.cfgKeys[key] {
			out = append(out, fmt.Sprintf("%s: yaml key %q not in CONFIG_KEY_REGISTRY", rel, key))
		}
	}
	return out
}
