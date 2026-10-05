// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package mail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// mail_driver: log writes the message — the link a flow mails included — to
// the log it was built with, and delivers nothing.
func TestLogDriverWritesTheMessage(t *testing.T) {
	var buf bytes.Buffer
	sender, err := NewSender(Config{Driver: "LOG", Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	if err != nil {
		t.Fatalf("NewSender(log): %v", err)
	}
	err = sender.Send(context.Background(), Message{
		From: "no-reply@example.test", To: []string{"ana@example.test"},
		Subject: "Confirm your email address", Body: "Open https://app.example.test/auth/verify-email?token=abc123 to confirm.",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"level=WARN", "driver=log", "to=ana@example.test", `subject="Confirm your email address"`, "token=abc123"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log line lacks %s:\n%s", want, out)
		}
	}
	if Discards(sender) {
		t.Error("the log driver delivers to the log; Discards must not call it a sink")
	}
}

// Discards tells the default driver, which delivers nothing, from the rest.
func TestDiscards(t *testing.T) {
	noop, _ := NewSender(Config{})
	memory, _ := NewSender(Config{Driver: "memory"})
	breaker, _ := NewSender(Config{Driver: "noop", CircuitBreaker: CircuitBreakerConfig{Enabled: true}})
	for name, c := range map[string]struct {
		s    Sender
		want bool
	}{
		"nil": {nil, true}, "noop": {noop, true}, "noop with a breaker configured": {breaker, true}, "memory": {memory, false},
	} {
		if got := Discards(c.s); got != c.want {
			t.Errorf("Discards(%s) = %v, want %v", name, got, c.want)
		}
	}
}
