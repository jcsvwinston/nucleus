// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// NU-124: profiling_enabled mounts /debug/pprof, and the only thing that
// keeps heap and goroutine dumps from anyone who reaches the port is the
// default stack's default-deny enforcer — the profiler is deliberately not
// in the bootstrap allow-list. An application built WithoutDefaults() builds
// no enforcer, so the profiles answered anyone, and the boot WARN told the
// operator to "keep it behind a policy" no such application can have. It
// still serves them (QADR-0010: what boots today keeps booting until the
// major), and one ERROR line now says so (refused from v2.0.0 unless an
// explicit opt-in guards it, DEP-2026-018).

// pprofIndexCode asks the profiler's index anonymously.
func pprofIndexCode(a *App) int {
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pprofPrefix+"/", nil))
	return rec.Code
}

func TestWithoutDefaults_Profiling_ServedAndSaysItIsUnguarded(t *testing.T) {
	cfg := testAppConfig()
	cfg.ProfilingEnabled = true
	cfg.LogLevel = "warn" // so a leftover WARN would be seen

	var a *App
	out := captureStdout(t, func() {
		var err error
		a, err = New(cfg, WithoutDefaults())
		if err != nil {
			t.Fatalf("an application that started before NU-124 no longer starts: %v", err)
		}
	})
	defer func() { _ = a.Shutdown(context.Background()) }()

	// Behaviour unchanged before the major: the profiler is served.
	if code := pprofIndexCode(a); code != http.StatusOK {
		t.Fatalf("GET %s/ on WithoutDefaults() with profiling_enabled: %d, want 200 (the profiler is still served)", pprofPrefix, code)
	}

	lines := linesContaining(out, "profiler UNGUARDED")
	if len(lines) != 1 {
		t.Fatalf("want exactly one ERROR line, got %d:\n%s", len(lines), out)
	}
	line := lines[0]
	for _, want := range []string{"level=ERROR", pprofPrefix, "WithoutDefaults()", "heap", "profiling_enabled", "DEP-2026-018", "v2.0.0"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line does not say %q:\n%s", want, line)
		}
	}
	// The WARN that advised a policy this application cannot have is not
	// said next to it.
	if strings.Contains(out, "keep it behind a policy") {
		t.Errorf("the boot log still advises a policy a WithoutDefaults() application cannot have:\n%s", out)
	}
}

// In production the line says what is at stake in so many words: a heap
// dump of this production process, live memory included.
func TestWithoutDefaults_Profiling_ProductionNamesHeapDumps(t *testing.T) {
	cfg := testAppConfig()
	cfg.ProfilingEnabled = true
	cfg.Env = "production"
	cfg.JWTSecret = strings.Repeat("k", 32) + "-production-test-secret"

	out := captureStdout(t, func() {
		a, err := New(cfg, WithoutDefaults())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_ = a.Shutdown(context.Background())
	})
	lines := linesContaining(out, "profiler UNGUARDED")
	if len(lines) != 1 {
		t.Fatalf("want exactly one ERROR line, got %d:\n%s", len(lines), out)
	}
	for _, want := range []string{"level=ERROR", "production", "heap dumps", "live memory", "DEP-2026-018"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the production line does not say %q:\n%s", want, lines[0])
		}
	}
}

// Nothing unguarded, nothing said: the profiler off on a core-only
// application, and the profiler on the default stack, where default-deny
// answers an anonymous request 403 and the WARN's advice can be followed.
func TestProfiling_Guarded_NoUnguardedLine(t *testing.T) {
	for name, tc := range map[string]struct {
		profiling bool
		opts      []Option
		wantCode  int
		wantWarn  bool
	}{
		"WithoutDefaults, profiling off": {profiling: false, opts: []Option{WithoutDefaults()}, wantCode: http.StatusNotFound},
		"the defaults, profiling on":     {profiling: true, opts: nil, wantCode: http.StatusForbidden, wantWarn: true},
		// WithAuthz() is DEP-2026-018's opt-in: the profiler sits behind the
		// default stack's gate on a core-only application too.
		"WithoutDefaults + WithAuthz, profiling on": {profiling: true, opts: []Option{WithoutDefaults(), WithAuthz()}, wantCode: http.StatusForbidden, wantWarn: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cfg := testAppConfig()
			cfg.ProfilingEnabled = tc.profiling
			cfg.LogLevel = "warn" // the default stack's line is a WARN
			var a *App
			out := captureStdout(t, func() {
				var err error
				a, err = New(cfg, tc.opts...)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
			})
			defer func() { _ = a.Shutdown(context.Background()) }()
			if strings.Contains(out, "profiler UNGUARDED") {
				t.Fatalf("the ERROR line fired for a profiler that is guarded or absent:\n%s", out)
			}
			if code := pprofIndexCode(a); code != tc.wantCode {
				t.Fatalf("GET %s/: %d, want %d", pprofPrefix, code, tc.wantCode)
			}
			if got := strings.Contains(out, "keep it behind a policy"); got != tc.wantWarn {
				t.Fatalf("the default stack's WARN said: %v, want %v:\n%s", got, tc.wantWarn, out)
			}
		})
	}
}

// The notice the boot log cites exists in the register.
func TestPprofUnguardedNoticeExists(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join("..", "..", "docs", "deprecations", depPprofUnguarded+"-*.md"))
	if len(matches) != 1 {
		t.Fatalf("docs/deprecations/%s-*.md: found %v, want exactly one", depPprofUnguarded, matches)
	}
}
