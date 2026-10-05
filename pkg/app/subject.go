// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/authz"
)

// Who a request is, for the default-deny layer.
//
// One namespace for identities, whatever the credential: the id of the
// person or the program behind the request — a bearer token's user id, the
// owner of an API key, the account a session was signed in as. A policy row
// written for that id applies to all three, and a role granted to it with
// `g, <id>, <role>` too. What a credential carries beyond the identity is a
// subject of its own: the token's role, and each scope of an API key as
// `scope:<name>` (apikeys.ScopeSubject), so a policy can grant a route to
// whatever key carries a scope. The anonymous subject comes last: an
// identified caller never loses what the bootstrap allow-list grants
// everyone.
//
// A request is allowed when any of its subjects is (QCD-FW-1), so a subject
// can only add to what a request may do — which is what keeps this list
// additive: a request that carried none of these before is decided exactly
// as it was.

// requestSubjects returns the subjects of a request, most specific first,
// each once.
func requestSubjects(r *http.Request, sessions *auth.SessionManager) []string {
	subjects := make([]string, 0, 4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, have := range subjects {
			if have == s {
				return
			}
		}
		subjects = append(subjects, s)
	}
	ctx := r.Context()
	if claims, ok := auth.ClaimsFromContext(ctx); ok && claims != nil {
		add(claims.UserID)
		add(claims.Role)
	}
	// An API key reaches this layer when WithAPIKeys mounted its middleware
	// ahead of it, next to the bearer decode (NU-112).
	if key, ok := apikeys.FromContext(ctx); ok {
		add(key.OwnerID)
		for _, scope := range key.Scopes {
			add(apikeys.ScopeSubject(scope))
		}
	}
	if sessions != nil && sessions.HasSession(ctx) {
		add(sessions.GetString(ctx, auth.SessionKeySubject))
	}
	add(authz.BootstrapSubject)
	return subjects
}

// requestIdentity is the first subject of a request that names who is
// behind it — the token's user, the key's owner, the signed-in account —
// or "" for an anonymous request. Presence on a realtime channel reports
// it.
func requestIdentity(r *http.Request, sessions *auth.SessionManager) string {
	ctx := r.Context()
	if claims, ok := auth.ClaimsFromContext(ctx); ok && claims != nil && strings.TrimSpace(claims.UserID) != "" {
		return strings.TrimSpace(claims.UserID)
	}
	if key, ok := apikeys.FromContext(ctx); ok && strings.TrimSpace(key.OwnerID) != "" {
		return strings.TrimSpace(key.OwnerID)
	}
	if sessions != nil && sessions.HasSession(ctx) {
		return strings.TrimSpace(sessions.GetString(ctx, auth.SessionKeySubject))
	}
	return ""
}
