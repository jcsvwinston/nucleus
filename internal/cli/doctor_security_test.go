// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Track E: `nucleus doctor security` — the high-risk misconfigurations that
// no other check catches today. Each case here is a configuration that
// LOADS and BOOTS fine: valid syntax, valid values, and a real security
// hole. That is the whole point of a deploy check — the config layers judge
// whether a file makes sense, this judges whether it is safe to expose.
package cli

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func prodConfig() *app.Config {
	cfg := app.DefaultConfig()
	cfg.Env = "production"
	cfg.JWTSecret = "K7f2Qx9LmZp4Rw8TvN3aHy6BdG1sJc5E"
	return &cfg
}

func TestCheckSecurity_CORSWildcardWithCredentials(t *testing.T) {
	cfg := prodConfig()
	cfg.CORSOrigins = []string{"*"}
	cfg.CORSAllowCredentials = true

	out := checkSecurity(cfg, "")
	if out.status != doctorStatusError {
		t.Fatalf("wildcard origin + credentials is the classic catastrophic CORS hole; want error, got %q (%s)", out.status, out.message)
	}
	if !strings.Contains(out.message, "cors_allow_credentials") {
		t.Errorf("message must name the offending key, got %q", out.message)
	}
}

func TestCheckSecurity_CORSWildcardInProduction(t *testing.T) {
	cfg := prodConfig()
	cfg.CORSOrigins = []string{"*"}

	out := checkSecurity(cfg, "")
	if out.status == doctorStatusPass {
		t.Fatalf("a wildcard allow-list in production must not pass silently, got %q", out.message)
	}
}

func TestCheckSecurity_TrustedProxyCatchAll(t *testing.T) {
	for _, entry := range []string{"0.0.0.0/0", "::/0"} {
		t.Run(entry, func(t *testing.T) {
			cfg := prodConfig()
			cfg.TrustedProxies = []string{entry}

			out := checkSecurity(cfg, "")
			if out.status != doctorStatusError {
				t.Fatalf("trusting every source makes X-Forwarded-For attacker-controlled (spoofed client IP, rate-limit evasion, forged audit trail); want error, got %q (%s)", out.status, out.message)
			}
			if !strings.Contains(out.message, "trusted_proxies") {
				t.Errorf("message must name the offending key, got %q", out.message)
			}
		})
	}
}

// The JWT secret length check that already exists in `health --deploy`
// passes 32 identical characters. Entropy, not length, is what makes a
// signing key unforgeable.
func TestCheckSecurity_WeakJWTSecret(t *testing.T) {
	cases := map[string]string{
		"repeated character": strings.Repeat("a", 40),
		"placeholder":        "changeme-changeme-changeme-changeme",
		"example value":      "your-secret-key-here-change-in-production",
	}
	for name, secret := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := prodConfig()
			cfg.JWTSecret = secret

			out := checkSecurity(cfg, "")
			if out.status != doctorStatusError {
				t.Fatalf("a long but guessable signing key is forgeable; want error, got %q (%s)", out.status, out.message)
			}
			if !strings.Contains(out.message, "jwt_secret") {
				t.Errorf("message must name the offending key, got %q", out.message)
			}
		})
	}
}

func TestCheckSecurity_CSRFInsecureCookieInProduction(t *testing.T) {
	cfg := prodConfig()
	cfg.CSRFEnabled = true
	cfg.CSRFInsecureCookie = true

	out := checkSecurity(cfg, "")
	if out.status != doctorStatusError {
		t.Fatalf("dropping Secure from the CSRF cookie in production exposes the double-submit token over plain HTTP; want error, got %q (%s)", out.status, out.message)
	}
}

// A hardened production configuration must come out clean, or the check is
// noise nobody will read.
func TestCheckSecurity_HardenedProductionPasses(t *testing.T) {
	cfg := prodConfig()
	cfg.CSRFEnabled = true
	cfg.CORSOrigins = []string{"https://app.example.com"}
	cfg.TrustedProxies = []string{"10.0.0.0/8"}
	cfg.RateLimitRequests = 100

	out := checkSecurity(cfg, "")
	if out.status != doctorStatusPass {
		t.Fatalf("a hardened production config must pass, got %q (%s)", out.status, out.message)
	}
}

// Development is not production: the same knobs that are errors when
// exposed to the internet are ordinary local convenience.
func TestCheckSecurity_DevelopmentIsNotJudgedAsProduction(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.CSRFInsecureCookie = true

	out := checkSecurity(&cfg, "")
	if out.status == doctorStatusError {
		t.Fatalf("local development must not be flagged as an error: %s", out.message)
	}
}
