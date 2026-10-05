// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/mail"
)

// WithoutDefaults builds no mail sender; WithMail builds the one the
// configuration declares.
func TestWithMail_BuildsTheSenderWithoutDefaults(t *testing.T) {
	bare, err := New(testAppConfig(), WithoutDefaults())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = bare.Shutdown(context.Background()) })
	if bare.Mailer != nil {
		t.Fatal("WithoutDefaults built a mail sender")
	}

	cfg := testAppConfig()
	cfg.MailDriver = "memory"
	withMail, err := New(cfg, WithoutDefaults(), WithMail())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = withMail.Shutdown(context.Background()) })
	if _, ok := withMail.Mailer.(*mail.MemorySender); !ok {
		t.Fatalf("WithMail with mail_driver: memory built %T", withMail.Mailer)
	}

	// With nothing declared it is the default driver, which delivers nothing.
	unset, err := New(testAppConfig(), WithoutDefaults(), WithMail())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = unset.Shutdown(context.Background()) })
	if !mail.Discards(unset.Mailer) {
		t.Fatalf("WithMail with no mail_driver built %T, want the noop driver", unset.Mailer)
	}
}

// mail_driver: log is development's: refused at load and at boot anywhere
// else, because what it writes to the log includes reset links.
func TestMailLogDriver_DevelopmentOnly(t *testing.T) {
	for _, env := range []string{"production", "staging", "test"} {
		cfg := testAppConfig()
		cfg.Env = env
		cfg.MailDriver = "log"
		if err := ValidateReferential(cfg); !errors.Is(err, ErrInvalidConfigReference) || !strings.Contains(err.Error(), "development") {
			t.Errorf("env %s: the configuration check let mail_driver: log through: %v", env, err)
		}
		if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "mail_driver") {
			t.Errorf("env %s: app.New built the log driver: %v", env, err)
		}
		if _, err := New(cfg, WithoutDefaults(), WithMail()); err == nil {
			t.Errorf("env %s: WithMail built the log driver", env)
		}
	}
	for _, env := range []string{"development", ""} {
		cfg := testAppConfig()
		cfg.Env = env
		cfg.MailDriver = "log"
		if err := ValidateReferential(cfg); err != nil {
			t.Errorf("env %q: %v", env, err)
		}
		a, err := New(cfg, WithoutDefaults(), WithMail())
		if err != nil {
			t.Fatalf("env %q: New: %v", env, err)
		}
		_ = a.Shutdown(context.Background())
		if a.Mailer == nil || mail.Discards(a.Mailer) {
			t.Errorf("env %q: the log driver was not built: %T", env, a.Mailer)
		}
	}
}
