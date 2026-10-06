// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// NU-122 and NU-123 in the CLI: `nucleus doctor --check security`, `doctor
// --check rbac` and `health --deploy` judged the rate limit and the RBAC
// policy file by the configuration alone, so on an application built
// WithoutDefaults() — which mounts neither — they reported a limit and a
// policy the running application does not enforce. They now read the
// composition root the way NU-99's storage check does; when they cannot find
// it they say where the setting is enforced instead of implying it is.

// projectWithRoot writes a main.go whose chain ends with the given calls and
// returns the path of the nucleus.yml beside it.
func projectWithRoot(t *testing.T, chain string) string {
	t.Helper()
	dir := t.TempDir()
	main := "package main\n\nimport \"github.com/jcsvwinston/nucleus/pkg/nucleus\"\n\n" +
		"// nucleus.New().WithoutDefaults().WithRateLimit() — a doc comment does not count.\n" +
		"func main() {\n\t_ = nucleus.New().FromConfigFile(\"nucleus.yml\")" + chain + ".Start()\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "nucleus.yml")
}

func rateLimitedDevConfig() *app.Config {
	cfg := app.DefaultConfig()
	cfg.RateLimitRequests = 100
	return &cfg
}

func TestCheckSecurity_RateLimitTheApplicationDoesNotMount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		chain   string
		ignored bool
	}{
		{name: "WithoutDefaults, no WithRateLimit", chain: ".WithoutDefaults()", ignored: true},
		{name: "WithoutDefaults and WithRateLimit", chain: ".WithoutDefaults().WithRateLimit()"},
		{name: "the default stack", chain: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := checkSecurity(rateLimitedDevConfig(), projectWithRoot(t, tc.chain))
			said := strings.Contains(out.message, "IGNORED")
			if said != tc.ignored {
				t.Fatalf("doctor reported the limit ignored = %v, want %v: %s", said, tc.ignored, out.message)
			}
			if !tc.ignored {
				if out.status != doctorStatusPass {
					t.Fatalf("status = %s (%s); want pass", out.status, out.message)
				}
				return
			}
			if out.status != doctorStatusWarning {
				t.Errorf("status = %s; want warning — the application starts", out.status)
			}
			for _, want := range []string{"rate_limit_requests=100", "WithRateLimit()", "DEP-2026-016", "v2.0.0", "main.go"} {
				if !strings.Contains(out.message, want) {
					t.Errorf("the report does not say %q: %s", want, out.message)
				}
			}
		})
	}
}

// Without a composition root to read, doctor cannot know how the application
// is built: it does not claim the limit, it says where the limit holds.
func TestCheckSecurity_RateLimitWithoutACompositionRoot(t *testing.T) {
	out := checkSecurity(rateLimitedDevConfig(), filepath.Join(t.TempDir(), "nucleus.yml"))
	if out.status != doctorStatusPass {
		t.Fatalf("status = %s (%s); want pass", out.status, out.message)
	}
	for _, want := range []string{"rate_limit_requests=100", "default stack", "WithRateLimit()"} {
		if !strings.Contains(out.message, want) {
			t.Errorf("the report does not say %q: %s", want, out.message)
		}
	}
}

func TestCheckRBAC_PolicyTheApplicationDoesNotLoad(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chain  string
		policy bool
		status doctorStatus
		says   []string
	}{
		{name: "WithoutDefaults, a policy file", chain: ".WithoutDefaults()", policy: true, status: doctorStatusWarning,
			says: []string{"IGNORED", "rbac_policy_file", "WithoutDefaults()", "WithAuthz()", "DEP-2026-017", "v2.0.0", "main.go"}},
		{name: "WithoutDefaults, no policy file", chain: ".WithoutDefaults()", status: doctorStatusInfo,
			says: []string{"WithoutDefaults()", "no RBAC enforcer", "Module.Policies", "discarded", "WithAuthz()"}},
		{name: "the default stack, a policy file", chain: "", policy: true, status: doctorStatusPass,
			says: []string{"RBAC policy file found"}},
		// WithAuthz() builds the default stack's enforcer on a core-only
		// application: the policy file is loaded, as on the default stack.
		{name: "WithoutDefaults and WithAuthz, a policy file", chain: ".WithoutDefaults().WithAuthz()", policy: true, status: doctorStatusPass,
			says: []string{"RBAC policy file found"}},
		{name: "WithoutDefaults and WithAuthz, no policy file", chain: ".WithoutDefaults().WithAuthz()", status: doctorStatusWarning,
			says: []string{"only serve bootstrap routes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath := projectWithRoot(t, tc.chain)
			cfg := app.DefaultConfig()
			if tc.policy {
				policy := filepath.Join(filepath.Dir(configPath), "rbac_policy.csv")
				if err := os.WriteFile(policy, []byte("p, anonymous, /healthz, read, allow\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				cfg.RBACPolicyFile = policy
			}
			out := checkRBAC(&cfg, configPath)
			if out.status != tc.status {
				t.Fatalf("status = %s (%s); want %s", out.status, out.message, tc.status)
			}
			for _, want := range tc.says {
				if !strings.Contains(out.message, want) {
					t.Errorf("the report does not say %q: %s", want, out.message)
				}
			}
		})
	}
}

func TestDeployRateLimitComponent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		requests int
		chain    *string // nil: no composition root beside the configuration
		status   string
		says     []string
	}{
		{name: "no limit", requests: 0, chain: ptr(""), status: "warning",
			says: []string{"rate_limit_requests should be > 0"}},
		{name: "WithoutDefaults, no WithRateLimit", requests: 100, chain: ptr(".WithoutDefaults()"), status: "warning",
			says: []string{"rate_limit_requests=100", "IGNORED", "WithRateLimit()", "DEP-2026-016", "main.go"}},
		{name: "WithoutDefaults and WithRateLimit", requests: 100, chain: ptr(".WithoutDefaults().WithRateLimit()"), status: "ok",
			says: []string{"rate_limit_requests=100", "WithRateLimit()", "main.go"}},
		{name: "the default stack", requests: 100, chain: ptr(""), status: "ok",
			says: []string{"rate_limit_requests=100", "default stack", "main.go"}},
		{name: "no composition root", requests: 100, chain: nil, status: "ok",
			says: []string{"rate_limit_requests=100", "only on the default stack", "WithRateLimit()"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "nucleus.yml")
			if tc.chain != nil {
				configPath = projectWithRoot(t, *tc.chain)
			}
			cfg := app.DefaultConfig()
			cfg.RateLimitRequests = tc.requests
			got := deployRateLimitComponent(&cfg, configPath)
			if got.Name != "deploy.rate_limit" {
				t.Fatalf("component name = %q", got.Name)
			}
			if got.Status != tc.status {
				t.Fatalf("status = %s (%s); want %s", got.Status, got.Details, tc.status)
			}
			for _, want := range tc.says {
				if !strings.Contains(got.Details, want) {
					t.Errorf("the details do not say %q: %s", want, got.Details)
				}
			}
		})
	}
}

func ptr(s string) *string { return &s }

// The notices doctor and health cite exist in the register.
func TestWithoutDefaultsNoticesExist(t *testing.T) {
	for _, id := range []string{"DEP-2026-016", "DEP-2026-017", "DEP-2026-018"} {
		matches, _ := filepath.Glob(filepath.Join(repoRootForTest(t), "docs", "deprecations", id+"-*.md"))
		if len(matches) != 1 {
			t.Errorf("docs/deprecations/%s-*.md: found %v, want exactly one", id, matches)
		}
	}
}

// NU-124 in the CLI: doctor said nothing about the profiler, which on an
// application built WithoutDefaults() serves heap and goroutine dumps to
// anyone who reaches the port — there is no enforcer to put it behind. It
// is a finding to review in development and a high-risk setting in
// production, where a heap dump carries the process's live memory.
func TestCheckSecurity_ProfilerTheApplicationCannotGuard(t *testing.T) {
	for _, tc := range []struct {
		name      string
		chain     *string // nil: no composition root beside the configuration
		profiling bool
		prod      bool
		status    doctorStatus
		says      []string
	}{
		{name: "WithoutDefaults, development", chain: ptr(".WithoutDefaults()"), profiling: true, status: doctorStatusWarning,
			says: []string{"profiling_enabled", "/debug/pprof", "UNGUARDED", "WithoutDefaults()", "main.go", "DEP-2026-018", "v2.0.0"}},
		{name: "WithoutDefaults, production", chain: ptr(".WithoutDefaults().WithRateLimit()"), profiling: true, prod: true, status: doctorStatusError,
			says: []string{"profiling_enabled", "UNGUARDED", "production", "heap dump", "live memory", "WithAuthz()", "DEP-2026-018"}},
		{name: "the default stack", chain: ptr(""), profiling: true, status: doctorStatusPass},
		// WithAuthz() is DEP-2026-018's opt-in: the profiler sits behind the
		// default-deny gate.
		{name: "WithoutDefaults and WithAuthz", chain: ptr(".WithoutDefaults().WithAuthz()"), profiling: true, status: doctorStatusPass},
		{name: "no composition root", chain: nil, profiling: true, status: doctorStatusPass,
			says: []string{"/debug/pprof", "only on the default stack", "WithAuthz()"}},
		{name: "WithoutDefaults, profiling off", chain: ptr(".WithoutDefaults()"), status: doctorStatusPass},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "nucleus.yml")
			if tc.chain != nil {
				configPath = projectWithRoot(t, *tc.chain)
			}
			cfg := app.DefaultConfig()
			cfg.ProfilingEnabled = tc.profiling
			if tc.prod {
				cfg.Env = "production"
			}
			out := checkSecurity(&cfg, configPath)
			if out.status != tc.status {
				t.Fatalf("status = %s (%s); want %s", out.status, out.message, tc.status)
			}
			if tc.status == doctorStatusPass && strings.Contains(out.message, "UNGUARDED") {
				t.Fatalf("a guarded or absent profiler is reported unguarded: %s", out.message)
			}
			for _, want := range tc.says {
				if !strings.Contains(out.message, want) {
					t.Errorf("the report does not say %q: %s", want, out.message)
				}
			}
		})
	}
}
