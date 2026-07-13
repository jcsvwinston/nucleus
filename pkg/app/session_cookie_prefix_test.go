package app

// Regression guards for NU-P1-7 (cookie-prefix preconditions on the
// session cookie) and NU-P1-1 (declarative CSRF keys reach the config).

import (
	"strings"
	"testing"
)

func prefixConfig(mutate func(*Config)) *Config {
	cfg := DefaultConfig()
	mutate(&cfg)
	return &cfg
}

func TestBuildSessionManager_HostPrefixPreconditions(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "host prefix requires secure",
			mutate: func(c *Config) {
				c.SessionCookieName = "__Host-session"
				c.SessionCookieSecure = false
			},
			wantErr: "session_cookie_secure=true",
		},
		{
			name: "host prefix forbids domain",
			mutate: func(c *Config) {
				c.SessionCookieName = "__Host-session"
				c.SessionCookieDomain = "example.com"
			},
			wantErr: "session_cookie_domain",
		},
		{
			name: "host prefix requires root path",
			mutate: func(c *Config) {
				c.SessionCookieName = "__Host-session"
				c.SessionCookiePath = "/app"
			},
			wantErr: "session_cookie_path",
		},
		{
			name: "secure prefix requires secure",
			mutate: func(c *Config) {
				c.SessionCookieName = "__Secure-session"
				c.SessionCookieSecure = false
			},
			wantErr: "session_cookie_secure=true",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := buildSessionManager(prefixConfig(tc.mutate), nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestBuildSessionManager_AcceptsCompliantHostPrefix(t *testing.T) {
	cfg := prefixConfig(func(c *Config) {
		c.SessionCookieName = "__Host-session"
		// Defaults already satisfy the prefix: Secure=true, Path=/, no Domain.
	})
	mgr, _, err := buildSessionManager(cfg, nil)
	if err != nil {
		t.Fatalf("compliant __Host- config must be accepted, got %v", err)
	}
	if mgr == nil {
		t.Fatal("expected a session manager")
	}
}

func TestDefaultConfig_CSRFAndMetricsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.CSRFEnabled {
		t.Fatal("csrf_enabled must default to false (opt-in)")
	}
	if !cfg.MetricsPublic {
		t.Fatal("metrics_public must default to true (historical behaviour)")
	}
}
