package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRealIP_XRealIPIsFilteredLikeXForwardedFor closes the asymmetry that
// made a catch-all `trusted_proxies` a spoofing vector (QCD-FW-18).
//
// The X-Forwarded-For walk already skips any hop that is itself a trusted
// proxy — the point is to find the real client as seen by the outermost
// trusted hop. The X-Real-IP fallback applied no such filter: whatever the
// header said was taken verbatim as long as the PEER was trusted.
//
// Under `trusted_proxies: [0.0.0.0/0]` every peer is trusted, the XFF walk
// skips every hop (all trusted) and falls through, and X-Real-IP is then
// honoured from any caller — a spoofed client IP, rate-limit evasion one
// header at a time, and an audit trail recording the attacker's choice.
// `doctor --check security` already reports that configuration; this makes
// the runtime stop honouring the header it cannot verify.
//
// A correctly configured deployment sees no change: a load balancer sets
// X-Real-IP to a real client, and a real client is not in trusted_proxies.
func TestRealIP_XRealIPIsFilteredLikeXForwardedFor(t *testing.T) {
	cases := []struct {
		name     string
		trusted  []string
		peer     string
		xff      string
		xRealIP  string
		wantAddr string
	}{
		{
			name:     "catch-all: X-Real-IP is not honoured",
			trusted:  []string{"0.0.0.0/0"},
			peer:     "10.0.0.9:1234",
			xRealIP:  "203.0.113.9",
			wantAddr: "10.0.0.9:1234", // untouched
		},
		{
			name:     "correct config: X-Real-IP from the LB still works",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xRealIP:  "203.0.113.9",
			wantAddr: "203.0.113.9",
		},
		{
			name:     "correct config: X-Forwarded-For still wins",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xff:      "203.0.113.7, 10.0.0.9",
			xRealIP:  "198.51.100.1",
			wantAddr: "203.0.113.7",
		},
		{
			name:     "untrusted peer: nothing is honoured",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "203.0.113.50:9999",
			xRealIP:  "203.0.113.9",
			wantAddr: "203.0.113.50:9999",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := realIPMiddleware(newTrustedProxyMatcher(tc.trusted))(
				http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.peer
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xRealIP != "" {
				req.Header.Set("X-Real-IP", tc.xRealIP)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if got != tc.wantAddr {
				t.Errorf("RemoteAddr = %q, want %q", got, tc.wantAddr)
			}
		})
	}
}

// TestRealIP_EntriesThatAreNotAddressesAreSkipped pins the second half of the
// same asymmetry: a forwarding entry that is not an IP address at all.
//
// The X-Forwarded-For walk returned the rightmost entry it did not recognise
// as a trusted proxy, and `trusts` answers false for anything that is not an
// address — so "not-an-ip" was recognised as a client and assigned to
// r.RemoteAddr, from where the rate limiter takes its bucket key and the audit
// trail takes the client IP. Any client whose hops to the right are all
// trusted (a catch-all trusted_proxies, an internal client inside the trusted
// range, a chain of known proxies) could put free text into both. X-Real-IP
// had the same hole.
//
// Found by FuzzRealIPForwarding, whose seed corpus carries these inputs.
func TestRealIP_EntriesThatAreNotAddressesAreSkipped(t *testing.T) {
	cases := []struct {
		name     string
		trusted  []string
		peer     string
		xff      string
		xRealIP  string
		wantAddr string
	}{
		{
			name:     "a single X-Forwarded-For entry that is not an address",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xff:      "not-an-ip",
			wantAddr: "10.0.0.9:1234", // untouched
		},
		{
			name:     "the walk continues past it to the real client",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xff:      "203.0.113.7, not-an-ip",
			wantAddr: "203.0.113.7",
		},
		{
			name:     "and past it to the X-Real-IP fallback",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xff:      "not-an-ip",
			xRealIP:  "203.0.113.9",
			wantAddr: "203.0.113.9",
		},
		{
			name:     "X-Real-IP that is not an address is not honoured",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xRealIP:  "not-an-ip",
			wantAddr: "10.0.0.9:1234", // untouched
		},
		{
			name:     "an address with a port is still honoured, verbatim",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xff:      "203.0.113.7:443",
			wantAddr: "203.0.113.7:443",
		},
		{
			name:     "IPv6 in brackets with a port is still honoured, verbatim",
			trusted:  []string{"10.0.0.0/8"},
			peer:     "10.0.0.9:1234",
			xff:      "[2001:db8::1]:443",
			wantAddr: "[2001:db8::1]:443",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := realIPMiddleware(newTrustedProxyMatcher(tc.trusted))(
				http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.RemoteAddr }))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.peer
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xRealIP != "" {
				req.Header.Set("X-Real-IP", tc.xRealIP)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if got != tc.wantAddr {
				t.Errorf("RemoteAddr = %q, want %q", got, tc.wantAddr)
			}
		})
	}
}
