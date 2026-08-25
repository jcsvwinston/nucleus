// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-FW-18: the trusted-proxy check judged each entry on its own, so a
// catch-all SPLIT IN TWO passed clean while covering exactly the same
// address space as the one the check rejects.
//
// The demo measured it: with 0.0.0.0/0 the check flags it; with
// 0.0.0.0/1 + 128.0.0.0/1 it says nothing. Same vector, same impact,
// opposite verdict.
package cli

import (
	"strings"
	"testing"
)

func TestCheckSecurity_TrustedProxyUnionCoversEverything(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
	}{
		{"whole space in one entry", []string{"0.0.0.0/0"}},
		{"whole space split in two", []string{"0.0.0.0/1", "128.0.0.0/1"}},
		{"whole space split in four", []string{"0.0.0.0/2", "64.0.0.0/2", "128.0.0.0/2", "192.0.0.0/2"}},
		{"ipv6 split in two", []string{"::/1", "8000::/1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := prodConfig()
			cfg.TrustedProxies = tc.entries

			out := checkSecurity(cfg, "")
			if out.status != doctorStatusError {
				t.Fatalf("trusting the whole address space makes the client IP attacker-controlled however it is spelled; want error, got %q (%s)", out.status, out.message)
			}
			if !strings.Contains(out.message, "trusted_proxies") {
				t.Errorf("message must name the key, got %q", out.message)
			}
		})
	}
}

// The message named the wrong header. Under a catch-all, X-Forwarded-For is
// NOT honoured — realIPFromRequest walks right to left skipping every hop
// that is itself trusted, and under a catch-all they all are, so it falls
// through. The vector that actually works is the X-Real-IP fallback,
// returned unconditionally once the peer is trusted.
func TestCheckSecurity_TrustedProxyMessageNamesTheRealVector(t *testing.T) {
	cfg := prodConfig()
	cfg.TrustedProxies = []string{"0.0.0.0/0"}

	out := checkSecurity(cfg, "")
	if !strings.Contains(out.message, "X-Real-IP") {
		t.Errorf("the message must name the header that is actually spoofable, got: %s", out.message)
	}
}

// A real load-balancer range must stay clean, or the check is noise.
func TestCheckSecurity_NarrowProxyRangesPass(t *testing.T) {
	cfg := prodConfig()
	cfg.CSRFEnabled = true
	cfg.RateLimitRequests = 100
	cfg.TrustedProxies = []string{"10.0.0.0/8", "172.16.0.0/12", "127.0.0.1/32"}

	if out := checkSecurity(cfg, ""); out.status != doctorStatusPass {
		t.Fatalf("ordinary private ranges must pass, got %q (%s)", out.status, out.message)
	}
}
