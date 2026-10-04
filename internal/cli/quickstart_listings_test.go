// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// The quickstart's notes slice, measured on the page itself (NU-74, NU-75).
//
// The quickstart tells the reader to copy a complete module onto the
// project `nucleus new` writes, and the minimal-API page says that module
// touches a number of symbols and no more. Both used to be proven against
// examples/mvc_api; the examples left the tree on 2026-09-12 and the two
// claims were left with nothing measuring them. The meter is now the page:
// its listings are extracted, pasted onto a fresh scaffold, built, migrated,
// booted and driven through the five verbs; and the symbols they reference
// are counted against the minimal-API page's tables and its headline.
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// pageListing is one file of the quickstart's slice: the path its heading
// names and the body of its code block.
type pageListing struct {
	path, body string
}

// sliceHeading is a listing's heading: **Entry point (`main.go`)**.
var sliceHeading = regexp.MustCompile("^\\*\\*[^*]*\\(`([^`]+)`\\)\\*\\*\\s*$")

// quickstartSlice extracts the slice step 3 of the quickstart walks
// through: every code block under a bold heading that names a path, up to
// the sentence that closes the slice. A heading naming a directory
// (migrations/) holds the up and the down migration, in that order. It
// returns the listings and the number of files the closing sentence
// claims.
func quickstartSlice(t *testing.T) ([]pageListing, int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootForTest(t), "website", "docs", "getting-started", "quickstart.md"))
	if err != nil {
		t.Fatalf("read the quickstart: %v", err)
	}
	words := map[string]int{"three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8}
	closing := regexp.MustCompile(`^Those (\w+) files are the complete slice\.`)

	var listings []pageListing
	current, claimed := "", 0
	inBlock := false
	var block []string
	blocksUnder := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if inBlock {
			if strings.TrimSpace(line) == "```" {
				inBlock = false
				path := current
				if strings.HasSuffix(current, "/") {
					kind := map[int]string{0: "up", 1: "down"}[blocksUnder]
					if kind == "" {
						t.Fatalf("the quickstart's %s heading holds more than an up and a down migration", current)
					}
					path = current + "000001_create_notes." + kind + ".sql"
				}
				listings = append(listings, pageListing{path: path, body: strings.Join(block, "\n") + "\n"})
				blocksUnder++
				continue
			}
			block = append(block, line)
			continue
		}
		if m := closing.FindStringSubmatch(line); m != nil {
			claimed = words[m[1]]
			break
		}
		if m := sliceHeading.FindStringSubmatch(line); m != nil {
			current, blocksUnder = m[1], 0
			continue
		}
		if current != "" && strings.HasPrefix(line, "```") {
			inBlock, block = true, nil
		}
	}
	if len(listings) == 0 || claimed == 0 {
		t.Fatalf("the quickstart no longer has the shape this test reads: bold headings naming a path, a code block under each, "+
			"closed by \"Those <n> files are the complete slice.\" (found %d listings, claim %d) — update the test with the page", len(listings), claimed)
	}
	return listings, claimed
}

// TestQuickstartSliceServesOnTheScaffold is NU-74's meter: the reader's
// walkthrough, literally. `nucleus new`, then the page's files pasted in at
// the paths its headings name (main.go replacing the scaffold's), the
// module path the page tells the reader to replace replaced and nothing
// else touched; then build, `nucleus migrate up`, boot, and the five verbs
// the controller implements, each answering what the page says. A page that
// drifts from what the scaffold writes — a config path the scaffold does
// not have, an import it does not resolve, a policy row the routes need —
// fails here.
func TestQuickstartSliceServesOnTheScaffold(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a scaffolded project; skipped with -short")
	}
	listings, claimed := quickstartSlice(t)
	files := map[string]bool{}
	for _, l := range listings {
		files[l.path] = true
	}
	// The migration pair is one file in the page's count.
	if got := len(files) - 1; got != claimed {
		t.Errorf("the quickstart says its slice is %d files and lists %d (the migration pair counted once): %v", claimed, got, sortedFileNames(files))
	}

	repoRoot := repoRootForTest(t)
	outDir := t.TempDir()
	port := freeLoopbackPort(t)
	var stdout, stderr bytes.Buffer
	if err := runNew([]string{"myapp", "--out", outDir, "--offline", "--module", "example.com/myapp", "--port", strconv.Itoa(port)}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("nucleus new: %v\n%s", err, stderr.String())
	}
	projectDir := filepath.Join(outDir, "myapp")
	pinGoModToLocalNucleus(t, projectDir, repoRoot)
	for _, l := range listings {
		dst := filepath.Join(projectDir, filepath.FromSlash(l.path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		body := strings.ReplaceAll(l.body, "github.com/acme/myapp", "example.com/myapp")
		if err := os.WriteFile(dst, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// One edit the reader does not make: the database file is named by
	// its absolute path, so `migrate up` from this test and the binary
	// started in the project directory open the same file.
	cfgPath := filepath.Join(projectDir, "nucleus.yml")
	cfg := readFile(t, cfgPath)
	dbFile := filepath.Join(projectDir, "app.db")
	if !strings.Contains(cfg, "url: sqlite://app.db") {
		t.Fatalf("the scaffold's nucleus.yml no longer points at sqlite://app.db:\n%s", cfg)
	}
	if err := os.WriteFile(cfgPath, []byte(strings.Replace(cfg, "url: sqlite://app.db", "url: sqlite://"+dbFile, 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	runGoInProject(t, projectDir, "mod", "tidy")
	runGoInProject(t, projectDir, "vet", "./...")
	runGoInProject(t, projectDir, "build", "-o", "app", ".")

	var migOut, migErr bytes.Buffer
	if err := runMigrate([]string{"--config", cfgPath, "--migrations", filepath.Join(projectDir, "migrations"), "up"}, strings.NewReader(""), &migOut, &migErr); err != nil {
		t.Fatalf("nucleus migrate up: %v\n%s%s", err, migOut.String(), migErr.String())
	}

	app := exec.Command(filepath.Join(projectDir, "app"))
	app.Dir = projectDir
	var appLog bytes.Buffer
	app.Stdout, app.Stderr = &appLog, &appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() string {
		if !stopped {
			stopped = true
			_ = app.Process.Kill()
			_ = app.Wait()
		}
		return appLog.String()
	}
	t.Cleanup(func() { stop() })

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}
	waitForHealthz(t, client, base, stop)
	do := func(method, path, body string, want int) map[string]any {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, stop())
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s on the quickstart's slice: want %d, got %d body=%s\n--- app log ---\n%s", method, path, want, resp.StatusCode, raw, stop())
		}
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out
	}

	do(http.MethodGet, "/notes", "", http.StatusOK)
	created := do(http.MethodPost, "/notes", `{"title":"first","body":"from the page"}`, http.StatusCreated)
	id, ok := created["id"].(float64)
	if !ok || id < 1 {
		t.Fatalf("POST /notes answered no id: %v", created)
	}
	item := fmt.Sprintf("/notes/%d", int(id))
	if got := do(http.MethodGet, item, "", http.StatusOK); got["title"] != "first" {
		t.Errorf("GET %s: want the created note, got %v", item, got)
	}
	if got := do(http.MethodPut, item, `{"title":"second","body":""}`, http.StatusOK); got["title"] != "second" {
		t.Errorf("PUT %s: want the updated note, got %v", item, got)
	}
	do(http.MethodDelete, item, "", http.StatusNoContent)
	do(http.MethodGet, item, "", http.StatusNotFound)
	if list := do(http.MethodGet, "/notes", "", http.StatusOK); list["count"] != float64(0) {
		t.Errorf("GET /notes after the delete: want an empty list, got %v", list)
	}
	// The slice is what the reader copies as the model for their own
	// modules: it boots and serves the walk above without one WARN.
	if log := stop(); len(warnLine.FindAllString(log, -1)) != 0 {
		t.Errorf("the quickstart's slice must serve the walk above with zero WARN lines:\n%s", log)
	}
}

func sortedFileNames(files map[string]bool) []string {
	out := make([]string, 0, len(files))
	for f := range files {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// TestMinimalAPIPageCountsTheQuickstartSlice is NU-75's meter. The page
// says the quickstart's notes module touches "the N symbols on this page"
// and nothing else; this counts them on the page's own listings. A symbol is
// a package-level name of one of the framework's packages the slice's Go
// files reference (the methods on them come with the type), plus the
// Resource interface each verb the slice selects requires its controller to
// implement — Router.Resource asserts it, so selecting nucleus.Index
// touches nucleus.Indexer. The page's tables must list exactly that set,
// each section's "(n symbols)" must count its rows, and the headline must
// be the total.
func TestMinimalAPIPageCountsTheQuickstartSlice(t *testing.T) {
	listings, _ := quickstartSlice(t)

	// The verb → interface pairing, read off the interfaces themselves.
	ifaceFor := map[string]string{}
	for _, iface := range []reflect.Type{
		reflect.TypeOf((*nucleus.Indexer)(nil)).Elem(),
		reflect.TypeOf((*nucleus.Shower)(nil)).Elem(),
		reflect.TypeOf((*nucleus.Creator)(nil)).Elem(),
		reflect.TypeOf((*nucleus.Updater)(nil)).Elem(),
		reflect.TypeOf((*nucleus.Patcher)(nil)).Elem(),
		reflect.TypeOf((*nucleus.Destroyer)(nil)).Elem(),
	} {
		ifaceFor[iface.Method(0).Name] = iface.Name()
	}

	touched := map[string]bool{}
	for _, l := range listings {
		if !strings.HasSuffix(l.path, ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), l.path, l.body, 0)
		if err != nil {
			t.Fatalf("the quickstart's %s does not parse: %v", l.path, err)
		}
		framework := map[string]string{} // local import name → package name
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(p, "github.com/jcsvwinston/nucleus/pkg/") {
				continue
			}
			name := filepath.Base(p)
			local := name
			if imp.Name != nil {
				local = imp.Name.Name
			}
			framework[local] = name
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			pkg, ok := framework[id.Name]
			if !ok {
				return true
			}
			touched[pkg+"."+sel.Sel.Name] = true
			if iface, ok := ifaceFor[sel.Sel.Name]; ok && pkg == "nucleus" {
				touched["nucleus."+iface] = true
			}
			return true
		})
	}

	page := readFile(t, filepath.Join(repoRootForTest(t), "website", "docs", "getting-started", "minimal-api.md"))
	listed := map[string]bool{}
	section, sectionClaim, sectionCount := "", 0, 0
	closeSection := func() {
		if section != "" && sectionClaim != sectionCount {
			t.Errorf("minimal-api.md: section %q says %d symbols and its table lists %d", section, sectionClaim, sectionCount)
		}
	}
	sectionRE := regexp.MustCompile(`^## (.+) \((\d+) symbols?\)\s*$`)
	symbolRE := regexp.MustCompile("`((?:nucleus|model)\\.[A-Z][A-Za-z]*)`")
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, "## ") {
			closeSection()
			section, sectionClaim, sectionCount = "", 0, 0
			if m := sectionRE.FindStringSubmatch(line); m != nil {
				section = m[1]
				sectionClaim, _ = strconv.Atoi(m[2])
			}
			continue
		}
		if section == "" || !strings.HasPrefix(line, "| `") {
			continue
		}
		symbolCell := strings.SplitN(strings.TrimPrefix(line, "|"), "|", 2)[0]
		for _, m := range symbolRE.FindAllStringSubmatch(symbolCell, -1) {
			if listed[m[1]] {
				t.Errorf("minimal-api.md lists %s twice", m[1])
			}
			listed[m[1]] = true
			sectionCount++
		}
	}
	closeSection()

	var missing, extra []string
	for s := range touched {
		if !listed[s] {
			missing = append(missing, s)
		}
	}
	for s := range listed {
		if !touched[s] {
			extra = append(extra, s)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("the quickstart's slice touches %v, which minimal-api.md does not list", missing)
	}
	if len(extra) > 0 {
		t.Errorf("minimal-api.md lists %v, which the quickstart's slice does not touch", extra)
	}

	headline := regexp.MustCompile(`touches the \*\*(\d+) symbols on this page\*\*`).FindStringSubmatch(page)
	if headline == nil {
		t.Fatal("minimal-api.md no longer says \"touches the **N symbols on this page**\" — update the test with the page")
	}
	if n, _ := strconv.Atoi(headline[1]); n != len(touched) || n != len(listed) {
		t.Errorf("minimal-api.md says the slice touches %d symbols; it touches %d and the page lists %d", n, len(touched), len(listed))
	}

	// The page's other number: how much it lets the reader ignore.
	frozen := regexp.MustCompile(`freezes more than ([\d ]+) symbols`).FindStringSubmatch(page)
	if frozen == nil {
		t.Fatal("minimal-api.md no longer says how many symbols the contract freezes — update the test with the page")
	}
	claim, _ := strconv.Atoi(strings.ReplaceAll(frozen[1], " ", ""))
	baseline := strings.Count(readFile(t, filepath.Join(repoRootForTest(t), "contracts", "baseline", "api_exported_symbols.txt")), "\n")
	if claim > baseline {
		t.Errorf("minimal-api.md says more than %d symbols are frozen; contracts/baseline lists %d", claim, baseline)
	}
}
