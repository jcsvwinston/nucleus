// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LinkedIn returns the catalog entries the PROJECT rooted at dir links: the
// ones whose blank import — the line `nucleus add` writes — appears in the
// imports of the project's Go files. It reads the files `nucleus add`
// writes into (every non-test .go file at the root, or cmd/*/*.go when the
// root has none) with go/parser in imports-only mode: no build, no module
// resolution, so it answers in the time it takes to read a few files.
//
// It exists for the commands that validate a configuration from inside the
// `nucleus` binary (doctor, health, config print). The binary links none of
// providers/*, so its registries do not know `s3`, `ldap` or `sentry`, and
// without this the block `nucleus add sentry` had just written was refused
// as an unknown key by the next command (NU-132). The project's imports
// are what the running application registers, so they are what the CLI
// should accept.
//
// A directory with no Go files, or one that cannot be read, links nothing:
// the caller validates with the registries alone, as before.
func LinkedIn(dir string) []Entry {
	files := goFilesIn(dir)
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(dir, "cmd", "*", "*.go"))
		sort.Strings(files)
	}
	imported := map[string]bool{}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, imp := range f.Imports {
			if p, err := strconv.Unquote(imp.Path.Value); err == nil {
				imported[p] = true
			}
		}
	}
	return LinkedByImports(imported)
}

// LinkedByImports is LinkedIn over a set of import paths already read: the
// entries whose ImportPath is among them, in catalog order. A core entry
// (Ships == InCore) registers through the framework module itself and is
// never "not linked", so only the entries shipped as modules count.
func LinkedByImports(imported map[string]bool) []Entry {
	var out []Entry
	for _, e := range catalog {
		if e.Ships != AsModule {
			continue
		}
		if path := e.ImportPath(); path != "" && imported[path] {
			out = append(out, e)
		}
	}
	return out
}

func goFilesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)
	return files
}
