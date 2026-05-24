package nucleus

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestValidateSemantics_AcceptsDefaultsZeroAndValid(t *testing.T) {
	cases := []struct {
		name string
		cfg  app.Config
	}{
		{"framework defaults", app.DefaultConfig()},
		{"zero value (all empty/0 → defaults later)", app.Config{}},
		{"port 0 = OS-assigned", app.Config{Port: 0}},
		{"valid explicit values", app.Config{
			Port: 8080, SMTPPort: 587,
			SessionStore: "redis", LogLevel: "DEBUG", LogFormat: "text",
			SessionCookieSameSite: "Strict",
			ReadTimeout:           5 * time.Second, SessionLifetime: 24 * time.Hour,
			RateLimitRequests: 100, RateLimitBurst: 10,
		}},
		{"log_level warning alias", app.Config{LogLevel: "warning"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if err := validateSemantics(&cfg); err != nil {
				t.Errorf("expected valid config to pass, got: %v", err)
			}
		})
	}
}

func TestValidateSemantics_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*app.Config)
		field  string
	}{
		{"unknown session_store", func(c *app.Config) { c.SessionStore = "postgres" }, "session_store"},
		{"unknown log_level", func(c *app.Config) { c.LogLevel = "trace" }, "log_level"},
		{"unknown log_format", func(c *app.Config) { c.LogFormat = "xml" }, "log_format"},
		{"unknown samesite", func(c *app.Config) { c.SessionCookieSameSite = "loose" }, "session_cookie_samesite"},
		{"port too high", func(c *app.Config) { c.Port = 70000 }, "port"},
		{"port negative", func(c *app.Config) { c.Port = -1 }, "port"},
		{"smtp_port too high", func(c *app.Config) { c.SMTPPort = 99999 }, "smtp_port"},
		{"smtp_port negative", func(c *app.Config) { c.SMTPPort = -1 }, "smtp_port"},
		{"negative rate_limit_requests", func(c *app.Config) { c.RateLimitRequests = -5 }, "rate_limit_requests"},
		{"negative rate_limit_burst", func(c *app.Config) { c.RateLimitBurst = -1 }, "rate_limit_burst"},
		{"negative read_timeout", func(c *app.Config) { c.ReadTimeout = -1 * time.Second }, "read_timeout"},
		{"negative write_timeout", func(c *app.Config) { c.WriteTimeout = -1 * time.Second }, "write_timeout"},
		{"negative jwt_expiry", func(c *app.Config) { c.JWTExpiry = -1 * time.Hour }, "jwt_expiry"},
		{"negative session_idle_timeout", func(c *app.Config) { c.SessionIdleTimeout = -1 * time.Minute }, "session_idle_timeout"},
		{"negative session_lifetime", func(c *app.Config) { c.SessionLifetime = -1 * time.Hour }, "session_lifetime"},
		{"negative rate_limit_window", func(c *app.Config) { c.RateLimitWindow = -1 * time.Second }, "rate_limit_window"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := app.DefaultConfig()
			tc.mutate(&cfg)
			err := validateSemantics(&cfg)
			if err == nil {
				t.Fatalf("expected %s to be rejected, got nil", tc.field)
			}
			if !errors.Is(err, ErrInvalidConfigValue) {
				t.Errorf("error %v should wrap ErrInvalidConfigValue", err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q should name the offending field %q", err.Error(), tc.field)
			}
		})
	}
}

// TestFromConfigFile_LayerThreeFailsAtLoad confirms layer-3 runs at load: a
// well-formed file with a semantically-invalid value fails at Build/Err.
func TestFromConfigFile_LayerThreeFailsAtLoad(t *testing.T) {
	unsetEnv(t, "NUCLEUS_SESSION_STORE")
	path := writeTempYAML(t, "session_store: postgres\n")

	_, err := New().FromConfigFile(path).Build()
	if err == nil {
		t.Fatal("expected Build to fail on an invalid session_store")
	}
	if !errors.Is(err, ErrInvalidConfigValue) {
		t.Errorf("error %v should wrap ErrInvalidConfigValue", err)
	}
}

// TestFromConfigFile_LayerThreePassesValid is the negative control: a valid
// file loads cleanly.
func TestFromConfigFile_LayerThreePassesValid(t *testing.T) {
	unsetEnv(t, "NUCLEUS_PORT")
	path := writeTempYAML(t, "port: 9091\nlog_level: warn\n")

	if _, err := New().FromConfigFile(path).Build(); err != nil {
		t.Fatalf("valid config should load, got: %v", err)
	}
}
