// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jcsvwinston/nucleus/internal/cli"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/storage"
	"github.com/jcsvwinston/nucleus/pkg/storage/provider"
)

// The testkit family asks what pkg/nucleustest gives a test author: the
// listón is Rails' integration tests and Encore's test client — a booted app,
// a client that speaks the API's language and carries a session, data
// factories, a transaction per test, and doubles that CAPTURE what the
// application sent out (mail, files, jobs, HTTP) so the test can assert on
// it. Most probes here ask the kit's own method set: a helper the author
// would call has to exist under some name before it can be measured for what
// it does, and the probe logs the names it found so the gap is concrete.

func kitMethods() []string { return methodNames((*nucleustest.Server)(nil)) }

// TK-01: an application boots in-process for a test, and is stopped on cleanup.
func probeBootInProcess(t *testing.T, e *env) verdict {
	srv := e.server()
	resp, _ := e.do(t, http.MethodGet, "/healthz", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Logf("the booted application answers /healthz with %d", resp.StatusCode)
		return partial
	}
	t.Logf("booted at %s; kit methods: %v", srv.BaseURL, kitMethods())
	return present
}

// TK-02: the kit has a request helper that speaks JSON — encodes a body,
// decodes the answer — instead of handing the author a bare *http.Client.
func probeJSONRequestHelper(t *testing.T, e *env) verdict {
	names := kitMethods()
	if _, ok := anyMethod(names, "Request", "JSON", "GetJSON", "PostJSON", "Do", "Call"); !ok {
		t.Logf("the kit offers Client() *http.Client and URL(path); every JSON round trip is the author's own encoding, request and decoding. methods: %v", names)
		return absent
	}
	// The helper exists: does it do the round trip? One call sends a body
	// and one call decodes the answer into a typed value.
	resp := e.server().Post("/bench/echo", echoInput{Name: "kit", Age: 7})
	if resp.Status != http.StatusOK {
		t.Logf("Post answered %d: %s", resp.Status, resp)
		return partial
	}
	var out echoInput
	resp.JSON(t, &out)
	if out.Name != "kit" || out.Age != 7 {
		t.Logf("decoded %+v", out)
		return partial
	}
	return present
}

// TK-03: cookies set by the application persist across the kit's requests —
// Secure ones included, since the application issues its session and CSRF
// cookies with the flag and the test server is plain HTTP on loopback.
func probeCookieJar(t *testing.T, e *env) verdict {
	srv := e.server()
	if srv.Client().Jar == nil {
		if n, ok := anyMethod(kitMethods(), "Cookies", "Jar", "WithCookies"); ok {
			t.Logf("cookies are opt-in through %s", n)
			return partial
		}
		t.Log("the kit's client has no cookie jar: a session cookie the application sets is dropped on the next request")
		return absent
	}
	if r := srv.Get("/bench/set-cookie"); r.Status != http.StatusNoContent {
		t.Logf("set-cookie answered %d", r.Status)
		return partial
	}
	var out map[string]string
	srv.Get("/bench/read-cookie").JSON(t, &out)
	if out["crumb"] != "kept" {
		t.Logf("the Secure cookie the application set did not come back: %v (held: %v)", out, srv.Cookies())
		return partial
	}
	return present
}

// TK-04: the kit obtains a CSRF token for state-changing requests.
func probeCSRFHelper(t *testing.T, _ *env) verdict {
	if _, ok := anyMethod(kitMethods(), "CSRFToken", "CSRF", "WithCSRF"); !ok {
		t.Log("no CSRF helper: a test of a form POST behind the CSRF middleware has to fetch and thread the token by hand")
		return absent
	}
	// With the middleware on: a POST without the token is refused, one
	// with the kit's token goes through.
	a := buildWith(t, nil, benchModule())
	a.Config.CSRFEnabled = true
	srv := nucleustest.StartApp(t, a)
	if r := srv.Post("/bench/echo", echoInput{Name: "x"}); r.Status != 419 && r.Status != http.StatusForbidden {
		t.Logf("a POST without the token answered %d: the middleware is not in the chain, so the helper cannot be measured", r.Status)
		return partial
	}
	if r := srv.Post("/bench/echo", echoInput{Name: "x"}, srv.WithCSRF()); r.Status != http.StatusOK {
		t.Logf("a POST with the kit's token answered %d: %s", r.Status, r)
		return partial
	}
	return present
}

// TK-05: the kit lets a test act as a user — a session the application
// recognises, not only a bearer token.
func probeActAsUser(t *testing.T, e *env) verdict {
	names := kitMethods()
	_, token := anyMethod(names, "MintToken", "Token", "Bearer")
	_, session := anyMethod(names, "SignIn", "SignInAccount", "LoginAs", "AsUser", "ActingAs", "OpenSession")
	switch {
	case !session && token:
		t.Log("MintToken issues a bearer token; there is no helper that opens a session the cookie-based routes recognise")
		return partial
	case !session:
		return absent
	}
	// The session the helper opens is the one the application reads.
	srv := e.server()
	var before map[string]string
	srv.Get("/bench/whoami").JSON(t, &before)
	srv.SignInAccount("acc-bench", "bench@example.test")
	var after map[string]string
	srv.Get("/bench/whoami").JSON(t, &after)
	srv.SignOut()
	if before["account_id"] != "" || after["account_id"] != "acc-bench" {
		t.Logf("before %v, after %v", before, after)
		return partial
	}
	return present
}

// TK-06: data factories — build persisted records with sensible defaults.
func probeFactories(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`(?m)^func (\([^)]*\) )?(Make|MakeN|Factory|Build)\b`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) == 0 {
		t.Log("pkg/nucleustest has no factories: every test inserts its rows by hand")
		return absent
	}
	// The factory exists: a registered model gets a row with defaults and a
	// key, and an override sticks.
	srv := startWith(t, thingsModule())
	if err := srv.Runtime().AutoMigrate(&BenchThing{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	one := nucleustest.Make[BenchThing](srv)
	two := nucleustest.Make[BenchThing](srv, func(x *BenchThing) { x.Name = "chosen" })
	if one.ID == 0 || one.Name == "" || two.Name != "chosen" || two.ID == one.ID {
		t.Logf("made %+v and %+v", one, two)
		return partial
	}
	var n int
	if err := srv.DB().QueryRow("SELECT COUNT(*) FROM bench_things").Scan(&n); err != nil || n != 2 {
		t.Logf("rows in the table: %d (%v)", n, err)
		return partial
	}
	return present
}

// TK-07: a transaction per test, rolled back on cleanup — for the
// application's routes, not only the test's own handle.
func probeTxPerTest(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`(?m)^func (Transactional|InTx|WithinTx|TxPerTest)\b`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) == 0 {
		t.Log("no transaction-per-test helper; TempSQLite gives each test its own database file instead, which is isolation by copy rather than by rollback")
		return absent
	}
	// Inside a transactional scope the application migrates and writes;
	// once the scope ends the database is as it was.
	path := filepath.Join(t.TempDir(), "tx.db")
	url := "sqlite://" + path
	t.Run("scope", func(t *testing.T) {
		dbs := nucleustest.Transactional(t, map[string]app.DatabaseConfig{"default": {URL: url}})
		a := buildWith(t, nil, thingsModule())
		a.Config.Databases = dbs
		srv := nucleustest.StartApp(t, a)
		if err := srv.Runtime().AutoMigrate(&BenchThing{}); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		nucleustest.MakeN[BenchThing](srv, 2)
		var n int
		if err := srv.DB().QueryRow("SELECT COUNT(*) FROM bench_things").Scan(&n); err != nil || n != 2 {
			t.Fatalf("inside the scope: %d rows (%v)", n, err)
		}
	})
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	var n int
	if err := raw.QueryRow("SELECT COUNT(*) FROM bench_things").Scan(&n); err == nil {
		t.Logf("after the scope the table is still there with %d rows: the rollback did not reach the application's writes", n)
		return partial
	}
	return present
}

// TK-08: a mail double captures what the application sent.
func probeMailCapture(t *testing.T, _ *env) verdict {
	providers := mail.RegisteredProviders()
	capturing := false
	for _, p := range providers {
		if strings.EqualFold(p, "memory") {
			capturing = true
		}
	}
	if !capturing {
		t.Logf("mail providers registered: %v — noop discards, smtp sends; nothing a test can read back", providers)
		return absent
	}
	if _, ok := anyMethod(kitMethods(), "SentMail", "Mail", "Sent", "Mails"); !ok {
		t.Log("a memory mail provider exists but the kit does not read it: the test still has to reach the sender by hand")
		return partial
	}
	srv := emittingServer(t, "http://127.0.0.1:1")
	if r := srv.Post("/emit/mail", nil); r.Status/100 != 2 {
		t.Logf("POST /emit/mail → %d %s", r.Status, r)
		return partial
	}
	sent := srv.SentMail()
	if len(sent) != 1 || sent[0].Subject != "bench" {
		t.Logf("captured %+v", sent)
		return partial
	}
	return present
}

// TK-09: a storage double the test can read back.
func probeStorageCapture(t *testing.T, _ *env) verdict {
	names := provider.Registered()
	memory := false
	for _, n := range names {
		if n == "memory" {
			memory = true
		}
	}
	_, reads := anyMethod(kitMethods(), "Stored", "StoredKeys", "Storage", "Uploads", "Files")
	switch {
	case !memory && !reads:
		t.Logf("storage providers: %v; nothing captures for a test and nothing reads back", names)
		return absent
	case !memory || !reads:
		t.Logf("storage providers: %v; kit reads back: %v", names, reads)
		return partial
	}
	srv := emittingServer(t, "http://127.0.0.1:1")
	if r := srv.Post("/emit/upload", nil); r.Status/100 != 2 {
		t.Logf("POST /emit/upload → %d %s", r.Status, r)
		return partial
	}
	if got := string(srv.Stored("bench/hello.txt")); got != "hello" {
		t.Logf("Stored = %q", got)
		return partial
	}
	if keys := srv.StoredKeys("bench/"); len(keys) != 1 {
		t.Logf("StoredKeys = %v", keys)
		return partial
	}
	return present
}

// TK-10: a tasks double — the test sees what was enqueued.
func probeTasksCapture(t *testing.T, _ *env) verdict {
	if _, ok := anyMethod(kitMethods(), "EnqueuedTasks", "Enqueued", "Tasks", "Jobs"); !ok {
		t.Log("the kit does not read what was enqueued; the in-process provider's inspector is an operations view, not a test double")
		return absent
	}
	srv := emittingServer(t, "http://127.0.0.1:1")
	if r := srv.Post("/emit/enqueue", nil); r.Status/100 != 2 {
		t.Logf("POST /emit/enqueue → %d %s", r.Status, r)
		return partial
	}
	recs := srv.EnqueuedTasks()
	if len(recs) != 1 || recs[0].Type != "bench.report" || !strings.Contains(string(recs[0].Payload), `"nightly"`) {
		t.Logf("recorded %+v", recs)
		return partial
	}
	return present
}

// TK-11: a double for the HTTP the application makes to other services.
func probeOutboundHTTPDouble(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`func NewHTTPRecorder|RoundTripper|httptest\.`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) == 0 {
		t.Log("nothing in the kit intercepts the HTTP the application makes to other services")
		return absent
	}
	rec := nucleustest.NewHTTPRecorder(t)
	rec.Respond(http.StatusAccepted, `{"queued":true}`)
	srv := emittingServer(t, rec.URL)
	var out map[string]int
	srv.Post("/emit/notify", nil).JSON(t, &out)
	reqs := rec.Requests()
	if out["upstream"] != http.StatusAccepted || len(reqs) != 1 || reqs[0].Path != "/hooks/bench" {
		t.Logf("upstream=%d recorded=%+v", out["upstream"], reqs)
		return partial
	}
	return present
}

// TK-12: a helper reads a server-sent event stream.
func probeStreamHelper(t *testing.T, _ *env) verdict {
	if _, ok := anyMethod(kitMethods(), "Stream"); ok {
		return present
	}
	return absent
}

// TK-13: the runtime is reachable from the test — database, migrations, services.
func probeRuntimeReachable(t *testing.T, e *env) verdict {
	names := kitMethods()
	_, rt := anyMethod(names, "Runtime")
	_, db := anyMethod(names, "DB")
	_, mig := anyMethod(names, "MigrateDir")
	if !rt || !db {
		t.Logf("kit methods: %v", names)
		return absent
	}
	if e.server().Runtime().DB() == nil {
		return partial
	}
	if !mig {
		return partial
	}
	return present
}

// TK-14: the generated code ships a test that uses the kit — and the test
// speaks through the kit's client. The probe renders what a user gets, the
// same way the user gets it: `nucleus new --template suite` (the starter
// the arc's gate is measured on) and `nucleus generate module`, read-only
// and open policy, through the CLI's own entry point. Every test file they
// write that boots through the kit is parsed; each one has to send its
// requests with the kit's client (srv.Get/srv.Post/…, a Response decoded
// with JSON) and none may fall back to a bare *http.Client.
func probeStarterShipsTest(t *testing.T, _ *env) verdict {
	files := generatedKitTests(t)
	if len(files) == 0 {
		t.Log("no file the CLI generates is a test on the kit")
		return absent
	}
	starter := "starter/shop/module_test.go"
	if _, ok := files[starter]; !ok {
		t.Logf("the suite starter writes no kit test at %s; kit tests found: %v", starter, sortedKeys(files))
		return partial
	}
	got := present
	for _, name := range sortedKeys(files) {
		use := kitClientUse(t, name, files[name])
		switch {
		case len(use.raw) > 0:
			t.Logf("%s reaches past the kit's client: %v", name, use.raw)
			got = partial
		case use.requests == 0 || use.decodes == 0:
			t.Logf("%s boots on the kit but sends %d requests and decodes %d answers through its client", name, use.requests, use.decodes)
			got = partial
		default:
			t.Logf("%s: %d requests and %d decoded answers through the kit's client", name, use.requests, use.decodes)
		}
	}
	return got
}

// generatedKitTests runs the CLI the way a user does and returns, by
// project-relative path, every test file it wrote that imports the kit.
func generatedKitTests(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		var out, errb bytes.Buffer
		if code := cli.Run(args, strings.NewReader(""), &out, &errb); code != 0 {
			t.Fatalf("nucleus %s: exit %d\n%s%s", strings.Join(args, " "), code, out.String(), errb.String())
		}
	}
	run("new", "starter", "--out", dir, "--template", "suite", "--module", "example.com/starter", "--offline")
	run("new", "app", "--out", dir, "--module", "example.com/app", "--offline")
	app := filepath.Join(dir, "app")
	run("generate", "module", "notes", "--out", app, "--offline")
	run("generate", "module", "widget", "--out", app, "--with-policy", "--offline")

	files := map[string]string{}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), `"github.com/jcsvwinston/nucleus/pkg/nucleustest"`) {
			rel, _ := filepath.Rel(dir, path)
			files[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return files
}

// clientUse is what a generated test does with HTTP: requests sent and
// answers decoded through the kit's client, and every place it reaches for
// net/http's client instead.
type clientUse struct {
	requests, decodes int
	raw               []string
}

// kitClientUse parses one test file. A kit server is a variable assigned
// from nucleustest.Start/StartApp or a parameter typed *nucleustest.Server;
// a request is a call of one of the client's methods on it; a decode is a
// .JSON call on what came back. Raw use is a .Client() call on a kit server
// or any reference to net/http's client surface.
func kitClientUse(t *testing.T, name, src string) clientUse {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	servers := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range x.Rhs {
				if call, ok := rhs.(*ast.CallExpr); ok && isSelector(call.Fun, "nucleustest", "Start", "StartApp") && i < len(x.Lhs) {
					if id, ok := x.Lhs[i].(*ast.Ident); ok {
						servers[id.Name] = true
					}
				}
			}
		case *ast.Field:
			if star, ok := x.Type.(*ast.StarExpr); ok && isSelector(star.X, "nucleustest", "Server") {
				for _, id := range x.Names {
					servers[id.Name] = true
				}
			}
		}
		return true
	})
	var use clientUse
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, _ := sel.X.(*ast.Ident)
			switch {
			case recv != nil && servers[recv.Name] && sel.Sel.Name == "Client":
				use.raw = append(use.raw, fset.Position(x.Pos()).String()+" "+recv.Name+".Client()")
			case recv != nil && servers[recv.Name] && isClientMethod(sel.Sel.Name):
				use.requests++
			case sel.Sel.Name == "JSON" && len(x.Args) == 2:
				use.decodes++
			}
		case *ast.SelectorExpr:
			if isSelector(x, "http", "Client", "DefaultClient", "Get", "Post", "PostForm", "Head", "NewRequest", "NewRequestWithContext") {
				use.raw = append(use.raw, fset.Position(x.Pos()).String()+" http."+x.Sel.Name)
			}
		}
		return true
	})
	return use
}

func isClientMethod(name string) bool {
	switch name {
	case "Request", "Get", "Post", "Put", "Patch", "Delete":
		return true
	}
	return false
}

// isSelector reports whether e is pkg.Name for one of names.
func isSelector(e ast.Expr, pkg string, names ...string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != pkg {
		return false
	}
	for _, n := range names {
		if sel.Sel.Name == n {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TK-15: contract tests for a module — a kit that checks a ModuleSpec
// against what the framework expects of it, without an application test.
// A well-formed module has to pass every check; each of a row of modules
// broken in one way has to fail exactly the check that names its defect;
// and one module broken six ways has to get all six named in one call — a
// kit that booted the module and reported boot's error would name one.
func probeModuleContractKit(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`func (Check|Verify|Conform|Assert)Module`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) == 0 {
		t.Log("the kit boots an application; it does not check a module against the contract (names, prefix, requires, migrations, hooks) on its own")
		return absent
	}
	got := present
	if failed := failingChecks(t, contractModule(nil)); len(failed) != 0 {
		t.Logf("a well-formed module fails %v", failed)
		got = partial
	}
	for _, c := range brokenModules() {
		failed := failingChecks(t, contractModule(c.mutate))
		if strings.Join(failed, ",") != c.want {
			t.Logf("%s: the kit names %v, the defect is %s", c.defect, failed, c.want)
			got = partial
		}
	}
	all := func(m *nucleus.Module[contractConfig]) {
		for _, c := range []string{"bad config", "malformed policy row", "malformed CSRF exemption", "migration that does not parse", "duplicate route", "failing OnShutdown"} {
			brokenModule(c).mutate(m)
		}
	}
	want := "config,csrf-exempt,migrations,policies,routes,shutdown"
	if failed := failingChecks(t, contractModule(all)); strings.Join(failed, ",") != want {
		t.Logf("one module broken six ways: the kit names %v, want %s", failed, want)
		got = partial
	}
	return got
}

// failingChecks runs the kit on spec and returns the checks it failed, in
// name order, after confirming the kit failed the test once for each and
// named the module and the check every time.
func failingChecks(t *testing.T, spec nucleus.ModuleSpec) []string {
	t.Helper()
	rec := &errorRecorder{TB: t}
	checks := nucleustest.CheckModule(rec, spec)
	var failed []string
	for _, c := range checks {
		if c.Err != nil {
			failed = append(failed, c.Name)
		}
	}
	sort.Strings(failed)
	if len(rec.errs) != len(failed) {
		t.Logf("the kit reported %d failures for %d failing checks: %v", len(rec.errs), len(failed), rec.errs)
		return append(failed, "(reports do not match)")
	}
	for i, e := range rec.errs {
		if !strings.Contains(e, fmt.Sprintf("module %q fails the ", spec.Name())) {
			t.Logf("report %d does not name the module and the check: %s", i, e)
			return append(failed, "(report unnamed)")
		}
	}
	return failed
}

// errorRecorder stands in for the probe's test, so a module the kit fails
// does not fail the bench.
type errorRecorder struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (r *errorRecorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

type contractConfig struct {
	Mode string `default:"fast" validate:"oneof=fast slow"`
}

// contractModule is a module that does everything right: a typed config
// with defaults, a model, an embedded migration, its own policy rows and
// CSRF exemption about the routes it serves, a job, a webhook, templates
// that parse, and hooks that succeed. mutate breaks it.
func contractModule(mutate func(m *nucleus.Module[contractConfig])) nucleus.ModuleSpec {
	ok := func(c *nucleus.Context) error { return c.NoContent() }
	m := nucleus.Module[contractConfig]{
		Name:   "contract",
		Prefix: "/contract",
		Models: []any{&BenchThing{}},
		Migrations: fstest.MapFS{
			"000001_contract.up.sql":   {Data: []byte("CREATE TABLE contract_items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);")},
			"000001_contract.down.sql": {Data: []byte("DROP TABLE contract_items;")},
		},
		Templates: fstest.MapFS{"page.html": {Data: []byte(`<p>{{ .name }}</p>`)}},
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/items", Action: "read"},
			{Subject: "anonymous", Object: "/items", Action: "create"},
		},
		CSRFExempt: []string{"/items"},
		OnStart: func(_ context.Context, _ nucleus.Runtime, cfg contractConfig) error {
			if cfg.Mode == "" {
				return errors.New("the config's default never arrived")
			}
			return nil
		},
		OnShutdown: func(context.Context, nucleus.Runtime, contractConfig) error { return nil },
		Jobs: func(j nucleus.JobRegistry, _ contractConfig) {
			_ = j.Register("sweep", nucleus.JobSpec{Every: time.Hour, Handler: func(context.Context) error { return nil }})
		},
		Webhooks: func(w nucleus.WebhookRegistry, _ contractConfig) {
			_ = w.Register("/ping", nucleus.WebhookSpec{Secret: "bench", Handler: func(http.ResponseWriter, *http.Request) {}})
		},
		Routes: func(r nucleus.Router, _ contractConfig) {
			r.Get("/items", ok)
			r.Post("/items", ok)
		},
	}
	if mutate != nil {
		mutate(&m)
	}
	return m.Build()
}

type brokenCase struct {
	defect string
	want   string // the check that has to fail, and no other
	mutate func(m *nucleus.Module[contractConfig])
}

// brokenModules is one defect per module, each one a mistake a module
// author makes: what boot refuses, and what boot lets through and the
// module then fails at run time without a word.
func brokenModules() []brokenCase {
	ok := func(c *nucleus.Context) error { return c.NoContent() }
	return []brokenCase{
		{"a name the environment layer cannot address", "name", func(m *nucleus.Module[contractConfig]) { m.Name = "Contract" }},
		{"the kit's reserved name", "name", func(m *nucleus.Module[contractConfig]) { m.Name = "nucleustest_probe" }},
		{"a prefix without its leading slash", "prefix", func(m *nucleus.Module[contractConfig]) { m.Prefix = "contract" }},
		{"bad config", "config", func(m *nucleus.Module[contractConfig]) { m.Config.Mode = "medium" }},
		{"a database nobody configured", "requires", func(m *nucleus.Module[contractConfig]) { m.Requires = []string{"analytics"} }},
		{"malformed policy row", "policies", func(m *nucleus.Module[contractConfig]) {
			m.Policies = append(m.Policies, nucleus.PolicyRule{Subject: "anonymous", Object: "/items", Action: "get"})
		}},
		{"a policy row about a route the module does not serve", "policies", func(m *nucleus.Module[contractConfig]) {
			m.Policies = append(m.Policies, nucleus.PolicyRule{Subject: "anonymous", Object: "/archive", Action: "read"})
		}},
		{"malformed CSRF exemption", "csrf-exempt", func(m *nucleus.Module[contractConfig]) { m.CSRFExempt = []string{"items"} }},
		{"a CSRF exemption that covers no route", "csrf-exempt", func(m *nucleus.Module[contractConfig]) { m.CSRFExempt = []string{"/elsewhere"} }},
		{"a template that does not parse", "templates", func(m *nucleus.Module[contractConfig]) {
			m.Templates = fstest.MapFS{"page.html": {Data: []byte(`<p>{{ .name </p>`)}}
		}},
		{"a model that is not a struct", "models", func(m *nucleus.Module[contractConfig]) { m.Models = []any{42} }},
		{"a failing OnStart", "start", func(m *nucleus.Module[contractConfig]) {
			m.OnStart = func(context.Context, nucleus.Runtime, contractConfig) error {
				return errors.New("upstream unreachable")
			}
		}},
		{"migration that does not parse", "migrations", func(m *nucleus.Module[contractConfig]) {
			m.Migrations = fstest.MapFS{"000001_contract.up.sql": {Data: []byte("CREATE TABLE (")}, "000001_contract.down.sql": {Data: []byte("")}}
		}},
		{"a job without a schedule", "jobs", func(m *nucleus.Module[contractConfig]) {
			m.Jobs = func(j nucleus.JobRegistry, _ contractConfig) {
				_ = j.Register("sweep", nucleus.JobSpec{Handler: func(context.Context) error { return nil }})
			}
		}},
		{"a webhook without a handler", "webhooks", func(m *nucleus.Module[contractConfig]) {
			m.Webhooks = func(w nucleus.WebhookRegistry, _ contractConfig) {
				_ = w.Register("/ping", nucleus.WebhookSpec{Secret: "bench"})
			}
		}},
		{"duplicate route", "routes", func(m *nucleus.Module[contractConfig]) {
			m.Routes = func(r nucleus.Router, _ contractConfig) {
				r.Get("/items", ok)
				r.Post("/items", ok)
				r.Get("/items", ok)
			}
		}},
		{"a Resource verb the controller does not implement", "routes", func(m *nucleus.Module[contractConfig]) {
			m.Routes = func(r nucleus.Router, _ contractConfig) {
				r.Resource("/items", struct{}{}, nucleus.Methods(nucleus.Index))
			}
		}},
		{"failing OnShutdown", "shutdown", func(m *nucleus.Module[contractConfig]) {
			m.OnShutdown = func(context.Context, nucleus.Runtime, contractConfig) error { return errors.New("left a file open") }
		}},
	}
}

func brokenModule(defect string) brokenCase {
	for _, c := range brokenModules() {
		if c.defect == defect {
			return c
		}
	}
	panic("no broken module " + defect)
}

// BenchThing is the model the data probes make rows of.
type BenchThing struct {
	model.BaseModel
	Name string `db:"column:name" json:"name"`
	Size int    `db:"column:size" json:"size"`
}

func thingsModule() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{Name: "things", Prefix: "/things", Models: []any{&BenchThing{}}}.Build()
}

// emittingServer boots an application whose routes emit the four things the
// doubles capture — a mail, a file, a job and an outgoing call — the way an
// application's own handlers would.
var emitRuntime nucleus.Runtime

func emittingServer(t *testing.T, webhookURL string) *nucleustest.Server {
	t.Helper()
	m := nucleus.Module[struct{}]{
		Name:   "emit",
		Prefix: "/emit",
		Jobs: func(j nucleus.JobRegistry, _ struct{}) {
			_ = j.Register("bench.tick", nucleus.JobSpec{Every: time.Hour, Handler: func(context.Context) error { return nil }})
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/mail", func(c *nucleus.Context) error {
				return emitRuntime.Mailer().Send(c.Request.Context(), mail.Message{To: []string{"to@example.test"}, Subject: "bench", Body: "hi"})
			})
			r.Post("/upload", func(c *nucleus.Context) error {
				_, err := emitRuntime.Storage().Put(c.Request.Context(), "bench/hello.txt", strings.NewReader("hello"), storage.PutOptions{ContentType: "text/plain"})
				if err != nil {
					return err
				}
				return c.NoContent()
			})
			r.Post("/enqueue", func(c *nucleus.Context) error {
				id, err := emitRuntime.Tasks().EnqueueJSON("bench.report", map[string]string{"kind": "nightly"})
				if err != nil {
					return err
				}
				return c.JSON(http.StatusAccepted, map[string]string{"id": id})
			})
			r.Post("/notify", func(c *nucleus.Context) error {
				resp, err := http.Post(webhookURL+"/hooks/bench", "application/json", strings.NewReader(`{"bench":1}`))
				if err != nil {
					return err
				}
				_ = resp.Body.Close()
				return c.JSON(http.StatusOK, map[string]int{"upstream": resp.StatusCode})
			})
		},
	}.Build()
	a := buildWith(t, nil, m)
	a.Config.Storage.Provider = "memory"
	srv := nucleustest.StartApp(t, a)
	emitRuntime = srv.Runtime()
	return srv
}
