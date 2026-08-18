// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-8 (external coverage demo, 2026-08-17): Mux.Route built its
// sub-router with a bare NewMux(), so the child never inherited the parent's
// session manager or template engine — while Group and With DO copy both.
// Since mountModule resolves a nucleus.Module's Prefix through Route, EVERY
// module that declares a Prefix (the documented way to build one) got
// ErrTemplateEngineNotSet from c.HTML and ErrSessionManagerNotSet from the
// session helpers, even though app.New had loaded templates correctly.
//
// Executed repro (two twin modules, same template, same app):
//
//	Prefix (→ Mux.Route)     GET /conprefijo/x → 500 "template engine is not configured"
//	sin Prefix (→ Mux.Group) GET /sinprefijo/x → 200 "<p>hola</p>"
package router

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// Route-mounted subtrees must inherit the SAME request-time dependencies
// Group and With already inherit.
func TestRouteSubRouterInheritsTemplatesAndSession(t *testing.T) {
	parent := NewMux()
	tpl := template.Must(template.New("hola.html").Parse("<p>hola {{.who}}</p>"))
	parent.SetHTMLTemplates(tpl)
	parent.SetSessionManager(auth.NewSessionManager(auth.SessionConfig{
		Lifetime: time.Hour,
	}))

	parent.Route("/pre", func(sub *Mux) {
		sub.Get("/render", func(c *Context) error {
			return c.HTML(http.StatusOK, "hola.html", map[string]interface{}{"who": "mundo"})
		})
		sub.Get("/session", func(c *Context) error {
			if c.SessionManager() == nil {
				return ErrSessionManagerNotSet
			}
			if err := c.SessionPutString("k", "v"); err != nil {
				return err
			}
			if got := c.SessionGetString("k"); got != "v" {
				t.Errorf("session round-trip inside Route: got %q", got)
			}
			return c.JSON(http.StatusOK, map[string]string{"ok": "1"})
		})
	})

	// The session middleware must wrap the mux for session helpers to work.
	sm := parent.session
	handler := sm.Middleware()(parent)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pre/render", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<p>hola mundo</p>") {
		t.Errorf("render inside Route subtree: want 200 with body, got %d body=%q — Route lost the parent's template engine (QCD-FW-8)", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/pre/session", nil))
	if rec2.Code != http.StatusOK {
		t.Errorf("session helpers inside Route subtree: want 200, got %d body=%q — Route lost the parent's session manager (QCD-FW-8)", rec2.Code, rec2.Body.String())
	}
}

// The twin control: Group HAS always inherited both. Pins the asymmetry the
// bug lived in — if this ever breaks, the inheritance contract itself broke.
func TestGroupSubRouterInheritsTemplatesAndSession(t *testing.T) {
	parent := NewMux()
	tpl := template.Must(template.New("hola.html").Parse("<p>hola</p>"))
	parent.SetHTMLTemplates(tpl)
	parent.SetSessionManager(auth.NewSessionManager(auth.SessionConfig{
		Lifetime: time.Hour,
	}))

	parent.Group(func(sub *Mux) {
		sub.Get("/sinprefijo/x", func(c *Context) error {
			if c.SessionManager() == nil {
				return ErrSessionManagerNotSet
			}
			return c.HTML(http.StatusOK, "hola.html", nil)
		})
	})

	handler := parent.session.Middleware()(parent)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sinprefijo/x", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<p>hola</p>") {
		t.Errorf("Group control case: want 200, got %d body=%q", rec.Code, rec.Body.String())
	}
}
