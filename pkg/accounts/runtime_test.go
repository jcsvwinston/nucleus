// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package accounts_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/accounts"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// runtimeConfig is a nucleus.yml for an application whose account flows are
// built from the runtime: a SQLite file, and the module's two required
// keys. extra is appended as written.
func runtimeConfig(t *testing.T, extra string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "nucleus.yml")
	body := "databases:\n  default:\n    url: sqlite://" + filepath.Join(dir, "app.db") + "\n" +
		"jwt_secret: " + strings.Repeat("accounts-runtime-secret", 2) + "\n" +
		"log_level: error\n" +
		"modules:\n  accounts:\n    base_url: https://app.example.test\n    from: no-reply@example.test\n" + extra
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

var tokenInLink = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func sendJSON(t *testing.T, srv *nucleustest.Server, path string, body any) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := srv.Client().Post(srv.URL(path), "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	_ = resp.Body.Close()
	return resp
}

// The api starter's shape — WithoutDefaults, WithMail — with the flows built
// from the runtime: registration answers 202 and mails the link, sign-in is
// refused until the address is confirmed, and the link confirms it.
func TestFromRuntime_RegisterVerifyLogin(t *testing.T) {
	srv := nucleustest.Start(t, nucleus.New().
		FromConfigFile(runtimeConfig(t, "")).
		WithoutDefaults().
		WithMail().
		Mount(accounts.FromRuntime()))

	creds := map[string]string{"email": "ana@example.test", "username": "ana", "password": "correct horse battery staple"}
	if resp := sendJSON(t, srv, accounts.RouteRegister, creds); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("register: %d, want 202", resp.StatusCode)
	}
	sent := srv.SentMail()
	if len(sent) != 1 || sent[0].To[0] != "ana@example.test" || sent[0].From != "no-reply@example.test" {
		t.Fatalf("mail sent: %+v", sent)
	}
	link := tokenInLink.FindStringSubmatch(sent[0].Body)
	if link == nil || !strings.Contains(sent[0].Body, "https://app.example.test"+accounts.RouteVerifyEmail) {
		t.Fatalf("the confirmation mail carries no link to base_url: %q", sent[0].Body)
	}

	login := map[string]string{"email": creds["email"], "password": creds["password"]}
	if resp := sendJSON(t, srv, accounts.RouteLogin, login); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("login before confirming: %d, want 403", resp.StatusCode)
	}
	resp, err := srv.Client().Get(srv.URL(accounts.RouteVerifyEmail + "?token=" + link[1]))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify: %d, want 200", resp.StatusCode)
	}
	if resp := sendJSON(t, srv, accounts.RouteLogin, login); resp.StatusCode != http.StatusOK {
		t.Fatalf("login after confirming: %d, want 200", resp.StatusCode)
	}
}

// What makes registration answer 500 at the first request is refused at
// boot, and the refusal names what to set.
func TestFromRuntime_RefusesAMailerThatDeliversNothing(t *testing.T) {
	cases := map[string]struct {
		builder func(path string) *nucleus.AppBuilder
		want    string
	}{
		"WithoutDefaults and no WithMail": {
			builder: func(path string) *nucleus.AppBuilder {
				return nucleus.New().FromConfigFile(path).WithoutDefaults().Mount(accounts.FromRuntime())
			},
			want: "WithMail()",
		},
		"mail_driver noop": {
			builder: func(path string) *nucleus.AppBuilder {
				return nucleus.New().FromConfigFile(path).WithoutDefaults().WithMail().Mount(accounts.FromRuntime())
			},
			want: "mail_driver is noop",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := bootErr(t, c.builder(runtimeConfig(t, ""))); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("boot: %v, want a refusal naming %q", err, c.want)
			}
		})
	}
}

func TestFromRuntime_RequiresBaseURLAndFrom(t *testing.T) {
	for name, c := range map[string]struct{ config, want string }{
		"no base_url": {"databases:\n  default:\n    url: \"sqlite://:memory:\"\nmail_driver: memory\nmodules:\n  accounts:\n    from: a@example.test\n", "base_url is empty"},
		"no from":     {"databases:\n  default:\n    url: \"sqlite://:memory:\"\nmail_driver: memory\nmodules:\n  accounts:\n    base_url: https://x.test\n", "from is empty"},
		"not a URL":   {"databases:\n  default:\n    url: \"sqlite://:memory:\"\nmail_driver: memory\nmodules:\n  accounts:\n    base_url: x.test\n    from: a@example.test\n", "not an http(s) address"},
		"bad mfa key": {"databases:\n  default:\n    url: \"sqlite://:memory:\"\nmail_driver: memory\nmodules:\n  accounts:\n    base_url: https://x.test\n    from: a@example.test\n    mfa_key_env: ACCOUNTS_TEST_UNSET_KEY\n", "ACCOUNTS_TEST_UNSET_KEY, which is not set"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nucleus.yml")
			if err := os.WriteFile(path, []byte(c.config), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := bootErr(t, nucleus.New().FromConfigFile(path).WithoutDefaults().WithMail().Mount(accounts.FromRuntime())); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("boot: %v, want %q", err, c.want)
			}
		})
	}
}

// On the default stack the module opens its own routes to the anonymous
// subject, and the account it signs in is the subject of its session: a
// policy row for the account's id reaches the signed-in session — the same
// row that reaches a token or an API key of that account.
func TestFromRuntime_DefaultStackAndTheSessionSubject(t *testing.T) {
	key := make([]byte, 32)
	t.Setenv("ACCOUNTS_TEST_MFA_KEY", base64.StdEncoding.EncodeToString(key))
	var accountID string
	probe := nucleus.Module[struct{}]{
		Name: "probe",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/members/area", func(c *nucleus.Context) error { return c.NoContent() })
		},
	}.Build()
	srv := nucleustest.Start(t, nucleus.New().
		FromConfigFile(runtimeConfig(t, "    allow_unverified_login: true\n    mfa_key_env: ACCOUNTS_TEST_MFA_KEY\n")).
		Mount(accounts.FromRuntime(), probe))

	creds := map[string]string{"email": "bo@example.test", "username": "bo", "password": "correct horse battery staple"}
	if resp := sendJSON(t, srv, accounts.RouteRegister, creds); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("register on the default stack: %d, want 202 (the module grants anonymous its routes)", resp.StatusCode)
	}
	resp, err := srv.Client().Get(srv.URL("/members/area"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a route nobody is granted, before sign-in: %d, want 403", resp.StatusCode)
	}

	raw, _ := json.Marshal(map[string]string{"email": creds["email"], "password": creds["password"]})
	resp, err = srv.Client().Post(srv.URL(accounts.RouteLogin), "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var signedIn struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&signedIn)
	_ = resp.Body.Close()
	accountID = signedIn.ID
	if resp.StatusCode != http.StatusOK || accountID == "" {
		t.Fatalf("login: %d %q", resp.StatusCode, accountID)
	}

	if err := srv.Runtime().Authorizer().AddPolicy(accountID, "/members/area", "read"); err != nil {
		t.Fatal(err)
	}
	resp, err = srv.Client().Get(srv.URL("/members/area"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("the signed-in session on a route granted to its account id: %d, want 204", resp.StatusCode)
	}

	// The second-factor key came from the variable mfa_key_env names:
	// enrolment needs a fresh sign-in and a key, and answers 200 here.
	resp, err = srv.Client().Post(srv.URL(accounts.RouteTOTP), "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("TOTP enrolment with mfa_key_env set: %d, want 200", resp.StatusCode)
	}
}

// bootErr boots the application on a loopback port and returns why it did
// not start; one that starts runs for two seconds and returns nil.
func bootErr(t *testing.T, b *nucleus.AppBuilder) error {
	t.Helper()
	a, err := b.Build()
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a.Config.Host, a.Config.Port = "127.0.0.1", l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return nucleus.RunContext(ctx, a)
}
