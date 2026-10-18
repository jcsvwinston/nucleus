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
// no enforcer, so the profiles once answered anyone, and the boot WARN told
// the operator to "keep it behind a policy" no such application can have.
// The combination refuses to start unless WithAuthz() guards the profiler
// (DEP-2026-018), and the error says what is at stake.

// pprofIndexCode asks the profiler's index anonymously.
func pprofIndexCode(a *App) int {
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, pprofPrefix+"/", nil))
	return rec.Code
}

func TestWithoutDefaults_Profiling_RefusesToStart(t *testing.T) {
	cfg := testAppConfig()
	cfg.ProfilingEnabled = true
	cfg.LogLevel = "warn" // so a leftover WARN would be seen

	var err error
	out := captureStdout(t, func() {
		var a *App
		a, err = New(cfg, WithoutDefaults())
		if err == nil {
			_ = a.Shutdown(context.Background())
		}
	})
	if err == nil {
		t.Fatal("WithoutDefaults() without WithAuthz() started with the profiler on")
	}
	for _, want := range []string{"profiler UNGUARDED", pprofPrefix, "WithoutDefaults()", "WithAuthz()", "heap", "profiling_enabled", "DEP-2026-018"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	// The WARN that advised a policy this application cannot have is not
	// said on the way out.
	if strings.Contains(out, "keep it behind a policy") {
		t.Errorf("the boot log still advises a policy a WithoutDefaults() application cannot have:\n%s", out)
	}
}

// In production the refusal says what is at stake in so many words: a heap
// dump of this production process, live memory included.
func TestWithoutDefaults_Profiling_ProductionNamesHeapDumps(t *testing.T) {
	cfg := testAppConfig()
	cfg.ProfilingEnabled = true
	cfg.Env = "production"
	cfg.JWTSecret = strings.Repeat("k", 32) + "-production-test-secret"

	a, err := New(cfg, WithoutDefaults())
	if err == nil {
		_ = a.Shutdown(context.Background())
		t.Fatal("WithoutDefaults() without WithAuthz() started in production with the profiler on")
	}
	for _, want := range []string{"profiler UNGUARDED in production", "heap dumps", "live memory", "DEP-2026-018"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the production refusal does not say %q:\n%v", want, err)
		}
	}
}

// Nothing unguarded, nothing refused: the profiler off on a core-only
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
				t.Fatalf("the refusal's line fired for a profiler that is guarded or absent:\n%s", out)
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

// The notice the refusal cites exists in the register.
func TestPprofUnguardedNoticeExists(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join("..", "..", "docs", "deprecations", depPprofUnguarded+"-*.md"))
	if len(matches) != 1 {
		t.Fatalf("docs/deprecations/%s-*.md: found %v, want exactly one", depPprofUnguarded, matches)
	}
}
