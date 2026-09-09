// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// `nucleus new --template suite` and `--with`: the scaffold that wires the
// suite together (Nucleus + Quark + Orbit, both bridges) and the flag that
// fetches sibling modules for the other templates. The rendering and the
// flag sequence are proven offline here; the only boot of the suite
// scaffold against real siblings in this repository is the checkout-gated
// test at the end (the umbrella's workspace lane is the one CI runs).
package cli

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// suiteScaffoldFiles is the file list of the suite template, sorted the
// way `find . -type f | sort` prints it: the freeze the docs page and the
// showcase example are checked against.
var suiteScaffoldFiles = []string{
	".dockerignore",
	".gitignore",
	"Dockerfile",
	"README.md",
	"go.mod",
	"main.go",
	"migrations/.gitkeep",
	"nucleus.yml",
	"rbac_policy.csv",
	"shop/models.go",
	"shop/module.go",
	"shop/module_test.go",
}

// stubScaffoldNetworkWithGoMod records the network sequence like
// stubScaffoldNetwork and, unlike it, edits go.mod the way the real
// commands do: `go get` appends a require for the module, `go mod tidy`
// drops every require no Go file in the project imports (the module or a
// package under it). It is what proves that a sibling nothing imports
// survives the scaffold — with a recorder alone, the sequence
// "get, get, tidy" and "get, tidy, get" look equally fine.
func stubScaffoldNetworkWithGoMod(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	const marker = " v0.0.0-stub // stubScaffoldNetworkWithGoMod"
	prevGet, prevTidy := goGet, goModTidy
	goGet = func(root, module string, _, _ io.Writer) error {
		calls = append(calls, "go get "+module+" in "+filepath.Base(root))
		goModPath := filepath.Join(root, "go.mod")
		goMod, err := os.ReadFile(goModPath)
		if err != nil {
			return err
		}
		if strings.Contains(string(goMod), "require "+module+" ") {
			return nil
		}
		return os.WriteFile(goModPath, append(goMod, []byte("require "+module+marker+"\n")...), 0o644)
	}
	goModTidy = func(root string, _, _ io.Writer) error {
		calls = append(calls, "go mod tidy in "+filepath.Base(root))
		imports := map[string]bool{}
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
				return err
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range parsed.Imports {
				imports[strings.Trim(imp.Path.Value, `"`)] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		imported := func(module string) bool {
			for path := range imports {
				if path == module || strings.HasPrefix(path, module+"/") {
					return true
				}
			}
			return false
		}
		goModPath := filepath.Join(root, "go.mod")
		goMod, err := os.ReadFile(goModPath)
		if err != nil {
			return err
		}
		var kept []string
		for _, line := range strings.Split(string(goMod), "\n") {
			if strings.HasSuffix(line, marker) && !imported(strings.TrimSuffix(strings.TrimPrefix(line, "require "), marker)) {
				continue
			}
			kept = append(kept, line)
		}
		return os.WriteFile(goModPath, []byte(strings.Join(kept, "\n")), 0o644)
	}
	t.Cleanup(func() { goGet, goModTidy = prevGet, prevTidy })
	return &calls
}

func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestRunNewSuiteTemplate(t *testing.T) {
	calls := stubScaffoldNetwork(t)
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{"store", "--out", outDir, "--template", "suite", "--port", "8091", "--module", "example.com/store"}
	if err := runNew(args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("runNew --template suite: %v\nstderr: %s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "store")

	if got := listFiles(t, projectDir); strings.Join(got, "\n") != strings.Join(suiteScaffoldFiles, "\n") {
		t.Errorf("suite scaffold file list:\n got %v\nwant %v", got, suiteScaffoldFiles)
	}

	// The network sequence: the framework driver, then the four siblings
	// in catalogue order with Quark's own driver right after Quark, then
	// one tidy — all in the project directory.
	want := []string{
		"go get github.com/jcsvwinston/nucleus/drivers/sqlite in store",
		"go get github.com/jcsvwinston/orbit in store",
		"go get github.com/jcsvwinston/quark in store",
		"go get github.com/jcsvwinston/quark/drivers/sqlite in store",
		"go get github.com/jcsvwinston/orbit/quarkbridge in store",
		"go get github.com/jcsvwinston/orbit/quarkdatasource in store",
		"go mod tidy in store",
	}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("suite network sequence:\n got %q\nwant %q", *calls, want)
	}

	mainSrc := readFile(t, filepath.Join(projectDir, "main.go"))
	code := stripComments(mainSrc)
	for _, needle := range []string{
		`quark.New("sqlite", "app.db")`,
		"shop.Migrate(ctx, client)",
		"quarkdatasource.New(client)",
		"quarkdatasource.Register[shop.Author](ds)",
		"quarkdatasource.Register[shop.Article](ds)",
		"Mount(shop.Module(client))",
		"Mount(orbit.Module(orbit.Config{",
		`Prefix:     "/admin"`,
		`Title:      "store"`,
		"DataSource: ds",
		`BootstrapUsername: "admin"`,
		"BootstrapPassword: bootstrapPassword()",
		`os.Getenv("ADMIN_BOOTSTRAP_PASSWORD")`,
		`return "quickstart"`,
		`_ "github.com/jcsvwinston/nucleus/drivers/sqlite"`,
		`_ "github.com/jcsvwinston/quark/drivers/sqlite"`,
		`"example.com/store/shop"`,
		`FromConfigFile("nucleus.yml")`,
	} {
		if !strings.Contains(code, needle) {
			t.Errorf("suite main.go must contain %q:\n%s", needle, mainSrc)
		}
	}
	// The showcase used to open the whole app (WithOpenAuthz) so its API
	// answered; the shop module carries its own rows instead, create
	// included, and the default-deny enforcer stays on.
	if strings.Contains(mainSrc, "WithOpenAuthz") {
		t.Errorf("suite main.go must not disable the default-deny enforcer:\n%s", mainSrc)
	}

	module := readFile(t, filepath.Join(projectDir, "shop", "module.go"))
	for _, row := range []string{
		`{Subject: "anonymous", Object: "/api/authors", Action: "read"}`,
		`{Subject: "anonymous", Object: "/api/articles", Action: "read"}`,
		`{Subject: "anonymous", Object: "/api/articles", Action: "create"}`,
		`CSRFExempt: []string{"/api/"}`,
		"quarkbridge.New(rt.Observability())",
		"quark.IsUniqueViolation(err)",
	} {
		if !strings.Contains(module, row) {
			t.Errorf("shop/module.go must carry %q:\n%s", row, module)
		}
	}
	policy := readFile(t, filepath.Join(projectDir, "rbac_policy.csv"))
	if strings.Contains(stripHashComments(policy), "/api") || strings.Contains(stripHashComments(policy), "/admin") {
		t.Errorf("rbac_policy.csv carries framework rows only; the module and orbit bring theirs:\n%s", policy)
	}
	cfg := readFile(t, filepath.Join(projectDir, "nucleus.yml"))
	for _, key := range []string{"url: sqlite://app.db", "port: 8091", "session_cookie_secure: false", "csrf_enabled: true", "rbac_policy_file: rbac_policy.csv"} {
		if !strings.Contains(cfg, key) {
			t.Errorf("suite nucleus.yml must carry %q:\n%s", key, cfg)
		}
	}
	test := readFile(t, filepath.Join(projectDir, "shop", "module_test.go"))
	for _, needle := range []string{"nucleustest.Start(", "http.StatusCreated", "http.StatusConflict", `"Hello, Quantum"`, `"sqlite://" + file`} {
		if !strings.Contains(test, needle) {
			t.Errorf("shop/module_test.go must carry %q:\n%s", needle, test)
		}
	}
	readme := readFile(t, filepath.Join(projectDir, "README.md"))
	if !strings.Contains(readme, "# store") || !strings.Contains(readme, "localhost:8091/admin") || !strings.Contains(readme, "Nucleus tags before Orbit") {
		t.Errorf("suite README must name the project, the admin URL and the tag-order note:\n%s", readme)
	}
	if !strings.Contains(readFile(t, filepath.Join(projectDir, ".gitignore")), "/store") {
		t.Error(".gitignore must ignore the binary go build writes")
	}

	out := stdout.String()
	for _, needle := range []string{
		"(template: suite, database: sqlite, with: orbit,quark,quarkbridge,quarkdatasource)",
		"curl -s localhost:8091/api/articles",
		"http://localhost:8091/admin",
		`ADMIN_BOOTSTRAP_PASSWORD (default "quickstart")`,
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("post-scaffold text must carry %q:\n%s", needle, out)
		}
	}
	if strings.Contains(out, "empty skeleton") {
		t.Errorf("the suite scaffold is not an empty skeleton:\n%s", out)
	}

	assertRenderedGoIsFormatted(t, projectDir)
}

// stripHashComments drops `#` comment lines from a CSV/YAML body so an
// assertion inspects the rows, not the prose explaining them.
func stripHashComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// assertRenderedGoIsFormatted parses every rendered Go file and demands it
// is already gofmt output: the templates carry conditionals whose leftover
// blank lines or indentation would otherwise be the reader's first edit.
func assertRenderedGoIsFormatted(t *testing.T, root string) {
	t.Helper()
	for _, rel := range listFiles(t, root) {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		src := readFile(t, path)
		if _, err := parser.ParseFile(token.NewFileSet(), path, src, parser.AllErrors); err != nil {
			t.Errorf("%s does not parse: %v", rel, err)
			continue
		}
		formatted, err := format.Source([]byte(src))
		if err != nil {
			t.Errorf("%s: gofmt: %v", rel, err)
			continue
		}
		if string(formatted) != src {
			t.Errorf("%s is not gofmt-clean as rendered:\n%s", rel, src)
		}
	}
}

// --db on the suite template moves both products to the same engine: the
// framework's driver module and Quark's, the nucleus.yml URL and the DSN
// quark.New takes, and the test's database source.
func TestRunNewSuiteTemplateDB(t *testing.T) {
	cases := []struct {
		db, quarkNew, nucleusDriver, quarkDriver string
	}{
		{"sqlite", `quark.New("sqlite", "app.db")`, "nucleus/drivers/sqlite", "quark/drivers/sqlite"},
		{"postgres", `quark.New("pgx", "postgres://postgres:postgres@localhost:5432/app?sslmode=disable")`, "nucleus/drivers/postgres", "quark/drivers/postgres"},
		{"mysql", `quark.New("mysql", "root:root@tcp(localhost:3306)/app?parseTime=true")`, "nucleus/drivers/mysql", "quark/drivers/mysql"},
		{"mssql", `quark.New("sqlserver", "sqlserver://sa:YourStrong!Passw0rd@localhost:1433?database=app")`, "nucleus/drivers/mssql", "quark/drivers/mssql"},
		{"oracle", `quark.New("oracle", "oracle://app:app@localhost:1521/FREEPDB1")`, "nucleus/drivers/oracle", "quark/drivers/oracle"},
	}
	for _, c := range cases {
		t.Run(c.db, func(t *testing.T) {
			calls := stubScaffoldNetwork(t)
			outDir := t.TempDir()
			var stdout, stderr bytes.Buffer
			if err := runNew([]string{"store", "--out", outDir, "--template", "suite", "--db", c.db}, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("runNew: %v\nstderr: %s", err, stderr.String())
			}
			projectDir := filepath.Join(outDir, "store")
			mainSrc := stripComments(readFile(t, filepath.Join(projectDir, "main.go")))
			for _, needle := range []string{c.quarkNew, `_ "github.com/jcsvwinston/` + c.nucleusDriver + `"`, `_ "github.com/jcsvwinston/` + c.quarkDriver + `"`} {
				if !strings.Contains(mainSrc, needle) {
					t.Errorf("--db %s: main.go must contain %q:\n%s", c.db, needle, mainSrc)
				}
			}
			joined := strings.Join(*calls, "\n")
			if !strings.Contains(joined, "go get github.com/jcsvwinston/"+c.quarkDriver+" in store") {
				t.Errorf("--db %s: the scaffold must go get Quark's driver module, ran %q", c.db, *calls)
			}
			test := readFile(t, filepath.Join(projectDir, "shop", "module_test.go"))
			if c.db == "sqlite" {
				if strings.Contains(test, "NUCLEUS_TEST_DATABASE_URL") {
					t.Errorf("sqlite test must run on a temporary file, not an env database:\n%s", test)
				}
			} else if !strings.Contains(test, "NUCLEUS_TEST_DATABASE_URL") || !strings.Contains(test, "QUARK_TEST_DSN") {
				t.Errorf("--db %s: the test must read both database sources from the environment and skip when unset:\n%s", c.db, test)
			}
			assertRenderedGoIsFormatted(t, projectDir)
		})
	}
}

// --with on the mvc and api templates fetches the named siblings; orbit is
// also mounted, because a fetched admin panel nobody mounts is a `go get`
// with nothing to show.
func TestRunNewWithFlag(t *testing.T) {
	t.Run("orbit on mvc and api: fetched and mounted", func(t *testing.T) {
		for _, tmpl := range []string{"mvc", "api"} {
			calls := stubScaffoldNetwork(t)
			outDir := t.TempDir()
			var stdout, stderr bytes.Buffer
			if err := runNew([]string{"blog", "--out", outDir, "--template", tmpl, "--with", "orbit"}, strings.NewReader(""), &stdout, &stderr); err != nil {
				t.Fatalf("%s: runNew --with orbit: %v\nstderr: %s", tmpl, err, stderr.String())
			}
			want := []string{
				"go get github.com/jcsvwinston/nucleus/drivers/sqlite in blog",
				"go get github.com/jcsvwinston/orbit in blog",
				"go mod tidy in blog",
			}
			if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
				t.Errorf("%s: network sequence\n got %q\nwant %q", tmpl, *calls, want)
			}
			projectDir := filepath.Join(outDir, "blog")
			mainSrc := readFile(t, filepath.Join(projectDir, "main.go"))
			code := stripComments(mainSrc)
			for _, needle := range []string{`"github.com/jcsvwinston/orbit"`, "Mount(orbit.Module(orbit.Config{", `Prefix: "/admin"`, "BootstrapPassword: bootstrapPassword()", `os.Getenv("ADMIN_BOOTSTRAP_PASSWORD")`} {
				if !strings.Contains(code, needle) {
					t.Errorf("%s --with orbit: main.go must contain %q:\n%s", tmpl, needle, mainSrc)
				}
			}
			if !strings.Contains(stdout.String(), "(template: "+tmpl+", database: sqlite, with: orbit)") || !strings.Contains(stdout.String(), "http://localhost:8080/admin") {
				t.Errorf("%s: the summary must name the sibling and the admin URL:\n%s", tmpl, stdout.String())
			}
			if got := listFiles(t, projectDir); strings.Contains(strings.Join(got, "\n"), "shop/") {
				t.Errorf("%s --with orbit must not add the suite's shop module: %v", tmpl, got)
			}
			assertRenderedGoIsFormatted(t, projectDir)
		}
	})

	t.Run("quark alone: fetched with its driver after the tidy, main.go untouched", func(t *testing.T) {
		calls := stubScaffoldNetworkWithGoMod(t)
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"blog", "--out", outDir, "--with", "quark", "--db", "postgres"}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew --with quark: %v", err)
		}
		// Nothing in the mvc scaffold imports Quark, so a `go get` before
		// the tidy is undone by it; the ORM and its driver are fetched
		// after, as indirect requires.
		want := []string{
			"go get github.com/jcsvwinston/nucleus/drivers/postgres in blog",
			"go mod tidy in blog",
			"go get github.com/jcsvwinston/quark in blog",
			"go get github.com/jcsvwinston/quark/drivers/postgres in blog",
		}
		if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
			t.Errorf("network sequence\n got %q\nwant %q", *calls, want)
		}
		goMod := readFile(t, filepath.Join(outDir, "blog", "go.mod"))
		for _, module := range []string{"github.com/jcsvwinston/nucleus/drivers/postgres", "github.com/jcsvwinston/quark", "github.com/jcsvwinston/quark/drivers/postgres"} {
			if !strings.Contains(goMod, "require "+module+" ") {
				t.Errorf("go.mod must still require %s once the scaffold is done (the tidy drops what nothing imports):\n%s", module, goMod)
			}
		}
		mainSrc := stripComments(readFile(t, filepath.Join(outDir, "blog", "main.go")))
		if strings.Contains(mainSrc, "Mount(") || strings.Contains(mainSrc, "orbit") {
			t.Errorf("--with quark mounts nothing:\n%s", mainSrc)
		}
		if !strings.Contains(stdout.String(), "Not wired by this template (nothing in the scaffold imports them yet): github.com/jcsvwinston/quark, github.com/jcsvwinston/quark/drivers/postgres.") {
			t.Errorf("the post-scaffold text must name what was fetched without being wired:\n%s", stdout.String())
		}
	})

	t.Run("orbit on mvc is wired: fetched before the tidy, which keeps it", func(t *testing.T) {
		calls := stubScaffoldNetworkWithGoMod(t)
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"blog", "--out", outDir, "--with", "orbit"}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew --with orbit: %v", err)
		}
		want := []string{
			"go get github.com/jcsvwinston/nucleus/drivers/sqlite in blog",
			"go get github.com/jcsvwinston/orbit in blog",
			"go mod tidy in blog",
		}
		if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
			t.Errorf("network sequence\n got %q\nwant %q", *calls, want)
		}
		goMod := readFile(t, filepath.Join(outDir, "blog", "go.mod"))
		if !strings.Contains(goMod, "require github.com/jcsvwinston/orbit ") {
			t.Errorf("go.mod must require orbit after the tidy (main.go imports it):\n%s", goMod)
		}
		if strings.Contains(stdout.String(), "Not wired by this template") {
			t.Errorf("orbit is wired by the template; nothing to report:\n%s", stdout.String())
		}
	})

	t.Run("--offline on mvc --with quark hands the two-step recipe back", func(t *testing.T) {
		calls := stubScaffoldNetwork(t)
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"blog", "--out", t.TempDir(), "--with", "quark", "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew --offline: %v", err)
		}
		if len(*calls) != 0 {
			t.Errorf("--offline must not touch the network, ran %q", *calls)
		}
		want := "go get github.com/jcsvwinston/nucleus/drivers/sqlite && go mod tidy && go get github.com/jcsvwinston/quark github.com/jcsvwinston/quark/drivers/sqlite   # skipped by --offline"
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("--offline must hand back the fetch of the unwired siblings after the tidy:\nwant %s\n got %s", want, stdout.String())
		}
	})

	t.Run("names are deduplicated, case-insensitive, catalogue-ordered", func(t *testing.T) {
		calls := stubScaffoldNetwork(t)
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"blog", "--out", outDir, "--with", "quarkdatasource, Orbit,orbit,,quark"}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew: %v", err)
		}
		if !strings.Contains(stdout.String(), "with: orbit,quark,quarkdatasource)") {
			t.Errorf("the summary must list the siblings once, in catalogue order:\n%s", stdout.String())
		}
		if got := strings.Join(*calls, "\n"); strings.Count(got, "go get github.com/jcsvwinston/orbit in") != 1 {
			t.Errorf("orbit must be fetched once, ran %q", *calls)
		}
	})

	t.Run("suite implies the four; --with may repeat them", func(t *testing.T) {
		calls := stubScaffoldNetwork(t)
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"store", "--out", t.TempDir(), "--template", "suite", "--with", "orbit"}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew: %v", err)
		}
		if !strings.Contains(stdout.String(), "with: orbit,quark,quarkbridge,quarkdatasource)") || len(*calls) != 7 {
			t.Errorf("suite must resolve all four siblings whatever --with says:\n%s\ncalls %q", stdout.String(), *calls)
		}
	})

	t.Run("unknown name lists the catalogue before writing anything", func(t *testing.T) {
		stubScaffoldNetwork(t)
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		err := runNew([]string{"blog", "--out", outDir, "--with", "admin"}, strings.NewReader(""), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), `unknown --with "admin"`) || !strings.Contains(err.Error(), "orbit, quark, quarkbridge, quarkdatasource") {
			t.Errorf("want an error listing the suite modules, got %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(outDir, "blog")); !os.IsNotExist(statErr) {
			t.Error("a refused --with must not leave a project directory behind")
		}
	})

	t.Run("--offline hands every go get back", func(t *testing.T) {
		calls := stubScaffoldNetwork(t)
		var stdout, stderr bytes.Buffer
		if err := runNew([]string{"store", "--out", t.TempDir(), "--template", "suite", "--offline"}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("runNew --offline: %v", err)
		}
		if len(*calls) != 0 {
			t.Errorf("--offline must not touch the network, ran %q", *calls)
		}
		want := "go get github.com/jcsvwinston/nucleus/drivers/sqlite github.com/jcsvwinston/orbit github.com/jcsvwinston/quark github.com/jcsvwinston/quark/drivers/sqlite github.com/jcsvwinston/orbit/quarkbridge github.com/jcsvwinston/orbit/quarkdatasource && go mod tidy   # skipped by --offline"
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("--offline must hand back the whole list:\nwant %s\n got %s", want, stdout.String())
		}
	})
}

// The unsupported-template error names the suite template too.
func TestRunNewUnknownTemplateListsSuite(t *testing.T) {
	stubScaffoldNetwork(t)
	var stdout, stderr bytes.Buffer
	err := runNew([]string{"x", "--out", t.TempDir(), "--template", "spa"}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "supported: mvc, api, suite") {
		t.Errorf("want the three templates listed, got %v", err)
	}
}
