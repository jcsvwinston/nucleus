// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// loadTemplatesRecursive walks dir and parses every .html file into one
// template namespace (QCD-FW-7). Each file registers under its path relative
// to dir, with forward slashes — so a file at the root keeps the flat name
// the old *.html glob gave it ("base.html"), and the scaffold's nested
// layout resolves as "fieldservice/index.html". {{define "..."}} blocks
// inside any file register under their declared names, exactly as before.
// Returns the parsed set and how many files were registered.
//
// QCD-FW-9 extension points, applied in this order: base (WithTemplates)
// is the namespace parsed into when non-nil; funcs (WithTemplateFuncs) are
// registered on it BEFORE any file parses, so every template can call them.
func loadTemplatesRecursive(dir string, base *template.Template, funcs template.FuncMap) (*template.Template, int, error) {
	root := base
	if root == nil {
		root = template.New("")
	}
	if len(funcs) > 0 {
		root = root.Funcs(funcs)
	}
	count := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".html") {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read template %s: %w", name, err)
		}
		if _, err := root.New(name).Parse(string(raw)); err != nil {
			return fmt.Errorf("parse template %s: %w", name, err)
		}
		count++
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return root, count, nil
}
