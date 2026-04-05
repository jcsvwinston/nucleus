package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
	for _, expected := range []string{"noop", "smtp", "sendgrid"} {
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

func TestSendGridSenderSuccess(t *testing.T) {
	type recipient struct {
		Email string `json:"email"`
	}
	type sendGridPayload struct {
		Personalizations []struct {
			To []recipient `json:"to"`
		} `json:"personalizations"`
		From struct {
			Email string `json:"email"`
		} `json:"from"`
		Subject string `json:"subject"`
		Content []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"content"`
	}

	var gotAuth string
	var gotType string
	var gotPayload sendGridPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotType = r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(body, &gotPayload); err != nil {
			t.Fatalf("decode sendgrid payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	sender, err := NewSender(Config{
		Driver:           "sendgrid",
		SendGridAPIKey:   "SG.TEST",
		SendGridEndpoint: srv.URL,
		Timeout:          time.Second,
	})
	if err != nil {
		t.Fatalf("NewSender(sendgrid) failed: %v", err)
	}

	if err := sender.Send(context.Background(), Message{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "Deploy",
		Body:    "ok",
	}); err != nil {
		t.Fatalf("sendgrid send failed: %v", err)
	}

	if gotAuth != "Bearer SG.TEST" {
		t.Fatalf("unexpected authorization header: %q", gotAuth)
	}
	if gotType != "application/json" {
		t.Fatalf("unexpected content type: %q", gotType)
	}
	if gotPayload.From.Email != "noreply@example.com" {
		t.Fatalf("unexpected from email: %q", gotPayload.From.Email)
	}
	if gotPayload.Subject != "Deploy" {
		t.Fatalf("unexpected subject: %q", gotPayload.Subject)
	}
	if len(gotPayload.Personalizations) != 1 || len(gotPayload.Personalizations[0].To) != 1 {
		t.Fatalf("unexpected recipients payload: %#v", gotPayload.Personalizations)
	}
	if gotPayload.Personalizations[0].To[0].Email != "dev@example.com" {
		t.Fatalf("unexpected recipient: %q", gotPayload.Personalizations[0].To[0].Email)
	}
	if len(gotPayload.Content) != 1 || gotPayload.Content[0].Type != "text/plain" || gotPayload.Content[0].Value != "ok" {
		t.Fatalf("unexpected content payload: %#v", gotPayload.Content)
	}
}

func TestSendGridSenderNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	sender, err := NewSender(Config{
		Driver:           "sendgrid",
		SendGridAPIKey:   "SG.TEST",
		SendGridEndpoint: srv.URL,
		Timeout:          time.Second,
	})
	if err != nil {
		t.Fatalf("NewSender(sendgrid) failed: %v", err)
	}

	err = sender.Send(context.Background(), Message{
		From:    "noreply@example.com",
		To:      []string{"dev@example.com"},
		Subject: "Deploy",
		Body:    "ok",
	})
	if err == nil {
		t.Fatal("expected sendgrid non-2xx error")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("unexpected error: %v", err)
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

func TestNewSender_UsesCapabilityPluginForMailSend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	pluginPath := filepath.Join(dir, "goframe-plugin-mailgun")
	writeMailExecutable(t, pluginPath, `#!/bin/sh
if [ "$1" = "capabilities" ] && [ "$2" = "--json" ]; then
  echo '{"capabilities":["mail.send"]}'
  exit 0
fi
if [ "$1" = "capabilities" ]; then
  echo "mail.send"
  exit 0
fi
cat >/dev/null
echo '{"version":"v1","ok":true,"output":{"accepted":true}}'
exit 0
`)

	previousPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+previousPath); err != nil {
		t.Fatalf("set PATH failed: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", previousPath)
	}()

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
}

func TestNewSender_FallsBackToLegacyPluginWhenCapabilityMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based executable test is unix-only")
	}

	dir := t.TempDir()
	genericPath := filepath.Join(dir, "goframe-plugin-mailgun")
	writeMailExecutable(t, genericPath, `#!/bin/sh
if [ "$1" = "capabilities" ] && [ "$2" = "--json" ]; then
  echo '{"capabilities":["queue.publish"]}'
  exit 0
fi
if [ "$1" = "capabilities" ]; then
  echo "queue.publish"
  exit 0
fi
exit 42
`)

	legacyPath := filepath.Join(dir, "goframe-mail-mailgun")
	writeMailExecutable(t, legacyPath, "#!/bin/sh\nexit 0\n")

	previousPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+previousPath); err != nil {
		t.Fatalf("set PATH failed: %v", err)
	}
	defer func() {
		_ = os.Setenv("PATH", previousPath)
	}()

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
		t.Fatalf("legacy fallback send failed: %v", err)
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
