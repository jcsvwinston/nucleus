// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The versions the CLI pins are the ones release-please tags. modules.json is
// the CLI's copy of .release-please-manifest.json, rewritten key by key by
// the same release PR that tags each module; a hand edit, or a release PR
// that rewrote one and not the other, shows up here before it ships.
func TestModulesJSONIsTheReleaseManifest(t *testing.T) {
	manifest := readJSON[map[string]string](t, ".release-please-manifest.json")
	got := ReleasedVersions()
	var diffs []string
	for path, want := range manifest {
		if have, ok := got[path]; !ok {
			diffs = append(diffs, path+": released as "+want+", missing from modules.json")
		} else if have != want {
			diffs = append(diffs, path+": released as "+want+", modules.json says "+have)
		}
	}
	for path, have := range got {
		if _, ok := manifest[path]; !ok {
			diffs = append(diffs, path+": in modules.json ("+have+"), not a release-please package")
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Fatalf("internal/knownproviders/modules.json and .release-please-manifest.json disagree — `nucleus add` would pin versions nobody released:\n  %s\n\n"+
			"modules.json is a copy of the manifest that release-please keeps in step (an extra file of type json per package).\n"+
			"If the manifest moved in a rebase, copy it over modules.json; if a package is new, give it its extra file too.",
			strings.Join(diffs, "\n  "))
	}
}

const versionsFile = "internal/knownproviders/modules.json"

type releaseConfig struct {
	Packages map[string]struct {
		ExtraFiles []json.RawMessage `json:"extra-files"`
	} `json:"packages"`
}

type extraFile struct {
	Type     string `json:"type"`
	Path     string `json:"path"`
	JSONPath string `json:"jsonpath"`
}

// Every release-please package rewrites its own key of modules.json. The
// path of a package other than the root starts with "/": release-please
// resolves an extra file relative to the PACKAGE unless it starts with a
// slash, which makes it relative to the repository root (BaseStrategy.addPath
// in release-please 17.6.0, the version release-please-action v5.0.0
// bundles). Without the slash the update would target
// drivers/mssql/internal/knownproviders/modules.json, a file that does not
// exist, and the version would silently stay behind.
func TestEveryReleasePackageRewritesItsVersion(t *testing.T) {
	config := readJSON[releaseConfig](t, "release-please-config.json")
	if len(config.Packages) == 0 {
		t.Fatal("release-please-config.json lists no packages")
	}
	for path, pkg := range config.Packages {
		wantPath := "/" + versionsFile
		if path == "." {
			wantPath = versionsFile
		}
		wantJSONPath := "$['" + path + "']"
		var found []extraFile
		for _, raw := range pkg.ExtraFiles {
			var f extraFile
			if err := json.Unmarshal(raw, &f); err != nil {
				continue // a bare string is a generic file
			}
			if strings.TrimPrefix(f.Path, "/") == versionsFile {
				found = append(found, f)
			}
		}
		switch {
		case len(found) == 0:
			t.Errorf("package %q has no extra file for %s: its release would tag the module and leave the version `nucleus add` pins behind.\n"+
				"Add {\"type\": \"json\", \"path\": %q, \"jsonpath\": %q}", path, versionsFile, wantPath, wantJSONPath)
		case len(found) > 1:
			t.Errorf("package %q lists %s %d times", path, versionsFile, len(found))
		default:
			f := found[0]
			if f.Type != "json" || f.Path != wantPath || f.JSONPath != wantJSONPath {
				t.Errorf("package %q rewrites %s as %+v, want type json, path %q, jsonpath %q", path, versionsFile, f, wantPath, wantJSONPath)
			}
		}
	}
}

// One release-please package per module entry, one module entry per package:
// a module this repository releases and the catalog does not name is a module
// `nucleus add` cannot install, and an entry with no package is a version the
// CLI cannot know.
func TestEveryModuleEntryIsAReleasedModule(t *testing.T) {
	versions := ReleasedVersions()
	owner := map[string]string{}
	for _, e := range Entries() {
		if e.Ships != AsModule {
			if e.ReleasePath() != "" || e.Version() != "" {
				t.Errorf("%s ships %s and must not carry a release path or a version", e.Name, e.Ships)
			}
			continue
		}
		path := e.ReleasePath()
		if _, ok := versions[path]; !ok {
			t.Errorf("%s lives in %s, which is not a release-please package", e.Name, path)
		}
		if prev, dup := owner[path]; dup {
			t.Errorf("%s and %s both claim %s", prev, e.Name, path)
		}
		owner[path] = e.Name
		if e.Version() == "" || e.Target() != e.Module+"@"+e.Version() {
			t.Errorf("%s is not pinned: Target() = %q", e.Name, e.Target())
		}
		// The module is real: its go.mod is in this repository and names it.
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(path), "go.mod"))
		if err != nil {
			t.Errorf("%s: %v", e.Name, err)
			continue
		}
		first, _, _ := strings.Cut(string(raw), "\n")
		if got := strings.TrimSpace(strings.TrimPrefix(first, "module")); got != e.Module {
			t.Errorf("%s points at %s, and %s/go.mod declares %s", e.Name, e.Module, path, got)
		}
	}
	for path := range versions {
		if path == "." {
			continue
		}
		if _, ok := owner[path]; !ok {
			t.Errorf("release-please package %s has no catalog entry: `nucleus add` cannot install it", path)
		}
	}
}

// Names and aliases are one namespace: a spelling that resolves to two
// entries would install whichever came first.
func TestEveryNameResolvesToOneEntry(t *testing.T) {
	seen := map[string]string{}
	for _, e := range Entries() {
		for _, n := range append([]string{e.Name}, e.Aliases...) {
			if n != normalize(n) {
				t.Errorf("%s: %q is not written the way Lookup compares it (%q)", e.Name, n, normalize(n))
			}
			if prev, dup := seen[n]; dup {
				t.Errorf("%q names both %s and %s", n, prev, e.Name)
			}
			seen[n] = e.Name
			got, ok := Lookup(strings.ToUpper(n) + " ")
			if !ok || got.Name != e.Name {
				t.Errorf("Lookup(%q) = %q, %v; want %s", n, got.Name, ok, e.Name)
			}
		}
	}
	if e, ok := Lookup("aws-sm:"); !ok || e.Name != "aws-sm" {
		t.Errorf("a scheme typed with its colon must resolve, got %q %v", e.Name, ok)
	}
	if _, ok := Lookup("mongodb"); ok {
		t.Error("a module this project does not publish must not resolve: the promise is that `go get` works")
	}
}

// Every entry says what it is, what wires it and what selects it — the three
// things a person needs after `nucleus add` and that the command prints.
func TestEveryEntryCanBeActedOn(t *testing.T) {
	root := repoRoot(t)
	for _, e := range Entries() {
		if e.Name == "" || e.Kind == "" || e.Module == "" || e.Wires == "" || e.Group == "" {
			t.Errorf("%+v: name, kind, module, group and wires are required", e)
		}
		switch e.Ships {
		case AsModule:
			if !strings.HasPrefix(e.Module, RepoModule+"/") || e.Key == "" || e.Selects == "" || e.ImportPath() == "" {
				t.Errorf("%s: a module entry lives under %s and has a key, a selecting configuration and an import", e.Name, RepoModule)
			}
		case InCore:
			if e.Module != RepoModule || e.Key == "" || e.Selects == "" || !strings.HasPrefix(e.Import, RepoModule+"/") {
				t.Errorf("%s: a core entry is a package of the framework module, with a key and a selecting configuration", e.Name)
			}
			if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(e.Import, RepoModule+"/")))); err != nil || !st.IsDir() {
				t.Errorf("%s imports %s, which is not a package of this repository", e.Name, e.Import)
			}
			if e.Target() != "" {
				t.Errorf("%s is in the core and has nothing to fetch, Target() = %q", e.Name, e.Target())
			}
		case InSuite:
			if strings.HasPrefix(e.Module, RepoModule) || e.Adds == "" || e.Selects != "" {
				t.Errorf("%s: a suite product lives outside this repository, says what it adds, and no configuration key selects it", e.Name)
			}
			if e.Target() != e.Module {
				t.Errorf("%s: a suite product is fetched as the bare module (its version is the umbrella's), Target() = %q", e.Name, e.Target())
			}
		default:
			t.Errorf("%s ships %q", e.Name, e.Ships)
		}
		groupKnown := false
		for _, g := range Groups() {
			groupKnown = groupKnown || g == e.Group
		}
		if !groupKnown {
			t.Errorf("%s is in group %q, which Groups() does not list", e.Name, e.Group)
		}
		hint := e.InstallHint()
		if !strings.Contains(hint, "nucleus add "+e.Name) {
			t.Errorf("%s: the install hint does not name the command:\n%s", e.Name, hint)
		}
		if imp := e.ImportPath(); imp != "" && !strings.Contains(hint, `import _ "`+imp+`"`) {
			t.Errorf("%s: the install hint does not name the import:\n%s", e.Name, hint)
		}
		if tg := e.Target(); tg != "" && !strings.Contains(hint, "go get "+tg) {
			t.Errorf("%s: the install hint does not fetch %s:\n%s", e.Name, tg, hint)
		}
	}
}

func TestSuggestFindsTheTypo(t *testing.T) {
	for typed, want := range map[string]string{
		"prometeus":  "prometheus",
		"postgers":   "postgres",
		"postgress":  "postgres",
		"Promethues": "prometheus",
		"prom":       "prometheus",
		"aws-s":      "aws-sm",
		"orbt":       "orbit",
		"quarkbrige": "quarkbridge",
		"mongodb":    "",
		"redis":      "",
		"kafka":      "",
	} {
		if got := Suggest(typed); got != want {
			t.Errorf("Suggest(%q) = %q, want %q", typed, got, want)
		}
	}
}

func readJSON[T any](t *testing.T, rel string) T {
	t.Helper()
	var v T
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	return v
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil && strings.HasPrefix(string(b), "module "+RepoModule+"\n") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}
