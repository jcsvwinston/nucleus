// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package sentry

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/observe"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/router/interceptor"
)

// No test here talks to Sentry. The DSN points at fakeSentry, an
// httptest server that accepts what the SDK posts to a project's envelope
// endpoint and keeps the events, so the tests read the event exactly as it
// would leave the process.

type fakeSentry struct {
	srv    *httptest.Server
	mu     sync.Mutex
	events []map[string]any
}

func newFakeSentry(t *testing.T) *fakeSentry {
	t.Helper()
	f := &fakeSentry{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodPost && r.URL.Path == "/api/42/envelope/" {
			f.mu.Lock()
			f.events = append(f.events, envelopeEvents(body)...)
			f.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// dsn is a DSN whose host is the fake: public key "benchkey", project 42.
func (f *fakeSentry) dsn() string {
	return "http://benchkey@" + strings.TrimPrefix(f.srv.URL, "http://") + "/42"
}

// envelopeEvents reads the event items of an envelope: a header line, then
// an item header and its payload per item, one JSON document per line.
func envelopeEvents(body []byte) []map[string]any {
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	var out []map[string]any
	for i := 1; i+1 < len(lines); i += 2 {
		var header struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(lines[i], &header) != nil || header.Type != "event" {
			continue
		}
		var event map[string]any
		if json.Unmarshal(lines[i+1], &event) == nil {
			out = append(out, event)
		}
	}
	return out
}

// waitFor returns the events once n have arrived, failing after ten
// seconds: the SDK sends on its own goroutine.
func (f *fakeSentry) waitFor(t *testing.T, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.mu.Lock()
		got := append([]map[string]any(nil), f.events...)
		f.mu.Unlock()
		if len(got) >= n || time.Now().After(deadline) {
			if len(got) < n {
				t.Fatalf("%d event(s) reached the fake Sentry, want %d", len(got), n)
			}
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// count is how many events arrived, after giving a stray one time to land.
func (f *fakeSentry) count() int {
	time.Sleep(300 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// app is the framework's router with the interceptor chain built the way
// the framework builds http_interceptors, an identity stand-in in front of
// it (the bearer decode runs there in an application), and handlers that
// fail every way a handler can.
func app(t *testing.T, cfg map[string]any) http.Handler {
	t.Helper()
	chain, err := interceptor.Build([]string{Name}, map[string]map[string]any{Name: cfg})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	r := router.New(slog.New(slog.DiscardHandler))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if id := req.Header.Get("X-Test-User"); id != "" {
				req = req.WithContext(observe.CtxWithUserID(req.Context(), id))
			}
			next.ServeHTTP(w, req)
		})
	})
	for _, ic := range chain {
		r.Use(router.Middleware(ic))
	}
	r.Get("/items/{id}", func(*router.Context) error { return errors.New("database exploded") })
	r.Get("/explode", func(*router.Context) error { panic("kaboom") })
	r.Get("/missing", func(*router.Context) error { return gferrors.NotFound("item", "7") })
	r.Get("/abort", func(*router.Context) error { panic(http.ErrAbortHandler) })
	r.Get("/fine", func(c *router.Context) error { return c.JSON(http.StatusOK, "fine") })
	return r
}

func send(t *testing.T, h http.Handler, path string, header http.Header) *http.Response {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The /abort route closes the connection on purpose.
		return nil
	}
	_ = resp.Body.Close()
	return resp
}

func field(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[p]
	}
	return cur
}

func exception(event map[string]any) map[string]any {
	values, _ := field(event, "exception", "values").([]any)
	if len(values) == 0 {
		// The SDK writes the list bare in newer protocols.
		values, _ = event["exception"].([]any)
	}
	if len(values) == 0 {
		return nil
	}
	last, _ := values[len(values)-1].(map[string]any)
	return last
}

// A handler's error answered with a 500 becomes one event carrying the
// request: method, route template, status, request id, user, and a request
// scrubbed the way a log line is.
func TestHandlerErrorIsReportedWithTheRequest(t *testing.T) {
	fake := newFakeSentry(t)
	h := app(t, map[string]any{
		"dsn": fake.dsn(), "environment": "bench", "release": "bench-1",
		"redact_extra_keys": []any{"X-Internal"},
	})
	resp := send(t, h, "/items/42?page=2&token=s3cret&api_key=k3y", http.Header{
		"Authorization": {"Bearer eyJhbGciOiJIUzI1NiJ9.e30.sig"},
		"X-Api-Key":     {"nk_live_abc"},
		"Cookie":        {"session=abc123"},
		"X-Internal":    {"internal-value"},
		"X-Trace-Label": {"keep-me"},
		"X-Test-User":   {"user-7"},
	})
	if resp == nil || resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("the request was not answered 500: %+v", resp)
	}
	event := fake.waitFor(t, 1)[0]

	ex := exception(event)
	if ex == nil || ex["value"] != "database exploded" {
		t.Fatalf("exception = %v, want the handler's error", ex)
	}
	want := map[string]string{
		"http.method": "GET", "http.route": "/items/{id}", "http.status_code": "500",
		"nucleus.source": "handler_error", "request_id": resp.Header.Get("X-Request-Id"),
	}
	for k, v := range want {
		if got := field(event, "tags", k); got != v || v == "" {
			t.Errorf("tag %s = %v, want %q", k, got, v)
		}
	}
	for path, v := range map[string]string{
		"level": "error", "transaction": "GET /items/{id}", "environment": "bench", "release": "bench-1",
	} {
		if got := field(event, path); got != v {
			t.Errorf("%s = %v, want %q", path, got, v)
		}
	}
	if got := field(event, "user", "id"); got != "user-7" {
		t.Errorf("user.id = %v, want the user the framework attributes the request to", got)
	}
	if got := field(event, "user", "ip_address"); got != nil {
		t.Errorf("user.ip_address = %v: the client's address does not leave", got)
	}

	req, _ := event["request"].(map[string]any)
	if req == nil {
		t.Fatal("the event carries no request")
	}
	if u, _ := req["url"].(string); strings.Contains(u, "?") || !strings.HasSuffix(u, "/items/42") {
		t.Errorf("request.url = %q: the path, without the query", u)
	}
	q, err := url.ParseQuery(req["query_string"].(string))
	if err != nil {
		t.Fatalf("request.query_string: %v", err)
	}
	if q.Get("page") != "2" || q.Get("token") != observe.RedactionPlaceholder || q.Get("api_key") != observe.RedactionPlaceholder {
		t.Errorf("query = %v: page kept, token and api_key redacted", q)
	}
	headers, _ := req["headers"].(map[string]any)
	for name, wantValue := range map[string]string{
		"Authorization": observe.RedactionPlaceholder,
		"X-Api-Key":     observe.RedactionPlaceholder,
		"X-Internal":    observe.RedactionPlaceholder,
		"X-Trace-Label": "keep-me",
	} {
		if got := headers[name]; got != wantValue {
			t.Errorf("header %s = %v, want %q", name, got, wantValue)
		}
	}
	if _, ok := headers["Cookie"]; ok {
		t.Error("the Cookie header left the process")
	}
	for _, absent := range []string{"cookies", "data", "env"} {
		if v, ok := req[absent]; ok {
			t.Errorf("request.%s = %v: it must not leave the process", absent, v)
		}
	}
	raw, _ := json.Marshal(event)
	for _, secret := range []string{"eyJhbGciOiJIUzI1NiJ9", "nk_live_abc", "session=abc123", "s3cret", "k3y", "internal-value"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("the event carries %q", secret)
		}
	}
}

// A panic is reported at level fatal, and still answered by the framework:
// the interceptor re-panics, so the recoverer writes the 500 and logs the
// stack as it always did.
func TestPanicIsReportedAndStillAnswered(t *testing.T) {
	fake := newFakeSentry(t)
	h := app(t, map[string]any{"dsn": fake.dsn()})
	resp := send(t, h, "/explode", nil)
	if resp == nil || resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("a panic must still be answered 500, got %+v", resp)
	}
	event := fake.waitFor(t, 1)[0]
	ex := exception(event)
	if ex == nil || ex["type"] != "panic" || ex["value"] != "kaboom" {
		t.Fatalf("exception = %v, want type panic, value kaboom", ex)
	}
	if event["level"] != "fatal" || field(event, "tags", "nucleus.source") != "panic" ||
		field(event, "tags", "http.route") != "/explode" || field(event, "tags", "http.status_code") != "500" {
		t.Errorf("event level %v, tags %v", event["level"], event["tags"])
	}
	frames, _ := field(ex, "stacktrace", "frames").([]any)
	if len(frames) == 0 {
		t.Error("a panic is reported without its stack")
	}
}

// What the application answered on purpose is not an error to report: a
// DomainError, a deliberate abort, a 200.
func TestAnswersAreNotReported(t *testing.T) {
	fake := newFakeSentry(t)
	h := app(t, map[string]any{"dsn": fake.dsn()})
	if resp := send(t, h, "/missing", nil); resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /missing: %+v", resp)
	}
	send(t, h, "/abort", nil)
	send(t, h, "/fine", nil)
	if n := fake.count(); n != 0 {
		t.Fatalf("%d event(s) sent for requests that did not fail", n)
	}
}

// An empty DSN reads SENTRY_DSN, the variable Sentry documents.
func TestTheDSNComesFromTheEnvironmentWhenEmpty(t *testing.T) {
	fake := newFakeSentry(t)
	t.Setenv("SENTRY_DSN", fake.dsn())
	send(t, app(t, map[string]any{"dsn": ""}), "/items/1", nil)
	fake.waitFor(t, 1)
}

// No DSN anywhere: the interceptor is in place, sends nothing, and the boot
// says so once.
func TestNoDSNSendsNothingAndSaysSo(t *testing.T) {
	t.Setenv("SENTRY_DSN", "")
	rep, err := newReporter(Config{SampleRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logBoot(slog.New(slog.NewTextHandler(&buf, nil)), rep)
	if !strings.Contains(buf.String(), "sends nothing") {
		t.Errorf("the boot line does not say nothing is sent: %s", buf.String())
	}
	h := app(t, nil)
	if resp := send(t, h, "/items/1", nil); resp == nil || resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("the application must answer as it would without the interceptor: %+v", resp)
	}
}

// The boot line names where events go — host and project — and never the
// key the DSN carries.
func TestTheBootLineDoesNotPrintTheKey(t *testing.T) {
	fake := newFakeSentry(t)
	rep, err := newReporter(Config{DSN: fake.dsn(), SampleRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	logBoot(slog.New(slog.NewTextHandler(&buf, nil)), rep)
	if !strings.Contains(buf.String(), "project=42") || strings.Contains(buf.String(), "benchkey") {
		t.Errorf("boot line: %s", buf.String())
	}
}

// The configuration is strict: a key this package does not declare, a
// sample rate that is not a fraction and a DSN that does not parse fail the
// boot — and the refusal never repeats the DSN.
func TestConfigurationIsStrict(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"a misspelled key", map[string]any{"dns": "https://k@o0.ingest.sentry.io/1"}, "dns"},
		{"a rate above one", map[string]any{"sample_rate": 2}, "sample_rate"},
		{"a DSN without a host", map[string]any{"dsn": "https://secret-key-material@/1"}, "not a Sentry DSN"},
		{"a DSN that is not a URL", map[string]any{"dsn": "::secret-key-material"}, "does not parse as a URL"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := interceptor.Build([]string{Name}, map[string]map[string]any{Name: c.cfg})
			if err == nil {
				t.Fatal("the boot must fail")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the refusal does not name %q: %v", c.want, err)
			}
			if strings.Contains(err.Error(), "secret-key-material") {
				t.Errorf("the refusal repeats the DSN: %v", err)
			}
		})
	}
}

// The writer handed down keeps what a handler behind it needs: Flush for a
// stream, Hijack for a WebSocket upgrade, Unwrap for http.ResponseController.
func TestTheWriterKeepsStreamingAndUpgrades(t *testing.T) {
	var w http.ResponseWriter = &writer{ResponseWriter: httptest.NewRecorder()}
	if _, ok := w.(http.Flusher); !ok {
		t.Error("not a Flusher")
	}
	if _, ok := w.(http.Hijacker); !ok {
		t.Error("not a Hijacker")
	}
	if _, ok := w.(interface{ Unwrap() http.ResponseWriter }); !ok {
		t.Error("no Unwrap")
	}
	// interceptor.ErrorReporter, written out: this module builds against
	// framework releases that predate the interface.
	if _, ok := w.(interface{ ReportError(*http.Request, error) }); !ok {
		t.Error("not an error reporter")
	}
}
