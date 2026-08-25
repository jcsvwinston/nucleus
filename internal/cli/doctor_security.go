// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"math/big"
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
		// The header named here is X-Real-IP, not X-Forwarded-For, and the
		// distinction is not pedantic (QCD-FW-18). Under a catch-all,
		// realIPFromRequest walks X-Forwarded-For right to left skipping
		// every hop that is itself trusted — and under a catch-all they
		// all are — so it falls through. What is honoured unconditionally
		// once the peer is trusted is the X-Real-IP fallback. An operator
		// hardening the wrong header would have fixed nothing.
		errs = append(errs, fmt.Sprintf("trusted_proxies trusts every address — %s — so X-Real-IP is taken from any caller (spoofed client IP, rate-limit evasion, forged audit trail); list your load balancer's addresses instead", entry))
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

// trustedProxyCatchAll reports whether the configured entries, TAKEN
// TOGETHER, trust an entire address family.
//
// It used to judge one entry at a time and flag only a zero-length prefix,
// so `0.0.0.0/0` was caught while `0.0.0.0/1` + `128.0.0.0/1` — the same
// address space, spelled in two lines — passed clean (QCD-FW-18). Same
// vector, same impact, opposite verdict; and an operator who wanted the
// catch-all could get it past the check by splitting it in half.
//
// Coverage is computed over merged intervals, so any spelling of "all of
// IPv4" or "all of IPv6" is caught: one entry, two, or a thousand.
func trustedProxyCatchAll(entries []string) (string, bool) {
	type span struct{ lo, hi *big.Int }
	byFamily := map[int][]span{}

	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		var ipnet *net.IPNet
		if _, parsed, err := net.ParseCIDR(e); err == nil {
			ipnet = parsed
		} else if ip := net.ParseIP(e); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			ipnet = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
		} else {
			continue
		}

		bits := 128
		base := ipnet.IP
		if v4 := ipnet.IP.To4(); v4 != nil {
			bits = 32
			base = v4
		}
		lo := new(big.Int).SetBytes(base)
		ones, _ := ipnet.Mask.Size()
		size := new(big.Int).Lsh(big.NewInt(1), uint(bits-ones))
		hi := new(big.Int).Add(lo, size)
		hi.Sub(hi, big.NewInt(1))
		byFamily[bits] = append(byFamily[bits], span{lo: lo, hi: hi})
	}

	for bits, spans := range byFamily {
		sort.Slice(spans, func(i, j int) bool { return spans[i].lo.Cmp(spans[j].lo) < 0 })
		// Walk the merged intervals; if they reach the top of the family's
		// space without a gap, everything is trusted.
		next := big.NewInt(0)
		max := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		max.Sub(max, big.NewInt(1))
		for _, sp := range spans {
			if sp.lo.Cmp(next) > 0 {
				break // gap: not full coverage
			}
			if after := new(big.Int).Add(sp.hi, big.NewInt(1)); after.Cmp(next) > 0 {
				next = after
			}
		}
		if next.Cmp(max) > 0 {
			family := "IPv4"
			if bits == 128 {
				family = "IPv6"
			}
			return fmt.Sprintf("%s (all of %s, via %s)", strings.Join(entries, ", "), family, pluralEntries(len(entries))), true
		}
	}
	return "", false
}

func pluralEntries(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
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
