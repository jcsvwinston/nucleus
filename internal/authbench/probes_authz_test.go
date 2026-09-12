// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/nucleus/pkg/router"
)

// AZ-01 — role-based access control over routes.
func probeRBAC(t *testing.T, _ *env) verdict {
	e, err := authz.New(nil)
	if err != nil {
		t.Logf("authz.New: %v", err)
		return absent
	}
	if err := e.AddRole("ana", "editor"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}
	if err := e.AllowResource("editor", "/posts", "GET"); err != nil {
		t.Fatalf("AllowResource: %v", err)
	}
	if !e.Can("ana", "/posts", "GET") {
		t.Log("a granted route was denied")
		return partial
	}
	if e.Can("ana", "/admin", "GET") {
		t.Log("an ungranted route was allowed")
		return absent
	}
	return present
}

// AZ-02 — an explicit deny that beats a grant.
func probeExplicitDeny(t *testing.T, _ *env) verdict {
	e, err := authz.New(nil)
	if err != nil {
		return absent
	}
	if err := e.AddRole("ana", "editor"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}
	if err := e.AllowAll("editor"); err != nil {
		t.Fatalf("AllowAll: %v", err)
	}
	if err := e.Deny("ana", "/admin", "GET"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if e.Can("ana", "/admin", "GET") {
		t.Log("the deny did not beat the grant")
		return partial
	}
	return present
}

// AZ-03 — permission on an OBJECT: "ana may edit the posts she owns". The
// probe states the question and checks the policy can tell two rows of the
// same resource apart.
func probeObjectPermission(t *testing.T, _ *env) verdict {
	e, err := authz.NewObjectEnforcer(nil)
	if err != nil {
		t.Logf("NewObjectEnforcer: %v", err)
		return absent
	}
	if err := e.AddRole("ana", "editor"); err != nil {
		t.Logf("AddRole: %v", err)
		return absent
	}
	if err := e.Allow("editor", "r.obj.AuthorID == r.sub", "edit"); err != nil {
		t.Logf("Allow: %v", err)
		return absent
	}

	type post struct {
		ID       string
		AuthorID string
	}
	hers := e.Can("ana", post{ID: "1", AuthorID: "ana"}, "edit")
	theirs := e.Can("ana", post{ID: "2", AuthorID: "beto"}, "edit")
	t.Logf("own row: %v · somebody else's: %v", hers, theirs)
	if hers && !theirs {
		return present
	}
	return partial
}

// AZ-04 — the authorization helper a handler reaches for, on the context
// it already has. The probe drives a real request through the middleware
// and asks from inside the handler.
func probeContextAuthorization(t *testing.T, _ *env) verdict {
	if !contextHasMethod("Can") || !contextHasMethod("CanObject") || !contextHasMethod("Claims") {
		return absent
	}

	objects, err := authz.NewObjectEnforcer(nil)
	if err != nil {
		return absent
	}
	if err := objects.AddRole("u1", "editor"); err != nil {
		return absent
	}
	if err := objects.Allow("editor", "r.obj.OwnerID == r.sub", "edit"); err != nil {
		return absent
	}

	type row struct{ OwnerID string }
	var sawIdentity, allowedOwn, deniedOther, closedWithout bool
	handler := authz.ObjectMiddleware(objects)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &nucleus.Context{Context: &router.Context{Request: r, Writer: w}}
		sawIdentity = c.UserID() == "u1"
		allowedOwn = c.CanObject(row{OwnerID: "u1"}, "edit")
		deniedOther = !c.CanObject(row{OwnerID: "u2"}, "edit")
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// And closed when the middleware is not mounted.
	bare := httptest.NewRequest(http.MethodGet, "/", nil)
	c := &nucleus.Context{Context: &router.Context{Request: bare, Writer: httptest.NewRecorder()}}
	closedWithout = !c.CanObject(row{OwnerID: "u1"}, "edit") && !c.Can("/posts", "GET")

	t.Logf("identity=%v own=%v other-denied=%v closed-without-middleware=%v",
		sawIdentity, allowedOwn, deniedOther, closedWithout)
	if sawIdentity && allowedOwn && deniedOther && closedWithout {
		return present
	}
	return partial
}

// AZ-05 — a denial is observable: the application can say WHY, not just 403.
func probeDenialVisibility(t *testing.T, _ *env) verdict {
	var d authz.Denial
	if strings.Contains(strings.ToLower(structFieldNames(d)), "reason") {
		return present
	}
	if structFieldNames(d) == "" {
		return absent
	}
	t.Logf("Denial carries %s", structFieldNames(d))
	return partial
}
