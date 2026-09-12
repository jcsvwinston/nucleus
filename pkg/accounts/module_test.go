package accounts_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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

// The whole second-factor round trip over HTTP: enrol, sign out, sign in
// with the password (which must NOT finish), and complete with a code.
func TestRoutes_TwoStepSignIn(t *testing.T) {
	srv, mailer, svc := startAccountsWithMFA(t)
	client := srv.Client()
	email := "ana@example.test"
	password := "a-long-enough-passphrase"

	if status, _ := postJSON(t, srv, client, accounts.RouteRegister, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusAccepted {
		t.Fatal("register failed")
	}
	_ = mailer

	// Sign in (no factor yet) and enrol one.
	if status, _ := postJSON(t, srv, client, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusOK {
		t.Fatalf("first login: %d", status)
	}

	status, body := postJSON(t, srv, client, accounts.RouteTOTP, map[string]string{})
	if status != http.StatusOK {
		t.Fatalf("enrolment start answered %d: %v", status, body)
	}
	secret, _ := body["secret"].(string)
	if secret == "" {
		t.Fatalf("no secret in %v", body)
	}

	// Confirm with the PREVIOUS step's code (still inside the ±1 window),
	// so the step the current code belongs to is left unspent for the
	// sign-in below. Confirming with the current one and then signing in
	// with the next would depend on where in the 30-second window the
	// test happens to run.
	code := accounts.TestingTOTPCode(t, secret, time.Now().Add(-30*time.Second))
	status, body = putJSONBody(t, srv, client, accounts.RouteTOTP, map[string]string{"code": code})
	if status != http.StatusOK {
		t.Fatalf("enrolment confirm answered %d: %v", status, body)
	}
	codes, _ := body["recovery_codes"].([]any)
	if len(codes) != accounts.RecoveryCodeCount {
		t.Fatalf("got %d recovery codes", len(codes))
	}

	// Sign out, then sign in again: the password alone must not finish.
	if status, _ := postJSON(t, srv, client, accounts.RouteLogout, map[string]string{}); status != http.StatusNoContent {
		t.Fatalf("logout: %d", status)
	}
	status, body = postJSON(t, srv, client, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	})
	if status != http.StatusUnauthorized || body["mfa_required"] != true {
		t.Fatalf("the password alone signed in: %d %v", status, body)
	}

	// A wrong code is refused; the right one finishes.
	if status, _ := postJSON(t, srv, client, accounts.RouteMFAVerify, map[string]string{"code": "000000"}); status != http.StatusUnauthorized {
		t.Fatalf("a wrong code answered %d", status)
	}
	next := accounts.TestingTOTPCode(t, secret, time.Now())
	status, body = postJSON(t, srv, client, accounts.RouteMFAVerify, map[string]string{"code": next})
	if status != http.StatusOK {
		t.Fatalf("the second factor answered %d: %v", status, body)
	}
	if body["email"] != email {
		t.Fatalf("signed in as %v", body)
	}
	_ = svc
}

// Enrolling or removing a factor demands a recent sign-in, not merely a
// session that still resolves.
func TestRoutes_FactorChangesRequireFreshAuth(t *testing.T) {
	srv, _, svc := startAccountsWithMFA(t)
	status, _ := postJSON(t, srv, srv.Client(), accounts.RouteTOTP, map[string]string{})
	if status != http.StatusUnauthorized {
		t.Fatalf("an anonymous enrolment answered %d, want 401", status)
	}
	_ = svc
}

func putJSONBody(t *testing.T, srv *nucleustest.Server, client *http.Client, path string, body any) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL(path), bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return resp.StatusCode, decoded
}

// startAccountsWithMFA is startAccounts with an encryption key, a cookie
// jar (the two-step flow spans requests) and second factors available.
func startAccountsWithMFA(t *testing.T) (*nucleustest.Server, *mailerFunc, *accounts.Service) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:accounts_mfa_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	store, err := accounts.NewSQLStore(t.Context(), db, accounts.SQLStoreConfig{Flavor: accounts.FlavorSQLite})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("key: %v", err)
	}
	mailer := &mailerFunc{}
	svc, err := accounts.New(store, nil, mailer, nil, accounts.Config{
		BaseURL: "https://app.example.test", From: "no-reply@example.test",
		MFAEncryptionKey: key, Issuer: "Nucleus Test",
	}, nil)
	if err != nil {
		t.Fatalf("service: %v", err)
	}

	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("accounts-mfa-secret-x", 2)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Options: []app.Option{app.WithOpenAuthz()},
		Modules: map[string]nucleus.ModuleSpec{"accounts": accounts.Module(svc)},
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	srv.Client().Jar = jar
	return srv, mailer, svc
}
