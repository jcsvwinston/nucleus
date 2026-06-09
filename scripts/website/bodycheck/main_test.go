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
		cfgKeys: map[string]bool{"port": true, "database": true, "url": true},
		cfgTop:  map[string]bool{"database": true},
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
