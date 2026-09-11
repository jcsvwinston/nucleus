// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

import (
	"net/http"
	"testing"
)

// MFA-01 — TOTP enrolment and verification.
func probeTOTP(t *testing.T, e *env) verdict {
	if k, ok := hasConfigKeyContaining("totp"); ok {
		t.Logf("config key %q exists", k)
		return partial
	}
	return e.unroutedVerdict(t, "/auth/mfa/totp", "/accounts/mfa/totp")
}

// MFA-02 — WebAuthn / passkeys.
func probeWebAuthn(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/mfa/webauthn", "/accounts/webauthn")
}

// MFA-03 — recovery codes for a lost second factor.
func probeRecoveryCodes(t *testing.T, e *env) verdict {
	return e.unroutedVerdict(t, "/auth/mfa/recovery-codes", "/accounts/recovery-codes")
}

// MFA-04 — step-up: forcing re-authentication before a sensitive operation.
func probeStepUp(t *testing.T, _ *env) verdict {
	if k, ok := hasConfigKeyContaining("step_up"); ok {
		t.Logf("config key %q exists", k)
		return partial
	}
	if k, ok := hasConfigKeyContaining("reauth"); ok {
		t.Logf("config key %q exists", k)
		return partial
	}
	return absent
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
