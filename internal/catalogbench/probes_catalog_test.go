// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"bytes"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The catalog probes measure the command and its table: what `nucleus add`
// accepts, what it says, what it writes and at which version, and whether
// the framework's own refusals send a person back to it.

// hintCase is a selection the starter refuses when the entry is missing.
type hintCase struct {
	name   string // the catalog name
	target string // the module (or package) the refusal should name
	config string // the selection, appended to the starter's nucleus.yml
}

// hintCases are the selections whose refusal should name `nucleus add`: the
// module-backed entries, a database driver, and the core entries whose
// provider registers by blank import (oidc, and the redis cache backend).
func hintCases(e *env) []hintCase {
	cases := []hintCase{{
		name:   "postgres",
		target: "github.com/jcsvwinston/nucleus/drivers/postgres",
		config: "databases:\n  default:\n    url: postgres://bench:bench@127.0.0.1:1/bench?sslmode=disable\n",
	}}
	for _, m := range moduleEntries {
		cases = append(cases, hintCase{name: m.name, target: m.module, config: m.config(e)})
	}
	cases = append(cases, hintCase{name: "oidc", target: "github.com/jcsvwinston/nucleus/pkg/auth/federated/oidc", config: oidcConfig})
	cases = append(cases, hintCase{name: "sentry", target: "github.com/jcsvwinston/nucleus/providers/errors-sentry", config: sentryConfig})
	cases = append(cases, hintCase{name: "redis-cache", target: redisCacheImport, config: "cache:\n  provider: redis\n  redis_url: redis://127.0.0.1:1/0\n"})
	return cases
}

var nucleusAddName = regexp.MustCompile("nucleus add ([a-z0-9:-]+)")

// CAT-01 — the refusal a person meets when an entry is selected and not
// installed names the `nucleus add` that installs it, and that command
// resolves to the module the refusal names. Measured on the real path: the
// starter, booted with each entry selected and not added.
func probeHintsNameTheFix(t *testing.T, e *env) verdict {
	base := e.scaffold(t)
	var good, total int
	for _, h := range hintCases(e) {
		total++
		config := starterConfig(t, base, h.config)
		if h.name == "postgres" {
			// The selection replaces the starter's database, so it is the
			// starter's file with the URL swapped, not a block appended.
			raw, _ := os.ReadFile(filepath.Join(base.dir, "nucleus.yml"))
			config = strings.Replace(string(raw), "url: sqlite://app.db", "url: postgres://bench:bench@127.0.0.1:1/bench?sslmode=disable", 1)
		}
		var env []string
		for _, m := range moduleEntries {
			if m.name == h.name {
				env = m.envFor(e)
			}
		}
		b := bootWith(t, base.bin, config, env, nil)
		if b.listening {
			t.Logf("%-10s ✗ the starter boots with %s selected and not installed: nothing tells the person", h.name, h.name)
			continue
		}
		refusal := b.output
		// The boot can print other `nucleus add` hints before the refusal
		// (the Prometheus exporter's, for one), so the probe keeps the one
		// that installs what this refusal names.
		named := nucleusAddName.FindAllStringSubmatch(refusal, -1)
		fix := ""
		for _, n := range named {
			r := e.dryRun(n[1])
			for _, target := range goGetTargets(r.stdout) {
				if moduleOf(target) == h.target {
					fix = n[1]
				}
			}
			if strings.Contains(r.stdout, h.target) {
				fix = n[1]
			}
			if fix != "" {
				break
			}
		}
		if fix != "" {
			good++
			t.Logf("%-10s ✓ the refusal names `nucleus add %s`, which installs %s", h.name, fix, h.target)
			continue
		}
		t.Logf("%-10s ✗ the refusal does not name a `nucleus add` that installs %s:\n%s", h.name, h.target, firstLines(lastLines(refusal, 8), 8))
	}
	t.Logf("%d of %d refusals send the person to the command that fixes them", good, total)
	switch {
	case good == total:
		return present
	case good > 0:
		return partial
	}
	return absent
}

// CAT-02 — `nucleus add --help` lists every name the command accepts.
func probeHelpListsEveryName(t *testing.T, e *env) verdict {
	help := e.cli("add", "--help")
	if help.code != 0 {
		t.Logf("nucleus add --help exited %d", help.code)
		return absent
	}
	var missing, listed []string
	for _, c := range e.catalog() {
		if strings.Contains(help.stdout, moduleOf(c.target)) {
			listed = append(listed, c.name)
			continue
		}
		missing = append(missing, c.name+" ("+moduleOf(c.target)+")")
	}
	t.Logf("listed by --help: %v", listed)
	if len(missing) == 0 {
		return present
	}
	t.Logf("accepted by nucleus add and absent from --help: %v", missing)
	if len(listed) == 0 {
		return absent
	}
	return partial
}

// releaseManifest is what this checkout's release-please manifest records
// for each module it publishes.
func releaseManifest(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".release-please-manifest.json"))
	if err != nil {
		t.Fatalf("read the release manifest: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse the release manifest: %v", err)
	}
	return m
}

// setVersion is the version the certified set ships for a module of this
// repository: what the release manifest of this checkout records for it —
// the release that carries this CLI publishes exactly that.
func setVersion(manifest map[string]string, module string) (string, bool) {
	rel := strings.TrimPrefix(module, "github.com/jcsvwinston/nucleus")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		rel = "."
	}
	v, ok := manifest[rel]
	if !ok {
		return "", false
	}
	return "v" + v, true
}

// CAT-03 — an entry installs the version of the certified set, not whatever
// the proxy calls latest: the version this checkout's release manifest
// records for the module. The probe reads it from the release manifest, not
// from the CLI's copy of it (internal/knownproviders/modules.json), so a copy
// that drifted measures as pinned to something else.
func probeAddPinsTheSet(t *testing.T, e *env) verdict {
	manifest := releaseManifest(t)
	var bare, matching, other []string
	for _, c := range e.catalog() {
		mod := moduleOf(c.target)
		want, known := setVersion(manifest, mod)
		switch {
		case !strings.Contains(c.target, "@"):
			bare = append(bare, c.name)
		case known && c.target == mod+"@"+want:
			matching = append(matching, c.name)
		default:
			other = append(other, c.name+" → "+c.target+" (set: "+want+")")
		}
	}
	t.Logf("go get with no version (resolves @latest): %v", bare)
	t.Logf("pinned to the set: %v", matching)
	if len(other) > 0 {
		t.Logf("pinned to something else: %v", other)
	}
	switch {
	case len(bare) == 0 && len(other) == 0 && len(matching) > 0:
		return present
	case len(matching) > 0 || len(other) > 0:
		return partial
	}
	return absent
}

// CAT-04 — `nucleus new` fetches what it scaffolds at the set's versions.
// The framework itself is pinned (the CLI's own version, rewritten on every
// release); the driver and the --with modules are fetched with no version.
func probeNewPinsWhatItFetches(t *testing.T, e *env) verdict {
	out := t.TempDir()
	r := e.cli("new", "pinned", "--out", out, "--template", "api", "--with", "quark", "--module", "example.com/pinned", "--offline")
	if r.code != 0 {
		t.Logf("nucleus new exited %d: %s", r.code, r.all())
		return absent
	}
	goMod, _ := os.ReadFile(filepath.Join(out, "pinned", "go.mod"))
	framework := regexp.MustCompile(`(?m)^require github\.com/jcsvwinston/nucleus (v\d+\.\d+\.\d+)$`).FindStringSubmatch(string(goMod))
	targets := goGetTargets(r.stdout)
	var bare []string
	for _, tg := range targets {
		if !strings.Contains(tg, "@") {
			bare = append(bare, tg)
		}
	}
	if framework != nil {
		t.Logf("the framework is pinned in go.mod: %s", framework[1])
	} else {
		t.Logf("the framework is not pinned in go.mod:\n%s", goMod)
	}
	t.Logf("the scaffold's go get list: %v", targets)
	switch {
	case framework != nil && len(bare) == 0 && len(targets) > 0:
		return present
	case framework != nil || len(bare) < len(targets):
		t.Logf("fetched with no version: %v", bare)
		return partial
	}
	return absent
}

// wiringEntries are the core entries the arc names whose wiring is more than
// an import: a Mount, an option, a configuration block, a route.
var wiringEntries = []string{"oidc", "apikeys", "sql-queue", "accounts", "websockets"}

// funcMainOnward is the part of main.go after `func main`: the composition
// root. A blank import changes what precedes it, a recipe what follows.
func funcMainOnward(src string) string {
	if i := strings.Index(src, "func main()"); i >= 0 {
		return src[i:]
	}
	return ""
}

// CAT-05 — an entry can carry more than `go get` and a blank import. The
// probe runs the real `nucleus add` of each core entry the command accepts
// on a copy of the starter and reads what it wrote beyond the import: a call
// in the nucleus.New() chain, a block in nucleus.yml. Present when every
// entry the command accepts wrote its wiring and the catalog carries both
// kinds; a name the command still refuses is left to its own entry control
// (EN-04, EN-07), which is where "the catalog cannot install it" is
// measured.
func probeEntryWritesMoreThanAnImport(t *testing.T, e *env) verdict {
	base := e.scaffold(t)
	baseMain, _ := os.ReadFile(filepath.Join(base.dir, "main.go"))
	baseConfig, _ := os.ReadFile(filepath.Join(base.dir, "nucleus.yml"))
	var chain, config, importOnly, refused []string
	for _, name := range wiringEntries {
		if r := e.dryRun(name); r.code != 0 {
			refused = append(refused, name)
			continue
		}
		v := e.added(t, name)
		if v.add.code != 0 {
			t.Logf("nucleus add %s exited %d: %s", name, v.add.code, firstLines(v.add.all(), 4))
			importOnly = append(importOnly, name)
			continue
		}
		main, _ := os.ReadFile(filepath.Join(v.dir, "main.go"))
		yml, _ := os.ReadFile(filepath.Join(v.dir, "nucleus.yml"))
		wroteChain := funcMainOnward(string(main)) != funcMainOnward(string(baseMain))
		wroteConfig := !bytes.Equal(yml, baseConfig)
		if wroteChain {
			chain = append(chain, name)
		}
		if wroteConfig {
			config = append(config, name)
		}
		if !wroteChain && !wroteConfig {
			importOnly = append(importOnly, name)
		}
		t.Logf("nucleus add %s wrote: chain call %v, configuration block %v", name, wroteChain, wroteConfig)
	}
	t.Logf("wrote a call into the nucleus.New() chain: %v", chain)
	t.Logf("wrote a block into nucleus.yml: %v", config)
	if len(importOnly) > 0 {
		t.Logf("accepted, and wrote nothing beyond the import: %v", importOnly)
	}
	if len(refused) > 0 {
		t.Logf("refused as unknown names, measured by their entry controls: %v", refused)
	}
	switch {
	case len(importOnly) == 0 && len(chain) > 0 && len(config) > 0:
		return present
	case len(chain) > 0 || len(config) > 0:
		return partial
	}
	return absent
}

// CAT-06 — adding an entry that is already there changes nothing: a module
// entry (the fetch and the import) and an entry with a recipe (a chain call
// and a configuration block).
func probeReAddIsNoOp(t *testing.T, e *env) verdict {
	var clean, changed []string
	for _, name := range []string{"prometheus", "oidc"} {
		v := e.added(t, name)
		if v.add.code != 0 {
			t.Logf("the first nucleus add %s failed: %s", name, v.add.all())
			return absent
		}
		dir := t.TempDir()
		must(t, copyProject(v.dir, dir))
		files := []string{"go.mod", "go.sum", "main.go", "nucleus.yml"}
		before := map[string][]byte{}
		for _, f := range files {
			before[f], _ = os.ReadFile(filepath.Join(dir, f))
		}
		again := e.cli("add", name, "--dir", dir)
		skipIfOffline(t, again.all())
		if again.code != 0 {
			t.Logf("the second nucleus add %s exited %d: %s", name, again.code, again.all())
			return absent
		}
		var diff []string
		for _, f := range files {
			after, _ := os.ReadFile(filepath.Join(dir, f))
			if !bytes.Equal(before[f], after) {
				diff = append(diff, f)
			}
		}
		t.Logf("second nucleus add %s said: %s", name, firstLines(again.stdout, 4))
		if len(diff) == 0 && strings.Contains(again.stdout, "already") {
			clean = append(clean, name)
		} else {
			changed = append(changed, fmt.Sprintf("%s (%v)", name, diff))
		}
	}
	main, _ := os.ReadFile(filepath.Join(e.added(t, "prometheus").dir, "main.go"))
	if n := strings.Count(string(main), `"github.com/jcsvwinston/nucleus/exporters/prometheus"`); n != 1 {
		t.Logf("the import appears %d times after one add", n)
		return absent
	}
	switch {
	case len(changed) == 0:
		return present
	case len(clean) > 0:
		t.Logf("changed on the second run: %v", changed)
		return partial
	}
	t.Logf("changed on the second run: %v", changed)
	return absent
}

// CAT-07 — a mistyped name gets the nearest entry suggested.
func probeUnknownNameSuggests(t *testing.T, e *env) verdict {
	r := e.dryRun("prometeus")
	text := strings.ToLower(r.all())
	t.Logf("nucleus add prometeus: %s", firstLines(r.all(), 4))
	switch {
	case r.code != 0 && strings.Contains(text, "did you mean") && strings.Contains(text, "prometheus"):
		return present
	case r.code != 0 && strings.Contains(text, "prometheus"):
		t.Log("the refusal lists the whole table and leaves the person to find the name in it")
		return partial
	}
	return absent
}

// configKeys is what a person has to write to select an entry after
// installing it.
var configKeys = map[string][]string{
	"s3":   {"storage.provider", "provider: s3"},
	"ldap": {"auth_backends"},
	"otlp": {"otlp_endpoint"},
}

// CAT-08 — after `nucleus add`, the person is told (or given) the
// configuration the entry reads. Without it, an installed entry does
// nothing until somebody finds the key in the reference.
func probeAddNamesTheConfiguration(t *testing.T, e *env) verdict {
	var told, silent []string
	names := make([]string, 0, len(configKeys))
	for n := range configKeys {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		v := e.added(t, name)
		dry := e.dryRun(name)
		yml, _ := os.ReadFile(filepath.Join(v.dir, "nucleus.yml"))
		said := v.add.all() + dry.all() + string(yml)
		hit := ""
		for _, k := range configKeys[name] {
			if strings.Contains(said, k) {
				hit = k
			}
		}
		if hit != "" {
			told = append(told, name+" ("+hit+")")
		} else {
			silent = append(silent, name)
		}
	}
	t.Logf("named the configuration: %v", told)
	t.Logf("installed and said nothing about configuration: %v (output, dry run and nucleus.yml read)", silent)
	switch {
	case len(silent) == 0:
		return present
	case len(told) > 0:
		return partial
	}
	return absent
}

// CAT-09 — the site's CLI reference lists every entry the command accepts.
func probeSiteListsTheCatalog(t *testing.T, e *env) verdict {
	page := filepath.Join(repoRoot(t), "website", "docs", "cli", "overview.md")
	raw, err := os.ReadFile(page)
	if err != nil {
		t.Logf("no CLI reference page: %v", err)
		return absent
	}
	row := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "`nucleus add ") {
			row = line
			break
		}
	}
	if row == "" {
		t.Log("the CLI reference has no row for nucleus add")
		return absent
	}
	var listed, missing []string
	for _, c := range e.catalog() {
		if strings.Contains(row, "`"+strings.TrimSuffix(c.name, ":")+"`") {
			listed = append(listed, c.name)
		} else {
			missing = append(missing, c.name)
		}
	}
	t.Logf("the reference row names: %v", listed)
	if len(missing) == 0 {
		return present
	}
	t.Logf("accepted by the command, absent from the reference: %v", missing)
	if len(listed) == 0 {
		return absent
	}
	return partial
}

// CAT-10 — one catalogue: what `nucleus add` installs and what `nucleus new
// --with` resolves are the same table, so a name one accepts the other
// accepts.
func probeOneCatalogue(t *testing.T, e *env) verdict {
	addTakesSuite := e.dryRun("quark").code == 0
	out := t.TempDir()
	newTakesEntry := e.cli("new", "one", "--out", out, "--template", "api", "--with", "s3", "--offline").code == 0
	t.Logf("nucleus add quark accepted: %v · nucleus new --with s3 accepted: %v", addTakesSuite, newTakesEntry)
	switch {
	case addTakesSuite && newTakesEntry:
		return present
	case addTakesSuite || newTakesEntry:
		return partial
	}
	t.Log("two tables: `--with` knows the suite's siblings (orbit, quark, the bridges), `add` knows the optional modules, and neither accepts the other's names")
	return absent
}

// foreignDeps are modules an application carries only because it added an
// entry. The starter adds the SQLite driver and nothing else.
var foreignDeps = []string{
	"github.com/jackc/pgx/v5",
	"github.com/go-sql-driver/mysql",
	"github.com/microsoft/go-mssqldb",
	"github.com/sijms/go-ora/v2",
	"github.com/aws/aws-sdk-go-v2/service/s3",
	"github.com/minio/minio-go/v7",
	"cloud.google.com/go/storage",
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob",
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace",
	"github.com/prometheus/client_golang",
	"github.com/go-ldap/ldap/v3",
	"github.com/getsentry/sentry-go",
}

// CAT-11 — an application links only the entries it added. Measured on the
// built starter's own build information: what is IN the binary.
func probeLinksOnlyWhatItAdded(t *testing.T, e *env) verdict {
	base := e.scaffold(t)
	info, err := buildinfo.ReadFile(base.bin)
	if err != nil {
		t.Fatalf("read the starter's build info: %v", err)
	}
	var linked []string
	own := false
	for _, d := range info.Deps {
		// The engine the starter did add: without it in the build
		// information, an empty foreign list would measure nothing.
		if d.Path == "modernc.org/sqlite" {
			own = true
		}
		for _, f := range foreignDeps {
			if d.Path == f || strings.HasPrefix(d.Path, f+"/") {
				linked = append(linked, d.Path)
			}
		}
	}
	if !own {
		t.Fatalf("the starter's build information does not list modernc.org/sqlite, the engine it added; nothing was measured")
	}
	sort.Strings(linked)
	st, _ := os.Stat(base.bin)
	if st != nil {
		t.Logf("the starter binary: %.1f MB", float64(st.Size())/1e6)
	}
	if len(linked) == 0 {
		return present
	}
	t.Logf("the starter added only the SQLite driver and links: %v", linked)
	if len(linked) < len(foreignDeps) {
		return partial
	}
	return absent
}

// lastLines keeps the tail of a boot's output, where the refusal is.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
