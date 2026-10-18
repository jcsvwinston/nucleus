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
	"time"

	"github.com/jcsvwinston/nucleus/pkg/router"
)

// NU-122: an application built WithoutDefaults() mounts no rate limiter —
// the limiter is mounted by the default path only — and the rate_limit_*
// keys its configuration wrote were once ignored without a word: with
// rate_limit_requests: 1 the application answered 200, 200, 200.
// WithRateLimit() mounts it; without the option a declared limit refuses
// the boot (DEP-2026-016). An application that sets no limit behaves as it
// always did.

// pingRoute registers GET /ping, answering 200.
func pingRoute(a *App) {
	a.Router.Get("/ping", router.FromHTTP(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
}

// pingCodes sends n GET /ping from the same client and returns the statuses.
func pingCodes(a *App, n int) []int {
	codes := make([]int, 0, n)
	for i := 0; i < n; i++ {
		rec := httptest.NewRecorder()
		a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
		codes = append(codes, rec.Code)
	}
	return codes
}

func rateLimitedConfig() *Config {
	cfg := testAppConfig()
	cfg.RateLimitRequests = 1
	cfg.RateLimitWindow = time.Minute
	return cfg
}

// The combination the v1.x line reported with an ERROR line refuses to start
// (DEP-2026-016): New returns an error that names the limit, the option that
// mounts it, and the notice.
func TestWithoutDefaults_DeclaredRateLimit_RefusesToStart(t *testing.T) {
	a, err := New(rateLimitedConfig(), WithoutDefaults())
	if err == nil {
		_ = a.Shutdown(context.Background())
		t.Fatal("WithoutDefaults() without WithRateLimit() started with a declared rate limit")
	}
	for _, want := range []string{"rate_limit_requests IGNORED", "requests=1", "WithRateLimit()", "DEP-2026-016"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
}

// With the option, the declared limiter is mounted and nothing is said or
// refused about it; on the default stack the limiter is one of the defaults.
func TestWithRateLimit_MountsTheDeclaredLimiter(t *testing.T) {
	for name, opts := range map[string][]Option{
		"WithoutDefaults + WithRateLimit": {WithoutDefaults(), WithRateLimit()},
		// The default stack's default-deny would answer 403 to an
		// unpoliced route before the limiter is the question.
		"the defaults": {WithOpenAuthz()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var a *App
			out := captureStdout(t, func() {
				var err error
				a, err = New(rateLimitedConfig(), opts...)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
			})
			defer func() { _ = a.Shutdown(context.Background()) }()
			pingRoute(a)
			got := pingCodes(a, 2)
			if got[0] != http.StatusOK || got[1] != http.StatusTooManyRequests {
				t.Fatalf("rate_limit_requests: 1 answered %v, want [200 429]", got)
			}
			if strings.Contains(out, "rate_limit_requests IGNORED") {
				t.Fatalf("the limiter was mounted and the boot log still says it is ignored:\n%s", out)
			}
		})
	}
}

// Nothing set, nothing refused, nothing mounted — with or without the
// option: rate_limit_requests: 0 asks for no limit, and none is one.
func TestWithoutDefaults_NoRateLimitDeclared_NoWarningNoLimiter(t *testing.T) {
	for name, opts := range map[string][]Option{
		"WithoutDefaults":                 {WithoutDefaults()},
		"WithoutDefaults + WithRateLimit": {WithoutDefaults(), WithRateLimit()},
	} {
		t.Run(name, func(t *testing.T) {
			var a *App
			out := captureStdout(t, func() {
				var err error
				a, err = New(testAppConfig(), opts...)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
			})
			defer func() { _ = a.Shutdown(context.Background()) }()
			pingRoute(a)
			if got := pingCodes(a, 3); got[2] != http.StatusOK {
				t.Fatalf("a configuration with no limit got one: %v", got)
			}
			if strings.Contains(out, "rate_limit_requests IGNORED") {
				t.Fatalf("the warning fired for a configuration that sets no limit:\n%s", out)
			}
		})
	}
}

// The notice the refusal cites exists in the register.
func TestRateLimitIgnoredNoticeExists(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join("..", "..", "docs", "deprecations", depRateLimitIgnored+"-*.md"))
	if len(matches) != 1 {
		t.Fatalf("docs/deprecations/%s-*.md: found %v, want exactly one", depRateLimitIgnored, matches)
	}
}
