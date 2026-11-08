// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
	"github.com/jcsvwinston/nucleus/internal/providerns"
)

// declareLinkedProviders tells the configuration validators which providers
// the PROJECT links, before a command loads its configuration (NU-132).
//
// The `nucleus` binary imports none of providers/*, so its registries do
// not know `s3`, `ldap` or `sentry`; the application whose main.go imports
// them does. Validating with the CLI's registries refused the block
// `nucleus add sentry` had just written — `doctor`, `health` and `config
// print` all reporting an unknown key against a file the server booted on.
// The project's imports are read from the directory of the configuration
// file (the working directory when none is named), the place `nucleus add`
// writes the import into; a directory without Go files declares nothing,
// and the registries alone decide, as before.
func declareLinkedProviders(configPath string) {
	dir := "."
	if p := strings.TrimSpace(configPath); p != "" {
		dir = filepath.Dir(p)
	}
	providerns.SetLinked(providerns.FromEntries(linkedProviders(dir)))
}

// linkedProviders returns the catalog entries the project rooted at dir
// links: the ones whose blank import appears in the imports of its Go files.
// It reads the files `nucleus add` writes into (every non-test .go file at
// the root, or cmd/*/*.go when the root has none) with go/parser in
// imports-only mode: no build, no module resolution, so it answers in the
// time it takes to read a few files. A directory with no Go files, or one
// that cannot be read, links nothing.
func linkedProviders(dir string) []knownproviders.Entry {
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
	return knownproviders.LinkedByImports(imported)
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
