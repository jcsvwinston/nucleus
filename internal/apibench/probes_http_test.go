// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package apibench

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// The http family asks what a handler author gets from the routing layer —
// the listón is Echo and Goa: typed binding of body, query, path and headers
// with one error shape (problem+json), content negotiation, declarative
// versioning, timeouts where the route says. Every probe boots an
// application whose routes USE the surface under test — a handler that binds
// the query, one that negotiates, a module that declares a version — calls
// it over HTTP and reads the answer: a measurement that trusted a method's
// name would measure the name.

func contextMethods() []string { return methodNames((*nucleus.Context)(nil)) }

// httpFixtures is the module the http probes call: one route per surface,
// each written the way an application author would write it.
func httpFixtures() nucleus.ModuleSpec {
	type filter struct {
		Page int      `query:"page" validate:"min=1"`
		Tags []string `query:"tag"`
	}
	type item struct {
		ID   int64  `path:"id" json:"id"`
		Name string `json:"name"`
	}
	type trace struct {
		TraceID string `header:"X-Trace-Id"`
		Retry   int    `header:"X-Retry"`
	}
	type note struct {
		ID    int    `json:"id" xml:"id"`
		Title string `json:"title" xml:"title"`
	}
	return nucleus.Module[struct{}]{
		Name:   "httpbench",
		Prefix: "/h",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/filter", func(c *nucleus.Context) error {
				var in filter
				if err := c.BindQuery(&in); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, map[string]any{"page": in.Page, "tags": in.Tags})
			})
			r.Get("/items/{id}", func(c *nucleus.Context) error {
				var in item
				if err := c.BindPath(&in); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, in)
			})
			r.Put("/items/{id}", func(c *nucleus.Context) error {
				var in item
				if err := c.BindRequest(&in); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, in)
			})
			r.Get("/trace", func(c *nucleus.Context) error {
				var in trace
				if err := c.BindHeaders(&in); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, map[string]any{"trace": in.TraceID, "retry": in.Retry})
			})
			r.Get("/note", func(c *nucleus.Context) error {
				return c.Negotiate(http.StatusOK, note{ID: 1, Title: "hello"})
			})
			r.Get("/table", func(c *nucleus.Context) error {
				// A map has no XML form: a client that accepts only XML
				// cannot be answered.
				return c.Negotiate(http.StatusOK, map[string]int{"n": 1})
			})
			r.Get("/page", func(c *nucleus.Context) error {
				return c.RawHTML(http.StatusOK, "<p>raw</p>")
			})
			r.Get("/missing", func(c *nucleus.Context) error {
				var in filter
				return c.BindQuery(&in) // page=0 → a validation failure
			})
		},
	}.Build()
}

// call sends one request with the given Accept (and optional body and
// headers) and returns status, content type, headers and body.
func call(t *testing.T, srv *nucleustest.Server, method, path, accept, body string, headers map[string]string) (int, string, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL(path), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header, raw
}

// envelopeOf reads the framework's envelope: {"error": {"code", "message",
// "details"?}}. ok is false for anything else.
func envelopeOf(raw []byte) (code, message string, details map[string]any, ok bool) {
	var env struct {
		Error *struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) != nil || env.Error == nil || env.Error.Code == "" {
		return "", "", nil, false
	}
	return env.Error.Code, env.Error.Message, env.Error.Details, true
}

// problemOf reads an RFC 9457 document; ok is false unless the standard
// members and the framework's code are there.
func problemOf(raw []byte) (p map[string]any, ok bool) {
	if json.Unmarshal(raw, &p) != nil {
		return nil, false
	}
	for _, k := range []string{"type", "title", "status", "code"} {
		if _, has := p[k]; !has {
			return p, false
		}
	}
	return p, true
}

// HT-01: a JSON body binds into a struct and is validated by its tags.
func probeJSONBinding(t *testing.T, e *env) verdict {
	ok, raw := e.do(t, http.MethodPost, "/bench/echo", map[string]any{"name": "one", "age": 3}, nil)
	if ok.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"name":"one"`) {
		t.Logf("valid body → %d %.200s", ok.StatusCode, raw)
		return absent
	}
	bad, raw := e.do(t, http.MethodPost, "/bench/echo", map[string]any{"age": 3}, nil)
	if bad.StatusCode != http.StatusBadRequest && bad.StatusCode != http.StatusUnprocessableEntity {
		t.Logf("a body missing a required field → %d %.200s", bad.StatusCode, raw)
		return partial
	}
	return present
}

// HT-02: query parameters bind into a struct, typed — and a value that does
// not convert, or a struct that does not validate, answers an error that
// names the query parameter.
func probeQueryBinding(t *testing.T, _ *env) verdict {
	srv := startWith(t, httpFixtures())
	status, _, _, raw := call(t, srv, http.MethodGet, "/h/filter?page=2&tag=a&tag=b", "application/json", "", nil)
	if status != http.StatusOK || !strings.Contains(string(raw), `"page":2`) || !strings.Contains(string(raw), `"tags":["a","b"]`) {
		t.Logf("GET /h/filter?page=2&tag=a&tag=b → %d %.200s", status, raw)
		return absent
	}
	status, _, _, raw = call(t, srv, http.MethodGet, "/h/filter?page=two", "application/json", "", nil)
	code, _, details, ok := envelopeOf(raw)
	if status != http.StatusBadRequest || !ok || details["page"] == nil {
		t.Logf("a page that is not a number → %d %s %.200s: the failure must name the parameter", status, code, raw)
		return partial
	}
	status, _, _, raw = call(t, srv, http.MethodGet, "/h/filter?page=0", "application/json", "", nil)
	code, _, details, ok = envelopeOf(raw)
	if status != http.StatusUnprocessableEntity || !ok || code != "VALIDATION_FAILED" || details["page"] == nil {
		t.Logf("page=0 against validate:\"min=1\" → %d %s %.200s", status, code, raw)
		return partial
	}
	t.Logf("typed query binding: page and repeated tags bound; conversion → 400 naming \"page\"; validation → 422 naming \"page\"")
	return present
}

// HT-03: path parameters bind typed — and the one input type of BindRequest
// keeps the id the route was called with whatever the body says.
func probePathBinding(t *testing.T, _ *env) verdict {
	srv := startWith(t, httpFixtures())
	status, _, _, raw := call(t, srv, http.MethodGet, "/h/items/42", "application/json", "", nil)
	if status != http.StatusOK || !strings.Contains(string(raw), `"id":42`) {
		t.Logf("GET /h/items/42 → %d %.200s", status, raw)
		return absent
	}
	status, _, _, raw = call(t, srv, http.MethodGet, "/h/items/forty-two", "application/json", "", nil)
	if _, _, details, ok := envelopeOf(raw); status != http.StatusBadRequest || !ok || details["id"] == nil {
		t.Logf("a non-numeric id → %d %.200s: the failure must name the path parameter", status, raw)
		return partial
	}
	status, _, _, raw = call(t, srv, http.MethodPut, "/h/items/42", "application/json", `{"id":999,"name":"renamed"}`, nil)
	if status != http.StatusOK || !strings.Contains(string(raw), `"id":42`) || !strings.Contains(string(raw), `"name":"renamed"`) {
		t.Logf("PUT /h/items/42 with a body that says id 999 → %d %.200s: the path must win", status, raw)
		return partial
	}
	t.Log("typed path binding: id 42 bound as int64; a non-numeric id → 400 naming \"id\"; BindRequest keeps the path id over the body's")
	return present
}

// HT-04: headers bind typed.
func probeHeaderBinding(t *testing.T, _ *env) verdict {
	srv := startWith(t, httpFixtures())
	status, _, _, raw := call(t, srv, http.MethodGet, "/h/trace", "application/json", "",
		map[string]string{"x-trace-id": "t-1", "X-Retry": "3"})
	if status != http.StatusOK || !strings.Contains(string(raw), `"trace":"t-1"`) || !strings.Contains(string(raw), `"retry":3`) {
		t.Logf("GET /h/trace → %d %.200s", status, raw)
		return absent
	}
	status, _, _, raw = call(t, srv, http.MethodGet, "/h/trace", "application/json", "", map[string]string{"X-Retry": "many"})
	if _, _, details, ok := envelopeOf(raw); status != http.StatusBadRequest || !ok || details["X-Retry"] == nil {
		t.Logf("X-Retry: many → %d %.200s: the failure must name the header", status, raw)
		return partial
	}
	t.Log("typed header binding: X-Trace-Id (sent lower-case) and X-Retry as int; a non-numeric X-Retry → 400 naming the header")
	return present
}

// HT-05: a validation failure names the field that failed.
func probeStructuredValidationErrors(t *testing.T, e *env) verdict {
	resp, raw := e.do(t, http.MethodPost, "/bench/echo", map[string]any{"age": 3}, nil)
	body := string(raw)
	if resp.StatusCode >= 500 || len(body) == 0 {
		t.Logf("%d %.200s", resp.StatusCode, body)
		return absent
	}
	if !strings.Contains(strings.ToLower(body), "name") {
		t.Logf("the failure does not name the field: %.300s", body)
		return partial
	}
	if jsonKeys(raw) == nil {
		t.Logf("the failure is not a JSON object: %.300s", body)
		return partial
	}
	t.Logf("validation failure: %.300s", body)
	return present
}

// HT-06: errors are problem+json (RFC 9457) — for the client that asks, for
// every error the framework answers (a domain error, a validation failure
// with its fields, the router's own 404), and for every client of an
// application that opted in.
func probeProblemJSON(t *testing.T, e *env) verdict {
	srv := startWith(t, httpFixtures())
	asked := 0
	for _, path := range []string{"/h/items/x", "/h/missing?page=0", "/h/nope"} {
		status, ct, _, raw := call(t, srv, http.MethodGet, path, "application/problem+json", "", nil)
		p, ok := problemOf(raw)
		if st, _ := p["status"].(float64); !strings.HasPrefix(ct, "application/problem+json") || !ok || int(st) != status {
			t.Logf("GET %s asking for problem+json → %d %q %.200s", path, status, ct, raw)
			continue
		}
		if path == "/h/missing?page=0" {
			d, _ := p["details"].(map[string]any)
			if d["page"] == nil {
				t.Logf("the validation failure as a problem lost its fields: %.200s", raw)
				continue
			}
		}
		asked++
	}
	if asked == 0 {
		resp, raw := e.do(t, http.MethodGet, "/bench/notfound", nil, map[string]string{"Accept": "application/problem+json"})
		t.Logf("a domain error answers %d as %q with keys %v even when problem+json is asked for", resp.StatusCode, resp.Header.Get("Content-Type"), jsonKeys(raw))
		return absent
	}
	// The default stays the envelope for a client that does not ask.
	if _, ct, _, raw := call(t, srv, http.MethodGet, "/h/items/x", "application/json", "", nil); !strings.HasPrefix(ct, "application/json") {
		t.Logf("the envelope is no longer the default: %q %.200s", ct, raw)
		return partial
	}
	// The opt-in: problem details for a client that names only JSON.
	a := buildWith(t, func(b *nucleus.AppBuilder) { b.WithProblemDetails() }, httpFixtures())
	opted := nucleustest.StartApp(t, a)
	_, ct, _, raw := call(t, opted, http.MethodGet, "/h/items/x", "application/json", "", nil)
	if _, ok := problemOf(raw); !ok {
		t.Logf("WithProblemDetails: a JSON client still gets %q %.200s", ct, raw)
		return partial
	}
	if asked < 3 {
		return partial
	}
	t.Log("problem+json: asked-for on a binding error, a validation failure (fields in details) and the router's 404; the envelope stays the default; WithProblemDetails makes it every client's")
	return present
}

// HT-07: content negotiation by Accept — one handler, the representation the
// client asked for, and a 406 in the framework's error shape when it accepts
// nothing the handler has.
func probeNegotiation(t *testing.T, _ *env) verdict {
	srv := startWith(t, httpFixtures())
	want := []struct{ accept, ct, body string }{
		{"application/json", "application/json", `"title":"hello"`},
		{"application/xml", "application/xml", "<title>hello</title>"},
		{"text/plain;q=0.2, application/xml;q=0.9", "application/xml", "<title>hello</title>"},
		{"", "application/json", `"title":"hello"`},
	}
	got := 0
	for _, w := range want {
		status, ct, _, raw := call(t, srv, http.MethodGet, "/h/note", w.accept, "", nil)
		if status == http.StatusOK && strings.HasPrefix(ct, w.ct) && strings.Contains(string(raw), w.body) {
			got++
			continue
		}
		t.Logf("Accept %q → %d %q %.120s", w.accept, status, ct, raw)
	}
	if got == 0 {
		return absent
	}
	status, _, _, raw := call(t, srv, http.MethodGet, "/h/table", "application/xml", "", nil)
	if code, _, _, ok := envelopeOf(raw); status != http.StatusNotAcceptable || !ok || code != "NOT_ACCEPTABLE" {
		t.Logf("nothing acceptable → %d %.200s", status, raw)
		return partial
	}
	if got < len(want) {
		return partial
	}
	t.Log("one handler answered JSON, XML and the q-ranked choice, and 406 in the envelope when nothing fits")
	return present
}

// HT-08: declarative API versioning — a module declares its version and the
// framework mounts it there and says, in the standard headers, that it is
// deprecated, when it sunsets and what replaces it; a route group can do the
// same inside a module.
func probeVersioning(t *testing.T, _ *env) verdict {
	sunset := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	v1 := nucleus.Module[struct{}]{
		Name:   "verbench_v1",
		Prefix: "/api",
		Version: nucleus.APIVersion{
			Name:       "v1",
			Deprecated: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			Sunset:     sunset,
			Successor:  "/api/v2/notes",
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/notes", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, []string{"v1"}) })
		},
	}.Build()
	grouped := nucleus.Module[struct{}]{
		Name:   "verbench_group",
		Prefix: "/g",
		Routes: func(r nucleus.Router, _ struct{}) {
			nucleus.Versioned(r, nucleus.APIVersion{Name: "v3", Sunset: sunset}, func(g nucleus.Router) {
				g.Get("/ping", func(c *nucleus.Context) error { return c.String(http.StatusOK, "pong") })
			})
		},
	}.Build()
	srv := startWith(t, v1, grouped)
	status, _, h, raw := call(t, srv, http.MethodGet, "/api/v1/notes", "application/json", "", nil)
	if status != http.StatusOK || !strings.Contains(string(raw), "v1") {
		t.Logf("GET /api/v1/notes → %d %.120s: the module is not mounted under its version", status, raw)
		return absent
	}
	headers := h.Get("Deprecation") != "" && h.Get("Sunset") == sunset.Format(http.TimeFormat) &&
		strings.Contains(h.Get("Link"), `</api/v2/notes>; rel="successor-version"`)
	if !headers {
		t.Logf("mounted under /v1 but the headers say Deprecation %q Sunset %q Link %q", h.Get("Deprecation"), h.Get("Sunset"), h.Get("Link"))
		return partial
	}
	status, _, h, raw = call(t, srv, http.MethodGet, "/g/v3/ping", "", "", nil)
	if status != http.StatusOK || string(raw) != "pong" || h.Get("Sunset") == "" {
		t.Logf("the route-group form: GET /g/v3/ping → %d %q Sunset %q", status, raw, h.Get("Sunset"))
		return partial
	}
	t.Log("Module.Version mounted /api/v1 with Deprecation, Sunset and Link rel=successor-version; nucleus.Versioned did the same for a group")
	return present
}

// HT-09: a timeout where the route says — a route that declares more time
// than the application's request_timeout gets it (the server's
// write_timeout included), and one that declares less is cut first, with
// the answer in the framework's error shape.
func probePerRouteTimeout(t *testing.T, _ *env) verdict {
	mod := nucleus.Module[struct{}]{
		Name:   "timebench",
		Prefix: "/t",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.With(nucleus.Timeout(4*time.Second)).Get("/export", func(c *nucleus.Context) error {
				time.Sleep(1500 * time.Millisecond)
				return c.String(http.StatusOK, "exported")
			})
			r.With(nucleus.Timeout(100*time.Millisecond)).Get("/lookup", func(c *nucleus.Context) error {
				select {
				case <-time.After(2 * time.Second):
				case <-c.Request.Context().Done():
				}
				return c.String(http.StatusOK, "too late")
			})
		},
	}.Build()
	a := buildWith(t, nil, mod)
	a.Config.RequestTimeout = time.Second
	a.Config.WriteTimeout = 1300 * time.Millisecond
	srv := nucleustest.StartApp(t, a)

	start := time.Now()
	status, _, _, raw := call(t, srv, http.MethodGet, "/t/lookup", "application/json", "", nil)
	took := time.Since(start)
	shorter := status == http.StatusServiceUnavailable && took < 800*time.Millisecond
	if code, _, _, ok := envelopeOf(raw); shorter && (!ok || code != "TIMEOUT") {
		t.Logf("the timeout answer is not the framework's error shape: %.200s", raw)
		shorter = false
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL("/t/export"), nil)
	if err != nil {
		t.Fatal(err)
	}
	longer := false
	if resp, err := srv.Client().Do(req); err != nil {
		t.Logf("GET /t/export (4s route timeout, 1s request_timeout, 1.3s write_timeout): %v", err)
	} else {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		longer = resp.StatusCode == http.StatusOK && string(body) == "exported"
		if !longer {
			t.Logf("GET /t/export → %d %.120s", resp.StatusCode, body)
		}
	}
	switch {
	case shorter && longer:
		t.Logf("a 100ms route timeout fired in %v with TIMEOUT; a 4s one outlived the 1s request_timeout and the 1.3s write_timeout", took)
		return present
	case shorter || longer:
		t.Logf("shorter fires first: %v (took %v); longer is honoured: %v", shorter, took, longer)
		return partial
	}
	t.Logf("neither: /t/lookup → %d after %v", status, took)
	return absent
}

// HT-10: an unknown route answers a JSON 404 in the framework's envelope —
// under a module's prefix, outside every module, and the 405 of a path
// registered for other methods — while a browser keeps the plain text.
func probeUnknownRouteJSON(t *testing.T, e *env) verdict {
	resp, raw := e.do(t, http.MethodGet, "/bench/nope", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Logf("unknown route → %d", resp.StatusCode)
		return absent
	}
	code, _, _, under := envelopeOf(raw)
	top, topRaw := e.do(t, http.MethodGet, "/nope", nil, nil)
	topCode, _, _, outside := envelopeOf(topRaw)
	method, methodRaw := e.do(t, http.MethodDelete, "/bench/things", nil, nil)
	methodCode, _, _, wrongMethod := envelopeOf(methodRaw)
	t.Logf("under the prefix: 404 %s %v; outside: %d %s %v; DELETE on a GET route: %d %s %v (Allow %q)",
		code, under, top.StatusCode, topCode, outside, method.StatusCode, methodCode, wrongMethod, method.Header.Get("Allow"))
	if !under && !outside {
		t.Logf("404 body under the module prefix is not the envelope: %.200s", raw)
		return partial
	}
	if !(under && outside && wrongMethod && code == "NOT_FOUND" && methodCode == "METHOD_NOT_ALLOWED" && method.Header.Get("Allow") != "") {
		return partial
	}
	browser, browserRaw := e.do(t, http.MethodGet, "/bench/nope", nil,
		map[string]string{"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"})
	if !strings.HasPrefix(browser.Header.Get("Content-Type"), "text/plain") {
		t.Logf("a browser now gets %q %.120s instead of the plain text", browser.Header.Get("Content-Type"), browserRaw)
		return partial
	}
	return present
}

// HT-11: the raw-HTML writer is named as such; HTML renders a template.
// RawHTML answers the string as text/html, and nucleus.Context.HTML — the
// raw writer under the template renderer's name — is deprecated pointing at
// it.
func probeRawHTMLNamed(t *testing.T, _ *env) verdict {
	if _, ok := anyMethod(contextMethods(), "RawHTML"); !ok {
		t.Logf("nucleus.Context has HTML (raw string) and Render (template) while router.Context.HTML renders a template; no RawHTML. methods: %v", contextMethods())
		return absent
	}
	srv := startWith(t, httpFixtures())
	status, ct, _, raw := call(t, srv, http.MethodGet, "/h/page", "", "", nil)
	if status != http.StatusOK || !strings.HasPrefix(ct, "text/html") || string(raw) != "<p>raw</p>" {
		t.Logf("GET /h/page through RawHTML → %d %q %q", status, ct, raw)
		return partial
	}
	doc := methodDoc(t, filepath.Join(repoRoot(t), "pkg", "nucleus", "context.go"), "Context", "HTML")
	if !strings.Contains(doc, "Deprecated:") || !strings.Contains(doc, "RawHTML") {
		t.Logf("RawHTML exists, but nucleus.Context.HTML is not deprecated in its favour — two names for one job: %q", doc)
		return partial
	}
	t.Log("RawHTML wrote the string as text/html; nucleus.Context.HTML is deprecated pointing at RawHTML and Render")
	return present
}

// methodDoc returns the doc comment of recv.name in file.
func methodDoc(t *testing.T, file, recv, name string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name || fd.Recv == nil || len(fd.Recv.List) == 0 {
			continue
		}
		star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if id, ok := star.X.(*ast.Ident); ok && id.Name == recv && fd.Doc != nil {
			return fd.Doc.Text()
		}
	}
	return ""
}

// HT-12: one error envelope — a domain error, a binding error, the router's
// own 404 and its 405 share one shape, in the envelope and in problem+json.
func probeErrorEnvelopeConsistent(t *testing.T, e *env) verdict {
	type sample struct{ method, path string }
	samples := []sample{
		{http.MethodGet, "/bench/notfound"}, // a domain error
		{http.MethodGet, "/bench/nope"},     // the router's 404 under a prefix
		{http.MethodGet, "/nope"},           // the router's 404 outside every module
		{http.MethodDelete, "/bench/things"},
	}
	_, domain := e.do(t, http.MethodGet, "/bench/notfound", nil, nil)
	_, routed := e.do(t, http.MethodGet, "/bench/nope", nil, nil)
	dk, rk := jsonKeys(domain), jsonKeys(routed)
	if dk == nil || rk == nil {
		t.Logf("domain: %.200s\nrouter: %.200s", domain, routed)
		return absent
	}
	shape := func(raw []byte) string {
		var m map[string]map[string]any
		if json.Unmarshal(raw, &m) != nil || m["error"] == nil {
			return ""
		}
		_, hasCode := m["error"]["code"]
		_, hasMsg := m["error"]["message"]
		if !hasCode || !hasMsg {
			return ""
		}
		return "error{code,message}"
	}
	for _, s := range samples {
		_, raw := e.do(t, s.method, s.path, nil, nil)
		if shape(raw) == "" {
			t.Logf("%s %s answers outside the envelope: %.200s", s.method, s.path, raw)
			return partial
		}
		_, praw := e.do(t, s.method, s.path, nil, map[string]string{"Accept": "application/problem+json"})
		if _, ok := problemOf(praw); !ok {
			t.Logf("%s %s asked for problem+json answers %.200s", s.method, s.path, praw)
			return partial
		}
	}
	t.Logf("a domain error, the router's 404 (under a prefix and outside) and its 405 share {error:{code,message}}, and all four answer problem+json when asked")
	return present
}
