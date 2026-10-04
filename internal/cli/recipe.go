// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

// An entry whose wiring is more than an import carries a recipe (ADR-035):
// calls spliced into the nucleus.New() chain of main.go, the configuration
// block it reads, the routes it serves, and what is left to the person.
// `nucleus add` and `nucleus new --with` apply it here. The editor is the one
// `generate module --mount` uses, and every step is idempotent: a recipe
// applied twice changes nothing the second time, which is what lets a person
// run `nucleus add` again without reading what it did the first time.

// recipeTargets are the two files a recipe writes into.
type recipeTargets struct {
	root   string // the module root, for relative paths in messages
	main   string // the file holding the nucleus.New() chain
	config string // the project's configuration file
}

// applyRecipe writes (or, on a dry run, describes) what an entry's recipe
// adds. A main.go without a builder chain is not guessed at: the calls to
// add are printed and the command fails, the way `generate module --mount`
// does.
func applyRecipe(e knownproviders.Entry, t recipeTargets, dryRun bool, w io.Writer) error {
	r := e.Recipe
	if r == nil {
		return nil
	}
	mainRel := rel(t.root, t.main)
	var chainErr error
	for _, call := range r.Chain {
		if dryRun {
			fmt.Fprintf(w, "would wire: .%s  →  %s (nucleus.New() chain)\n", call, mainRel)
			continue
		}
		added, err := ensureChainCall(t.main, call, r.Imports...)
		switch {
		case errors.Is(err, errNoBuilderChain):
			fmt.Fprintf(w, "wire it in %s yourself — no nucleus.New() builder chain to edit:\n  nucleus.New().%s\n", mainRel, call)
			for _, imp := range r.Imports {
				fmt.Fprintf(w, "  import %q\n", imp)
			}
			chainErr = fmt.Errorf("%s: %w", e.Name, err)
		case err != nil:
			return err
		case added:
			fmt.Fprintf(w, "wired  .%s into %s\n", call, mainRel)
		default:
			fmt.Fprintf(w, "already wired in %s: .%s\n", mainRel, call)
		}
	}
	if r.Config != "" {
		if err := applyRecipeConfig(e, t, dryRun, w); err != nil {
			return err
		}
	}
	if len(r.Routes) > 0 {
		fmt.Fprintf(w, "serves: %s\n", strings.Join(r.Routes, ", "))
	}
	for _, next := range r.Then {
		fmt.Fprintf(w, "then: %s\n", next)
	}
	return chainErr
}

// topLevelKey matches a key at the start of a line of a YAML block mapping.
var topLevelKey = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_-]*)\s*:`)

// yamlTopLevelKeys returns the top-level keys a YAML document sets, in
// order. Comments do not start with a key, and an indented line is not at
// the top level, so a line-start match is enough for the block mappings
// nucleus.yml is written in.
func yamlTopLevelKeys(doc string) []string {
	var keys []string
	for _, m := range topLevelKey.FindAllStringSubmatch(doc, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// applyRecipeConfig appends the recipe's block to the configuration file
// when none of its top-level keys is set there, and otherwise says what is
// already set and prints the block to merge by hand: a key the person has
// set is theirs, and rewriting it is how a tool loses somebody's issuer.
func applyRecipeConfig(e knownproviders.Entry, t recipeTargets, dryRun bool, w io.Writer) error {
	block := strings.TrimRight(e.Recipe.Config, "\n") + "\n"
	blockKeys := yamlTopLevelKeys(block)
	configRel := rel(t.root, t.config)

	printBlock := func() {
		for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}
	}

	ext := strings.ToLower(filepath.Ext(t.config))
	raw, err := os.ReadFile(t.config)
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(w, "no %s to write the configuration into; add this block to the application's configuration:\n", configRel)
		printBlock()
		return nil
	case err != nil:
		return err
	case ext != ".yml" && ext != ".yaml":
		fmt.Fprintf(w, "%s is not YAML; add the equivalent of this block to it:\n", configRel)
		printBlock()
		return nil
	}

	existing := map[string]bool{}
	for _, k := range yamlTopLevelKeys(string(raw)) {
		existing[k] = true
	}
	var set, unset []string
	for _, k := range blockKeys {
		if existing[k] {
			set = append(set, k)
		} else {
			unset = append(unset, k)
		}
	}
	switch {
	case len(unset) == 0:
		fmt.Fprintf(w, "already configured in %s: %s (left as it is)\n", configRel, strings.Join(set, ", "))
		return nil
	case len(set) > 0:
		fmt.Fprintf(w, "%s already sets %s, so nothing was written to it; merge this block by hand:\n", configRel, strings.Join(set, ", "))
		printBlock()
		return nil
	case dryRun:
		fmt.Fprintf(w, "would write to %s:\n", configRel)
		printBlock()
		return nil
	}

	out := string(raw)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "\n" + block
	if err := os.WriteFile(t.config, []byte(out), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(w, "wrote  %s to %s\n", strings.Join(blockKeys, ", "), configRel)
	return nil
}
