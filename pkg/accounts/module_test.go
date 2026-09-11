package accounts_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/accounts"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"

	_ "modernc.org/sqlite"
)

// mailerFunc records what the service asked to send, so a test can read a
// link out of it the way a user reads their inbox.
type mailerFunc struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *mailerFunc) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mailerFunc) tokens() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, msg := range m.sent {
		if _, after, ok := strings.Cut(msg.Body, "token="); ok {
			token, _, _ := strings.Cut(after, " ")
			out = append(out, strings.TrimSpace(strings.TrimSuffix(token, ".")))
		}
	}
	return out
}

// startAccounts boots a real application with the accounts module mounted,
// so the probes below exercise the routes an application actually gets.
func startAccounts(t *testing.T) (*nucleustest.Server, *mailerFunc) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:accounts_http_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	store, err := accounts.NewSQLStore(t.Context(), db, accounts.SQLStoreConfig{Flavor: accounts.FlavorSQLite})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	mailer := &mailerFunc{}
	sessions := auth.NewSessionManager(auth.SessionConfig{})
	svc, err := accounts.New(store, sessions, mailer, nil, accounts.Config{
		BaseURL: "https://app.example.test", From: "no-reply@example.test",
	}, nil)
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("accounts-module-secret", 2)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Options: []app.Option{app.WithOpenAuthz()},
		Modules: map[string]nucleus.ModuleSpec{"accounts": accounts.Module(svc)},
	})
	return srv, mailer
}

func postJSON(t *testing.T, srv *nucleustest.Server, client *http.Client, path string, body any) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := client.Post(srv.URL(path), "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return resp.StatusCode, decoded
}

// The registration endpoint answers identically for a new address and for
// one already registered — status AND body. Anything else is an account
// enumeration oracle served over HTTP.
func TestRoutes_RegisterDoesNotDiscloseExistingAccounts(t *testing.T) {
	srv, _ := startAccounts(t)
	client := srv.Client()

	firstStatus, firstBody := postJSON(t, srv, client, accounts.RouteRegister, map[string]string{
		"email": "ana@example.test", "password": "a-long-enough-passphrase",
	})
	secondStatus, secondBody := postJSON(t, srv, client, accounts.RouteRegister, map[string]string{
		"email": "ana@example.test", "password": "another-long-passphrase",
	})

	if firstStatus != http.StatusAccepted || secondStatus != http.StatusAccepted {
		t.Fatalf("statuses differ: %d then %d", firstStatus, secondStatus)
	}
	if fmt.Sprint(firstBody) != fmt.Sprint(secondBody) {
		t.Fatalf("bodies differ:\n %v\n %v", firstBody, secondBody)
	}
}

func TestRoutes_LoginAndSessionCookie(t *testing.T) {
	srv, mailer := startAccounts(t)
	client := srv.Client()

	if status, _ := postJSON(t, srv, client, accounts.RouteRegister, map[string]string{
		"email": "ana@example.test", "password": "a-long-enough-passphrase",
	}); status != http.StatusAccepted {
		t.Fatalf("register: %d", status)
	}

	// Confirm the address through the link, the way a user clicks it.
	tokens := mailer.tokens()
	if len(tokens) == 0 {
		t.Fatal("no verification link was sent")
	}
	resp, err := client.Get(srv.URL(accounts.RouteVerifyEmail + "?token=" + tokens[0]))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify answered %d", resp.StatusCode)
	}

	status, body := postJSON(t, srv, client, accounts.RouteLogin, map[string]string{
		"email": "ana@example.test", "password": "a-long-enough-passphrase",
	})
	if status != http.StatusOK {
		t.Fatalf("login answered %d: %v", status, body)
	}
	if body["email"] != "ana@example.test" {
		t.Fatalf("login body is %v", body)
	}
}

func TestRoutes_WrongPasswordIs401AndLockoutIs429(t *testing.T) {
	srv, _ := startAccounts(t)
	client := srv.Client()
	if status, _ := postJSON(t, srv, client, accounts.RouteRegister, map[string]string{
		"email": "ana@example.test", "password": "a-long-enough-passphrase",
	}); status != http.StatusAccepted {
		t.Fatal("register failed")
	}

	status, _ := postJSON(t, srv, client, accounts.RouteLogin, map[string]string{
		"email": "ana@example.test", "password": "wrong-passphrase-here",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("a wrong password answered %d, want 401", status)
	}

	var last int
	for i := 0; i < 12; i++ {
		last, _ = postJSON(t, srv, client, accounts.RouteLogin, map[string]string{
			"email": "ana@example.test", "password": "wrong-passphrase-here",
		})
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("a locked account answered %d, want 429", last)
	}
}

// The account whose password changes is the one in the SESSION, never one
// named in the body.
func TestRoutes_PasswordChangeRequiresASession(t *testing.T) {
	srv, _ := startAccounts(t)
	status, _ := postJSON(t, srv, srv.Client(), accounts.RoutePasswordChange, map[string]string{
		"current_password": "a-long-enough-passphrase", "new_password": "a-new-long-passphrase",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("an anonymous password change answered %d, want 401", status)
	}
}

func TestRoutes_InvalidTokenIsAlwaysTheSameAnswer(t *testing.T) {
	srv, _ := startAccounts(t)
	client := srv.Client()

	resp, err := client.Get(srv.URL(accounts.RouteVerifyEmail + "?token=not-a-real-token"))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an unknown token answered %d", resp.StatusCode)
	}
}
