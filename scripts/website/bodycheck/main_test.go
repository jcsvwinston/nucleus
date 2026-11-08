package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFile_MDXMetaAndVersion(t *testing.T) {
	v := testVerifier()
	page := filepath.Join(t.TempDir(), "p.md")
	content := "# Title\n\n" +
		"Requires Go 1.24+ to build.\n\n" + // version mismatch (1.24 != 1.26) -> hard
		"```go file=<rootDir>/x.go\n" + // MDX/Docusaurus meta-string on the fence
		"import \"github.com/jcsvwinston/nucleus/pkg/auth\"\n" +
		"x := auth.VerifyPassword(\"x\")\n" + // not in baseline -> hard (proves MDX block is parsed)
		"```\n"
	if err := os.WriteFile(page, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	hard, _ := v.checkFile(page)
	if len(hard) != 2 {
		t.Fatalf("expected 2 hard findings (Go-version + symbol in an MDX-meta go block), got %d: %v", len(hard), hard)
	}
}

func testVerifier() *verifier {
	return &verifier{
		goMinor:   "1.26",
		goVersion: "1.26.4",
		pkgSyms: map[string]map[string]bool{
			"auth": {"NewJWTManager": true, "Claims": true},
		},
		cfgKeys: map[string]bool{"port": true, "database": true, "url": true, "modules": true},
		cfgTop:  map[string]bool{"database": true, "modules": true},
	}
}

func TestCheckGoVersionLine(t *testing.T) {
	v := testVerifier()
	cases := []struct {
		line    string
		wantHit bool
	}{
		{"This framework requires Go 1.24+ to build.", true}, // the 2026-05-24 P0 class
		{"Minimum supported Go: `1.26` (go 1.26.4 in go.mod)", false},
		{"Requires Go 1.26 or later.", false},
		{"Built with go 1.25 historically.", true},
		{"No version here.", false},
		{"Use **1.26+** going forward.", false},
		{"Legacy note: **1.21+** required generics.", true},
	}
	for _, tc := range cases {
		got := v.checkGoVersionLine("p.md", 1, tc.line)
		if (len(got) > 0) != tc.wantHit {
			t.Errorf("checkGoVersionLine(%q) = %v, wantHit=%v", tc.line, got, tc.wantHit)
		}
	}
}

func TestCheckGoBlock(t *testing.T) {
	v := testVerifier()

	// Imports the Nucleus auth package; one real symbol, one non-existent.
	block := []string{
		`import "github.com/jcsvwinston/nucleus/pkg/auth"`,
		`mgr := auth.NewJWTManager("secret", time.Hour)`,
		`ok := auth.VerifyPassword("x")`, // not in baseline -> must flag (the auth.VerifyPassword P0)
	}
	got := v.checkGoBlock("p.md", block, 10)
	if len(got) != 1 || !strings.Contains(got[0], "auth.VerifyPassword") {
		t.Fatalf("expected exactly one finding for auth.VerifyPassword, got %v", got)
	}

	// Stdlib errors.Is must NOT be flagged (short-name collides with pkg/errors,
	// but the block does not import the Nucleus package).
	stdlib := []string{
		`import "errors"`,
		`if errors.Is(err, target) { return }`,
	}
	if got := v.checkGoBlock("p.md", stdlib, 1); len(got) != 0 {
		t.Errorf("stdlib errors.Is should not be flagged, got %v", got)
	}

	// A Nucleus symbol referenced WITHOUT importing the package is skipped
	// (conservative; errs toward false-negatives).
	noImport := []string{`x := auth.VerifyPassword("x")`}
	if got := v.checkGoBlock("p.md", noImport, 1); len(got) != 0 {
		t.Errorf("unimported pkg ref should be skipped, got %v", got)
	}

	// Aliased import is honored.
	aliased := []string{
		`import myauth "github.com/jcsvwinston/nucleus/pkg/auth"`,
		`_ = myauth.Nope(1)`,
	}
	if got := v.checkGoBlock("p.md", aliased, 1); len(got) != 1 {
		t.Errorf("aliased import: expected one finding for myauth.Nope, got %v", got)
	}
}

// A Quark model block (pk:"true" / quark:"…" tags, or the Quark import)
// speaks Quark's db grammar: column names pass, pkg/model directives in it
// are the falsehood; a block without those markers keeps the pkg/model
// check.
func TestCheckGoBlock_QuarkModelTags(t *testing.T) {
	v := testVerifier()
	quark := []string{
		`type Gear struct {`,
		"\tID    uint   `db:\"id\" pk:\"true\"`",
		"\tName  string `db:\"name\" quark:\"not_null\"`",
		"\tPrice string `db:\"price,precision=18,scale=4\"`",
		"\tNote  string `db:\"-\"`",
		`}`,
	}
	if got := v.checkGoBlock("p.md", quark, 1); len(got) != 0 {
		t.Errorf("Quark column tags must pass in a Quark block, got %v", got)
	}
	mixed := []string{
		"\tID   uint   `db:\"id\" pk:\"true\"`",
		"\tName string `db:\"column:name;required\"`",
	}
	if got := v.checkGoBlock("p.md", mixed, 1); len(got) != 1 || !strings.Contains(got[0], "column:name") {
		t.Errorf("pkg/model directives in a Quark block must be flagged once, got %v", got)
	}
	plain := []string{"\tName string `db:\"name\"`"}
	if got := v.checkGoBlock("p.md", plain, 1); len(got) != 1 {
		t.Errorf("without Quark markers the pkg/model parser still rules, got %v", got)
	}
}

func TestCheckYamlBlock_AnchorOnly(t *testing.T) {
	v := testVerifier()
	// Anchored as nucleus config (top-level `database:` is a known section);
	// `boguskey` is not a registry segment -> advisory finding.
	cfg := []string{`database:`, `  url: "sqlite://x"`, `  boguskey: 1`}
	if got := v.checkYamlBlock("p.md", cfg); len(got) != 1 || !strings.Contains(got[0], "boguskey") {
		t.Errorf("expected one advisory finding for boguskey, got %v", got)
	}
	// Not anchored (arbitrary YAML, e.g. a CI workflow) -> ignored entirely.
	arbitrary := []string{`jobs:`, `  build:`, `    runs-on: ubuntu`}
	if got := v.checkYamlBlock("p.md", arbitrary); len(got) != 0 {
		t.Errorf("non-config yaml should be ignored, got %v", got)
	}
}

// Keys nested under `modules:` belong to the MODULE's own config struct,
// not to the framework: the registry lists what Nucleus itself defines, and
// a module's keys are namespaced under its name by design. Checking them
// against the registry produced a permanent false positive on the page that
// teaches module configuration — and an advisory that is always wrong is an
// advisory people learn to skip.
func TestCheckYamlBlock_ModuleKeysAreNotFrameworkKeys(t *testing.T) {
	v := testVerifier()

	block := []string{
		`modules:`,
		`  billing:`,
		`    stripe_key_env: STRIPE_SECRET_KEY`,
		`    invoice_due_days: 30`,
	}
	if got := v.checkYamlBlock("p.md", block); len(got) != 0 {
		t.Errorf("module-owned keys must not be judged against the framework registry, got %v", got)
	}

	// Leaving the `modules:` subtree restores the check: a bogus top-level
	// key after it is still the framework's business.
	mixed := []string{
		`modules:`,
		`  billing:`,
		`    invoice_due_days: 30`,
		`database:`,
		`  boguskey: 1`,
	}
	got := v.checkYamlBlock("p.md", mixed)
	if len(got) != 1 || !strings.Contains(got[0], "boguskey") {
		t.Errorf("expected exactly the boguskey finding once out of the modules subtree, got %v", got)
	}
}

// The registry documents three sections whose subtree belongs to a
// REGISTERED third party — `auth.<backend>.*`, `interceptors.<name>.*`,
// `storage.<provider>.*` — and the checker used to skip those rows
// altogether (the `*` segment did not parse), so the pages teaching LDAP,
// OIDC, SAML, Sentry and a third-party storage provider carried 39
// permanent advisories and `--strict` never ran clean. Below a name the
// registry does not know, the keys are the third party's own and are not
// judged; a typed section the registry does know keeps its keys checked.
func TestIndexRegistry_PatternRows(t *testing.T) {
	v := &verifier{cfgKeys: map[string]bool{}, cfgTop: map[string]bool{}}
	v.indexRegistry("| `auth.<backend>.*` | — |\n| `interceptors.<name>.*` | — |\n" +
		"| `storage.<provider>.*` | — |\n| `storage.s3.bucket` | — |\n| `databases.<alias>.url` | — |\n")
	for _, key := range []string{"auth", "interceptors", "storage", "s3", "bucket", "databases", "url"} {
		if !v.cfgKeys[key] {
			t.Errorf("%q must be a registry key", key)
		}
	}
	if v.cfgKeys["*"] || v.cfgKeys["<backend>"] {
		t.Errorf("neither the wildcard nor a placeholder is a key: %v", v.cfgKeys)
	}
	for _, top := range []string{"auth", "interceptors", "storage"} {
		if !v.cfgTop[top] || !v.cfgUserKeyed[top] || !v.cfgOpenSubtree[top] {
			t.Errorf("%q must be a top-level, user-keyed section with an open subtree (top=%v userKeyed=%v open=%v)",
				top, v.cfgTop[top], v.cfgUserKeyed[top], v.cfgOpenSubtree[top])
		}
	}
	if !v.cfgUserKeyed["databases"] || v.cfgOpenSubtree["databases"] {
		t.Errorf("databases.<alias>.url is user-keyed and NOT an open subtree: its keys below the alias are the framework's")
	}
}

func TestCheckYamlBlock_OpenSubtreeBelongsToTheRegisteredThirdParty(t *testing.T) {
	v := &verifier{cfgKeys: map[string]bool{}, cfgTop: map[string]bool{}}
	v.indexRegistry("`auth.<backend>.*` `interceptors.<name>.*` `storage.<provider>.*` `storage.s3.bucket` `databases.<alias>.url`")

	for _, block := range [][]string{
		{`auth:`, `  ldap:`, `    base_dn: dc=example,dc=com`, `    bind_dn: cn=reader`},
		{`interceptors:`, `  sentry:`, `    dsn: https://example`, `    sample_rate: 0.2`},
		{`storage:`, `  ceph:`, `    pool: data`},
	} {
		if got := v.checkYamlBlock("p.md", block); len(got) != 0 {
			t.Errorf("keys below a registered third party's section are its own, not the framework's; got %v for %v", got, block)
		}
	}
	// A typed section the registry knows keeps its keys checked.
	typed := []string{`storage:`, `  s3:`, `    bucket: assets`, `    bogus: 1`}
	if got := v.checkYamlBlock("p.md", typed); len(got) != 1 || !strings.Contains(got[0], "bogus") {
		t.Errorf("storage.s3.* is typed: want exactly the bogus key flagged, got %v", got)
	}
	// A user-keyed section without `*` is one level only: the keys below the
	// alias are the framework's again.
	alias := []string{`databases:`, `  primary:`, `    url: sqlite://x`, `    bogus: 1`}
	if got := v.checkYamlBlock("p.md", alias); len(got) != 1 || !strings.Contains(got[0], "bogus") {
		t.Errorf("databases.<alias>.* keys stay checked: want the bogus key flagged, got %v", got)
	}
}
