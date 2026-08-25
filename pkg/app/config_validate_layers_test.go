// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Regression tests for the "same file, two verdicts" gap of ADR-010 §2
// layers 3–4: validateSemantics/validateReferential ran only on the
// pkg/nucleus surfaces, so `log_level: verbose` failed `go run .` but
// sailed through every `nucleus <cmd>` (whose LoadConfig skipped both
// layers). The layers now live in pkg/app and LoadConfig applies them on
// the EFFECTIVE config — post-profile, post-normalisation, the same order
// the builder uses.
package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nucleus.yml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_AppliesSemanticLayer(t *testing.T) {
	path := writeConfigFile(t, "log_level: verbose\n")
	_, err := LoadConfig(path)
	if !errors.Is(err, ErrInvalidConfigValue) {
		t.Fatalf("the CLI path must reject what the builder rejects: want ErrInvalidConfigValue, got %v", err)
	}
	for _, want := range []string{"log_level", "verbose"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name %q, got %v", want, err)
		}
	}
}

func TestLoadConfig_AppliesReferentialLayer(t *testing.T) {
	path := writeConfigFile(t, "mail_driver: smtp\n")
	_, err := LoadConfig(path)
	if !errors.Is(err, ErrInvalidConfigReference) {
		t.Fatalf("want ErrInvalidConfigReference for smtp without smtp_host, got %v", err)
	}
	if !strings.Contains(err.Error(), "smtp_host") {
		t.Errorf("error must name smtp_host, got %v", err)
	}
}

// The layers validate the EFFECTIVE config: `profile: dev` swaps backing
// services before validation, so a production config that the profile
// rewrites must still load.
func TestLoadConfig_ValidatesAfterProfile(t *testing.T) {
	path := writeConfigFile(t, "profile: dev\nsession_store: redis\nlog_level: info\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("profile: dev must produce a valid effective config, got %v", err)
	}
	if cfg.SessionStore != "memory" {
		t.Fatalf("profile: dev should have swapped session_store to memory, got %q", cfg.SessionStore)
	}
}

// A valid config keeps loading — the layers only reject explicitly wrong
// values (empty/zero mean "use the default").
func TestLoadConfig_ValidConfigStillLoads(t *testing.T) {
	path := writeConfigFile(t, "log_level: info\nport: 8080\n")
	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("valid config must load, got %v", err)
	}
}

// The credential-source class, closed end-to-end: the shapes the README
// promises for sensitive values must LOAD through the real config path —
// both the full source shape and the legacy plain string.
func TestLoadConfig_CredentialSourceShapesBind(t *testing.T) {
	path := writeConfigFile(t, `
storage:
  provider: s3
  s3:
    bucket: b
    access_key_id:
      env_var: AWS_ACCESS_KEY_ID
    secret_access_key: literal-secret
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("the documented credential shape must load, got %v", err)
	}
	if cfg.Storage.S3.AccessKeyID.EnvVar != "AWS_ACCESS_KEY_ID" {
		t.Fatalf("env_var source did not bind: %+v", cfg.Storage.S3.AccessKeyID)
	}
	// Legacy scalar promotes to {Value: …} via the decode hook.
	if cfg.Storage.S3.SecretAccessKey.Value != "literal-secret" {
		t.Fatalf("plain-string credential did not promote to Value: %+v", cfg.Storage.S3.SecretAccessKey)
	}
}

// The __Host- / __Secure- cookie-name prefixes are not decoration: browsers
// reject a cookie whose attributes contradict its prefix, so the pairing is
// part of the config's meaning, not of the session subsystem's plumbing.
// Until this test, the three rules lived ONLY in the session builder — a
// contradiction loaded clean, `nucleus config validate` called the file
// good, and the app died at boot instead. Same class as the SameSite=None
// rule right next to it: reject it where the file is judged.
func TestValidateReferential_HostCookiePrefix(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		wantIn   string
	}{
		{
			name:     "__Host- requires Secure",
			contents: "session_cookie_name: \"__Host-session\"\nsession_cookie_secure: false\n",
			wantIn:   "session_cookie_secure=true",
		},
		{
			name:     "__Host- forbids Domain",
			contents: "session_cookie_name: \"__Host-session\"\nsession_cookie_domain: example.com\n",
			wantIn:   "session_cookie_domain",
		},
		{
			name:     "__Host- requires Path=/",
			contents: "session_cookie_name: \"__Host-session\"\nsession_cookie_path: /app\n",
			wantIn:   "session_cookie_path",
		},
		{
			name:     "__Secure- requires Secure",
			contents: "session_cookie_name: \"__Secure-session\"\nsession_cookie_secure: false\n",
			wantIn:   "session_cookie_secure=true",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfigFile(t, tc.contents))
			if !errors.Is(err, ErrInvalidConfigReference) {
				t.Fatalf("want ErrInvalidConfigReference, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error must name the offending key (%q), got %v", tc.wantIn, err)
			}
		})
	}
}

// The prefixes are opt-in: the default cookie name carries none, so none of
// the three rules may fire for a configuration that never asked for them.
func TestValidateReferential_HostCookiePrefix_NotOptIn(t *testing.T) {
	path := writeConfigFile(t, "session_cookie_name: session\nsession_cookie_secure: false\nsession_cookie_domain: example.com\nsession_cookie_path: /app\n")
	if _, err := LoadConfig(path); err != nil {
		t.Fatalf("a name without a prefix must not trigger the prefix rules: %v", err)
	}
}
