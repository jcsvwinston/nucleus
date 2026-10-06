// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// WithAuthz (owner decision 2026-10-06): an application built
// WithoutDefaults() had no authorization layer at all — no RBAC enforcer, no
// default-deny gate, no global bearer decode — so a policy file, the rows
// modules declare, an API key's scopes, a private /metrics, the profiler and
// the realtime topics were all unguarded (NU-123…126, NU-128, NU-130).
// WithAuthz() gives such an application the default stack's authorization,
// mounted in the default stack's order, and nothing else of that stack. An
// application that does not call it is unchanged.

// coreAuthzApp is a core-only application with the default stack's
// authorization, built in a directory without a policy file to discover.
func coreAuthzApp(t *testing.T, cfg *Config, opts ...Option) (*App, string) {
	t.Helper()
	t.Chdir(t.TempDir())
	var a *App
	out := captureStdout(t, func() {
		var err error
		a, err = New(cfg, append([]Option{WithoutDefaults(), WithAuthz()}, opts...)...)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
	})
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	return a, out
}

func getCode(a *App, path string) int {
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

// No policy file, no rows: default-deny means every registered route outside
// the bootstrap allow-list answers an anonymous request 403, a path no route
// serves answers 404, and the boot log says it in so many words.
func TestWithAuthz_CoreOnly_DefaultDenyWithNoPolicy(t *testing.T) {
	cfg := testAppConfig()
	cfg.LogLevel = "info"
	a, out := coreAuthzApp(t, cfg)
	if a.Authorizer == nil {
		t.Fatal("WithAuthz() built no authorizer")
	}
	pingRoute(a)
	if code := getCode(a, "/ping"); code != http.StatusForbidden {
		t.Fatalf("an anonymous request to an unpoliced route: %d, want 403", code)
	}
	if code := getCode(a, "/healthz"); code == http.StatusForbidden {
		t.Fatalf("/healthz is on the bootstrap allow-list and answered 403")
	}
	if code := getCode(a, "/nowhere"); code != http.StatusNotFound {
		t.Fatalf("a path no route serves: %d, want 404 (the gate runs after routing, ADR-033)", code)
	}
	if err := a.Authorizer.AddPolicy("anonymous", "/ping", "read"); err != nil {
		t.Fatal(err)
	}
	if code := getCode(a, "/ping"); code != http.StatusOK {
		t.Fatalf("the route once a row allows it: %d, want 200", code)
	}

	lines := linesContaining(out, "authz: default-deny with 0 policy rows")
	if len(lines) != 1 {
		t.Fatalf("want exactly one line saying default-deny with 0 rows, got %d:\n%s", len(lines), out)
	}
	for _, want := range []string{"level=WARN", "403", "/healthz", "/readyz", "rbac_policy_file", "Module.Policies"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the line does not say %q:\n%s", want, lines[0])
		}
	}
	for _, ignored := range []string{"authz configuration IGNORED", "WithOpenAuthz() in effect"} {
		if strings.Contains(out, ignored) {
			t.Errorf("the boot log says %q on an application that enforces:\n%s", ignored, out)
		}
	}
}

// The default stack says the same thing about the same state: WithAuthz() is
// its authorization, not a variant of it.
func TestDefaultStack_DefaultDenyBootLine(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := testAppConfig()
	cfg.LogLevel = "info"
	for name, opts := range map[string][]Option{
		"the defaults":             nil,
		"the defaults + WithAuthz": {WithAuthz()},
	} {
		t.Run(name, func(t *testing.T) {
			var a *App
			out := captureStdout(t, func() {
				var err error
				a, err = New(cfg, opts...)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
			})
			defer func() { _ = a.Shutdown(context.Background()) }()
			pingRoute(a)
			if code := getCode(a, "/ping"); code != http.StatusForbidden {
				t.Fatalf("an anonymous request to an unpoliced route: %d, want 403", code)
			}
			if n := len(linesContaining(out, "authz: default-deny with 0 policy rows")); n != 1 {
				t.Fatalf("want exactly one default-deny line, got %d:\n%s", n, out)
			}
		})
	}
}

// A policy file is loaded, its rows are counted at boot, the keys NU-123
// reported as ignored are not reported, and the profiler sits behind the
// gate (DEP-2026-018's opt-in): 403 to an anonymous request, the default
// stack's WARN, no ERROR line.
func TestWithAuthz_LoadsThePolicyAndGuardsTheProfiler(t *testing.T) {
	dir := t.TempDir()
	policy := filepath.Join(dir, "rbac_policy.csv")
	if err := os.WriteFile(policy, []byte("p, anonymous, /ping, read, allow\np, oncall, /debug/pprof/*, read, allow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testAppConfig()
	cfg.LogLevel = "info"
	cfg.RBACPolicyFile = policy
	cfg.ProfilingEnabled = true
	a, out := coreAuthzApp(t, cfg)
	pingRoute(a)
	if code := getCode(a, "/ping"); code != http.StatusOK {
		t.Fatalf("a route the policy file allows: %d, want 200", code)
	}
	if code := pprofIndexCode(a); code != http.StatusForbidden {
		t.Fatalf("GET %s/ anonymously with WithAuthz(): %d, want 403", pprofPrefix, code)
	}
	lines := linesContaining(out, "authz: default-deny with 2 policy rows")
	if len(lines) != 1 {
		t.Fatalf("want exactly one line counting the policy file's rows, got %d:\n%s", len(lines), out)
	}
	for _, want := range []string{"level=INFO", "policy_path=" + policy} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the line does not say %q:\n%s", want, lines[0])
		}
	}
	for _, said := range []string{"authz configuration IGNORED", "profiler UNGUARDED"} {
		if strings.Contains(out, said) {
			t.Errorf("WithAuthz() guards it and the boot log still says %q:\n%s", said, out)
		}
	}
	if !strings.Contains(out, "keep it behind a policy") {
		t.Errorf("the profiler is behind the gate and the boot log does not say the default stack's WARN:\n%s", out)
	}
}

// An API key's scopes are subjects of the gate (NU-130): a route granted to
// scope:<name> answers the keys that carry it and refuses the others. Without
// WithAuthz() the application is unchanged: nothing reads the scopes.
func TestWithAuthz_APIKeyScopesAreEnforced(t *testing.T) {
	a, _ := coreAuthzApp(t, testAppConfig(), WithAPIKeys())
	a.Router.Get("/reports", router.FromHTTP(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if err := a.Authorizer.AddPolicy("scope:reports:read", "/reports", "read"); err != nil {
		t.Fatal(err)
	}
	if rec := keyGet(t, a, "/reports", issueKey(t, a, "svc-one", "reports:read")); rec.Code != http.StatusNoContent {
		t.Fatalf("a key carrying reports:read on a route granted to scope:reports:read: %d, want 204", rec.Code)
	}
	if rec := keyGet(t, a, "/reports", issueKey(t, a, "svc-two", "reports:write")); rec.Code != http.StatusForbidden {
		t.Fatalf("a key carrying another scope: %d, want 403", rec.Code)
	}
	if rec := keyGet(t, a, "/reports", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("no key: %d, want 403", rec.Code)
	}

	plain, err := New(testAppConfig(), WithoutDefaults(), WithAPIKeys())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = plain.Shutdown(context.Background()) })
	plain.Router.Get("/reports", router.FromHTTP(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	if rec := keyGet(t, plain, "/reports", issueKey(t, plain, "svc-two", "reports:write")); rec.Code != http.StatusNoContent {
		t.Fatalf("without WithAuthz() a core-only application changed: %d, want 204", rec.Code)
	}
}

// The bearer is decoded globally, ahead of the API-key read, the limiter
// and the interceptors, as on the default stack (NU-125): an interceptor sees
// the token's claims, and WithRateLimit() keys each user apart. Without
// WithAuthz() nothing decodes it there, and the limiter keys by address — as
// before.
func TestWithAuthz_TheIdentityReachesTheInterceptorsAndTheLimiter(t *testing.T) {
	for name, withAuthz := range map[string]bool{
		"WithoutDefaults + WithAuthz + WithRateLimit": true,
		"WithoutDefaults + WithRateLimit":             false,
	} {
		t.Run(name, func(t *testing.T) {
			probeName := "zzwithauthzprobe"
			if !withAuthz {
				probeName += "off"
			}
			probe := &claimsProbe{}
			registerClaimsProbe(t, probeName, probe)

			cfg := testAppConfig()
			cfg.JWTSecret = "nu-125-with-authz-test-secret-0123456789"
			cfg.HTTPInterceptors = []string{probeName}
			cfg.RateLimitRequests = 1
			cfg.RateLimitWindow = time.Minute
			opts := []Option{WithoutDefaults(), WithRateLimit()}
			if withAuthz {
				opts = append(opts, WithAuthz())
			}
			t.Chdir(t.TempDir())
			a, err := New(cfg, opts...)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
			a.Router.Get("/api/mine", router.FromHTTP(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
			if withAuthz {
				if err := a.Authorizer.AddPolicy("user", "/api/mine", "read"); err != nil {
					t.Fatal(err)
				}
			}
			alice, _ := a.JWT.Generate("u-alice", "alice", "user")
			bob, _ := a.JWT.Generate("u-bob", "bob", "user")

			if rec := bearerGet(t, a, "/api/mine", alice); rec.Code != http.StatusOK {
				t.Fatalf("alice #1: %d, want 200", rec.Code)
			}
			got := probe.snapshot()
			if got.sawOK != withAuthz {
				t.Fatalf("the interceptor saw the token's claims = %v, want %v (uid=%q)", got.sawOK, withAuthz, got.uid)
			}
			if withAuthz && got.uid != "u-alice" {
				t.Fatalf("the interceptor saw uid=%q, want u-alice", got.uid)
			}
			bobCode := bearerGet(t, a, "/api/mine", bob).Code
			switch {
			case withAuthz && bobCode != http.StatusOK:
				t.Fatalf("bob #1 answered %d: the limiter keys on the address, not on the token's user", bobCode)
			case !withAuthz && bobCode != http.StatusTooManyRequests:
				t.Fatalf("bob #1 answered %d without WithAuthz(): the limiter is keyed by address there, want 429", bobCode)
			}
			if withAuthz {
				if rec := bearerGet(t, a, "/api/mine", alice); rec.Code != http.StatusTooManyRequests {
					t.Fatalf("alice #2: %d, want 429 (her own bucket is spent)", rec.Code)
				}
			}
		})
	}
}

// A realtime channel is a route (NU-128): with WithAuthz() a topic answers
// only the subjects a policy grants it to.
func TestWithAuthz_RealtimeTopicsAreAuthorized(t *testing.T) {
	t.Chdir(t.TempDir())
	a, srv := realtimeApp(t, WithoutDefaults(), WithAuthz(), WithAPIKeys())
	if _, _, status := wsDial(t, srv.URL, RealtimeChannelPath("orders"), nil); !strings.Contains(status, "403") {
		t.Fatalf("an anonymous subscription with no policy: %q, want 403", status)
	}
	if err := a.Authorizer.AddPolicy("svc-dash", "/realtime/orders", "read"); err != nil {
		t.Fatal(err)
	}
	_, presented, err := apikeys.Issue(context.Background(), a.apiKeys, apikeys.Key{Name: "dash", OwnerID: "svc-dash"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, status := wsDial(t, srv.URL, RealtimeChannelPath("orders"), http.Header{apikeys.HeaderName: {presented}}); !strings.Contains(status, "101") {
		t.Fatalf("a subscription by the owner the policy names: %q, want 101", status)
	}
}

// Without WithAuthz() a core-only application's channels are open, as
// before; the boot line that announces them now says so.
func TestWithoutDefaults_RealtimeBootLineSaysTopicsAreOpen(t *testing.T) {
	cfg := testAppConfig()
	cfg.LogLevel = "info"
	for name, tc := range map[string]struct {
		opts []Option
		open bool
	}{
		"WithoutDefaults":             {opts: []Option{WithoutDefaults(), WithRealtime()}, open: true},
		"WithoutDefaults + WithAuthz": {opts: []Option{WithoutDefaults(), WithAuthz(), WithRealtime()}},
		"the defaults":                {opts: []Option{WithRealtime()}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var a *App
			out := captureStdout(t, func() {
				var err error
				a, err = New(cfg, tc.opts...)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
			})
			defer func() { _ = a.Shutdown(context.Background()) }()
			lines := linesContaining(out, "realtime channels at GET /realtime/{topic}")
			if len(lines) != 1 {
				t.Fatalf("want one realtime boot line, got %d:\n%s", len(lines), out)
			}
			said := strings.Contains(lines[0], "every topic is open") && strings.Contains(lines[0], "WithAuthz()")
			if said != tc.open {
				t.Fatalf("the line says the topics are open (naming WithAuthz()) = %v, want %v:\n%s", said, tc.open, lines[0])
			}
		})
	}
}

// WithOpenAuthz() switches off the gate WithAuthz() mounts, as it switches
// off the default stack's: the enforcer is built and the bearer decoded, and
// no route is refused.
func TestWithAuthz_WithOpenAuthzSwitchesTheGateOff(t *testing.T) {
	cfg := testAppConfig()
	cfg.LogLevel = "info"
	a, out := coreAuthzApp(t, cfg, WithOpenAuthz())
	if a.Authorizer == nil {
		t.Fatal("WithAuthz() + WithOpenAuthz() built no authorizer")
	}
	pingRoute(a)
	if code := getCode(a, "/ping"); code != http.StatusOK {
		t.Fatalf("WithOpenAuthz() and an unpoliced route: %d, want 200", code)
	}
	if strings.Contains(out, "authz: default-deny") {
		t.Fatalf("the boot log announces default-deny on an application that skips enforcement:\n%s", out)
	}
}
