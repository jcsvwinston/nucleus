// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package interceptor_test

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/router/interceptor"
)

// reportingWriter is the writer an error-reporting interceptor hands down.
type reportingWriter struct {
	http.ResponseWriter
	sink *reports
}

func (w *reportingWriter) ReportError(r *http.Request, err error) {
	w.sink.add(w.sink.tag + ":" + err.Error() + "@" + router.RouteFromContext(r.Context()))
}

type reports struct {
	tag string
	mu  sync.Mutex
	got []string
	// panicky makes ReportError panic after recording.
	panicky bool
}

func (s *reports) add(line string) {
	s.mu.Lock()
	s.got = append(s.got, line)
	s.mu.Unlock()
	if s.panicky {
		panic("reporter exploded")
	}
}

func (s *reports) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

// registerReporter registers an interceptor whose writer implements
// interceptor.ErrorReporter.
func registerReporter(t *testing.T, name string, sink *reports) {
	t.Helper()
	sink.tag = name
	reg(t, name, func(interceptor.Config) (interceptor.Interceptor, error) {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(&reportingWriter{ResponseWriter: w, sink: sink}, r)
			})
		}, nil
	})
}

// registerPassThrough registers an interceptor that hands the writer it
// received down unchanged.
func registerPassThrough(t *testing.T, name string) {
	t.Helper()
	reg(t, name, func(interceptor.Config) (interceptor.Interceptor, error) {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
		}, nil
	})
}

func reg(t *testing.T, name string, f interceptor.Factory) {
	t.Helper()
	if err := interceptor.Register(name, f); err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
	t.Cleanup(func() { interceptor.Unregister(name) })
}

// app is the framework router with the chain mounted the way pkg/app mounts
// it, and routes that fail in every way a handler can.
func app(t *testing.T, names ...string) http.Handler {
	t.Helper()
	chain, err := interceptor.Build(names, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// No router-wide deadline: the route below that declares its own runs
	// its handler on a goroutine of its own, behind a buffered writer.
	r := router.New(slog.New(slog.DiscardHandler), router.WithTimeout(0))
	for _, ic := range chain {
		r.Use(router.Middleware(ic))
	}
	r.Get("/items/{id}", func(*router.Context) error { return errors.New("database exploded") })
	r.Get("/missing", func(*router.Context) error { return gferrors.NotFound("item", "7") })
	r.Get("/teapot", func(*router.Context) error { return router.NewHTTPError(http.StatusTeapot, "short and stout") })
	r.Group(func(g *router.Mux) {
		g.Use(router.Timeout(5 * time.Second))
		g.Get("/slow/{id}", func(*router.Context) error { return errors.New("slow failure") })
	})
	return r
}

func get(h http.Handler, path string) int {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

// The error behind a 500 reaches the interceptor that asked for it, with the
// request the handler saw (its route template is in the context) — and the
// errors a handler classified itself do not: a 404 or a 418 is an answer,
// not a failure.
func TestErrorReporter_GetsTheErrorBehindA500(t *testing.T) {
	sink := &reports{}
	registerReporter(t, "zzreporter", sink)
	h := app(t, "zzreporter")

	if code := get(h, "/items/42"); code != http.StatusInternalServerError {
		t.Fatalf("GET /items/42 answered %d, want 500", code)
	}
	if code := get(h, "/missing"); code != http.StatusNotFound {
		t.Fatalf("GET /missing answered %d", code)
	}
	if code := get(h, "/teapot"); code != http.StatusTeapot {
		t.Fatalf("GET /teapot answered %d", code)
	}
	want := "zzreporter:database exploded@/items/{id}"
	if got := sink.lines(); len(got) != 1 || got[0] != want {
		t.Fatalf("reports = %q, want exactly [%q]", got, want)
	}
}

// A route with its own Timeout runs its handler on another goroutine behind
// a buffered writer that wraps nothing; the reporter travels in the context,
// so it is still told.
func TestErrorReporter_ReachesAHandlerBehindARouteTimeout(t *testing.T) {
	sink := &reports{}
	registerReporter(t, "zzreporter", sink)
	h := app(t, "zzreporter")
	if code := get(h, "/slow/1"); code != http.StatusInternalServerError {
		t.Fatalf("GET /slow/1 answered %d, want 500", code)
	}
	if got := sink.lines(); len(got) != 1 || got[0] != "zzreporter:slow failure@/slow/{id}" {
		t.Fatalf("reports = %q", got)
	}
}

// An interceptor that hands the request down with the writer it received
// must not make the reporter outside it look like two: every reporter is
// told once. Two reporters are each told once, outermost first.
func TestErrorReporter_EachReporterIsToldOnce(t *testing.T) {
	outer, inner := &reports{}, &reports{}
	registerReporter(t, "zzouter", outer)
	registerPassThrough(t, "zzpass")
	registerReporter(t, "zzinner", inner)
	h := app(t, "zzouter", "zzpass", "zzinner")
	if code := get(h, "/items/1"); code != http.StatusInternalServerError {
		t.Fatalf("answered %d", code)
	}
	if got := outer.lines(); len(got) != 1 {
		t.Errorf("the outer reporter was told %d times: %q", len(got), got)
	}
	if got := inner.lines(); len(got) != 1 {
		t.Errorf("the inner reporter was told %d times: %q", len(got), got)
	}
}

// A reporter is a bystander: one that panics neither stops the next one nor
// turns the 500 into a dropped connection.
func TestErrorReporter_APanickingReporterChangesNothing(t *testing.T) {
	bad, good := &reports{panicky: true}, &reports{}
	registerReporter(t, "zzbad", bad)
	registerReporter(t, "zzgood", good)
	h := app(t, "zzbad", "zzgood")
	if code := get(h, "/items/1"); code != http.StatusInternalServerError {
		t.Fatalf("answered %d, want the 500 the error deserves", code)
	}
	if len(bad.lines()) != 1 || len(good.lines()) != 1 {
		t.Fatalf("bad told %d, good told %d; each must be told once", len(bad.lines()), len(good.lines()))
	}
}

// An interceptor that does not ask is not told, and costs nothing: no
// writer of its own, no reporter in the context.
func TestErrorReporter_NotAskedNotTold(t *testing.T) {
	registerPassThrough(t, "zzpass")
	if code := get(app(t, "zzpass"), "/items/1"); code != http.StatusInternalServerError {
		t.Fatalf("answered %d", code)
	}
}

// A name of the catalog that is not in the build is refused naming the
// command that installs it, not as an unknown name.
func TestBuild_ACatalogNameNotInTheBuildNamesNucleusAdd(t *testing.T) {
	_, err := interceptor.Build([]string{"sentry"}, nil)
	if err == nil {
		t.Fatal("an interceptor nothing registered must fail the build")
	}
	for _, want := range []string{"nucleus add sentry", "github.com/jcsvwinston/nucleus/providers/errors-sentry", "ships as its own module"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
}
