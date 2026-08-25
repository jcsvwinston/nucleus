// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
)

// checkSecurity looks for configurations that are VALID and DANGEROUS.
//
// The config layers (ADR-010 §2) judge whether a file makes sense; every
// case here loads clean and boots clean, and is still a hole once the app
// faces the internet. That gap is what a deploy check is for.
//
// It deliberately does not repeat `nucleus health --deploy` (env, debug,
// log format, session store, mail, cookie flags): a check that restates
// another one teaches operators to skim both. The one overlap is the JWT
// secret, and it is not a repeat — health measures LENGTH, and 32 identical
// characters is long and guessable.
func checkSecurity(cfg *app.Config, _ string) doctorCheckOutcome {
	if cfg == nil {
		return doctorError("No configuration loaded", nil)
	}

	prod := cfg.IsProd()
	var errs, warns []string

	// CORS. A wildcard allow-list means every site on the internet can read
	// authenticated responses from a browser that has a session here. With
	// credentials on top, the Fetch standard forbids the combination
	// outright — browsers reject it, so the deployment is both unsafe in
	// intent and broken in practice.
	if corsHasWildcard(cfg.CORSOrigins) {
		if cfg.CORSAllowCredentials {
			errs = append(errs, "cors_origins contains \"*\" together with cors_allow_credentials=true — the Fetch standard forbids credentialed wildcard CORS, so browsers reject it; name the exact origins instead")
		} else if prod {
			warns = append(warns, "cors_origins contains \"*\" in production — any site can read responses from a browser session; name the exact origins instead")
		}
	}

	// Trusted proxies. A catch-all range means the framework believes
	// whatever X-Forwarded-For any client sends: spoofed client IP,
	// rate-limit evasion one header at a time, and an audit trail that
	// records the attacker's choice of address.
	if entry, ok := trustedProxyCatchAll(cfg.TrustedProxies); ok {
		errs = append(errs, fmt.Sprintf("trusted_proxies contains the catch-all %q — X-Forwarded-For becomes attacker-controlled (spoofed client IP, rate-limit evasion, forged audit trail); list your load balancer's addresses instead", entry))
	}

	// Signing key. Length is not entropy — `health --deploy` already
	// measures length, and 32 identical characters passes it.
	//
	// An EMPTY jwt_secret is not a hole: with no jwt_keys[] either, the
	// framework builds no JWT manager at all and never mints a token
	// (jwt_setup.go). Flagging it would train operators to ignore the
	// check on every app that does not use JWT.
	if len(cfg.JWTKeys) == 0 && strings.TrimSpace(cfg.JWTSecret) != "" {
		if reason, weak := weakSigningSecret(cfg.JWTSecret); weak {
			errs = append(errs, fmt.Sprintf("jwt_secret %s — anyone who guesses it mints valid tokens for any user; generate a random 32+ byte value and load it from the environment", reason))
		}
	}

	// CSRF.
	if cfg.CSRFEnabled && cfg.CSRFInsecureCookie && prod {
		errs = append(errs, "csrf_insecure_cookie=true in production — the double-submit token travels over plain HTTP, which is exactly what an attacker on the network needs to forge a request")
	}
	if prod && !cfg.CSRFEnabled {
		warns = append(warns, "csrf_enabled=false — cookie-authenticated forms have no CSRF protection; token-only APIs may leave this off deliberately")
	}

	// Rate limiting. Not a hole by itself, but on an internet-facing
	// deployment its absence is what turns a leaked password into a
	// successful credential-stuffing run.
	if prod && cfg.RateLimitRequests <= 0 {
		warns = append(warns, "rate_limit_requests=0 in production — no limit on login or API brute force")
	}

	sort.Strings(errs)
	sort.Strings(warns)

	switch {
	case len(errs) > 0:
		return doctorError(fmt.Sprintf("%d high-risk setting(s): %s", len(errs), strings.Join(append(errs, warns...), " | ")), nil)
	case len(warns) > 0:
		return doctorWarning(fmt.Sprintf("%d setting(s) to review: %s", len(warns), strings.Join(warns, " | ")))
	}

	scope := "development"
	if prod {
		scope = "production"
	}
	return doctorPass(fmt.Sprintf("No high-risk security settings found for env=%s (deployment posture beyond this: nucleus health --deploy)", scope))
}

func corsHasWildcard(origins []string) bool {
	for _, o := range origins {
		if strings.TrimSpace(o) == "*" {
			return true
		}
	}
	return false
}

// trustedProxyCatchAll reports the first entry that trusts every address.
// Parsing mirrors router.newTrustedProxyMatcher: a CIDR whose prefix length
// is zero matches the whole address space.
func trustedProxyCatchAll(entries []string) (string, bool) {
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(e); err == nil {
			if ones, _ := ipnet.Mask.Size(); ones == 0 {
				return e, true
			}
		}
	}
	return "", false
}

// weakSigningSecret catches keys that pass a length check and would not
// survive a dictionary: placeholders shipped by scaffolds and tutorials,
// and strings with almost no distinct characters.
func weakSigningSecret(secret string) (string, bool) {
	s := strings.TrimSpace(secret)
	// Length is left to `health --deploy` (and to the boot-time check in
	// jwt_setup.go, which refuses to start below 32 bytes). What is not
	// checked anywhere else is whether those bytes are guessable.
	lower := strings.ToLower(s)
	for _, marker := range []string{"changeme", "change-me", "change_me", "your-secret", "your_secret", "secret-key-here", "replace-me", "placeholder", "example", "insecure", "supersecret", "todo"} {
		if strings.Contains(lower, marker) {
			return fmt.Sprintf("contains the placeholder %q", marker), true
		}
	}

	distinct := map[rune]struct{}{}
	for _, r := range s {
		distinct[r] = struct{}{}
	}
	if len(distinct) < 8 {
		return fmt.Sprintf("uses only %d distinct characters", len(distinct)), true
	}

	return "", false
}
