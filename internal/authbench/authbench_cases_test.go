// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package authbench

// controls is the bench: every capability the auth surface is measured on,
// with the verdict this repository RECORDS for it. The list is a policy
// choice — it comes from what a product needs from authentication, taken
// from the frameworks that already answer it (Laravel Fortify/Sanctum,
// Django allauth, Spring Security, Rails 8) and from ASVS L2 — so it lives
// here, written down, rather than being derived from what happens to exist.
//
// A verdict is what the probe MEASURED on the day it was recorded. Moving
// one is a deliberate edit in the change that moves the code.
func controls() []control {
	return []control{
		// ---- sessions ---------------------------------------------------
		{id: "SES-01", family: "sessions", title: "server-side session survives a request cycle",
			want: present, probe: probeSessionRoundTrip},
		{id: "SES-02", family: "sessions", title: "session token rotates on demand (fixation)",
			want: present, probe: probeSessionRenew},
		{id: "SES-03", family: "sessions", title: "active sessions can be enumerated",
			want: present, probe: probeSessionEnumerate},
		{id: "SES-04", family: "sessions", title: "revoke another session by token",
			want: present, probe: probeSessionRevokeOther},
		{id: "SES-05", family: "sessions", title: "session records the device it belongs to",
			want: present, probe: probeSessionDeviceMetadata},
		{id: "SES-06", family: "sessions", title: "inactivity timeout out of the box",
			want: partial, note: "the knob is honoured but ships disabled (session_idle_timeout = 0), so the default deployment has no idle expiry",
			probe: probeSessionIdleTimeout},

		// ---- credentials ------------------------------------------------
		{id: "CRED-01", family: "credentials", title: "passwords hashed with a modern KDF",
			want: present, probe: probePasswordHash},
		{id: "CRED-02", family: "credentials", title: "a long passphrase is not silently truncated",
			want: present, probe: probeLongPassphrase},
		{id: "CRED-03", family: "credentials", title: "progressive lockout after repeated failures",
			want: present, probe: probeLockout},
		{id: "CRED-04", family: "credentials", title: "password reset by single-use token",
			want: present, probe: probePasswordReset},
		{id: "CRED-05", family: "credentials", title: "password change with re-authentication",
			want: present, probe: probePasswordChange},
		{id: "CRED-06", family: "credentials", title: "registration with email verification",
			want: present, probe: probeEmailVerification},
		{id: "CRED-07", family: "credentials", title: "magic-link sign-in",
			want: present, probe: probeMagicLink},
		{id: "CRED-08", family: "credentials", title: "a login route the framework serves",
			want: present, probe: probeLoginRoute},

		// ---- multi-factor -----------------------------------------------
		{id: "MFA-01", family: "multi-factor", title: "TOTP enrolment and verification",
			want: present, probe: probeTOTP},
		{id: "MFA-02", family: "multi-factor", title: "WebAuthn / passkeys",
			want: absent, note: "nothing in tree", probe: probeWebAuthn},
		{id: "MFA-03", family: "multi-factor", title: "recovery codes",
			want: present, probe: probeRecoveryCodes},
		{id: "MFA-04", family: "multi-factor", title: "step-up re-authentication",
			want: present, probe: probeStepUp},

		// ---- API keys ---------------------------------------------------
		{id: "KEY-01", family: "api-keys", title: "issue a key, stored hashed, shown once",
			want: present, probe: probeAPIKeyIssue},
		{id: "KEY-02", family: "api-keys", title: "scopes on a key, projected onto the policy",
			want: present, probe: probeAPIKeyScopes},
		{id: "KEY-03", family: "api-keys", title: "a key-bearing request is authenticated",
			want: present, probe: probeAPIKeyMiddleware},
		{id: "KEY-04", family: "api-keys", title: "rotation and revocation",
			want: present, probe: probeAPIKeyRotation},
		// Recorded PRESENT against the hypothesis this bench started with.
		// The limiter keys on the authenticated user id, with the tenant as
		// a prefix, and falls back to the address only for anonymous
		// traffic — measured by exhausting one user's budget and finding
		// another user, same role and same address, still served. The
		// guess that it "keys on the address" came from reading the name
		// of a configuration knob, which is the A4 lesson in its other
		// form: a name is not a measurement either. What A5 has to keep is
		// that an API-key middleware must put the key's identity where the
		// limiter already looks.
		{id: "KEY-05", family: "api-keys", title: "rate limit per identity, not per address",
			want: present, probe: probeAPIKeyRateLimit},
		{id: "KEY-06", family: "api-keys", title: "CLI to create, list and revoke keys",
			want: present, probe: probeAPIKeyCLI},

		// ---- federated identity ------------------------------------------
		{id: "FED-01", family: "federated", title: "browser-redirect contract with framework-owned state",
			want: present, probe: probeFederatedContract},
		{id: "FED-02", family: "federated", title: "an OIDC provider ships with the framework",
			want: absent, note: "the seam has no implementation: every deployment writes discovery, PKCE and JWKS itself",
			probe: probeOIDCProvider},
		{id: "FED-03", family: "federated", title: "a SAML provider ships with the framework",
			want: absent, note: "same seam, same gap", probe: probeSAMLProvider},
		{id: "FED-04", family: "federated", title: "claims map onto roles",
			want: present, probe: probeClaimMapping},

		// ---- authorization -----------------------------------------------
		{id: "AZ-01", family: "authorization", title: "role-based access control over routes",
			want: present, probe: probeRBAC},
		{id: "AZ-02", family: "authorization", title: "explicit deny beats a grant",
			want: present, probe: probeExplicitDeny},
		{id: "AZ-03", family: "authorization", title: "permission on an object, not just a path",
			want: absent, note: "the Casbin model is sub/obj/act: 'ana may edit the posts she owns' cannot be expressed, and every application re-implements ownership in its handlers",
			probe: probeObjectPermission},
		{id: "AZ-04", family: "authorization", title: "authorization helper on the handler context",
			want: absent, note: "nucleus.Context exposes neither the identity nor a Can(): a handler reaches for the claims through the request context by hand",
			probe: probeContextAuthorization},
		{id: "AZ-05", family: "authorization", title: "a denial says why",
			want: present, probe: probeDenialVisibility},

		// ---- transactional mail -------------------------------------------
		{id: "MAIL-01", family: "mail", title: "send a plain-text message",
			want: present, probe: probeMailPlain},
		{id: "MAIL-02", family: "mail", title: "send an HTML / multipart message",
			want: present, probe: probeMailHTML},
		{id: "MAIL-03", family: "mail", title: "attachments",
			want: present, probe: probeMailAttachments},
		{id: "MAIL-04", family: "mail", title: "render a message from a template",
			want: present, probe: probeMailTemplates},
		{id: "MAIL-05", family: "mail", title: "queued mail survives a crash",
			want: present, probe: probeMailDurability},

		// ---- tokens --------------------------------------------------------
		{id: "TOK-01", family: "tokens", title: "signed tokens with a keyring and rotation",
			want: present, probe: probeJWTRotation},
		{id: "TOK-02", family: "tokens", title: "a token from another issuer is rejected",
			want: present, probe: probeJWTAudience},
		{id: "TOK-03", family: "tokens", title: "revoke an issued token before it expires",
			want: present, probe: probeJWTRevocation},

		// ---- posture --------------------------------------------------------
		{id: "POS-01", family: "posture", title: "default security posture frozen as observed values",
			want: present, probe: probeSecurityPosture},
		{id: "POS-02", family: "posture", title: "posture mapped to a named standard, control by control",
			want: absent, note: "the baseline records what the framework emits; nothing ties a line of it to an ASVS requirement, so 'ASVS L2' is not yet a checkable claim",
			probe: probeASVSCoverage},
	}
}
