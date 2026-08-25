// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Tests for module-declared policies and CSRF exemptions (ADR-022): the
// declarative Module.Policies / Module.CSRFExempt fields that let a mounted
// module open its own routes under the default-deny enforcer without the
// host editing rbac_policy.csv or csrf_exempt_paths by hand. The E2E boots
// through the public Run surface with defaults ON — the exact configuration
// where a generated resource used to answer a mute 403/419.
package nucleus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

func TestValidatePolicyRule(t *testing.T) {
	cases := []struct {
		name    string
		rule    PolicyRule
		wantErr string
	}{
		{"valid allow", PolicyRule{Subject: "anonymous", Object: "/things", Action: "read", Effect: "allow"}, ""},
		{"valid empty effect", PolicyRule{Subject: "anonymous", Object: "/things", Action: "create"}, ""},
		{"valid deny wildcard", PolicyRule{Subject: "anonymous", Object: "/things/*", Action: "*", Effect: "deny"}, ""},
		{"empty subject", PolicyRule{Object: "/things", Action: "read"}, "subject is empty"},
		{"relative object", PolicyRule{Subject: "anonymous", Object: "things", Action: "read"}, "must be a route path"},
		{"http verb as action", PolicyRule{Subject: "anonymous", Object: "/things", Action: "GET"}, "not one the authz middleware ever requests"},
		{"bad effect", PolicyRule{Subject: "anonymous", Object: "/things", Action: "read", Effect: "block"}, "not \"allow\", \"deny\", or empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePolicyRule(tc.rule)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestValidateModulePolicyDeclarations_NamesModuleAndIndex(t *testing.T) {
	specs := map[string]ModuleSpec{
		"bad": Module[struct{}]{
			Name: "bad",
			Policies: []PolicyRule{
				{Subject: "anonymous", Object: "/ok", Action: "read"},
				{Subject: "anonymous", Object: "/ok", Action: "POST"},
			},
		}.Build(),
	}
	err := validateModulePolicyDeclarations(specs)
	if !errors.Is(err, ErrInvalidModulePolicy) {
		t.Fatalf("want ErrInvalidModulePolicy, got %v", err)
	}
	for _, want := range []string{`module "bad"`, "Policies[1]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q, got: %v", want, err)
		}
	}

	specs = map[string]ModuleSpec{
		"bad": Module[struct{}]{Name: "bad", CSRFExempt: []string{"api/"}}.Build(),
	}
	err = validateModulePolicyDeclarations(specs)
	if !errors.Is(err, ErrInvalidModulePolicy) || !strings.Contains(err.Error(), "CSRFExempt[0]") {
		t.Fatalf("want ErrInvalidModulePolicy naming CSRFExempt[0], got %v", err)
	}
}

func TestResolveModulePolicyPath(t *testing.T) {
	cases := []struct{ prefix, p, want string }{
		{"", "/things", "/things"},
		{"/api", "/things", "/api/things"},
		{"/api/", "/things", "/api/things"},
		{"/api", "/things/*", "/api/things/*"},
		{"/api", "/things/", "/api/things/"}, // trailing slash survives (CSRF prefix match)
	}
	for _, tc := range cases {
		if got := resolveModulePolicyPath(tc.prefix, tc.p); got != tc.want {
			t.Errorf("resolveModulePolicyPath(%q, %q) = %q, want %q", tc.prefix, tc.p, got, tc.want)
		}
	}
}

func TestModuleCSRFExemptions_ResolvesAgainstPrefix(t *testing.T) {
	specs := map[string]ModuleSpec{
		"b": Module[struct{}]{Name: "b", Prefix: "/api", CSRFExempt: []string{"/things/"}}.Build(),
		"a": Module[struct{}]{Name: "a", CSRFExempt: []string{"/widgets"}}.Build(),
	}
	got := moduleCSRFExemptions(specs, nil)
	want := []string{"/widgets", "/api/things/"} // sorted module order: a, b
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// A malformed declaration must fail boot before any pool or listener exists.
func TestRun_InvalidModulePolicyFailsBoot(t *testing.T) {
	err := RunContext(context.Background(), App{
		Config: app.DefaultConfig(),
		Modules: map[string]ModuleSpec{
			"bad": Module[struct{}]{
				Name:     "bad",
				Policies: []PolicyRule{{Subject: "anonymous", Object: "/x", Action: "GET"}},
			}.Build(),
		},
	})
	if !errors.Is(err, ErrInvalidModulePolicy) {
		t.Fatalf("want ErrInvalidModulePolicy from boot, got %v", err)
	}
}

// TestRun_ModulePoliciesEndToEnd boots a real application through the public
// Run surface with the default-deny enforcer and CSRF both ACTIVE, and one
// module that declares its own policy rows and CSRF exemption. Over the wire:
//
//  1. GET on the module's route answers 200 — the module's allow row opened
//     it (without Policies this is the mute 403 of the DX audit's cliff);
//  2. POST (cookie-less JSON, no CSRF token) answers 200 — the module's
//     CSRFExempt entry covers it, and its create row authorizes it;
//  3. a route the module did NOT declare stays 403 — default-deny intact;
//  4. POST on a non-exempt path is rejected by the CSRF check — the
//     exemption is scoped, not global.
func TestRun_ModulePoliciesEndToEnd(t *testing.T) {
	port := freeLocalPort(t)

	modDef := Module[struct{}]{
		Name: "things",
		Policies: []PolicyRule{
			{Subject: "anonymous", Object: "/things", Action: "read"},
			{Subject: "anonymous", Object: "/things", Action: "create"},
			{Subject: "anonymous", Object: "/private", Action: "create"},
		},
		CSRFExempt: []string{"/things"},
		Routes: func(r Router, _ struct{}) {
			r.Get("/things", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"ok": "true"}) })
			r.Post("/things", func(c *Context) error { return c.JSON(http.StatusOK, map[string]string{"created": "true"}) })
			r.Get("/things-private", func(c *Context) error { return c.JSON(http.StatusOK, nil) })
			r.Post("/private", func(c *Context) error { return c.JSON(http.StatusOK, nil) })
		},
	}

	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.CSRFEnabled = true
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(t.TempDir(), "e2e.db")},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(App{
			Config:  cfg,
			Modules: map[string]ModuleSpec{"things": modDef.Build()},
		})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	waitForServer(t, client, base, runDone)

	get := func(path string) int {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	post := func(path string) int {
		resp, err := client.Post(base+path, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// (1) The module's allow row opens its own route under default-deny.
	if code := get("/things"); code != http.StatusOK {
		t.Errorf("GET /things: want 200 (module allow row), got %d", code)
	}
	// (2) Cookie-less JSON POST: CSRFExempt + create row → 200.
	if code := post("/things"); code != http.StatusOK {
		t.Errorf("POST /things: want 200 (module CSRF exemption + create row), got %d", code)
	}
	// (3) Undeclared route stays dark: default-deny is intact.
	if code := get("/things-private"); code != http.StatusForbidden {
		t.Errorf("GET /things-private: want 403 (no module row), got %d", code)
	}
	// (4) The CSRF exemption is scoped: /private is authorized (create row)
	// but NOT exempted, so the cookie-less POST dies at the CSRF check.
	if code := post("/private"); code == http.StatusOK {
		t.Errorf("POST /private: want CSRF rejection, got 200 — the module exemption leaked beyond its declared paths")
	}

	shutDownRunApp(t, runDone)
}

// TestRun_HostDenyOverridesModuleAllow pins the governance contract: module
// rows join the live ruleset, but a deny row in the HOST's policy file wins
// (Casbin policy effect: some(allow) && !some(deny)). The module proposes,
// the operator disposes.
func TestRun_HostDenyOverridesModuleAllow(t *testing.T) {
	port := freeLocalPort(t)

	dir := t.TempDir()
	policyPath := filepath.Join(dir, "rbac_policy.csv")
	if err := os.WriteFile(policyPath, []byte("p, anonymous, /things, read, deny\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	modDef := Module[struct{}]{
		Name:     "things",
		Policies: []PolicyRule{{Subject: "anonymous", Object: "/things", Action: "read"}},
		Routes: func(r Router, _ struct{}) {
			r.Get("/things", func(c *Context) error { return c.JSON(http.StatusOK, nil) })
		},
	}

	cfg := app.DefaultConfig()
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.RBACPolicyFile = policyPath
	cfg.Databases = map[string]app.DatabaseConfig{
		"default": {URL: "sqlite://" + filepath.Join(dir, "e2e.db")},
	}

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(App{
			Config:  cfg,
			Modules: map[string]ModuleSpec{"things": modDef.Build()},
		})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	waitForServer(t, client, base, runDone)

	resp, err := client.Get(base + "/things")
	if err != nil {
		t.Fatalf("GET /things: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET /things: want 403 (host deny row overrides module allow), got %d", resp.StatusCode)
	}

	shutDownRunApp(t, runDone)
}

// waitForServer polls base until the app accepts requests, failing fast if
// Run exits during startup. Same contract as the jobs/webhooks E2E.
func waitForServer(t *testing.T, client *http.Client, base string, runDone <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("server did not come up within 10s")
		}
		resp, err := client.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			return
		}
		select {
		case err := <-runDone:
			t.Fatalf("Run exited during startup: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// shutDownRunApp sends SIGTERM to the process (the signal Run traps) and
// waits for Run to return cleanly.
func shutDownRunApp(t *testing.T, runDone <-chan error) {
	t.Helper()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned error on shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return within 10s of SIGTERM")
	}
}
