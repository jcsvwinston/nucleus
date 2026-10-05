// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/internal/knownproviders"
)

// The catalog is one table read by five surfaces: `nucleus add`, `nucleus
// add --help`, `nucleus new --with`, `nucleus new --help` and the refusals
// the runtime prints when an entry is selected and not in the build
// (ADR-034). Before it there were two tables and four partial lists of the
// first one; `nucleus add aws-sm` worked and the help never said so. This
// test reads what each surface actually prints or accepts — not the table —
// and fails when one of them names something the others do not.
func TestOneCatalogAcrossTheSurfaces(t *testing.T) {
	want := knownproviders.Names()
	sort.Strings(want)

	// 1. What `nucleus add` accepts: every name and every alias resolves,
	// and the dry run installs exactly that entry.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var accepted []string
	for _, e := range knownproviders.Entries() {
		for _, spelling := range append([]string{e.Name}, e.Aliases...) {
			var out bytes.Buffer
			if err := runAdd([]string{spelling, "--dry-run", "--dir", dir}, nil, &out, io.Discard); err != nil {
				t.Errorf("nucleus add %s --dry-run: %v", spelling, err)
				continue
			}
			names := e.Module
			if imp := e.ImportPath(); imp != "" {
				names = imp
			} else if e.Recipe != nil {
				// An entry with no import is named by what its recipe
				// writes: the chain call, or the configuration it sets.
				names = recipeSummaryFirst(e.Recipe)
			}
			if !strings.Contains(out.String(), names) {
				t.Errorf("nucleus add %s --dry-run does not name %s:\n%s", spelling, names, out.String())
			}
		}
		accepted = append(accepted, e.Name)
	}
	sort.Strings(accepted)

	// 2. What `nucleus add --help` lists: the rows of the catalog section.
	var help bytes.Buffer
	if err := runAdd([]string{"--help"}, nil, &help, io.Discard); err != nil {
		t.Fatal(err)
	}
	helpRow := regexp.MustCompile(`(?m)^    ([a-z0-9-]+)\s+\S`)
	var listed []string
	for _, m := range helpRow.FindAllStringSubmatch(help.String(), -1) {
		listed = append(listed, m[1])
	}
	sort.Strings(listed)

	// 3. What `nucleus new --with` accepts.
	var withAccepts []string
	for _, name := range knownproviders.Names() {
		entries, err := resolveWith(name, "api")
		if err != nil || len(entries) != 1 || entries[0].Name != name {
			t.Errorf("nucleus new --with %s: %v %v", name, entries, err)
			continue
		}
		withAccepts = append(withAccepts, name)
	}
	sort.Strings(withAccepts)

	// 4. What `nucleus new --help` lists under --with.
	var withListed []string
	for _, sec := range commandUsages["new"].Sections {
		if strings.Contains(sec.Title, "--with") {
			for _, r := range sec.Rows {
				withListed = append(withListed, r.Name)
			}
		}
	}
	sort.Strings(withListed)

	for surface, got := range map[string][]string{
		"nucleus add accepts":      accepted,
		"nucleus add --help lists": listed,
		"nucleus new --with takes": withAccepts,
		"nucleus new --help lists": withListed,
	} {
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s %v; the catalog is %v", surface, got, want)
		}
	}

	// 5. The runtime's refusals: every entry a subsystem can refuse for not
	// being imported names a `nucleus add` that installs that same entry.
	type refusal struct {
		subsystem string
		lookup    func(string) (knownproviders.Provider, bool)
		keys      []string
	}
	refusals := []refusal{
		{"pkg/db", knownproviders.DBDriver, knownproviders.DBDriverNames()},
		{"pkg/observe", knownproviders.TelemetryExporter, knownproviders.TelemetryExporterNames()},
		{"pkg/storage", knownproviders.StorageProvider, knownproviders.StorageProviderNames()},
		{"pkg/auth", knownproviders.AuthBackend, knownproviders.AuthBackendNames()},
		{"pkg/auth/secrets", knownproviders.SecretsResolver, []string{"aws-sm:"}},
		{"pkg/auth (federated)", knownproviders.FederatedProvider, []string{"oidc"}},
	}
	addLine := regexp.MustCompile(`nucleus add ([a-z0-9-]+)`)
	hinted := map[string]bool{}
	for _, r := range refusals {
		for _, key := range r.keys {
			p, ok := r.lookup(key)
			if !ok {
				t.Errorf("%s refuses %q and the catalog does not know it", r.subsystem, key)
				continue
			}
			m := addLine.FindStringSubmatch(p.InstallHint())
			if m == nil {
				t.Errorf("%s's refusal for %q names no `nucleus add`:\n%s", r.subsystem, key, p.InstallHint())
				continue
			}
			got, ok := lookupAddable(m[1])
			if !ok || got.Module != p.Module || got.ImportPath() != p.ImportPath() {
				t.Errorf("%s's refusal for %q says `nucleus add %s`, which installs %q", r.subsystem, key, m[1], got.ImportPath())
			}
			hinted[m[1]] = true
		}
	}
	// Every entry that registers by import has a refusal pointing at it;
	// the suite products have no runtime registry to refuse from, and an
	// entry wired by its recipe alone has no import to be missing.
	for _, e := range knownproviders.Entries() {
		if e.Ships != knownproviders.InSuite && e.ImportPath() != "" && !hinted[e.Name] {
			t.Errorf("%s registers by import, and no runtime refusal names `nucleus add %s`", e.Name, e.Name)
		}
	}
}

// CAT-09: the CLI reference on the site lists the catalog. The repository does
// not generate its CLI reference, so this compares the `nucleus add` row of
// website/docs/cli/overview.md with the table, both ways: a name the command
// takes and the page omits, and a name the page lists that the command
// refuses.
func TestCLIReferenceListsTheCatalog(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootForTest(t), "website", "docs", "cli", "overview.md"))
	if err != nil {
		t.Fatal(err)
	}
	row := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "| `nucleus add ") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("website/docs/cli/overview.md has no row for `nucleus add`")
	}
	inRow := map[string]bool{}
	for _, m := range regexp.MustCompile("`([a-z0-9][a-z0-9-]*)`").FindAllStringSubmatch(row, -1) {
		inRow[m[1]] = true
	}
	for _, name := range knownproviders.Names() {
		if !inRow[name] {
			t.Errorf("the CLI reference row for `nucleus add` does not list %q", name)
		}
		delete(inRow, name)
	}
	for word := range inRow {
		if _, ok := knownproviders.Lookup(word); !ok {
			t.Errorf("the CLI reference row for `nucleus add` lists %q, which the command refuses", word)
		}
	}
}

// NU-104: the post-scaffold text points at pages that exist. It used to end
// on examples/mvc_api, removed with the rest of examples/.
func TestNewPointsAtDocsThatExist(t *testing.T) {
	root := repoRootForTest(t)
	pages := []string{docsQuickstart}
	for tmpl, g := range templateGuide {
		if g.path == "" || g.what == "" {
			t.Errorf("template %s has no guide", tmpl)
		}
		pages = append(pages, g.path)
	}
	for _, page := range pages {
		if _, err := os.Stat(filepath.Join(root, "website", "docs", filepath.FromSlash(page)+".md")); err != nil {
			t.Errorf("the post-scaffold text points at %s%s, which is not a page of website/docs: %v", docsBase, page, err)
		}
	}
	for _, tmpl := range []string{"mvc", "api", "suite", "module"} {
		stubScaffoldNetwork(t)
		var stdout bytes.Buffer
		if err := runNew([]string{"docs", "--out", t.TempDir(), "--template", tmpl, "--offline"}, strings.NewReader(""), &stdout, io.Discard); err != nil {
			t.Fatalf("%s: %v", tmpl, err)
		}
		out := stdout.String()
		if strings.Contains(out, "examples/") {
			t.Errorf("%s: the post-scaffold text still points at examples/:\n%s", tmpl, out)
		}
		for _, page := range []string{docsQuickstart, templateGuide[tmpl].path} {
			if !strings.Contains(out, page) {
				t.Errorf("%s: the post-scaffold text does not point at %s:\n%s", tmpl, page, out)
			}
		}
	}
}

// The framework version the scaffold pins and the one the catalog records
// for the root package are the same release: release-please rewrites both
// in the release PR, one through the marker in new.go, the other through
// modules.json.
func TestScaffoldPinAndCatalogAgreeOnTheFramework(t *testing.T) {
	if got := "v" + knownproviders.ReleasedVersions()["."]; got != defaultPinnedFrameworkVersion {
		t.Errorf("modules.json records the framework at %s, new.go pins %s", got, defaultPinnedFrameworkVersion)
	}
}

// recipeSummaryFirst is the first thing a recipe writes, as a dry run names
// it: the chain call, else the first configuration key.
func recipeSummaryFirst(r *knownproviders.Recipe) string {
	if len(r.Chain) > 0 {
		return "." + r.Chain[0]
	}
	if keys := yamlTopLevelKeys(r.Config); len(keys) > 0 {
		return keys[0]
	}
	return ""
}
