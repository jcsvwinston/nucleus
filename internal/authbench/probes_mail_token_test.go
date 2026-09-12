// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/mail"
)

// MAIL-01 — sending a plain-text message through the framework's sender.
func probeMailPlain(t *testing.T, _ *env) verdict {
	sender, err := mail.NewSender(mail.Config{Driver: "noop"})
	if err != nil {
		t.Logf("NewSender: %v", err)
		return absent
	}
	if err := sender.Send(t.Context(), mail.Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "probe", Body: "hello",
	}); err != nil {
		t.Logf("Send: %v", err)
		return partial
	}
	return present
}

// MAIL-02 — an HTML message. Verification and reset emails are HTML in every
// framework this arc copies from, and a Message that carries one Body has
// one representation to offer.
func probeMailHTML(t *testing.T, _ *env) verdict {
	fields := structFieldNames(mail.Message{})
	if strings.Contains(strings.ToLower(fields), "html") {
		return present
	}
	t.Logf("Message carries %s", fields)
	return absent
}

// MAIL-03 — attachments.
func probeMailAttachments(t *testing.T, _ *env) verdict {
	if strings.Contains(strings.ToLower(structFieldNames(mail.Message{})), "attach") {
		return present
	}
	return absent
}

// MAIL-04 — rendering a message from a template, the way a reset email is
// written once and sent many times.
func probeMailTemplates(t *testing.T, _ *env) verdict {
	typ := reflect.TypeOf(mail.Message{})
	for i := 0; i < typ.NumField(); i++ {
		if n := strings.ToLower(typ.Field(i).Name); strings.Contains(n, "template") {
			return present
		}
	}
	return absent
}

// MAIL-05 — durability: a verification email that is queued survives a crash
// between the database write and the SMTP call.
func probeMailDurability(t *testing.T, _ *env) verdict {
	// The outbox exists as a general mechanism; what a probe can settle is
	// whether the mail sender goes through it by default.
	if _, ok := hasConfigKeyContaining("mail_outbox"); ok {
		return present
	}
	if keys := configKeys(); keys["outbox_enabled"] || keys["mail_driver"] {
		return partial
	}
	return absent
}

// TOK-01 — signed tokens with a keyring and rotation.
func probeJWTRotation(t *testing.T, _ *env) verdict {
	m, err := auth.NewJWTManagerFromKeys([]auth.SigningKey{
		{KID: "k1", Algorithm: auth.HS256, HMACSecret: []byte(strings.Repeat("authbench-secret", 2))},
	}, "k1", time.Hour, "authbench")
	if err != nil {
		t.Logf("NewJWTManagerFromKeys: %v", err)
		return absent
	}
	token, err := m.Generate("1", "ana", "editor")
	if err != nil {
		t.Logf("Generate: %v", err)
		return absent
	}
	if _, err := m.Validate(token); err != nil {
		t.Logf("Validate: %v", err)
		return partial
	}
	if m.CurrentKID() != "k1" {
		return partial
	}
	return present
}

// TOK-02 — a token minted for another audience is rejected.
func probeJWTAudience(t *testing.T, _ *env) verdict {
	mine := auth.NewJWTManager(strings.Repeat("authbench-secret-a", 2), time.Hour, "authbench")
	theirs := auth.NewJWTManager(strings.Repeat("authbench-secret-b", 2), time.Hour, "elsewhere")
	token, err := theirs.Generate("1", "ana", "editor")
	if err != nil {
		return absent
	}
	if _, err := mine.Validate(token); err == nil {
		t.Log("a token signed by another issuer validated")
		return absent
	}
	return present
}

// TOK-03 — revoking an issued token before it expires.
func probeJWTRevocation(t *testing.T, _ *env) verdict {
	typ := reflect.TypeOf(&auth.JWTManager{})
	for i := 0; i < typ.NumMethod(); i++ {
		if n := strings.ToLower(typ.Method(i).Name); strings.Contains(n, "revoke") || strings.Contains(n, "blocklist") || strings.Contains(n, "denylist") {
			t.Logf("JWTManager has %s", typ.Method(i).Name)
			return present
		}
	}
	return absent
}

// POS-01 — the default security posture is frozen as OBSERVED values.
func probeSecurityPosture(t *testing.T, _ *env) verdict {
	path := filepath.Join("..", "..", "contracts", "baseline", "security_posture.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Logf("read baseline: %v", err)
		return absent
	}
	if !strings.Contains(string(b), "[config-defaults]") {
		return partial
	}
	return present
}

// POS-02 — the posture is mapped to a named standard, control by control,
// which is what "ASVS L2 verified" has to mean to be checkable.
func probeASVSCoverage(t *testing.T, _ *env) verdict {
	path := filepath.Join("..", "..", "contracts", "baseline", "security_posture.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		return absent
	}
	if strings.Contains(strings.ToUpper(string(b)), "ASVS") {
		return present
	}
	return absent
}
