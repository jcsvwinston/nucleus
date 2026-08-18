// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// SSR conformance (QCD-FW-8/9 systemic guard): three findings in the same
// afternoon — QCD-FW-7 (scaffold template outside the loader's glob),
// QCD-FW-8 (Prefix modules lose the template engine and session manager)
// and QCD-FW-9 (no FuncMap extension point) — all in the server-side render
// layer, the first time anyone built a real SSR application against the
// framework. Everything consuming nucleus before was API-first JSON, or
// orbit (which mounts its own http.Handler and never touches nucleus
// rendering). This suite exercises the layer the way a real application
// does: a module WITH a Prefix, served over real HTTP by the full runtime.
package nucleus_test

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/nucleustest"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

func writeSSRTemplate(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ssrConsoleModule is the shape the external demo's MVC app uses: a module
// with a Prefix, template rendering, session state, a CSRF-protected form
// and module-mounted statics.
func ssrConsoleModule(staticDir string) nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name:   "consola",
		Prefix: "/consola",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/panel", func(c *nucleus.Context) error {
				return c.Context.HTML(http.StatusOK, "consola/panel.html", map[string]interface{}{"who": "operador"})
			})
			// (3) CSRF: the GET hands out the token the middleware resolved;
			// the POST is only reachable with it.
			r.Get("/form", func(c *nucleus.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"csrf": router.CSRFToken(c.Context.Request)})
			})
			r.Post("/form", func(c *nucleus.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"posted": "1"})
			})
			// (4) Statics mounted by the module.
			r.Mount("/static", http.FileServer(http.Dir(staticDir)))
			r.Get("/remember", func(c *nucleus.Context) error {
				if err := c.SessionPutString("ssr_k", "ssr_v"); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, map[string]string{"ok": "1"})
			})
			r.Get("/recall", func(c *nucleus.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"got": c.SessionGetString("ssr_k")})
			})
			r.Get("/forget", func(c *nucleus.Context) error {
				if err := c.SessionDestroy(); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, map[string]string{"ok": "1"})
			})
		},
	}.Build()
}

func ssrApp(t *testing.T, tplDir, staticDir string) nucleus.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.TemplatesDir = tplDir
	cfg.SessionCookieSecure = false // plain-HTTP loopback test client
	cfg.CSRFEnabled = true
	cfg.CSRFInsecureCookie = true // ditto: cookiejar over http://127.0.0.1
	m := ssrConsoleModule(staticDir)
	return nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{m.Name(): m},
		Options: []app.Option{
			app.WithOpenAuthz(),
			// (5) The QCD-FW-9 extension point, exercised in the render.
			app.WithTemplateFuncs(template.FuncMap{
				"grita": strings.ToUpper,
			}),
		},
	}
}

// Points 1 and 2 of the SSR conformance contract: a template loaded by
// app.New renders BY NAME inside a Prefix module, and the session works
// there (write → read → destroy). On v1.8.2 both fail: Mux.Route builds its
// sub-router with a bare NewMux(), so the Prefix subtree never receives the
// engine or the session manager (QCD-FW-8).
func TestSSRConformance_PrefixModuleRendersAndKeepsSession(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	tplDir := filepath.Join(dir, "templates")
	writeSSRTemplate(t, tplDir, "consola/panel.html", "<h1>panel de {{grita .who}}</h1>")
	staticDir := filepath.Join(dir, "static")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "app.css"), []byte("body{margin:0}"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := nucleustest.StartApp(t, ssrApp(t, tplDir, staticDir))
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 5 * time.Second, Jar: jar}

	get := func(path string) (int, string) {
		resp, err := client.Get(srv.URL(path))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp.StatusCode, string(body)
	}

	// (1)+(5) Render by name inside the Prefix subtree, with the function
	// registered through WithTemplateFuncs applied (QCD-FW-8/9).
	if code, body := get("/consola/panel"); code != http.StatusOK || !strings.Contains(body, "<h1>panel de OPERADOR</h1>") {
		t.Errorf("render in a Prefix module with template funcs: want 200 with uppercased body, got %d body=%q (QCD-FW-8/9)", code, body)
	}

	// (2) Session write → read → destroy across requests.
	if code, body := get("/consola/remember"); code != http.StatusOK {
		t.Fatalf("session write in a Prefix module: want 200, got %d body=%q (QCD-FW-8)", code, body)
	}
	if code, body := get("/consola/recall"); code != http.StatusOK || !strings.Contains(body, `"got":"ssr_v"`) {
		t.Errorf("session read back: want ssr_v, got %d body=%q", code, body)
	}
	if code, body := get("/consola/forget"); code != http.StatusOK {
		t.Errorf("session destroy: want 200, got %d body=%q", code, body)
	}
	if code, body := get("/consola/recall"); code != http.StatusOK || !strings.Contains(body, `"got":""`) {
		t.Errorf("session after destroy: want empty, got %d body=%q", code, body)
	}

	// (4) Module-mounted statics.
	if code, body := get("/consola/static/app.css"); code != http.StatusOK || !strings.Contains(body, "body{margin:0}") {
		t.Errorf("module statics: want 200 with css body, got %d body=%q", code, body)
	}

	// (3) CSRF: token accessible via router.CSRFToken; POST passes with it
	// and fails 419 without it.
	_, formBody := get("/consola/form")
	var tokenResp struct {
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal([]byte(formBody), &tokenResp); err != nil || tokenResp.CSRF == "" {
		t.Fatalf("router.CSRFToken not accessible from the module handler: body=%q err=%v", formBody, err)
	}

	post := func(withToken bool) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL("/consola/form"), strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if withToken {
			req.Header.Set("X-CSRF-Token", tokenResp.CSRF)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("POST /consola/form: %v", err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if code := post(false); code != 419 {
		t.Errorf("POST without CSRF token: want 419, got %d", code)
	}
	if code := post(true); code != http.StatusOK {
		t.Errorf("POST with CSRF token: want 200, got %d", code)
	}
}

// QCD-FW-9: templates parse with no extension point — no way to register a
// FuncMap or inject a prebuilt *template.Template before parsing, so every
// piece of presentation logic must be precomputed in Go. The extension point
// must exist as a documented app option.
func TestSSRConformance_TemplateFuncsExtensionPointExists(t *testing.T) {
	for _, symbol := range []string{
		"github.com/jcsvwinston/nucleus/pkg/app.WithTemplateFuncs",
		"github.com/jcsvwinston/nucleus/pkg/app.WithTemplates",
	} {
		out, err := exec.Command("go", "doc", symbol).CombinedOutput()
		if err != nil {
			t.Errorf("no extension point %s (QCD-FW-9): %v\n%s", symbol, err, out)
		}
	}
}
