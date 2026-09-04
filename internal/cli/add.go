// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

// The optional pieces of the framework — database drivers, telemetry
// exporters, cloud storage, authentication backends — each ship as their own
// module so an application links only what it uses. Adding one is two steps,
// a `go get` and a blank import, and the second is the one people forget:
// the build then succeeds and the failure arrives at run time as "unknown
// driver".
//
// `nucleus add` does both. It exists because the two-step version is exactly
// the kind of instruction that reads as trivial in a README and costs a
// afternoon when a name is mistyped.

func runAdd(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	// The flag package prints its own usage on --help AND on a bad flag,
	// and this command prints the real usage itself below — with the
	// output going to stderr the reader saw it twice. Silence the package
	// and keep one printer.
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".", "Module root to modify")
	into := fs.String("into", "", "File to write the import into (default: the file with package main, else the first .go file at the module root)")
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

	mods := make([]knownproviders.Provider, 0, len(names))
	for _, name := range names {
		p, ok := lookupAddable(name)
		if !ok {
			return fmt.Errorf("unknown module %q\n\navailable:\n%s", name, addableList())
		}
		mods = append(mods, p)
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

	for _, p := range mods {
		if *dryRun {
			fmt.Fprintf(stdout, "would run: go get %s\n", p.Module)
			fmt.Fprintf(stdout, "would add: import _ %q  →  %s\n", p.Module, rel(root, target))
			continue
		}
		fmt.Fprintf(stdout, "go get %s\n", p.Module)
		cmd := exec.Command("go", "get", p.Module)
		cmd.Dir = root
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go get %s: %w", p.Module, err)
		}
		added, err := ensureBlankImport(target, p.Module)
		if err != nil {
			return err
		}
		if added {
			fmt.Fprintf(stdout, "added  import _ %q to %s\n", p.Module, rel(root, target))
		} else {
			fmt.Fprintf(stdout, "already imported in %s\n", rel(root, target))
		}
	}
	return nil
}

// lookupAddable resolves a name the way a person would type it. Drivers are
// keyed internally by their database/sql name — "pgx", "sqlserver" — which
// nobody types, so the human names are accepted too.
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

func lookupAddable(name string) (knownproviders.Provider, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, alias := range []struct{ from, to string }{
		{"postgres", "pgx"}, {"postgresql", "pgx"}, {"pg", "pgx"},
		{"mssql", "sqlserver"}, {"sqlserver", "sqlserver"},
	} {
		if name == alias.from {
			name = alias.to
			break
		}
	}
	if p, ok := knownproviders.DBDriver(name); ok {
		return p, true
	}
	if p, ok := knownproviders.TelemetryExporter(name); ok {
		return p, true
	}
	if p, ok := knownproviders.StorageProvider(name); ok {
		return p, true
	}
	if p, ok := knownproviders.AuthBackend(name); ok {
		return p, true
	}
	if p, ok := knownproviders.SecretsResolver(strings.TrimSuffix(name, ":") + ":"); ok {
		return p, true
	}
	return knownproviders.Provider{}, false
}

func addableList() string {
	var b strings.Builder
	for _, group := range []struct {
		label string
		names []string
		look  func(string) (knownproviders.Provider, bool)
	}{
		{"database drivers", knownproviders.DBDriverNames(), knownproviders.DBDriver},
		{"telemetry exporters", knownproviders.TelemetryExporterNames(), knownproviders.TelemetryExporter},
		{"storage providers", knownproviders.StorageProviderNames(), knownproviders.StorageProvider},
		{"authentication backends", knownproviders.AuthBackendNames(), knownproviders.AuthBackend},
	} {
		names := append([]string(nil), group.names...)
		sort.Strings(names)
		fmt.Fprintf(&b, "  %s:\n", group.label)
		for _, n := range names {
			p, _ := group.look(n)
			fmt.Fprintf(&b, "    %-12s %s\n", p.Name, p.Module)
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
// touch is a tool people stop running.
func ensureBlankImport(path, module string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == module {
			return false, nil
		}
	}

	line := "\t_ " + strconv.Quote(module) + "\n"
	var out []byte

	// Prefer the last parenthesised import block: appending there keeps the
	// file's existing grouping instead of inventing a second block.
	var block *ast.GenDecl
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if ok && gd.Tok == token.IMPORT && gd.Lparen.IsValid() {
			block = gd
		}
	}
	if block != nil {
		pos := fset.Position(block.Rparen).Offset
		// Back up over the indentation preceding the closing paren.
		start := pos
		for start > 0 && (src[start-1] == ' ' || src[start-1] == '\t') {
			start--
		}
		out = append(out, src[:start]...)
		out = append(out, '\n')
		out = append(out, line...)
		out = append(out, src[start:]...)
	} else {
		// No block: put one right after the package clause.
		pkgEnd := fset.Position(f.Name.End()).Offset
		nl := bytes.IndexByte(src[pkgEnd:], '\n')
		if nl < 0 {
			return false, fmt.Errorf("%s: cannot find the end of the package clause", path)
		}
		at := pkgEnd + nl + 1
		out = append(out, src[:at]...)
		out = append(out, []byte("\nimport (\n"+line+")\n")...)
		out = append(out, src[at:]...)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	// gofmt sorts the block; failing to find it is not worth failing the
	// command over, since the import is already correct Go.
	if gofmt, err := exec.LookPath("gofmt"); err == nil {
		_ = exec.Command(gofmt, "-w", path).Run()
	}
	return true, nil
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}

func printAddUsage(w io.Writer) {
	bw := bufio.NewWriter(w)
	defer bw.Flush()
	fmt.Fprintln(bw, "Usage:")
	fmt.Fprintln(bw, "  nucleus add <module>... [--dir <path>] [--into <file>] [--dry-run]")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "Runs `go get` for each module and writes the blank import that")
	fmt.Fprintln(bw, "registers it. Both steps are needed; the import is the one that is")
	fmt.Fprintln(bw, "easy to forget, because the build succeeds without it.")
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "Available:")
	fmt.Fprint(bw, addableList())
	fmt.Fprintln(bw, "")
	fmt.Fprintln(bw, "Examples:")
	fmt.Fprintln(bw, "  nucleus add postgres")
	fmt.Fprintln(bw, "  nucleus add s3 --into cmd/server/main.go")
	fmt.Fprintln(bw, "  nucleus add mysql --dry-run")
}
