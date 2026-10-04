// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package knownproviders

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// The catalog is one table (ADR-034). `nucleus add`, its --help, `nucleus
// new --with` and the "not imported yet" refusals of the runtime read it, so
// a name one of them knows is a name all of them know — the tests in this
// package and in internal/cli fail the build when one of them drifts.
//
// Before it, there were two tables (the optional modules `add` installed and
// the suite products `new --with` resolved) and four places that listed a
// subset of the first one; `nucleus add aws-sm` worked and its help never
// said so.

// Ships says how an entry reaches an application, which decides what `nucleus
// add` does with it.
type Ships string

const (
	// AsModule is a module this repository publishes beside the framework
	// (drivers/*, exporters/*, providers/*). `nucleus add` fetches it at the
	// version released with the CLI and writes the blank import that
	// registers it.
	AsModule Ships = "module"
	// InCore is a package of the framework module itself. There is nothing
	// to fetch: the blank import is the wiring.
	InCore Ships = "core"
	// InSuite is a sibling product of the Quantum suite: the admin panel,
	// the ORM and the two bridges between them. Its version belongs to the
	// umbrella's certified set, which is written after this CLI is tagged
	// (Nucleus tags first in every train, and Orbit requires the Nucleus it
	// is cut against), so no release of this repository can carry it: the
	// module is fetched at the tag the proxy calls latest.
	InSuite Ships = "suite"
)

// Group is the section of the catalog an entry is listed under.
type Group string

// The groups, in listing order.
const (
	GroupDriver    Group = "database drivers"
	GroupExporter  Group = "telemetry exporters"
	GroupStorage   Group = "storage providers"
	GroupAuth      Group = "authentication backends"
	GroupSecrets   Group = "secrets resolvers"
	GroupFederated Group = "federated sign-in"
	GroupSuite     Group = "suite products"
)

// Groups returns the groups in listing order.
func Groups() []Group {
	return []Group{GroupDriver, GroupExporter, GroupStorage, GroupAuth, GroupSecrets, GroupFederated, GroupSuite}
}

// Entry is one row of the catalog.
type Entry struct {
	// Name is what a person types after `nucleus add` or `nucleus new
	// --with`.
	Name string
	// Aliases are the other spellings both commands accept: nobody types
	// "pgx" when they mean postgres, but some people do.
	Aliases []string
	// Ships says how the entry reaches the application: a module of this
	// repository, a package of the framework, or a suite product.
	Ships Ships
	// Group is the section it is listed under.
	Group Group
	// Kind is how an error sentence calls it, e.g. "authentication
	// backend".
	Kind string
	// Key is the name the runtime looks it up by: the database/sql driver
	// name ("pgx"), the secret-reference scheme ("aws-sm:"), the registered
	// provider name. The refusals of the runtime are keyed by it.
	Key string
	// Module is the module the entry lives in: the `go get` target of a
	// module or a suite product, the framework itself for a core entry.
	Module string
	// Import is the package `nucleus add` writes as a blank import; empty
	// when the entry registers nothing by import (the suite products are
	// wired by code, not by an import for side effects).
	Import string
	// Selects is the configuration that selects the entry once it is in the
	// build: installing a storage provider does nothing until
	// storage.provider names it. Empty for the suite products, which no
	// configuration key selects.
	Selects string
	// Wires is what connects the entry to the application.
	Wires string
	// Adds says what a suite product gives the project, for the usage text
	// of `nucleus new`.
	Adds string
	// DriverModule, when non-empty, is the pattern of a suite product's own
	// per-engine driver module (the ORM registers its error classifier
	// through it); the caller substitutes the engine directory.
	DriverModule string
	// RequiresConfig reports whether the entry is unusable without a
	// configuration subtree of its own — a directory client cannot invent
	// the address of the directory.
	RequiresConfig bool
	// Remote reports whether the entry depends on a system outside the
	// application, which is what makes a chain without a local fallback a
	// single point of failure.
	Remote bool
}

// Provider is the name the runtime's refusals have always used for an entry.
type Provider = Entry

// SuiteModule is the name `nucleus new --with` used for the suite rows.
type SuiteModule = Entry

// RepoModule is the module path of this repository; its sibling modules
// live under it.
const RepoModule = "github.com/jcsvwinston/nucleus"

// catalog is the whole table, in listing order. A name belongs here only if
// this project publishes it — the promise every refusal makes, `nucleus add`
// this and it works, is one we have to keep.
var catalog = []Entry{
	// ---- database drivers: keyed by the database/sql driver NAME that
	// pkg/db resolves a URL scheme to ("pgx" for postgres://, "sqlserver"
	// for both sqlserver:// and mssql://), because that is the name
	// sql.Open fails on, and the failure is where the guidance has to
	// appear. Two of them, sqlserver and oracle, used to sit behind build
	// tags; as modules they fail while compiling, and the error names the
	// fix (ADR-031).
	{
		Name: "postgres", Aliases: []string{"postgresql", "pg", "pgx"}, Ships: AsModule, Group: GroupDriver,
		Kind: "database driver", Key: "pgx", Module: RepoModule + "/drivers/postgres",
		Selects: "databases.default.url: postgres://user:password@host:5432/app",
		Wires:   "its blank import registers the pgx database/sql driver and its error classifier",
	},
	{
		Name: "mysql", Ships: AsModule, Group: GroupDriver,
		Kind: "database driver", Key: "mysql", Module: RepoModule + "/drivers/mysql",
		Selects: "databases.default.url: mysql://user:password@host:3306/app",
		Wires:   "its blank import registers the mysql database/sql driver and its error classifier",
	},
	{
		Name: "sqlite", Ships: AsModule, Group: GroupDriver,
		Kind: "database driver", Key: "sqlite", Module: RepoModule + "/drivers/sqlite",
		Selects: "databases.default.url: sqlite://app.db",
		Wires:   "its blank import registers the sqlite database/sql driver and its error classifier",
	},
	{
		Name: "sqlserver", Aliases: []string{"mssql"}, Ships: AsModule, Group: GroupDriver,
		Kind: "database driver", Key: "sqlserver", Module: RepoModule + "/drivers/mssql",
		Selects: "databases.default.url: sqlserver://user:password@host:1433?database=app",
		Wires:   "its blank import registers the sqlserver database/sql driver and its error classifier",
	},
	{
		Name: "oracle", Ships: AsModule, Group: GroupDriver,
		Kind: "database driver", Key: "oracle", Module: RepoModule + "/drivers/oracle",
		Selects: "databases.default.url: oracle://user:password@host:1521/service",
		Wires:   "its blank import registers the oracle database/sql driver and its error classifier",
	},

	// ---- telemetry exporters: the framework keeps the OpenTelemetry SDK
	// and only the exporters left, because they are where the weight is.
	{
		Name: "otlp", Ships: AsModule, Group: GroupExporter,
		Kind: "telemetry exporter", Key: "otlp", Module: RepoModule + "/exporters/otlp",
		Selects: "otlp_endpoint: http://collector:4318",
		Wires:   "its blank import registers the otlp exporter with pkg/observe",
	},
	{
		Name: "prometheus", Ships: AsModule, Group: GroupExporter,
		Kind: "telemetry exporter", Key: "prometheus", Module: RepoModule + "/exporters/prometheus",
		Selects: "metrics_path: /metrics (the default; an empty value turns the endpoint off)",
		Wires:   "its blank import registers the prometheus exporter with pkg/observe",
	},

	// ---- storage providers: the cloud SDKs weighed 42.6 MB of a 75.6 MB
	// hello-world (ADR-030).
	{
		Name: "s3", Ships: AsModule, Group: GroupStorage,
		Kind: "storage provider", Key: "s3", Module: RepoModule + "/providers/storage-s3",
		Selects:        "storage.provider: s3 (bucket, region and credentials under storage.s3)",
		Wires:          "its blank import registers the s3 provider with pkg/storage",
		RequiresConfig: true, Remote: true,
	},
	{
		Name: "gcs", Ships: AsModule, Group: GroupStorage,
		Kind: "storage provider", Key: "gcs", Module: RepoModule + "/providers/storage-gcs",
		Selects:        "storage.provider: gcs (the bucket under storage.gcs)",
		Wires:          "its blank import registers the gcs provider with pkg/storage",
		RequiresConfig: true, Remote: true,
	},
	{
		Name: "azure", Ships: AsModule, Group: GroupStorage,
		Kind: "storage provider", Key: "azure", Module: RepoModule + "/providers/storage-azure",
		Selects:        "storage.provider: azure (account and container under storage.azure)",
		Wires:          "its blank import registers the azure provider with pkg/storage",
		RequiresConfig: true, Remote: true,
	},

	// ---- authentication backends (ADR-024).
	{
		Name: "ldap", Ships: AsModule, Group: GroupAuth,
		Kind: "authentication backend", Key: "ldap", Module: RepoModule + "/providers/ldap",
		Selects:        "auth_backends: [ldap] (the directory under auth.ldap: url, base_dn)",
		Wires:          "its blank import registers the ldap backend with the authentication chain",
		RequiresConfig: true, Remote: true,
	},

	// ---- secrets resolvers: keyed by the reference SCHEME they own, which
	// has to end in ":" to prefix-match a reference.
	{
		Name: "aws-sm", Ships: AsModule, Group: GroupSecrets,
		Kind: "secrets resolver", Key: "aws-sm:", Module: RepoModule + "/providers/secrets-aws",
		Selects: "a key reference written aws-sm:<secret-id>[#json-key], e.g. jwt_keys[].secret_env: aws-sm:myapp/prod/jwt",
		Wires:   "its blank import registers the aws-sm: scheme with pkg/auth/secrets",
		Remote:  true,
	},

	// ---- federated sign-in: the provider lives in the framework module
	// and registers itself when its package is imported (ADR-028).
	{
		Name: "oidc", Ships: InCore, Group: GroupFederated,
		Kind: "federated sign-in provider", Key: "oidc", Module: RepoModule,
		Import:         RepoModule + "/pkg/auth/federated/oidc",
		Selects:        "auth_federated: [{name: corp, provider: oidc}] (issuer and client_id under auth.corp; public_base_url is required)",
		Wires:          "its blank import registers the oidc provider with the federated registry; the sign-in routes are the application's to mount",
		RequiresConfig: true, Remote: true,
	},

	// ---- suite products, in the order a scaffold resolves them: orbit and
	// quark are the two products, the bridges depend on both.
	{
		Name: "orbit", Ships: InSuite, Group: GroupSuite,
		Kind: "admin panel", Module: "github.com/jcsvwinston/orbit",
		Adds:  "the admin panel mounted under /admin with its own login gate, Data Studio and the live observability feed",
		Wires: "Mount(orbit.Module(orbit.Config{...})) in main.go — `nucleus new --with orbit` writes it in a new project",
	},
	{
		Name: "quark", Ships: InSuite, Group: GroupSuite,
		Kind: "ORM", Module: "github.com/jcsvwinston/quark",
		Adds:         "the Quark ORM (Active Record models, migrations from the model registry) for the application's domain",
		Wires:        "a module whose storage runs on Quark — `nucleus generate module <name> --data quark` writes one, with the import of Quark's driver module for the engine",
		DriverModule: "github.com/jcsvwinston/quark/drivers/%s",
	},
	{
		Name: "quarkbridge", Ships: InSuite, Group: GroupSuite,
		Kind: "observability bridge", Module: "github.com/jcsvwinston/orbit/quarkbridge",
		Adds:  "a Quark middleware that publishes every statement on the observability bus, so orbit's live feed shows the SQL correlated to the request",
		Wires: "quark.WithMiddleware(quarkbridge.New(rt.Observability())) on the Quark client a module opens",
	},
	{
		Name: "quarkdatasource", Ships: InSuite, Group: GroupSuite,
		Kind: "Data Studio adapter", Module: "github.com/jcsvwinston/orbit/quarkdatasource",
		Adds:  "the adapter that backs orbit's Data Studio with Quark models, so /admin browses and edits them",
		Wires: "quarkdatasource.New(client) and Register[Model] for each model, handed to orbit.Config",
	},
}

// modulesJSON is the version of every module this repository releases,
// keyed by its release-please package path ("." is the framework). It is
// kept in step by release-please itself: each package lists this file as an
// extra file of type json with the jsonpath of its own key, so the release
// PR that tags a module rewrites its line in the same commit — and a test
// compares the file with .release-please-manifest.json (ADR-034).
//
//go:embed modules.json
var modulesJSON []byte

// releasedVersions parses modules.json on first use. Every application
// links this package for its refusals, so the parse is lazy and never
// panics: a file that does not parse leaves the entries unpinned, and the
// test that compares it with the release manifest fails the build first.
var releasedVersions = sync.OnceValue(func() map[string]string {
	var m map[string]string
	if err := json.Unmarshal(modulesJSON, &m); err != nil {
		return map[string]string{}
	}
	return m
})

// ReleasedVersions returns a copy of modules.json: the release-please
// package path of every module this repository releases, and the version
// released with this CLI (without the "v").
func ReleasedVersions() map[string]string {
	src := releasedVersions()
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// ReleasePath is the release-please package an entry's module is released
// by ("drivers/postgres"), or "" for an entry this repository does not
// release on its own.
func (e Entry) ReleasePath() string {
	if e.Ships != AsModule {
		return ""
	}
	return strings.TrimPrefix(e.Module, RepoModule+"/")
}

// Version is the version of the entry's module released with this CLI
// ("v0.1.7"), or "" when the entry is not pinned: a core entry has nothing
// to fetch, and a suite product's version is the umbrella's (see InSuite).
func (e Entry) Version() string {
	path := e.ReleasePath()
	if path == "" {
		return ""
	}
	if v, ok := releasedVersions()[path]; ok && v != "" {
		return "v" + v
	}
	return ""
}

// Target is what `go get` is given for the entry: module@version for a
// module of this repository, the bare module for a suite product, and ""
// for a core entry, which has nothing to fetch.
func (e Entry) Target() string {
	switch e.Ships {
	case InCore:
		return ""
	case AsModule:
		if v := e.Version(); v != "" {
			return e.Module + "@" + v
		}
	}
	return e.Module
}

// ImportPath is the package written as the entry's blank import: Import
// when set, the module itself for a module of this repository, "" for a
// suite product.
func (e Entry) ImportPath() string {
	if e.Import != "" {
		return e.Import
	}
	if e.Ships == AsModule {
		return e.Module
	}
	return ""
}

// Entries returns the catalog in listing order.
func Entries() []Entry {
	out := make([]Entry, len(catalog))
	copy(out, catalog)
	return out
}

// Names returns every catalog name (aliases left out), in listing order.
func Names() []string {
	names := make([]string, 0, len(catalog))
	for _, e := range catalog {
		names = append(names, e.Name)
	}
	return names
}

// normalize is how a typed name is compared: case and surrounding space do
// not matter, and a secret scheme may be typed with its colon.
func normalize(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ":")
}

// Lookup resolves a name the way a person types it: the catalog name or an
// alias, in any case, a scheme with or without its trailing colon.
func Lookup(name string) (Entry, bool) {
	n := normalize(name)
	if n == "" {
		return Entry{}, false
	}
	for _, e := range catalog {
		if e.Name == n {
			return e, true
		}
		for _, a := range e.Aliases {
			if a == n {
				return e, true
			}
		}
	}
	return Entry{}, false
}

// Suggest returns the catalog name closest to a name that did not resolve,
// or "" when nothing is close enough to be a typo of it. A name a person
// typed is closest to an entry when few edits separate them (prometeus →
// prometheus), or when it is the start of exactly one name (prom →
// prometheus).
func Suggest(name string) string {
	n := normalize(name)
	if n == "" {
		return ""
	}
	best, bestDist := "", -1
	var prefixed []string
	for _, e := range catalog {
		for _, candidate := range append([]string{e.Name}, e.Aliases...) {
			d := editDistance(n, candidate)
			if bestDist < 0 || d < bestDist {
				best, bestDist = e.Name, d
			}
			if len(n) >= 3 && strings.HasPrefix(candidate, n) && !hasString(prefixed, e.Name) {
				prefixed = append(prefixed, e.Name)
			}
		}
	}
	shorter := len(n)
	if len(best) < shorter {
		shorter = len(best)
	}
	limit := shorter / 3
	if limit < 1 {
		limit = 1
	}
	if bestDist >= 0 && bestDist <= limit {
		return best
	}
	if len(prefixed) == 1 {
		return prefixed[0]
	}
	return ""
}

func hasString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// editDistance is the optimal-string-alignment distance: insertions,
// deletions, substitutions and the transposition of two adjacent letters
// each count one — "postgers" is one edit from "postgres", not two.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// byKey finds the entry of a group the runtime knows by key.
func byKey(g Group, key string) (Entry, bool) {
	for _, e := range catalog {
		if e.Group == g && e.Key == key {
			return e, true
		}
	}
	return Entry{}, false
}

// keysOf returns the keys of a group, sorted so that an error message lists
// them the same way every time.
func keysOf(g Group) []string {
	var keys []string
	for _, e := range catalog {
		if e.Group == g {
			keys = append(keys, e.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

// AddHint is the line that installs the entry, for a refusal to print.
func (e Entry) AddHint() string {
	return "nucleus add " + e.Name
}

// InstallHint is the recipe that turns "unknown backend" into something an
// operator can act on without leaving the terminal: the command that does
// it, and the two steps it stands for.
func (e Entry) InstallHint() string {
	var b strings.Builder
	b.WriteString("\t\t" + e.AddHint() + "\n\n")
	imp := e.ImportPath()
	switch {
	case e.Ships == InCore:
		b.WriteString("\tor import it for its side effect yourself — it is part of the framework, there is nothing to fetch:\n\n")
		b.WriteString("\t\timport _ \"" + imp + "\"")
	default:
		b.WriteString("\tor by hand: fetch it, and import it for its side effect, the way database/sql drivers are wired:\n\n")
		b.WriteString("\t\tgo get " + e.Target() + "\n")
		if imp != "" {
			b.WriteString("\t\timport _ \"" + imp + "\"")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
