// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// NU-114: an application built WithoutDefaults() builds no mail sender, and
// a mail_driver its configuration wrote was ignored without a word — the
// same silence NU-99 removed for storage. WithMail() builds it; without the
// option the application still starts with the driver ignored, as it always
// did, and one ERROR line says so (refused from v2.0.0, DEP-2026-015). An
// application that writes no mail_driver behaves as it always did.

// The configuration that booted before NU-114 still boots (QADR-0010: no
// behaviour flip before the major) — with no sender, as always, and one
// ERROR line that says the driver is ignored, names the option, and
// announces the flip.
func TestWithoutDefaults_DeclaredMailDriver_StartsAndSaysItIsIgnored(t *testing.T) {
	cfg := testAppConfig()
	cfg.MailDriver = "smtp"
	cfg.SMTPHost = "smtp.example.test"
	cfg.SMTPPort = 587
	cfg.MailDeclared = true

	var a *App
	out := captureStdout(t, func() {
		var err error
		a, err = New(cfg, WithoutDefaults())
		if err != nil {
			t.Fatalf("an application that started before NU-114 no longer starts: %v", err)
		}
	})
	defer func() { _ = a.Shutdown(context.Background()) }()
	if a.Mailer != nil {
		t.Fatalf("WithoutDefaults() without WithMail() built a mail sender (%T)", a.Mailer)
	}
	lines := linesContaining(out, "mail_driver IGNORED")
	if len(lines) != 1 {
		t.Fatalf("want exactly one warning line, got %d:\n%s", len(lines), out)
	}
	line := lines[0]
	for _, want := range []string{"level=ERROR", "driver=smtp", "WithMail()", "DEP-2026-015", "v2.0.0"} {
		if !strings.Contains(line, want) {
			t.Errorf("the warning does not say %q:\n%s", want, line)
		}
	}
}

// Nothing written, nothing said — and neither for a mail_driver nobody
// wrote: nucleustest swaps the noop default for the memory driver on every
// application it starts, WithMail() or not, and that is not the
// application's configuration declaring a sender. Nor for noop written by
// hand: it asks for no delivery, and no sender delivers none.
func TestWithoutDefaults_NoMailDriverDeclared_NoWarning(t *testing.T) {
	for name, set := range map[string]func(*Config){
		"nothing written":                          func(*Config) {},
		"a driver the configuration did not write": func(c *Config) { c.MailDriver = "memory" },
		"noop written by hand": func(c *Config) {
			c.MailDriver = "noop"
			c.MailDeclared = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testAppConfig()
			set(cfg)
			out := captureStdout(t, func() {
				a, err := New(cfg, WithoutDefaults())
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				_ = a.Shutdown(context.Background())
			})
			if strings.Contains(out, "mail_driver IGNORED") {
				t.Fatalf("the warning fired for a configuration that declares no sender:\n%s", out)
			}
		})
	}
}

// With the option, the declared driver is built and nothing is said about it;
// on the default stack the sender is one of the defaults.
func TestWithMail_DeclaredMailDriver_NoWarning(t *testing.T) {
	for name, opts := range map[string][]Option{
		"WithoutDefaults + WithMail": {WithoutDefaults(), WithMail()},
		"the defaults":               {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			cfg := testAppConfig()
			cfg.MailDriver = "memory"
			cfg.MailDeclared = true
			var a *App
			out := captureStdout(t, func() {
				var err error
				a, err = New(cfg, opts...)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
			})
			defer func() { _ = a.Shutdown(context.Background()) }()
			if a.Mailer == nil {
				t.Fatal("the declared mail driver was not built")
			}
			if strings.Contains(out, "mail_driver IGNORED") {
				t.Fatalf("the sender was built and the boot log still says it is ignored:\n%s", out)
			}
		})
	}
}

func TestLoadConfig_RecordsWhetherMailIsDeclared(t *testing.T) {
	cases := []struct {
		name string
		file string
		env  map[string]string
		want bool
	}{
		{name: "no mail_driver", file: "port: 8080\n", want: false},
		{name: "a driver", file: "mail_driver: smtp\nsmtp_host: smtp.example.test\nsmtp_port: 587\n", want: true},
		{name: "the default written by hand", file: "mail_driver: noop\n", want: true},
		// smtp_* without a driver selects nothing: the default driver is noop
		// on every application.
		{name: "smtp keys without a driver", file: "smtp_host: smtp.example.test\n", want: false},
		{name: "the environment", file: "port: 8080\n", env: map[string]string{"NUCLEUS_MAIL_DRIVER": "memory"}, want: true},
		{name: "an empty variable", file: "port: 8080\n", env: map[string]string{"NUCLEUS_MAIL_DRIVER": ""}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			path := filepath.Join(t.TempDir(), "nucleus.yml")
			if err := os.WriteFile(path, []byte(tc.file), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.MailDeclared != tc.want {
				t.Fatalf("MailDeclared = %v, want %v", cfg.MailDeclared, tc.want)
			}
		})
	}
}

// The notice the boot log cites exists in the register.
func TestMailIgnoredNoticeExists(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join("..", "..", "docs", "deprecations", depMailIgnored+"-*.md"))
	if len(matches) != 1 {
		t.Fatalf("docs/deprecations/%s-*.md: found %v, want exactly one", depMailIgnored, matches)
	}
}
