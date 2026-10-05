// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
)

// signInIdP is a stand-in identity provider: Begin says where to send the
// browser, Complete trusts the user the callback names. What is measured is
// the module around it — the routes, the state custody, the session.
type signInIdP struct{ instance string }

func (p *signInIdP) Name() string { return p.instance }

func (p *signInIdP) Begin(_ context.Context, r federated.BeginRequest) (federated.Redirect, error) {
	return federated.Redirect{URL: "https://idp.test/authorize?redirect_uri=" + url.QueryEscape(r.CallbackURL)}, nil
}

func (p *signInIdP) Complete(_ context.Context, r federated.CompleteRequest) (*federated.User, error) {
	return &federated.User{ID: "sub-" + r.Query.Get("user"), Username: r.Query.Get("user"), Email: r.Query.Get("user") + "@example.test", Roles: []string{"staff"}}, nil
}

func registerSignInIdP(t *testing.T) {
	t.Helper()
	if err := federated.Register("signin-idp", func(cfg backend.Config) (federated.Provider, error) {
		return &signInIdP{instance: cfg.Name}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { federated.Unregister("signin-idp") })
}

func signInConfig(t *testing.T) app.Config {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.LogLevel = "error"
	cfg.JWTSecret = strings.Repeat("federated-signin-secret", 2)
	cfg.Databases = map[string]app.DatabaseConfig{"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "signin.db")}}
	cfg.PublicBaseURL = "http://app.example.test"
	cfg.AuthFederated = []auth.FederatedInstance{{Name: "corp", Provider: "signin-idp"}}
	return cfg
}

// browser keeps cookies and does not follow redirects: the start route sends
// it to an identity provider that does not exist.
func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func fetch(t *testing.T, c *http.Client, method, u string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

// whoami reads what the callback wrote into the session.
var whoami = nucleus.Module[struct{}]{
	Name: "whoami",
	Routes: func(r nucleus.Router, _ struct{}) {
		r.Get("/whoami", func(c *nucleus.Context) error {
			return c.JSON(http.StatusOK, map[string]string{
				"instance": c.SessionGetString(nucleus.SessionKeyFederatedInstance),
				"id":       c.SessionGetString(nucleus.SessionKeyFederatedUserID),
				"username": c.SessionGetString(nucleus.SessionKeyFederatedUsername),
				"email":    c.SessionGetString(nucleus.SessionKeyFederatedEmail),
			})
		})
	},
}.Build()

// The whole flow on the api starter's stack: start redirects to the
// identity provider with the callback the operator registers, the callback
// refuses a browser that did not start here, completes one that did, writes
// the identity into the session, and refuses the same state twice.
func TestFederatedSignIn_TheFlowEndToEnd(t *testing.T) {
	registerSignInIdP(t)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  signInConfig(t),
		Options: []app.Option{app.WithoutDefaults()},
		Modules: map[string]nucleus.ModuleSpec{nucleus.FederatedSignInModuleName: nucleus.FederatedSignIn(), "whoami": whoami},
	})
	b := browser(t)

	resp, _ := fetch(t, b, http.MethodGet, srv.URL(auth.FederatedCallbackPath("corp"))+"?user=ana")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a callback the browser did not start answers %d, want 400", resp.StatusCode)
	}

	resp, _ = fetch(t, b, http.MethodGet, srv.URL(auth.FederatedStartPath("corp")))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start answered %d, want 302", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "https://idp.test/authorize") || !strings.Contains(loc, url.QueryEscape("http://app.example.test/auth/corp/callback")) {
		t.Fatalf("start redirected to %q, want the provider with the registered callback", loc)
	}
	var state *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "nucleus_federated_state" {
			state = c
		}
	}
	if state == nil || !state.HttpOnly || state.Path != "/auth/corp/" || state.SameSite != http.SameSiteLaxMode {
		t.Fatalf("the state cookie is %+v, want HttpOnly, Lax, scoped to /auth/corp/", state)
	}

	resp, body := fetch(t, b, http.MethodGet, srv.URL(auth.FederatedCallbackPath("corp"))+"?user=ana")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback answered %d: %s", resp.StatusCode, body)
	}
	var identity map[string]any
	if err := json.Unmarshal([]byte(body), &identity); err != nil || identity["username"] != "ana" || identity["instance"] != "corp" {
		t.Fatalf("callback body %s, want the identity", body)
	}

	_, body = fetch(t, b, http.MethodGet, srv.URL("/whoami"))
	var session map[string]string
	_ = json.Unmarshal([]byte(body), &session)
	if session["username"] != "ana" || session["id"] != "sub-ana" || session["instance"] != "corp" || session["email"] != "ana@example.test" {
		t.Fatalf("the session carries %v after sign-in", session)
	}

	// The state is single use: replaying the callback with the cookie the
	// start set is refused, by the framework, before the provider is asked.
	replay := browser(t)
	u, _ := url.Parse(srv.URL("/"))
	replay.Jar.SetCookies(u, []*http.Cookie{{Name: state.Name, Value: state.Value, Path: "/auth/corp/"}})
	resp, _ = fetch(t, replay, http.MethodGet, srv.URL(auth.FederatedCallbackPath("corp"))+"?user=mallory")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a replayed state answered %d, want 401", resp.StatusCode)
	}
}

// On the default stack the routes answer an anonymous browser: the module
// grants the anonymous subject its two routes, and nothing else.
func TestFederatedSignIn_AnswersAnonymousOnTheDefaultStack(t *testing.T) {
	registerSignInIdP(t)
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  signInConfig(t),
		Modules: map[string]nucleus.ModuleSpec{nucleus.FederatedSignInModuleName: nucleus.FederatedSignIn(), "whoami": whoami},
	})
	b := browser(t)
	if resp, body := fetch(t, b, http.MethodGet, srv.URL(auth.FederatedStartPath("corp"))); resp.StatusCode != http.StatusFound {
		t.Fatalf("start on the default stack answered %d: %s", resp.StatusCode, body)
	}
	if resp, _ := fetch(t, b, http.MethodGet, srv.URL("/whoami")); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a route the module does not own answered %d to an anonymous browser, want the default-deny 403", resp.StatusCode)
	}
}

// modules.federated.redirect sends the browser on after sign-in; an
// absolute address is refused at boot, because the callback is reachable by
// anyone and would redirect wherever it says.
func TestFederatedSignIn_Redirect(t *testing.T) {
	registerSignInIdP(t)
	dir := t.TempDir()
	write := func(redirect string) string {
		path := filepath.Join(dir, "nucleus.yml")
		yml := "databases:\n  default:\n    url: sqlite://" + filepath.Join(dir, "r.db") + "\n" +
			"log_level: error\npublic_base_url: http://app.example.test\n" +
			"auth_federated:\n  - name: corp\n    provider: signin-idp\n" +
			"modules:\n  federated:\n    redirect: " + redirect + "\n"
		if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	srv := nucleustest.Start(t, nucleus.New().FromConfigFile(write("/home")).WithoutDefaults().Mount(nucleus.FederatedSignIn()))
	b := browser(t)
	fetch(t, b, http.MethodGet, srv.URL(auth.FederatedStartPath("corp")))
	resp, _ := fetch(t, b, http.MethodGet, srv.URL(auth.FederatedCallbackPath("corp"))+"?user=ana")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/home" {
		t.Fatalf("callback answered %d to %q, want 303 to /home", resp.StatusCode, resp.Header.Get("Location"))
	}

	built, err := nucleus.New().FromConfigFile(write("https://evil.example/")).WithoutDefaults().Mount(nucleus.FederatedSignIn()).Build()
	if err != nil {
		t.Fatal(err)
	}
	built.Config.Port = 0
	err = nucleus.Run(built)
	if err == nil || !strings.Contains(err.Error(), "open redirect") {
		t.Fatalf("an absolute redirect booted: %v", err)
	}
}

// Mounted with nothing declared, the module serves nothing and the
// application still starts.
func TestFederatedSignIn_NoInstanceMountsNothing(t *testing.T) {
	cfg := signInConfig(t)
	cfg.AuthFederated = nil
	cfg.PublicBaseURL = ""
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Options: []app.Option{app.WithoutDefaults()},
		Modules: map[string]nucleus.ModuleSpec{nucleus.FederatedSignInModuleName: nucleus.FederatedSignIn()},
	})
	if resp := srv.Get(auth.FederatedStartPath("corp")); resp.Status != http.StatusNotFound {
		t.Fatalf("with no instance declared, start answered %d", resp.Status)
	}
}

// formPostIdP is the SAML shape seen from the framework: it publishes
// service metadata and its identity provider returns the browser with a
// cross-site form POST. Both through the optional interfaces, with built-in
// types only.
type formPostIdP struct{ signInIdP }

func (p *formPostIdP) CallbackIsCrossSiteFormPost() bool { return true }

func (p *formPostIdP) ServiceMetadata(_ context.Context, callbackURL string) (string, []byte, error) {
	return "application/samlmetadata+xml", []byte(`<EntityDescriptor acs="` + callbackURL + `"/>`), nil
}

func (p *formPostIdP) Complete(_ context.Context, r federated.CompleteRequest) (*federated.User, error) {
	return &federated.User{ID: "sub-" + r.Form.Get("user"), Username: r.Form.Get("user")}, nil
}

func registerFormPostIdP(t *testing.T) {
	t.Helper()
	if err := federated.Register("formpost-idp", func(cfg backend.Config) (federated.Provider, error) {
		return &formPostIdP{signInIdP{instance: cfg.Name}}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(func() { federated.Unregister("formpost-idp") })
}

// echo is a module with a POST route of its own, which CSRF still guards.
var echo = nucleus.Module[struct{}]{
	Name:     "echo",
	Policies: []nucleus.PolicyRule{{Subject: "anonymous", Object: "/echo", Action: "create"}},
	Routes: func(r nucleus.Router, _ struct{}) {
		r.Post("/echo", func(c *nucleus.Context) error { return c.JSON(http.StatusOK, map[string]string{"ok": "true"}) })
	},
}.Build()

// A provider whose identity provider answers with a cross-site form post
// (SAML): its metadata is served to an anonymous browser, its state cookie
// is SameSite=None and Secure when the application is https — a Lax cookie
// does not ride that post — and with csrf_enabled its callback takes the
// identity provider's POST while the application's other POST routes stay
// guarded.
func TestFederatedSignIn_CrossSiteFormPostProvider(t *testing.T) {
	registerFormPostIdP(t)
	cfg := signInConfig(t)
	cfg.PublicBaseURL = "https://app.example.test"
	cfg.CSRFEnabled = true
	cfg.AuthFederated = []auth.FederatedInstance{{Name: "corp", Provider: "formpost-idp"}}
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{nucleus.FederatedSignInModuleName: nucleus.FederatedSignIn(), "echo": echo},
	})
	b := browser(t)

	resp, body := fetch(t, b, http.MethodGet, srv.URL(auth.FederatedMetadataPath("corp")))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/samlmetadata+xml" ||
		!strings.Contains(body, `acs="https://app.example.test/auth/corp/callback"`) {
		t.Fatalf("metadata answered %d %q: %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}

	resp, _ = fetch(t, b, http.MethodGet, srv.URL(auth.FederatedStartPath("corp")))
	var state *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "nucleus_federated_state" {
			state = c
		}
	}
	if state == nil || state.SameSite != http.SameSiteNoneMode || !state.Secure || !state.HttpOnly {
		t.Fatalf("the state cookie of a cross-site form-post provider over https is %+v, want SameSite=None, Secure, HttpOnly", state)
	}

	// The identity provider's post: no CSRF token, the state cookie (sent
	// by hand — the test talks plain http to a Secure cookie).
	post := func(path string, cookie *http.Cookie) (int, string) {
		req, err := http.NewRequest(http.MethodPost, srv.URL(path), strings.NewReader(url.Values{"user": {"ana"}}.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	if code, body := post(auth.FederatedCallbackPath("corp"), state); code != http.StatusOK || !strings.Contains(body, `"username":"ana"`) {
		t.Fatalf("the identity provider's form post answered %d: %s", code, body)
	}
	if code, body := post("/echo", nil); code == http.StatusOK {
		t.Fatalf("a POST route the sign-in does not own lost its CSRF check: %d %s", code, body)
	}
}

// Over plain http the cookie stays Lax — SameSite=None without Secure is
// refused by browsers — and a provider that publishes no metadata gets no
// metadata route.
func TestFederatedSignIn_LaxOverHTTPAndNoMetadataRouteForOIDC(t *testing.T) {
	registerFormPostIdP(t)
	registerSignInIdP(t)
	cfg := signInConfig(t)
	cfg.AuthFederated = []auth.FederatedInstance{{Name: "corp", Provider: "formpost-idp"}, {Name: "oidc", Provider: "signin-idp"}}
	srv := nucleustest.StartApp(t, nucleus.App{
		Config:  cfg,
		Options: []app.Option{app.WithoutDefaults()},
		Modules: map[string]nucleus.ModuleSpec{nucleus.FederatedSignInModuleName: nucleus.FederatedSignIn()},
	})
	b := browser(t)
	resp, _ := fetch(t, b, http.MethodGet, srv.URL(auth.FederatedStartPath("corp")))
	for _, c := range resp.Cookies() {
		if c.Name == "nucleus_federated_state" && (c.SameSite != http.SameSiteLaxMode || c.Secure) {
			t.Fatalf("over plain http the state cookie is %+v, want Lax and not Secure", c)
		}
	}
	if resp, _ := fetch(t, b, http.MethodGet, srv.URL(auth.FederatedMetadataPath("oidc"))); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a provider that publishes no metadata has a metadata route answering %d", resp.StatusCode)
	}
}
