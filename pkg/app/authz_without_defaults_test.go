// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// NU-123: an application built WithoutDefaults() builds no RBAC enforcer, and
// the authorization its configuration asked for — rbac_policy_file, or
// metrics_public: false on a served /metrics — was ignored without a word:
// the policy file was never read, and /metrics answered anyone. The
// application still starts (QADR-0010), and one ERROR line now says what is
// not enforced and what to do (refused from v2.0.0, DEP-2026-017).

// policyFile writes a one-row policy to a temporary file and returns its path.
func policyFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rbac_policy.csv")
	if err := os.WriteFile(path, []byte("p, anonymous, /healthz, read, allow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWithoutDefaults_RBACPolicyFile_StartsAndSaysItIsIgnored(t *testing.T) {
	cfg := testAppConfig()
	cfg.RBACPolicyFile = policyFile(t)

	var a *App
	out := captureStdout(t, func() {
		var err error
		a, err = New(cfg, WithoutDefaults())
		if err != nil {
			t.Fatalf("an application that started before NU-123 no longer starts: %v", err)
		}
	})
	defer func() { _ = a.Shutdown(context.Background()) }()
	if a.Authorizer != nil {
		t.Fatalf("WithoutDefaults() built an authorizer (%T)", a.Authorizer)
	}
	pingRoute(a)
	if got := pingCodes(a, 1); got[0] != http.StatusOK {
		t.Fatalf("WithoutDefaults() enforced a policy: %v", got)
	}
	lines := linesContaining(out, "authz configuration IGNORED")
	if len(lines) != 1 {
		t.Fatalf("want exactly one warning line, got %d:\n%s", len(lines), out)
	}
	line := lines[0]
	for _, want := range []string{"level=ERROR", "rbac_policy_file=" + cfg.RBACPolicyFile, "WithoutDefaults()", "DEP-2026-017", "v2.0.0"} {
		if !strings.Contains(line, want) {
			t.Errorf("the warning does not say %q:\n%s", want, line)
		}
	}
}

// Nothing asked, nothing said: no policy file, and metrics_public: false on
// an application that serves no /metrics (the exporter is not linked into
// this test binary) gates nothing that exists. On the default stack the
// enforcer is one of the defaults.
func TestAuthzConfiguration_NothingIgnored_NoWarning(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(*testing.T, *Config)
		opts []Option
	}{
		"WithoutDefaults, nothing written": {
			set:  func(*testing.T, *Config) {},
			opts: []Option{WithoutDefaults()},
		},
		"WithoutDefaults, metrics_public: false and no /metrics served": {
			set:  func(_ *testing.T, c *Config) { c.MetricsPublic = false; c.MetricsPath = "/metrics" },
			opts: []Option{WithoutDefaults()},
		},
		"the defaults, a policy file": {
			set:  func(t *testing.T, c *Config) { c.RBACPolicyFile = policyFile(t) },
			opts: nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cfg := testAppConfig()
			tc.set(t, cfg)
			out := captureStdout(t, func() {
				a, err := New(cfg, tc.opts...)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				_ = a.Shutdown(context.Background())
			})
			if strings.Contains(out, "authz configuration IGNORED") {
				t.Fatalf("the warning fired for a configuration whose authorization is not ignored:\n%s", out)
			}
		})
	}
}

// Which keys are reported: rbac_policy_file whenever it is written, and
// metrics_public: false only while /metrics is actually served — that is
// when it answers anyone instead of only the callers a policy grants.
func TestAuthzConfigIgnored(t *testing.T) {
	cases := []struct {
		name   string
		cfg    Config
		served bool
		want   []string
	}{
		{name: "nothing", cfg: Config{MetricsPublic: true}, served: true, want: nil},
		{name: "a policy file", cfg: Config{MetricsPublic: true, RBACPolicyFile: "rbac_policy.csv"}, want: []string{"rbac_policy_file"}},
		{name: "a blank policy file", cfg: Config{MetricsPublic: true, RBACPolicyFile: "  "}, want: nil},
		{name: "private metrics, served", cfg: Config{MetricsPublic: false}, served: true, want: []string{"metrics_public"}},
		{name: "private metrics, not served", cfg: Config{MetricsPublic: false}, served: false, want: nil},
		{name: "both", cfg: Config{RBACPolicyFile: "rbac_policy.csv"}, served: true, want: []string{"rbac_policy_file", "metrics_public"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := authzConfigIgnored(&tc.cfg, tc.served); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("authzConfigIgnored = %v, want %v", got, tc.want)
			}
		})
	}
}

// The notice the boot log cites exists in the register.
func TestAuthzIgnoredNoticeExists(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join("..", "..", "docs", "deprecations", depAuthzIgnored+"-*.md"))
	if len(matches) != 1 {
		t.Fatalf("docs/deprecations/%s-*.md: found %v, want exactly one", depAuthzIgnored, matches)
	}
}
