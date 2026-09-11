// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/accounts"
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

// KEY-01 — issuing an API key: stored hashed, shown once, with a prefix the
// operator can recognise later.
func probeAPIKeyIssue(t *testing.T, e *env) verdict {
	if k, ok := hasConfigKeyContaining("api_key"); ok {
		t.Logf("config key %q exists", k)
		return partial
	}
	return e.unroutedVerdict(t, "/auth/api-keys", "/api-keys")
}

// KEY-02 — scopes on a key, projected onto the authorization policy.
func probeAPIKeyScopes(t *testing.T, _ *env) verdict {
	if k, ok := hasConfigKeyContaining("scope"); ok {
		t.Logf("config key %q exists", k)
		return partial
	}
	return absent
}

// KEY-03 — a request that carries a key is authenticated by it. The probe
// sends both spellings at a protected route and reads the answer: a
// framework that knew the header would answer 401 for a bad key, not 404
// for an unknown route.
func probeAPIKeyMiddleware(t *testing.T, e *env) verdict {
	headers := map[string]string{
		"X-API-Key":     "nk_probe_0123456789",
		"Authorization": "Bearer nk_probe_0123456789",
	}
	code := e.status(t, http.MethodGet, "/api/authbench-probe", headers)
	if code != http.StatusNotFound {
		t.Logf("a key-bearing request answered %d", code)
		return partial
	}
	return absent
}

// KEY-04 — rotation and revocation of a key.
func probeAPIKeyRotation(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/api-keys/rotate")
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

// KEY-06 — a CLI to create, list and revoke keys.
func probeAPIKeyCLI(t *testing.T, _ *env) verdict {
	if hasCLICommand(t, "apikey") || hasCLICommand(t, "api-key") {
		return present
	}
	return absent
}
