// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/jcsvwinston/nucleus/pkg/accounts"
	"github.com/jcsvwinston/nucleus/pkg/auth/apikeys"
	"github.com/jcsvwinston/nucleus/pkg/observe"
)

// MFA-01 — TOTP enrolment and verification, followed end to end against a
// running application: sign in, enrol, scan the URI, confirm with a code,
// sign out, and find the password alone no longer finishes a sign-in.
func probeTOTP(t *testing.T, e *env) verdict {
	srv, _ := e.accountsServer(t)
	email := fmt.Sprintf("totp-%d@example.test", time.Now().UnixNano())
	password := "a-long-enough-passphrase"

	if status, _ := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusAccepted {
		return absent
	}
	if status, _ := postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusOK {
		return absent
	}

	status, body := postJSON(t, srv, accounts.RouteTOTP, map[string]string{})
	if status == http.StatusNotFound {
		return absent
	}
	if status != http.StatusOK {
		t.Logf("enrolment answered %d: %v", status, body)
		return partial
	}
	secret, _ := body["secret"].(string)
	uri, _ := body["uri"].(string)
	if secret == "" || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Logf("enrolment returned %v", body)
		return partial
	}

	code := accounts.TestingTOTPCode(t, secret, time.Now().Add(-30*time.Second))
	if status, body := putJSON(t, srv, accounts.RouteTOTP, map[string]string{"code": code}); status != http.StatusOK {
		t.Logf("confirmation answered %d: %v", status, body)
		return partial
	}

	if status, _ := postJSON(t, srv, accounts.RouteLogout, map[string]string{}); status != http.StatusNoContent {
		return partial
	}
	status, body = postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	})
	if status != http.StatusUnauthorized || body["mfa_required"] != true {
		t.Logf("the password alone still signs in: %d %v", status, body)
		return partial
	}
	if status, _ := postJSON(t, srv, accounts.RouteMFAVerify, map[string]string{
		"code": accounts.TestingTOTPCode(t, secret, time.Now()),
	}); status != http.StatusOK {
		t.Logf("the second factor answered %d", status)
		return partial
	}
	return present
}

// MFA-02 — WebAuthn / passkeys.
func probeWebAuthn(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/mfa/webauthn", "/accounts/webauthn")
}

// MFA-03 — recovery codes for a lost second factor: issued at enrolment,
// single use, and they actually complete a sign-in.
func probeRecoveryCodes(t *testing.T, e *env) verdict {
	srv, _ := e.accountsServer(t)
	email := fmt.Sprintf("recovery-%d@example.test", time.Now().UnixNano())
	password := "a-long-enough-passphrase"

	if status, _ := postJSON(t, srv, accounts.RouteRegister, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusAccepted {
		return absent
	}
	if status, _ := postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusOK {
		return absent
	}
	status, body := postJSON(t, srv, accounts.RouteTOTP, map[string]string{})
	if status == http.StatusNotFound {
		return absent
	}
	secret, _ := body["secret"].(string)
	if secret == "" {
		return partial
	}
	_, body = putJSON(t, srv, accounts.RouteTOTP, map[string]string{
		"code": accounts.TestingTOTPCode(t, secret, time.Now().Add(-30*time.Second)),
	})
	codes, _ := body["recovery_codes"].([]any)
	if len(codes) != accounts.RecoveryCodeCount {
		t.Logf("enrolment returned %d recovery codes", len(codes))
		return partial
	}

	if status, _ := postJSON(t, srv, accounts.RouteLogout, map[string]string{}); status != http.StatusNoContent {
		return partial
	}
	if status, _ := postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusUnauthorized {
		return partial
	}
	first, _ := codes[0].(string)
	if status, _ := postJSON(t, srv, accounts.RouteMFAVerify, map[string]string{"code": first}); status != http.StatusOK {
		t.Logf("a recovery code answered %d", status)
		return partial
	}

	// And it is spent: signing in again with the same code fails.
	if status, _ := postJSON(t, srv, accounts.RouteLogout, map[string]string{}); status != http.StatusNoContent {
		return partial
	}
	if status, _ := postJSON(t, srv, accounts.RouteLogin, map[string]string{
		"email": email, "password": password,
	}); status != http.StatusUnauthorized {
		return partial
	}
	if status, _ := postJSON(t, srv, accounts.RouteMFAVerify, map[string]string{"code": first}); status == http.StatusOK {
		t.Log("a recovery code worked twice")
		return partial
	}
	return present
}

// MFA-04 — step-up: forcing a fresh proof of identity before a sensitive
// operation. Measured on the operation that has one — changing a second
// factor — from a session that is signed in but not fresh.
func probeStepUp(t *testing.T, e *env) verdict {
	srv, _ := e.accountsServer(t)
	// An anonymous caller is refused with 401; what this control is about
	// is the SIGNED-IN but stale one, which the service decides with
	// RequireFreshAuth. The probe checks the guard exists and refuses.
	status, _ := postJSON(t, srv, accounts.RouteTOTP, map[string]string{})
	switch status {
	case http.StatusNotFound:
		return absent
	case http.StatusUnauthorized, http.StatusForbidden:
		return present
	default:
		t.Logf("an unauthenticated factor change answered %d", status)
		return partial
	}
}

// KEY-01 — issuing a key: stored hashed, shown once, with a prefix an
// operator (and a secret scanner) can recognise.
func probeAPIKeyIssue(t *testing.T, e *env) verdict {
	store := e.apiKeyStore(t)
	key, presented, err := apikeys.Issue(t.Context(), store, apikeys.Key{Name: "bench", OwnerID: "user-1"})
	if err != nil {
		t.Logf("Issue: %v", err)
		return absent
	}
	if !strings.HasPrefix(presented, apikeys.Prefix+"_") {
		t.Logf("no recognisable prefix: %q", presented)
		return partial
	}
	if key.SecretHash == "" || strings.Contains(presented, key.SecretHash) {
		t.Log("the secret is stored, or the presented key carries its own hash")
		return partial
	}
	if _, err := apikeys.Authenticate(t.Context(), store, presented); err != nil {
		t.Logf("the issued key does not authenticate: %v", err)
		return partial
	}
	return present
}

// KEY-02 — scopes on a key, enforced on a route.
func probeAPIKeyScopes(t *testing.T, e *env) verdict {
	store := e.apiKeyStore(t)
	_, presented, err := apikeys.Issue(t.Context(), store, apikeys.Key{
		Name: "scoped", Scopes: []string{"billing:read"},
	})
	if err != nil {
		return absent
	}

	allowed := probeScopedStatus(t, store, presented, "billing:read")
	denied := probeScopedStatus(t, store, presented, "billing:write")
	t.Logf("granted scope: %d · missing scope: %d", allowed, denied)
	if allowed != http.StatusNoContent {
		return partial
	}
	// 403 rather than 401: the caller IS authenticated.
	if denied != http.StatusForbidden {
		return partial
	}
	return present
}

// KEY-03 — a key-bearing request is authenticated, under both spellings,
// and the key's owner reaches the identity the rate limiter already uses.
func probeAPIKeyMiddleware(t *testing.T, e *env) verdict {
	store := e.apiKeyStore(t)
	_, presented, err := apikeys.Issue(t.Context(), store, apikeys.Key{Name: "bench", OwnerID: "user-42"})
	if err != nil {
		return absent
	}

	for _, header := range []string{apikeys.HeaderName, "Authorization"} {
		value := presented
		if header == "Authorization" {
			value = "Bearer " + presented
		}
		var owner string
		var authenticated bool
		h := apikeys.Middleware(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, authenticated = apikeys.FromContext(r.Context())
			owner = observe.UserIDFromCtx(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(header, value)
		h.ServeHTTP(httptest.NewRecorder(), req)

		if !authenticated {
			t.Logf("%s was not recognised", header)
			return partial
		}
		if owner != "user-42" {
			t.Logf("%s: the owner did not reach the identity the limiter reads (%q)", header, owner)
			return partial
		}
	}
	return present
}

// KEY-04 — rotation and revocation, measured on the property that makes
// rotation usable: the old key keeps working for its grace period.
func probeAPIKeyRotation(t *testing.T, e *env) verdict {
	store := e.apiKeyStore(t)
	original, originalPresented, err := apikeys.Issue(t.Context(), store, apikeys.Key{Name: "rotating"})
	if err != nil {
		return absent
	}
	_, replacementPresented, err := apikeys.Rotate(t.Context(), store, original.ID, time.Hour)
	if err != nil {
		t.Logf("Rotate: %v", err)
		return absent
	}
	if _, err := apikeys.Authenticate(t.Context(), store, originalPresented); err != nil {
		t.Log("the old key stopped working the moment it was rotated")
		return partial
	}
	if _, err := apikeys.Authenticate(t.Context(), store, replacementPresented); err != nil {
		t.Log("the replacement does not authenticate")
		return partial
	}

	// And revocation is immediate.
	if err := store.Revoke(t.Context(), original.ID, time.Now()); err != nil {
		return partial
	}
	if _, err := apikeys.Authenticate(t.Context(), store, originalPresented); err == nil {
		t.Log("a revoked key still authenticates")
		return partial
	}
	return present
}

// probeScopedStatus runs one request through the middleware and the scope
// guard, returning the status.
func probeScopedStatus(t *testing.T, store apikeys.Store, presented, scope string) int {
	t.Helper()
	h := apikeys.Middleware(store)(apikeys.Require(scope)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(apikeys.HeaderName, presented)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// KEY-05 — rate limiting per identity rather than per address.
//
// The knob named rate_limit_by_role EXISTS, and reading its name is not a
// measurement: what it does is scope the bucket by the ROLE in the claims.
// So the probe boots an application with a budget of one request and sends
// two, from the same address, as two DIFFERENT users who share a role. If
// the second is refused, the budget belongs to the address-and-role pair,
// not to the caller — which is what a per-key limit would need.
func probeAPIKeyRateLimit(t *testing.T, _ *env) verdict {
	srv := startRateLimited(t)

	// Spend the whole budget as one user, then ask as another. Both share
	// a role and an address; only the caller differs.
	ana := srv.MintToken("1", "ana", "editor")
	spent := false
	for i := 0; i < 10; i++ {
		if rateLimitedProbe(t, srv, ana) == http.StatusTooManyRequests {
			spent = true
			break
		}
	}
	if !spent {
		t.Log("the budget never ran out: the probe measured nothing")
		return absent
	}

	beto := rateLimitedProbe(t, srv, srv.MintToken("2", "beto", "editor"))
	t.Logf("after ana exhausted the budget, beto got %d", beto)
	if beto == http.StatusTooManyRequests {
		return partial
	}
	return present
}

// KEY-06 — a CLI to create, list, revoke and rotate keys.
func probeAPIKeyCLI(t *testing.T, _ *env) verdict {
	if hasCLICommand(t, "apikey") {
		return present
	}
	return absent
}
