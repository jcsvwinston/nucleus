package router

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FuzzRealIPForwarding fuzzes the two forwarding headers and the peer address
// against a fuzzed trusted-proxy list. Everything here is attacker-controlled
// except the list: X-Forwarded-For and X-Real-IP are set by whoever sent the
// request, and what this function returns is written straight into
// r.RemoteAddr — which becomes the rate-limit bucket key (ratelimit.clientIP)
// and the client IP in session metadata and the audit trail
// (auth.ClientIPFromRequest). H-N3 and QCD-FW-18 are both failures of this
// one function.
//
// Three properties, all invariants rather than "does not panic":
//
//  1. Nothing is honoured unless the immediate peer is a trusted proxy. An
//     untrusted peer leaves RemoteAddr alone, whatever it claims to be.
//  2. The result is never an address the deployment itself calls a proxy.
//     That is the QCD-FW-18 invariant: under a catch-all `trusted_proxies`
//     the X-Forwarded-For walk skips every hop, and the X-Real-IP fallback
//     used to hand the attacker's chosen address back verbatim.
//  3. The result is an IP address. It is written into RemoteAddr as one, and
//     every consumer downstream reads it as one.
//
// Seeds: the four cases of TestRealIP_XRealIPIsFilteredLikeXForwardedFor plus
// the shapes a client can spell that a load balancer never does — an entry
// that is not an address at all, IPv6 with and without brackets and ports,
// empty entries, a header that is only commas. The entries that are not
// addresses are the ones this target caught: they are committed as named
// corpus files in testdata/fuzz/FuzzRealIPForwarding, and the fix they forced
// is pinned deterministically by
// TestRealIP_EntriesThatAreNotAddressesAreSkipped.
func FuzzRealIPForwarding(f *testing.F) {
	type seed struct{ trusted, peer, xff, xRealIP string }
	seeds := []seed{
		{"0.0.0.0/0", "10.0.0.9:1234", "", "203.0.113.9"},
		{"10.0.0.0/8", "10.0.0.9:1234", "", "203.0.113.9"},
		{"10.0.0.0/8", "10.0.0.9:1234", "203.0.113.7, 10.0.0.9", "198.51.100.1"},
		{"10.0.0.0/8", "203.0.113.50:9999", "", "203.0.113.9"},
		{"10.0.0.0/8", "10.0.0.9:1234", "not-an-ip", ""},
		{"10.0.0.0/8", "10.0.0.9:1234", "203.0.113.7, not-an-ip", ""},
		{"10.0.0.0/8", "10.0.0.9:1234", "", "not-an-ip"},
		{"10.0.0.0/8", "10.0.0.9:1234", ",,,", "  "},
		{"10.0.0.0/8", "10.0.0.9:1234", "203.0.113.7:443", ""},
		{"10.0.0.0/8", "10.0.0.9:1234", "[2001:db8::1]:443", ""},
		{"10.0.0.0/8", "10.0.0.9:1234", "2001:db8::1", ""},
		{"::/0", "[2001:db8::9]:1234", "2001:db8::1", ""},
		{"", "10.0.0.9:1234", "203.0.113.7", "203.0.113.9"},
		{"10.0.0.9", "10.0.0.9", "203.0.113.7", ""},
	}
	for _, s := range seeds {
		f.Add(s.trusted, s.peer, s.xff, s.xRealIP)
	}

	f.Fuzz(func(t *testing.T, trustedCSV, peer, xff, xRealIP string) {
		trusted := newTrustedProxyMatcher(strings.Split(trustedCSV, ","))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", xff)
		req.Header.Set("X-Real-IP", xRealIP)

		got := realIPFromRequest(req, trusted)
		if got == "" {
			return // RemoteAddr is left untouched: always safe
		}
		if !trusted.trusts(peer) {
			t.Fatalf("peer %q is not a trusted proxy but its forwarding headers were honoured (xff=%q, x-real-ip=%q) -> %q",
				peer, xff, xRealIP, got)
		}
		if trusted.trusts(got) {
			t.Fatalf("returned %q, which the deployment lists as a proxy: a hop is not a client (trusted=%q, xff=%q, x-real-ip=%q)",
				got, trustedCSV, xff, xRealIP)
		}
		host := strings.TrimSpace(got)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if net.ParseIP(host) == nil {
			t.Fatalf("returned %q, which is not an IP address; it is written into RemoteAddr and read back as one by the rate limiter and the audit trail (trusted=%q, peer=%q, xff=%q, x-real-ip=%q)",
				got, trustedCSV, peer, xff, xRealIP)
		}
	})
}
