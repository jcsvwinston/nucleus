// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

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
	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// env carries what several probes share: one booted application, started at
// most once per run and torn down by the parent test.
//
// The application is a DEFAULT one — no module mounted, nothing wired by
// hand. What a probe asks it is what an application gets for FREE. A
// control that only works because the probe wired it by hand would measure
// the probe, not the framework.
//
// accountsServer below is the deliberate exception, with its own criterion:
// an opt-in module the framework ships is capability an application HAS,
// the way an optional driver is. Every case measured there says so.
type env struct {
	tb   testing.TB
	once sync.Once
	srv  *nucleustest.Server

	accountsOnce sync.Once
	accounts     *nucleustest.Server
	accountsMail *benchMailer
}

func newEnv(tb testing.TB) *env { return &env{tb: tb} }

func (e *env) server() *nucleustest.Server {
	e.once.Do(func() {
		cfg := app.DefaultConfig()
		cfg.Env = "development"
		// The default database URL is a file relative to the working
		// directory; a temp one keeps the package clean.
		cfg.Databases = nucleustest.TempSQLite(e.tb)
		cfg.JWTSecret = strings.Repeat("authbench-probe-secret", 2)
		e.srv = nucleustest.StartApp(e.tb, nucleus.App{Config: cfg})
	})
	return e.srv
}

// status returns the status code the default application answers for one
// request. It is the measurement behind every "absent" verdict on an HTTP
// surface: a route nobody serves answers 404, and that is what an application
// author gets today.
func (e *env) status(t *testing.T, method, path string, headers map[string]string) int {
	t.Helper()
	srv := e.server()
	req, err := http.NewRequest(method, srv.URL(path), strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// unroutedVerdict turns "none of these paths is served" into a verdict. Any
// answer other than 404 means the framework serves something there and the
// control needs a real probe instead of this one.
func (e *env) unroutedVerdict(t *testing.T, paths ...string) verdict {
	t.Helper()
	for _, p := range paths {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			if code := e.status(t, m, p, nil); code != http.StatusNotFound {
				t.Logf("%s %s answered %d, not 404: the surface exists", m, p, code)
				return partial
			}
		}
	}
	return absent
}

// startRateLimited boots an application whose limiter allows one request per
// window and scopes by role — the tightest per-identity setting the
// configuration offers — with one route to spend it on.
func startRateLimited(t *testing.T) *nucleustest.Server {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Env = "development"
	cfg.Databases = nucleustest.TempSQLite(t)
	cfg.JWTSecret = strings.Repeat("authbench-probe-secret", 2)
	cfg.RateLimitRequests = 1
	cfg.RateLimitBurst = 1
	cfg.RateLimitWindow = time.Hour
	cfg.RateLimitByRole = true

	return nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Options: []app.Option{app.WithOpenAuthz()},
		Modules: map[string]nucleus.ModuleSpec{
			"authbench-probe": nucleus.Module[struct{}]{
				Name: "authbench-probe",
				Routes: func(r nucleus.Router, _ struct{}) {
					r.Get("/authbench-probe/spend", func(c *nucleus.Context) error {
						return c.NoContent()
					})
				},
			}.Build(),
		},
	})
}

// rateLimitedProbe spends one request as the bearer of the given token.
func rateLimitedProbe(t *testing.T, srv *nucleustest.Server, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL("/authbench-probe/spend"), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// accountsServer boots an application with the accounts module mounted,
// backed by its own SQLite database.
//
// Two servers rather than one, and the difference is the bench's criterion:
// e.server() answers "what does an application get by doing nothing", and
// this one answers "what can an application have without writing it
// itself". A5 is about the second question — an opt-in module is shipped
// capability, the way an optional driver is — so a control the module
// serves is measured here, and the case says so.
func (e *env) accountsServer(t *testing.T) (*nucleustest.Server, *benchMailer) {
	t.Helper()
	e.accountsOnce.Do(func() {
		db, err := sql.Open("sqlite", fmt.Sprintf("file:authbench_accounts_%d?mode=memory&cache=shared", time.Now().UnixNano()))
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		db.SetMaxOpenConns(1)
		e.tb.Cleanup(func() { _ = db.Close() })

		store, err := accounts.NewSQLStore(context.Background(), db, accounts.SQLStoreConfig{Flavor: accounts.FlavorSQLite})
		if err != nil {
			t.Fatalf("accounts store: %v", err)
		}
		mailer := &benchMailer{}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			t.Fatalf("mfa key: %v", err)
		}
		svc, err := accounts.New(store, nil, mailer, nil, accounts.Config{
			BaseURL: "https://authbench.example.test", From: "no-reply@example.test",
			MFAEncryptionKey: key, Issuer: "Authbench",
		}, nil)
		if err != nil {
			t.Fatalf("accounts service: %v", err)
		}

		cfg := app.DefaultConfig()
		cfg.Env = "development"
		cfg.Databases = nucleustest.TempSQLite(e.tb)
		cfg.JWTSecret = strings.Repeat("authbench-accounts-secret", 2)
		e.accounts = nucleustest.StartApp(e.tb, nucleus.App{
			Config:  cfg,
			Options: []app.Option{app.WithOpenAuthz()},
			Modules: map[string]nucleus.ModuleSpec{"accounts": accounts.Module(svc)},
		})
		// A cookie jar, because the flows that matter here span requests:
		// sign in, enrol, sign out, sign in again.
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("cookie jar: %v", err)
		}
		e.accounts.Client().Jar = jar
		e.accountsMail = mailer
	})
	return e.accounts, e.accountsMail
}

// benchMailer records account mail so a probe can follow a link.
type benchMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *benchMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

// lastToken returns the token in the most recent message, the way a user
// takes it out of their inbox.
func (m *benchMailer) lastToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return ""
	}
	_, after, ok := strings.Cut(m.sent[len(m.sent)-1].Body, "token=")
	if !ok {
		return ""
	}
	token, _, _ := strings.Cut(after, " ")
	return strings.TrimSpace(strings.TrimSuffix(token, "."))
}

// postJSON posts a JSON body to one of the bench's servers and returns the
// status and decoded body.
func postJSON(t *testing.T, srv *nucleustest.Server, path string, body any) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := srv.Client().Post(srv.URL(path), "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return resp.StatusCode, decoded
}
