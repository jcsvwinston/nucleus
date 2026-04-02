package cli

import (
	"strings"
	"testing"
)

func TestCollectSendTestEmailRecipients(t *testing.T) {
	recipients, err := collectSendTestEmailRecipients(
		"alice@example.com,bob@example.com",
		[]string{"carol@example.com", "alice@example.com"},
	)
	if err != nil {
		t.Fatalf("collectSendTestEmailRecipients failed: %v", err)
	}

	got := strings.Join(recipients, ",")
	want := "alice@example.com,bob@example.com,carol@example.com"
	if got != want {
		t.Fatalf("unexpected recipients: got %q want %q", got, want)
	}
}

func TestBuildSMTPMessage(t *testing.T) {
	payload := string(buildSMTPMessage(
		"noreply@example.com",
		[]string{"dev@example.com"},
		"Test Subject",
		"Body line",
	))
	if !strings.Contains(payload, "From: noreply@example.com") {
		t.Fatalf("missing From header: %s", payload)
	}
	if !strings.Contains(payload, "To: dev@example.com") {
		t.Fatalf("missing To header: %s", payload)
	}
	if !strings.Contains(payload, "Subject: Test Subject") {
		t.Fatalf("missing Subject header: %s", payload)
	}
	if !strings.HasSuffix(payload, "Body line") {
		t.Fatalf("missing body: %s", payload)
	}
}
