// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
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
	re := regexp.MustCompile(`(?m)^func (\([^)]*\) )?(New)?(Factory|Make[A-Z]|Build[A-Z]|Fixture)`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) > 0 {
		t.Logf("factory-like API in %v", files)
		return present
	}
	t.Log("pkg/nucleustest has no factories: every test inserts its rows by hand")
	return absent
}

// TK-07: a transaction per test, rolled back on cleanup.
func probeTxPerTest(t *testing.T, _ *env) verdict {
	if n, ok := anyMethod(kitMethods(), "Tx", "Transaction", "InTx", "WithinTx", "Rollback"); ok {
		t.Logf("transaction helper: %s", n)
		return present
	}
	t.Log("no transaction-per-test helper; TempSQLite gives each test its own database file instead, which is isolation by copy rather than by rollback")
	return absent
}

// TK-08: a mail double that captures what the application sent.
func probeMailCapture(t *testing.T, _ *env) verdict {
	providers := mail.RegisteredProviders()
	for _, p := range providers {
		switch strings.ToLower(p) {
		case "memory", "capture", "test", "record", "inmemory":
			t.Logf("capturing mail provider: %s", p)
			return present
		}
	}
	if n, ok := anyMethod(kitMethods(), "Mail", "Sent", "Mails", "Outbox"); ok {
		t.Logf("kit exposes %s", n)
		return partial
	}
	t.Logf("mail providers registered: %v — noop discards, smtp sends; nothing a test can read back", providers)
	return absent
}

// TK-09: a storage double the test can read back.
func probeStorageCapture(t *testing.T, e *env) verdict {
	names := provider.Registered()
	if n, ok := anyMethod(kitMethods(), "Storage", "Uploads", "Files", "Stored"); ok {
		t.Logf("kit exposes %s", n)
		return present
	}
	store := e.server().Runtime().Storage()
	if store == nil {
		t.Logf("storage providers: %v; the runtime has no store", names)
		return absent
	}
	sm := methodNames(store)
	if n, ok := anyMethod(sm, "Open", "Get", "Read", "List", "Stat", "Exists"); ok {
		t.Logf("storage providers: %v; the real store is reachable through Runtime().Storage() and readable with %s — a test can look, but nothing captures for it (methods: %v)", names, n, sm)
		return partial
	}
	t.Logf("storage providers: %v; store methods: %v", names, sm)
	return absent
}

// TK-10: a tasks double — the test sees what was enqueued without running it.
func probeTasksCapture(t *testing.T, _ *env) verdict {
	if n, ok := anyMethod(kitMethods(), "Tasks", "Enqueued", "Jobs", "Queue"); ok {
		t.Logf("kit exposes %s", n)
		return present
	}
	// The jobs runtime only exists once a module registers a job; a
	// default application has none, so this probe mounts one.
	jobs := nucleus.Module[struct{}]{
		Name:   "benchjobs",
		Prefix: "/benchjobs",
		Jobs: func(j nucleus.JobRegistry, _ struct{}) {
			_ = j.Register("bench.tick", nucleus.JobSpec{Every: time.Hour, Handler: func(context.Context) error { return nil }})
		},
	}.Build()
	srv := startWith(t, jobs)
	rt := srv.Runtime()
	if rt.Tasks() == nil {
		t.Log("even with a job registered the runtime has no task manager the test can reach")
		return absent
	}
	insp, ok := nucleus.TaskInspectorFrom(rt)
	if !ok {
		t.Logf("the task manager is %T and exposes no inspector", rt.Tasks())
		return absent
	}
	snap := insp.InspectRuntime()
	t.Logf("TaskInspectorFrom reads the runtime — enabled=%v queues=%d workers=%d — an operations view; nothing lists enqueued payloads for a test to assert on", snap.Enabled, snap.TotalQueues, snap.TotalWorkers)
	return partial
}

// TK-11: a double for outgoing HTTP the application makes.
func probeOutboundHTTPDouble(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`RoundTripper|httptest\.|Transport|FakeHTTP|HTTPRecorder`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) > 0 {
		t.Logf("outbound HTTP double in %v", files)
		return present
	}
	t.Log("nothing in the kit intercepts the HTTP the application makes to other services")
	return absent
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

// TK-14: the generated code ships a test that uses the kit.
func probeStarterShipsTest(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`nucleustest\.Start`)
	files := sourceMatches(t, "internal/cli", re)
	if len(files) == 0 {
		t.Log("no CLI template writes a test on the kit")
		return absent
	}
	t.Logf("templates that write a kit test: %v", files)
	return present
}

// TK-15: contract tests for a module — a kit that checks a ModuleSpec against
// what the framework expects of it.
func probeModuleContractKit(t *testing.T, _ *env) verdict {
	re := regexp.MustCompile(`func (Check|Verify|Conform|Assert)Module`)
	if files := sourceMatches(t, "pkg/nucleustest", re); len(files) > 0 {
		t.Logf("module contract helpers in %v", files)
		return present
	}
	t.Log("the kit boots an application; it does not check a module against the contract (names, prefix, requires, migrations, hooks) on its own")
	return absent
}
