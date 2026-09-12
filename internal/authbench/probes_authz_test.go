// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"strings"
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/authz"
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

// AZ-03 — permission on an OBJECT: "ana may edit the posts SHE owns". The
// probe states the question the policy model has to be able to ask, and the
// model takes three terms — subject, path, action — with no place for an
// attribute of the row.
func probeObjectPermission(t *testing.T, _ *env) verdict {
	e, err := authz.New(nil)
	if err != nil {
		return absent
	}
	if err := e.AddRole("ana", "editor"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}
	if err := e.AllowResource("editor", "/posts/*", "PUT"); err != nil {
		t.Fatalf("AllowResource: %v", err)
	}

	// Two rows of the same resource, one owned by ana and one not. A model
	// with object attributes separates them; a path-only model cannot,
	// and answers the same for both.
	ownedByAna := e.Can("ana", "/posts/1", "PUT")
	ownedBySomeoneElse := e.Can("ana", "/posts/2", "PUT")
	if ownedByAna && !ownedBySomeoneElse {
		return present
	}
	t.Log("the policy cannot tell one row from another: the model is sub/obj/act")
	return absent
}

// AZ-04 — the authorization helper an application reaches for inside a
// handler, on the request context.
func probeContextAuthorization(t *testing.T, _ *env) verdict {
	for _, name := range []string{"Can", "Authorize", "Allows"} {
		if contextHasMethod(name) {
			t.Logf("Context has %s", name)
			return present
		}
	}
	if contextHasMethod("Claims") || contextHasMethod("User") {
		return partial
	}
	return absent
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
