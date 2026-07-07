package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

type senderFunc func(context.Context, Message) error

func (f senderFunc) Send(ctx context.Context, msg Message) error {
	return f(ctx, msg)
}

func TestNewSender_DefaultsToNoop(t *testing.T) {
	sender, err := NewSender(Config{})
	if err != nil {
		t.Fatalf("NewSender returned error: %v", err)
	}

	err = sender.Send(context.Background(), Message{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "hello",
		Body:    "world",
	})
	if err != nil {
		t.Fatalf("noop sender should succeed, got %v", err)
	}
}

func TestNewSender_UnknownDriver(t *testing.T) {
	_, err := NewSender(Config{Driver: "missing-provider"})
	if err == nil {
		t.Fatal("expected unknown driver error")
	}
	if !strings.Contains(err.Error(), `unknown mail driver "missing-provider"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisteredProvidersIncludesBuiltins(t *testing.T) {
	registered := RegisteredProviders()
	joined := strings.Join(registered, ",")
	for _, expected := range []string{"noop", "smtp"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected built-in provider %q in registered providers: %v", expected, registered)
		}
	}
}

func TestRegisterProviderAndResolve(t *testing.T) {
	name := strings.ToLower(fmt.Sprintf("testprovider%d", time.Now().UnixNano()))
	called := false

	err := RegisterProvider(name, func(Config) (Sender, error) {
		return senderFunc(func(context.Context, Message) error {
			called = true
			return nil
		}), nil
	})
	if err != nil {
		t.Fatalf("RegisterProvider failed: %v", err)
	}

	sender, err := NewSender(Config{Driver: name})
	if err != nil {
		t.Fatalf("NewSender failed: %v", err)
	}
	if err := sender.Send(context.Background(), Message{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "ping",
		Body:    "pong",
	}); err != nil {
		t.Fatalf("custom sender send failed: %v", err)
	}
	if !called {
		t.Fatal("expected registered provider sender to be called")
	}
}

func TestBuildRFC822MessageIncludesSortedCustomHeaders(t *testing.T) {
	payload := string(buildRFC822Message(Message{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "Hello",
		Body:    "Line",
		Headers: map[string]string{
			"X-Beta":  "2",
			"X-Alpha": "1",
		},
	}))

	alphaPos := strings.Index(payload, "X-Alpha: 1")
	betaPos := strings.Index(payload, "X-Beta: 2")
	if alphaPos == -1 || betaPos == -1 {
		t.Fatalf("missing custom headers in payload: %s", payload)
	}
	if alphaPos > betaPos {
		t.Fatalf("expected headers sorted alphabetically, got payload: %s", payload)
	}
	if !strings.Contains(payload, "\r\n\r\nLine") {
		t.Fatalf("expected body separator in payload: %s", payload)
	}
}

func TestValidateMessageRejectsHeaderInjection(t *testing.T) {
	base := func(headers map[string]string) Message {
		return Message{
			From:    "noreply@example.com",
			To:      []string{"dev@example.com"},
			Subject: "Hello",
			Body:    "Line",
			Headers: headers,
		}
	}

	cases := []struct {
		name    string
		headers map[string]string
		wantErr bool
	}{
		{name: "clean custom headers pass", headers: map[string]string{"X-Alpha": "1"}, wantErr: false},
		{name: "no custom headers pass", headers: nil, wantErr: false},
		{name: "CRLF in value rejected", headers: map[string]string{"X-Alpha": "1\r\nBcc: evil@example.com"}, wantErr: true},
		{name: "bare LF in value rejected", headers: map[string]string{"X-Alpha": "1\nBcc: evil@example.com"}, wantErr: true},
		{name: "CRLF in key rejected", headers: map[string]string{"X-Alpha\r\nBcc": "evil@example.com"}, wantErr: true},
		{name: "bare CR in key rejected", headers: map[string]string{"X-Alpha\r": "1"}, wantErr: true},
		{name: "blank key rejected", headers: map[string]string{"  ": "1"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMessage(base(tc.headers))
			if tc.wantErr && err == nil {
				t.Fatalf("expected validation error for headers %v", tc.headers)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestNewSender_UsesCapabilityPluginForMailSend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	// Skip on macOS due to potential process execution restrictions in test environments
	if runtime.GOOS == "darwin" && os.Getenv("CI") != "true" {
		t.Skip("skipping on macOS outside CI due to process execution restrictions")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "nucleus-plugin-mailgun")
	writeMailExecutable(t, pluginPath, "#!/bin/sh\nexit 0\n")

	previousPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+previousPath); err != nil {
		t.Fatalf("set PATH failed: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", previousPath)
	}()
	host := &fakePluginHost{}
	SetPluginHost(host)
	defer SetPluginHost(nil)

	sender, err := NewSender(Config{Driver: "mailgun", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewSender failed: %v", err)
	}

	if err := sender.Send(context.Background(), Message{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "hello",
		Body:    "world",
	}); err != nil {
		t.Fatalf("capability plugin send failed: %v", err)
	}
	if host.probeCalls == 0 {
		t.Fatal("expected ProbeCapabilities to be called")
	}
	if host.execCalls == 0 {
		t.Fatal("expected ExecuteRequest to be called")
	}
}

type fakePluginHost struct {
	probeCalls int
	execCalls  int
}

func (f *fakePluginHost) CollectInventory(string, []string, time.Duration) []plugins.Descriptor {
	return nil
}

func (f *fakePluginHost) ProbeCapabilities(context.Context, string, time.Duration) ([]string, error) {
	f.probeCalls++
	return []string{plugins.CapabilityMailSend}, nil
}

func (f *fakePluginHost) ExecuteRequest(context.Context, string, plugins.RequestEnvelope, time.Duration) (plugins.ResponseEnvelope, error) {
	f.execCalls++
	return plugins.ResponseEnvelope{
		Version: plugins.EnvelopeVersionV1,
		OK:      true,
		Output:  json.RawMessage(`{"accepted":true}`),
	}, nil
}

func TestSetPluginHost_AllowsRuntimeOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	// Skip on macOS due to potential process execution restrictions in test environments
	if runtime.GOOS == "darwin" && os.Getenv("CI") != "true" {
		t.Skip("skipping on macOS outside CI due to process execution restrictions")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "nucleus-plugin-mailgun")
	writeMailExecutable(t, pluginPath, "#!/bin/sh\nexit 0\n")

	previousPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+previousPath); err != nil {
		t.Fatalf("set PATH failed: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", previousPath)
	}()

	host := &fakePluginHost{}
	SetPluginHost(host)
	defer SetPluginHost(nil)

	sender, err := NewSender(Config{Driver: "mailgun", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewSender failed: %v", err)
	}

	if err := sender.Send(context.Background(), Message{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "hello",
		Body:    "world",
	}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if host.probeCalls == 0 {
		t.Fatal("expected ProbeCapabilities to be called on overridden plugin host")
	}
	if host.execCalls == 0 {
		t.Fatal("expected ExecuteRequest to be called on overridden plugin host")
	}
}

func writeMailExecutable(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o755); err != nil {
		t.Fatalf("write executable failed: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod executable failed: %v", err)
		}
	}
}

func TestContainsCapability(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		capabilities := []string{"mail.send", "queue.publish"}
		if !containsCapability(capabilities, "mail.send") {
			t.Error("expected true for exact match")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		capabilities := []string{"mail.send", "queue.publish"}
		if !containsCapability(capabilities, "MAIL.SEND") {
			t.Error("expected true for case insensitive match")
		}
	})

	t.Run("with whitespace", func(t *testing.T) {
		capabilities := []string{"mail.send", "queue.publish"}
		if !containsCapability(capabilities, "  mail.send  ") {
			t.Error("expected true with whitespace")
		}
	})

	t.Run("not found", func(t *testing.T) {
		capabilities := []string{"mail.send", "queue.publish"}
		if containsCapability(capabilities, "cache.get") {
			t.Error("expected false for not found")
		}
	})

	t.Run("empty capability", func(t *testing.T) {
		capabilities := []string{"mail.send", "queue.publish"}
		if containsCapability(capabilities, "") {
			t.Error("expected false for empty capability")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		capabilities := []string{}
		if containsCapability(capabilities, "mail.send") {
			t.Error("expected false for empty list")
		}
	})

	t.Run("capability with whitespace in list", func(t *testing.T) {
		capabilities := []string{"  mail.send  ", "queue.publish"}
		if !containsCapability(capabilities, "mail.send") {
			t.Error("expected true with whitespace in list")
		}
	})
}
