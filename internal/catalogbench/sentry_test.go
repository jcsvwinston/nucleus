// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package catalogbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// standInSentry is a Sentry project small enough to read: it accepts what
// the SDK posts to a project's envelope endpoint and keeps the events. The
// starter's DSN points at it, so EN-09 reads the events exactly as they
// would leave the application, and nothing reaches Sentry.
type standInSentry struct {
	srv    *httptest.Server
	mu     sync.Mutex
	events []map[string]any
}

const standInSentryProject = "4242"

func newStandInSentry(tb testing.TB) *standInSentry {
	tb.Helper()
	s := &standInSentry{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodPost && r.URL.Path == "/api/"+standInSentryProject+"/envelope/" {
			s.mu.Lock()
			s.events = append(s.events, sentryEnvelopeEvents(body)...)
			s.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	tb.Cleanup(s.srv.Close)
	return s
}

func (e *env) sentry() *standInSentry {
	e.sentryOnce.Do(func() { e.sentrySrv = newStandInSentry(e.tb) })
	return e.sentrySrv
}

// dsn is the DSN of the stand-in project.
func (s *standInSentry) dsn() string {
	return "http://catalogbench@" + strings.TrimPrefix(s.srv.URL, "http://") + "/" + standInSentryProject
}

// sentryEnvelopeEvents reads the event items of an envelope: a header line,
// then an item header and its payload per item, one JSON document a line.
func sentryEnvelopeEvents(body []byte) []map[string]any {
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

// eventFor waits up to ten seconds for the event whose transaction is
// method+route; the SDK sends on its own goroutine.
func (s *standInSentry) eventFor(transaction string) map[string]any {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, ev := range s.events {
			if ev["transaction"] == transaction {
				s.mu.Unlock()
				return ev
			}
		}
		s.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// benchFailingModule is the code a person's application already has: routes
// that fail. No entry writes them — the failures are the application's — so
// the probe adds them to the project `nucleus add sentry` left: one handler
// returns an error the framework cannot classify, one panics.
const benchFailingModule = `package main

import (
	"errors"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

func benchFailures() nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:   "benchfail",
		Prefix: "/bench",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/orders/{id}", func(*nucleus.Context) error { return errors.New("catalogbench: the orders table is gone") })
			r.Get("/panic", func(*nucleus.Context) error { panic("catalogbench: a nil map") })
		},
	}.Build()
}
`

func addBenchFailures(t *testing.T, dir string) {
	must(t, os.WriteFile(filepath.Join(dir, "benchfail.go"), []byte(benchFailingModule), 0o644))
	mainGo := filepath.Join(dir, "main.go")
	src, err := os.ReadFile(mainGo)
	must(t, err)
	edited := strings.Replace(string(src), "\t\tStart(); err != nil", "\t\tMount(benchFailures()).\n\t\tStart(); err != nil", 1)
	if edited == string(src) {
		t.Fatalf("the starter's main.go has no Start() to mount the failing module before:\n%s", src)
	}
	must(t, os.WriteFile(mainGo, []byte(edited), 0o644))
}

// sentryEmptyDSN is the DSN line the recipe writes; the person sets their
// project's, and the probe sets the stand-in's.
const sentryEmptyDSN = `dsn: ""`

// sentryReports is EN-09's wiring check: a handler's error and a panic, each
// driven through the running starter, arrive at the stand-in project as
// events that carry the request — method, route template, status, request
// id — and the 500 is still what the client gets.
func sentryReports(t *testing.T, e *env, run coreRun) (bool, string) {
	base := fmt.Sprintf("http://127.0.0.1:%d", run.port)
	var evidence []string
	for _, c := range []struct {
		path, route, source, level, value string
	}{
		{"/bench/orders/42", "/bench/orders/{id}", "handler_error", "error", "catalogbench: the orders table is gone"},
		{"/bench/panic", "/bench/panic", "panic", "fatal", "catalogbench: a nil map"},
	} {
		resp, err := http.Get(base + c.path)
		if err != nil {
			return false, err.Error()
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			return false, fmt.Sprintf("GET %s answered %d, not 500", c.path, resp.StatusCode)
		}
		ev := e.sentry().eventFor("GET " + c.route)
		if ev == nil {
			return false, fmt.Sprintf("GET %s answered 500 and no event for GET %s reached the stand-in Sentry", c.path, c.route)
		}
		tags, _ := ev["tags"].(map[string]any)
		got := map[string]any{
			"level": ev["level"], "http.status_code": tags["http.status_code"], "http.route": tags["http.route"],
			"nucleus.source": tags["nucleus.source"], "request_id": tags["request_id"],
		}
		want := map[string]any{
			"level": c.level, "http.status_code": "500", "http.route": c.route,
			"nucleus.source": c.source, "request_id": resp.Header.Get("X-Request-Id"),
		}
		for k, v := range want {
			if got[k] != v || v == "" {
				return false, fmt.Sprintf("the event for GET %s has %s=%v, want %v", c.path, k, got[k], v)
			}
		}
		if raw, _ := json.Marshal(ev["exception"]); !bytes.Contains(raw, []byte(c.value)) {
			return false, fmt.Sprintf("the event for GET %s does not carry %q: %s", c.path, c.value, firstLines(string(raw), 2))
		}
		evidence = append(evidence, fmt.Sprintf("GET %s → 500 and an event at level %s (%s, route %s, request id %s)",
			c.path, c.level, c.source, c.route, resp.Header.Get("X-Request-Id")))
	}
	return true, strings.Join(evidence, "; ")
}

// sentryConfig is the selection CAT-01 boots the starter with when the
// module is not added: the interceptor in the request path and its DSN.
const sentryConfig = "http_interceptors: [sentry]\ninterceptors:\n  sentry:\n    dsn: http://catalogbench@127.0.0.1:1/4242\n"
