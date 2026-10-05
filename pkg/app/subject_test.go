// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// NU-112: on the default stack the default-deny layer authorised a request
// that presented an API key as `anonymous`, so a policy could neither grant
// a route to a key's owner nor to a scope, and only apikeys.Require stood
// between a key and a route. The subject of a key's request is now its
// owner, with each scope as `scope:<name>`, then anonymous.

// keyedApp is the default stack (default-deny on) with API keys and four
// routes, each granted to a different subject.
func keyedApp(t *testing.T) *App {
	t.Helper()
	a, err := New(testAppConfig(), WithAPIKeys())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ok := router.FromHTTP(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, path := range []string{"/owned", "/reports", "/public", "/role"} {
		a.Router.Get(path, ok)
	}
	for _, row := range [][3]string{
		{"svc-billing", "/owned", "read"},          // granted to an owner
		{"scope:reports:read", "/reports", "read"}, // granted to a scope
		{"anonymous", "/public", "read"},           // granted to everyone
		{"auditors", "/role", "read"},              // granted to a role the owner holds
	} {
		if err := a.Authorizer.AddPolicy(row[0], row[1], row[2]); err != nil {
			t.Fatalf("AddPolicy %v: %v", row, err)
		}
	}
	if err := a.Authorizer.AddRole("svc-audit", "auditors"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}
	return a
}

func issueKey(t *testing.T, a *App, owner string, scopes ...string) string {
	t.Helper()
	_, presented, err := apikeys.Issue(context.Background(), a.apiKeys, apikeys.Key{Name: "test", OwnerID: owner, Scopes: scopes})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return presented
}

func TestKeySubject_PolicyAllowsByOwner(t *testing.T) {
	a := keyedApp(t)
	if rec := keyGet(t, a, "/owned", issueKey(t, a, "svc-billing")); rec.Code != http.StatusNoContent {
		t.Fatalf("a key owned by svc-billing on a route granted to svc-billing: %d, want 204", rec.Code)
	}
	if rec := keyGet(t, a, "/owned", issueKey(t, a, "svc-other")); rec.Code != http.StatusForbidden {
		t.Fatalf("another owner's key on that route: %d, want 403", rec.Code)
	}
	if rec := keyGet(t, a, "/owned", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("no key on that route: %d, want 403", rec.Code)
	}
}

func TestKeySubject_PolicyAllowsByScope(t *testing.T) {
	a := keyedApp(t)
	if rec := keyGet(t, a, "/reports", issueKey(t, a, "svc-other", "reports:read")); rec.Code != http.StatusNoContent {
		t.Fatalf("a key carrying reports:read on a route granted to scope:reports:read: %d, want 204", rec.Code)
	}
	if rec := keyGet(t, a, "/reports", issueKey(t, a, "svc-other", "reports:write")); rec.Code != http.StatusForbidden {
		t.Fatalf("a key carrying another scope: %d, want 403", rec.Code)
	}
}

// A key with no scopes carries none: it reaches what its owner and anonymous
// reach, and no route granted only to a scope.
func TestKeySubject_UnscopedKeyIsDeniedOnAScopedRoute(t *testing.T) {
	a := keyedApp(t)
	unscoped := issueKey(t, a, "svc-billing")
	if rec := keyGet(t, a, "/reports", unscoped); rec.Code != http.StatusForbidden {
		t.Fatalf("an unscoped key on a route granted to scope:reports:read: %d, want 403", rec.Code)
	}
	if rec := keyGet(t, a, "/owned", unscoped); rec.Code != http.StatusNoContent {
		t.Fatalf("the same key on its owner's route: %d, want 204", rec.Code)
	}
}

// The owner is an identity like a token's user id: a role granted to it with
// `g` reaches its keys.
func TestKeySubject_OwnerRolesApply(t *testing.T) {
	a := keyedApp(t)
	if rec := keyGet(t, a, "/role", issueKey(t, a, "svc-audit")); rec.Code != http.StatusNoContent {
		t.Fatalf("a key whose owner holds auditors on a route granted to auditors: %d, want 204", rec.Code)
	}
}

// Additive: what anonymous may do, a key may still do — an application that
// granted a route to anonymous and guarded it with apikeys.Require alone
// keeps working.
func TestKeySubject_AnonymousGrantStillApplies(t *testing.T) {
	a := keyedApp(t)
	a.Router.With(apikeys.Require()).Get("/required", router.FromHTTP(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := a.Authorizer.AddPolicy("anonymous", "/required", "read"); err != nil {
		t.Fatal(err)
	}
	key := issueKey(t, a, "svc-nobody")
	if rec := keyGet(t, a, "/public", key); rec.Code != http.StatusNoContent {
		t.Fatalf("a key on a route granted to anonymous: %d, want 204", rec.Code)
	}
	if rec := keyGet(t, a, "/required", key); rec.Code != http.StatusNoContent {
		t.Fatalf("a key on an anonymous route behind apikeys.Require: %d, want 204", rec.Code)
	}
	if rec := keyGet(t, a, "/required", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no key on that route: %d, want 401 from apikeys.Require", rec.Code)
	}
}

// The subjects of a request, in the order the layer tries them.
func TestRequestSubjects(t *testing.T) {
	cfg := testAppConfig()
	a, err := New(cfg, WithoutDefaults(), WithAPIKeys())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })

	var got []string
	var identity string
	a.Router.Get("/who", router.FromHTTP(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("signin") != "" {
			a.Session.Put(r.Context(), auth.SessionKeySubject, "acct-42")
		}
		got = requestSubjects(r, a.Session)
		identity = requestIdentity(r, a.Session)
		w.WriteHeader(http.StatusNoContent)
	}))

	// A bearer token's claims and a key owned by the same id.
	req := httptest.NewRequest(http.MethodGet, "/who", nil)
	req.Header.Set(apikeys.HeaderName, issueKey(t, a, "u-1", "a:read", "b:write"))
	ctx := auth.ContextWithClaims(req.Context(), &auth.Claims{UserID: "u-1", Role: "editor"})
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req.WithContext(ctx))
	want := []string{"u-1", "editor", "scope:a:read", "scope:b:write", "anonymous"}
	if !equalStrings(got, want) || identity != "u-1" {
		t.Fatalf("subjects %v identity %q, want %v and u-1", got, identity, want)
	}

	// A session signed in by the account flows: its subject, read on the
	// next request that carries the cookie.
	rec = httptest.NewRecorder()
	a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/who?signin=1", nil))
	next := httptest.NewRequest(http.MethodGet, "/who", nil)
	for _, c := range rec.Result().Cookies() {
		next.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	a.Router.ServeHTTP(rec, next)
	if want := []string{"acct-42", "anonymous"}; !equalStrings(got, want) || identity != "acct-42" {
		t.Fatalf("a signed-in session: subjects %v identity %q, want %v and acct-42", got, identity, want)
	}

	rec = httptest.NewRecorder()
	a.Router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/who", nil))
	if want := []string{"anonymous"}; !equalStrings(got, want) || identity != "" {
		t.Fatalf("an anonymous request: subjects %v identity %q, want %v and none", got, identity, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
