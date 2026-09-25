// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleustest

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// clientApp is pingApp plus the routes the client helpers are measured
// against: a JSON echo, a route that sets a cookie and one that reads it
// back, a route that reads the session, and a POST behind CSRF.
func clientApp(t *testing.T, csrf bool) nucleus.App {
	t.Helper()
	cfg := app.DefaultConfig()
	cfg.JWTSecret = strings.Repeat("nucleustest-secret-", 3)
	cfg.JWTIssuer = "nucleustest"
	cfg.JWTExpiry = time.Hour
	cfg.CSRFEnabled = csrf
	type echo struct {
		Name string `json:"name"`
	}
	m := nucleus.Module[struct{}]{
		Name: "kit",
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post("/echo", func(c *nucleus.Context) error {
				var in echo
				if err := c.BindJSON(&in); err != nil {
					return err
				}
				return c.JSON(http.StatusOK, in)
			})
			r.Get("/set-cookie", func(c *nucleus.Context) error {
				http.SetCookie(c.Writer, &http.Cookie{Name: "crumb", Value: "kept", Path: "/", Secure: true, HttpOnly: true})
				return c.NoContent()
			})
			r.Get("/read-cookie", func(c *nucleus.Context) error {
				ck, err := c.Request.Cookie("crumb")
				if err != nil {
					return c.JSON(http.StatusOK, map[string]string{"crumb": ""})
				}
				return c.JSON(http.StatusOK, map[string]string{"crumb": ck.Value})
			})
			r.Get("/whoami", func(c *nucleus.Context) error {
				return c.JSON(http.StatusOK, map[string]string{"account_id": c.SessionGetString("account_id")})
			})
		},
	}.Build()
	return nucleus.App{
		Config:  cfg,
		Modules: map[string]nucleus.ModuleSpec{m.Name(): m},
		Options: []app.Option{app.WithOpenAuthz()},
	}
}

func TestRequestSpeaksJSON(t *testing.T) {
	srv := StartApp(t, clientApp(t, false))
	resp := srv.Post("/echo", map[string]string{"name": "one"})
	if resp.Status != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Status, resp)
	}
	var out struct {
		Name string `json:"name"`
	}
	resp.JSON(t, &out)
	if out.Name != "one" {
		t.Fatalf("echo returned %+v", out)
	}
	if got := srv.Get("/nope").Status; got != http.StatusNotFound {
		t.Fatalf("unknown route: %d", got)
	}
}

func TestCookiesPersistAcrossRequestsSecureIncluded(t *testing.T) {
	srv := StartApp(t, clientApp(t, false))
	if resp := srv.Get("/set-cookie"); resp.Status != http.StatusNoContent {
		t.Fatalf("set-cookie: %d", resp.Status)
	}
	var out map[string]string
	srv.Get("/read-cookie").JSON(t, &out)
	if out["crumb"] != "kept" {
		t.Fatalf("the Secure cookie the application set did not come back: %v (held: %v)", out, srv.Cookies())
	}
}

func TestCSRFTokenOpensAProtectedPost(t *testing.T) {
	srv := StartApp(t, clientApp(t, true))
	// The middleware answers 419 (the "page expired" status the CSRF world
	// settled on), never 403: a token failure is not an authorization
	// failure.
	without := srv.Post("/echo", map[string]string{"name": "x"})
	if without.Status != 419 {
		t.Fatalf("a POST without the token should be refused by the CSRF middleware with 419, got %d: %s", without.Status, without)
	}
	tok := srv.CSRFToken()
	if tok == "" {
		t.Fatal("CSRFToken returned nothing on an application with csrf_enabled")
	}
	with := srv.Post("/echo", map[string]string{"name": "x"}, srv.WithCSRF())
	if with.Status != http.StatusOK {
		t.Fatalf("a POST with the token: %d: %s", with.Status, with)
	}
}

func TestSignInAccountIsSeenBySessionRoutes(t *testing.T) {
	srv := StartApp(t, clientApp(t, false))
	var before map[string]string
	srv.Get("/whoami").JSON(t, &before)
	if before["account_id"] != "" {
		t.Fatalf("a fresh client is already signed in: %v", before)
	}
	if tok := srv.SignInAccount("acc-7", "seven@example.test"); tok == "" {
		t.Fatal("SignInAccount returned no token")
	}
	var after map[string]string
	srv.Get("/whoami").JSON(t, &after)
	if after["account_id"] != "acc-7" {
		t.Fatalf("the session route does not see the signed-in account: %v", after)
	}
	srv.SignOut()
	var gone map[string]string
	srv.Get("/whoami").JSON(t, &gone)
	if gone["account_id"] != "" {
		t.Fatalf("after SignOut the route still sees %v", gone)
	}
}
