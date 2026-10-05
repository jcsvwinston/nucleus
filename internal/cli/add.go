// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

// The optional pieces of the framework — database drivers, telemetry
// exporters, cloud storage, authentication backends, a secrets resolver —
// each ship as their own module so an application links only what it uses.
// Adding one is two steps, a `go get` and a blank import, and the second is
// the one people forget: the build then succeeds and the failure arrives at
// run time as "unknown driver". Then there is a third step nobody mentions:
// an installed storage provider does nothing until storage.provider names it.
//
// `nucleus add` does the first two and names the third. What it can install
// is the catalog in internal/knownproviders (ADR-034) — the same table
// `nucleus new --with` resolves and the runtime's refusals point at — and
// what it fetches is the version released with this CLI, not whatever the
// module proxy calls latest today.

// goGet runs `go get <target>` in root. A variable so the command's tests
// can drive runAdd end to end without a network or a module proxy (NU-23:
// the command edits go.mod and a source file and nothing exercised it).
var goGet = func(root, target string, stdout, stderr io.Writer) error {
	cmd := exec.Command("go", "get", target)
	cmd.Dir = root
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func runAdd(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	// The flag package prints its own usage on --help AND on a bad flag,
	// and this command prints the real usage itself below — with the
	// output going to stderr the reader saw it twice. Silence the package
	// and keep one printer.
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "Module root to modify")
	into := fs.String("into", "", "File to write the import into (default: the file with package main, else the first .go file at the module root)")
	configFile := fs.String("config", "nucleus.yml", "Configuration file an entry's configuration block is written into, relative to --dir")
	dryRun := fs.Bool("dry-run", false, "Print what would change and modify nothing")
	fs.Usage = func() {}
	// Go's flag package stops at the first non-flag argument, so
	// `nucleus add postgres --dry-run` would treat --dry-run as a module
	// name. People write the flag last; the parser has to cope rather than
	// the person having to remember.
	flags, names := splitFlagsAndArgs(args)
	if err := fs.Parse(flags); err != nil {
		// `nucleus add --help` is a request, not a mistake: the flag package
		// reports it as an error, and every other command in this CLI exits
		// zero for it.
		if errors.Is(err, flag.ErrHelp) {
			printAddUsage(stdout)
			return nil
		}
		return err
	}
	names = append(names, fs.Args()...)
	if len(names) == 0 {
		printAddUsage(stdout)
		return nil
	}

	entries := make([]knownproviders.Entry, 0, len(names))
	for _, name := range names {
		e, err := resolveCatalogName(name)
		if err != nil {
			return err
		}
		entries = append(entries, e)
	}

	root, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("%s has no go.mod — run this from your module root, or pass --dir", root)
	}

	target := *into
	if target == "" {
		target, err = pickImportFile(root)
		if err != nil {
			return err
		}
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}

	config := *configFile
	if !filepath.IsAbs(config) {
		config = filepath.Join(root, config)
	}
	for _, e := range entries {
		if err := addEntry(e, root, target, config, *dryRun, stdout, stderr); err != nil {
			return err
		}
	}
	return nil
}

// resolveCatalogName resolves a name the way a person would type it — the
// catalog name, an alias (postgresql, pg, pgx, mssql), any case — and turns
// a miss into the nearest name instead of a table to search.
func resolveCatalogName(name string) (knownproviders.Entry, error) {
	if e, ok := knownproviders.Lookup(name); ok {
		return e, nil
	}
	if near := knownproviders.Suggest(name); near != "" {
		return knownproviders.Entry{}, fmt.Errorf("%q is not in the catalog — did you mean %s? (nucleus add --help lists every entry)", name, near)
	}
	return knownproviders.Entry{}, fmt.Errorf("%q is not in the catalog: %s (nucleus add --help says what each one installs)",
		name, strings.Join(knownproviders.Names(), ", "))
}

// addEntry installs one entry: the `go get` its kind needs, the blank import
// that registers it, the recipe that wires what an import does not (a call
// in the nucleus.New() chain, a configuration block — ADR-035), and the line
// that says what selects or wires it.
func addEntry(e knownproviders.Entry, root, target, config string, dryRun bool, stdout, stderr io.Writer) error {
	recipe := recipeTargets{root: root, main: target, config: config}
	var fetch []string
	if t := e.Target(); t != "" {
		fetch = append(fetch, t)
	}
	if e.DriverModule != "" {
		if engine := projectEngine(root); engine != "" {
			fetch = append(fetch, fmt.Sprintf(e.DriverModule, engine))
		}
	}
	imp := e.ImportPath()

	if dryRun {
		for _, t := range fetch {
			fmt.Fprintf(stdout, "would run: go get %s\n", t)
		}
		if imp != "" {
			fmt.Fprintf(stdout, "would add: import _ %q  →  %s\n", imp, rel(root, target))
		}
		if err := applyRecipe(e, recipe, true, stdout); err != nil {
			return err
		}
		printAfterAdd(stdout, e, root, "then ")
		return nil
	}

	if e.Ships == knownproviders.InCore {
		fmt.Fprintf(stdout, "%s is part of the framework: nothing to fetch\n", e.Name)
	}
	for _, t := range fetch {
		fmt.Fprintf(stdout, "go get %s\n", t)
		if err := goGet(root, t, stdout, stderr); err != nil {
			return fmt.Errorf("go get %s: %w", t, err)
		}
	}
	if imp != "" {
		added, err := ensureBlankImport(target, imp)
		if err != nil {
			return err
		}
		if added {
			fmt.Fprintf(stdout, "added  import _ %q to %s\n", imp, rel(root, target))
		} else {
			fmt.Fprintf(stdout, "already imported in %s\n", rel(root, target))
		}
	}
	if err := applyRecipe(e, recipe, false, stdout); err != nil {
		return err
	}
	printAfterAdd(stdout, e, root, "")
	return nil
}

// printAfterAdd says what the person still has to do: the configuration
// that selects the entry, or — for what no configuration selects — what
// wires it. An installed entry that nothing selects does nothing.
func printAfterAdd(w io.Writer, e knownproviders.Entry, root, prefix string) {
	// A recipe that writes the configuration block has already said what it
	// wrote, or printed it; repeating the selecting key would be noise.
	if e.Selects != "" && (e.Recipe == nil || e.Recipe.Config == "") {
		fmt.Fprintf(w, "%sselect it in nucleus.yml: %s\n", prefix, e.Selects)
	}
	switch e.Ships {
	case knownproviders.InCore:
		if e.Recipe == nil {
			fmt.Fprintf(w, "  %s\n", e.Wires)
		}
	case knownproviders.InSuite:
		fmt.Fprintf(w, "  not pinned: a suite product's version belongs to the umbrella's certified set, which this CLI does not carry, so it is the tag the module proxy calls latest\n")
		if projectImports(root, e.Module) {
			break
		}
		fmt.Fprintf(w, "%swire it: %s\n", prefix, e.Wires)
		fmt.Fprintf(w, "  nothing imports it yet: go.mod keeps it as an indirect require, and the next go mod tidy drops it until code does\n")
		if e.DriverModule != "" && projectEngine(root) == "" {
			fmt.Fprintf(w, "  %s also has a driver module per engine (%s); no framework driver in go.mod says which one this project needs\n",
				e.Name, fmt.Sprintf(e.DriverModule, "<engine>"))
		}
	}
}

// projectImports reports whether any Go file of the project imports the
// module or a package under it — a suite product the project already wires
// needs no "wire it" line.
func projectImports(root, module string) bool {
	found := false
	fset := token.NewFileSet()
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" || d.Name() == "node_modules" || d.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == module || strings.HasPrefix(p, module+"/") {
				found = true
			}
		}
		return nil
	})
	return found
}

// projectEngine is the engine directory of the framework driver the
// project's go.mod requires ("postgres" for drivers/postgres), when it
// requires exactly one — the suite's ORM ships its own driver module per
// engine under the same directory names.
func projectEngine(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	prefix := knownproviders.RepoModule + "/drivers/"
	var engines []string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) == 0 || !strings.HasPrefix(fields[0], prefix) {
			continue
		}
		engine := strings.TrimPrefix(fields[0], prefix)
		if !slices.Contains(engines, engine) {
			engines = append(engines, engine)
		}
	}
	if len(engines) != 1 {
		return ""
	}
	return engines[0]
}

// splitFlagsAndArgs separates flags from positional arguments, keeping the
// value of a `--flag value` pair with its flag. The boolean flags are known
// here rather than guessed: `--dry-run postgres` must not swallow the module
// name as the flag's value.
func splitFlagsAndArgs(args []string) (flags, positional []string) {
	boolFlag := map[string]bool{"--dry-run": true, "-dry-run": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		if strings.Contains(a, "=") || boolFlag[a] {
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			flags = append(flags, args[i])
		}
	}
	return flags, positional
}

// lookupAddable resolves a name the way `nucleus add` does.
func lookupAddable(name string) (knownproviders.Entry, bool) {
	return knownproviders.Lookup(name)
}

// groupNote is what a group's heading adds about how its entries arrive.
var groupNote = map[knownproviders.Group]string{
	knownproviders.GroupFederated:  " (part of the framework: nothing to fetch; the import registers the provider, and the routes are mounted in main.go)",
	knownproviders.GroupCapability: " (part of the framework: nothing to fetch; the wiring is written into main.go and nucleus.yml)",
	knownproviders.GroupSuite:      " (fetched at the tag the module proxy calls latest; nothing is imported for you — --dry-run says what wires each one)",
}

// catalogListing renders the catalog the way --help prints it: one line per
// entry, under its group — the name, what is fetched or imported, at which
// version, and the other spellings.
func catalogListing() string {
	var b strings.Builder
	for _, g := range knownproviders.Groups() {
		var rows []knownproviders.Entry
		for _, e := range knownproviders.Entries() {
			if e.Group == g {
				rows = append(rows, e)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s%s:\n", g, groupNote[g])
		for _, e := range rows {
			what := e.Module
			if e.Ships == knownproviders.InCore {
				what = e.ImportPath()
				if e.Recipe != nil {
					if what != "" {
						what += " + "
					}
					what += recipeSummary(e.Recipe)
				}
			}
			line := fmt.Sprintf("    %-16s %s", e.Name, what)
			if v := e.Version(); v != "" {
				line += " " + v
			}
			if len(e.Aliases) > 0 {
				line += "  (also: " + strings.Join(e.Aliases, ", ") + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// pickImportFile chooses where the blank import goes. package main is the
// right answer when there is one: a side-effect import belongs with the
// program that assembles the application, not in a library package where it
// would impose the dependency on every importer.
func pickImportFile(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		candidates = append(candidates, filepath.Join(root, e.Name()))
	}
	sort.Strings(candidates)
	for _, c := range candidates {
		if pkgNameOf(c) == "main" {
			return c, nil
		}
	}
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	// Also look one level down for the conventional cmd/<name>/main.go.
	matches, _ := filepath.Glob(filepath.Join(root, "cmd", "*", "*.go"))
	sort.Strings(matches)
	for _, m := range matches {
		if pkgNameOf(m) == "main" {
			return m, nil
		}
	}
	return "", fmt.Errorf("found no Go file to add the import to under %s — pass --into <file>", root)
}

func pkgNameOf(path string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	if err != nil {
		return ""
	}
	return f.Name.Name
}

// ensureBlankImport adds `import _ "module"` to path unless it is already
// there, reporting whether it wrote anything.
//
// It edits the text rather than printing the AST back: go/printer would
// reformat the whole file, and a tool that reflows code it was not asked to
// touch is a tool people stop running. The editor itself lives in
// mountedit.go (ensureImport), shared with `generate module --mount`.
func ensureBlankImport(path, module string) (bool, error) {
	return ensureImport(path, module, "_")
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}

// recipeSummary is what --help says an import-less core entry writes.
func recipeSummary(r *knownproviders.Recipe) string {
	var parts []string
	for _, call := range r.Chain {
		parts = append(parts, "."+call)
	}
	if keys := yamlTopLevelKeys(r.Config); len(keys) > 0 {
		parts = append(parts, strings.Join(keys, ", ")+" in nucleus.yml")
	}
	return strings.Join(parts, " + ")
}

func printAddUsage(w io.Writer) {
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	fmt.Fprintln(bw, "Usage:")
	fmt.Fprintln(bw, "  nucleus add <name>... [--dir <path>] [--into <file>] [--config <file>] [--dry-run]")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "Installs a catalog entry: runs `go get` at the version released with this")
	fmt.Fprintln(bw, "CLI, writes the blank import that registers the entry, and names the")
	fmt.Fprintln(bw, "configuration that selects it. The import is the step that is easy to")
	fmt.Fprintln(bw, "forget, because the build succeeds without it.")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "An entry an import does not wire carries a recipe: the call it needs is")
	fmt.Fprintln(bw, "spliced into the nucleus.New() chain of main.go, or of --into (Mount, or a")
	fmt.Fprintln(bw, "With… option), its configuration block is written into --config when none")
	fmt.Fprintln(bw, "of its keys is set there (and printed when one is), and the routes it")
	fmt.Fprintln(bw, "serves are named. Running the command again changes nothing.")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "The catalog (nucleus new --with takes the same names):")
	fmt.Fprint(bw, catalogListing())
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "Examples:")
	fmt.Fprintln(bw, "  nucleus add postgres")
	fmt.Fprintln(bw, "  nucleus add s3 --into cmd/server/main.go")
	fmt.Fprintln(bw, "  nucleus add mysql --dry-run")
	fmt.Fprintln(bw, "  nucleus add oidc apikeys sql-queue")
}
