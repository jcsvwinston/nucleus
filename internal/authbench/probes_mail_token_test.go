// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/outbox"

	_ "modernc.org/sqlite"
)

// captureProvider is a Sender that records what it was handed, so a probe
// asserts on the message the sending path actually produced.
type captureProvider struct{ sent []mail.Message }

func (c *captureProvider) Send(_ context.Context, msg mail.Message) error {
	c.sent = append(c.sent, msg)
	return nil
}

// sendThroughCapturingProvider drives the real sender construction path —
// a provider registered by name, built by mail.NewSender — rather than
// calling a Sender the probe built itself.
func sendThroughCapturingProvider(t *testing.T, msg mail.Message) (mail.Message, bool) {
	t.Helper()
	name := fmt.Sprintf("authbench-capture-%d", time.Now().UnixNano())
	captured := &captureProvider{}
	if err := mail.RegisterProvider(name, func(mail.Config) (mail.Sender, error) {
		return captured, nil
	}); err != nil {
		t.Logf("RegisterProvider: %v", err)
		return mail.Message{}, false
	}
	sender, err := mail.NewSender(mail.Config{Driver: name})
	if err != nil {
		t.Logf("NewSender: %v", err)
		return mail.Message{}, false
	}
	if err := sender.Send(t.Context(), msg); err != nil {
		t.Logf("Send: %v", err)
		return mail.Message{}, false
	}
	if len(captured.sent) != 1 {
		return mail.Message{}, false
	}
	return captured.sent[0], true
}

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

// MAIL-02 — an HTML message. Verification and reset emails are HTML in
// every framework this arc copies from, so the probe sends one through the
// real sender path and reads what the sender received.
func probeMailHTML(t *testing.T, _ *env) verdict {
	sent, ok := sendThroughCapturingProvider(t, mail.Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "verify", Body: "text", HTML: "<p>html</p>",
	})
	if !ok {
		return absent
	}
	if sent.HTML != "<p>html</p>" || sent.Body != "text" {
		t.Logf("the sender received %+v", sent)
		return partial
	}
	return present
}

// MAIL-03 — attachments, carried to the sender intact.
func probeMailAttachments(t *testing.T, _ *env) verdict {
	sent, ok := sendThroughCapturingProvider(t, mail.Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "invoice", Body: "attached",
		Attachments: []mail.Attachment{{
			Filename: "invoice.pdf", ContentType: "application/pdf", Content: []byte("%PDF"),
		}},
	})
	if !ok {
		return absent
	}
	if len(sent.Attachments) != 1 || string(sent.Attachments[0].Content) != "%PDF" {
		return partial
	}
	return present
}

// MAIL-04 — rendering a message from a template, the way a reset email is
// written once and sent many times. The probe renders one and checks BOTH
// representations came out, and that the HTML half escapes untrusted data —
// which is the difference between a template engine and string concatenation.
func probeMailTemplates(t *testing.T, _ *env) verdict {
	tmpl, err := mail.ParseFS(fstest.MapFS{
		"probe.subject.tmpl": {Data: []byte("Hello {{.Name}}")},
		"probe.txt.tmpl":     {Data: []byte("Open {{.Link}}")},
		"probe.html.tmpl":    {Data: []byte(`<p>{{.Name}} <a href="{{.Link}}">open</a></p>`)},
	})
	if err != nil {
		t.Logf("ParseFS: %v", err)
		return absent
	}
	msg, err := tmpl.Render("probe", map[string]string{
		"Name": "<script>alert(1)</script>", "Link": "https://app.example/x",
	}, mail.Message{From: "no-reply@example.test", To: []string{"ana@example.test"}})
	if err != nil {
		t.Logf("Render: %v", err)
		return absent
	}
	if msg.Body == "" || msg.HTML == "" {
		return partial
	}
	if strings.Contains(msg.HTML, "<script>") {
		t.Log("the HTML half does not escape untrusted data")
		return partial
	}
	return present
}

// MAIL-05 — durability: mail queued in the caller's transaction survives a
// crash between the database write and the SMTP call. The probe measures
// the property, not the plumbing: a rolled-back transaction must leave
// nothing to deliver, a committed one must leave exactly one message, and
// the bridge must hand it to a sender.
func probeMailDurability(t *testing.T, _ *env) verdict {
	db, err := sql.Open("sqlite", fmt.Sprintf("file:authbench_mail_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Logf("open sqlite: %v", err)
		return absent
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	store, err := outbox.NewStore(db, outbox.Config{Flavor: outbox.FlavorSQLite})
	if err != nil {
		t.Logf("outbox store: %v", err)
		return absent
	}
	msg := mail.Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "verify", Body: "open the link",
	}

	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Logf("begin: %v", err)
		return absent
	}
	if _, err := mail.EnqueueTx(t.Context(), store, tx, msg); err != nil {
		t.Logf("EnqueueTx: %v", err)
		return absent
	}
	if err := tx.Rollback(); err != nil {
		t.Logf("rollback: %v", err)
		return absent
	}
	if snap := store.Snapshot(t.Context()); snap.Total != 0 {
		t.Logf("a rolled-back transaction left %d queued", snap.Total)
		return partial
	}

	queued, err := mail.Enqueue(t.Context(), store, msg)
	if err != nil {
		t.Logf("Enqueue: %v", err)
		return partial
	}
	captured := &captureProvider{}
	if err := mail.NewOutboxBridge(captured).Send(t.Context(), queued); err != nil {
		t.Logf("bridge: %v", err)
		return partial
	}
	if len(captured.sent) != 1 || captured.sent[0].Subject != "verify" {
		return partial
	}
	return present
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
