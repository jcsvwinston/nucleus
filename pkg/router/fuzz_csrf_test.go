package router

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fuzzCSRFKey is a fixed 32-byte AES-256 key for the XSRF-cookie crypto
// exercised below. It is test material, never a default: ADR-006 removed the
// derived key precisely because a predictable one is not a key.
var fuzzCSRFKey = []byte("0123456789abcdef0123456789abcdef")

// fuzzCSRFExemptPrefix is the one exempt prefix the gate under test carries,
// so the fuzzer can reach the ExemptPaths branch (a prefix match on the
// DECODED path — "/public/x" and "/pub%6cic/x" are the same request).
const fuzzCSRFExemptPrefix = "/public/"

// FuzzCSRFGate fuzzes everything a cross-site attacker controls about a
// request — method, path, Sec-Fetch-Site, the token header, the token cookie
// and the form body — against the CSRF middleware, in the two origin modes
// (same-site allowed or not, origin-only or token fallback).
//
// The property is a decision table, not "does not panic": the gate must let a
// request through exactly when the middleware's own rules say so, in the
// middleware's own order — exempt prefix, then the Sec-Fetch-Site shortcuts,
// then safe methods, then an exact match between the submitted token and the
// one the server holds. Asserting equality in both directions catches the two
// failure shapes that matter: a state-changing request accepted without a
// matching token (the CSRF hole) and a legitimate request rejected (a lockout
// regression). The submitted token and the server-side token are recomputed
// with this package's own helpers on an identical request, so the model
// cannot drift from stdlib form-parsing rules — a DELETE body is not parsed,
// a body value outranks a query value, an unreadable cookie is no cookie.
//
// The same target pins the XSRF-TOKEN crypto around it: encrypt/decrypt is a
// round trip for any token, and a ciphertext the fuzzer made up must FAIL to
// decrypt. That second half is a regression freeze — decryptToken once
// returned ("", nil) for any ciphertext shorter than the GCM nonce, so a
// truncated header decrypted to the empty string with no error.
//
// Seeds: the cases from csrf_origin_test.go and csrf_hardening_test.go
// (cross-site POST, forged same-origin, missing header, matching token pair),
// plus the short and malformed ciphertexts that shape freezes. The named ones
// are committed as corpus files in testdata/fuzz/FuzzCSRFGate.
func FuzzCSRFGate(f *testing.F) {
	type seed struct {
		method, path, site, header, cookie, body string
		allowSameSite, originOnly                bool
	}
	seeds := []seed{
		{http.MethodPost, "/transfer", "cross-site", "", "", "", false, false},
		{http.MethodPost, "/transfer", "cross-site", "", "", "", false, true},
		{http.MethodPost, "/transfer", "same-origin", "", "", "", false, false},
		{http.MethodPost, "/transfer", "same-site", "", "", "", false, false},
		{http.MethodPost, "/transfer", "same-site", "", "", "", true, false},
		{http.MethodPost, "/transfer", "", "", "", "", false, false},
		{http.MethodGet, "/form", "cross-site", "", "", "", false, false},
		{http.MethodGet, "/form", "cross-site", "", "", "", false, true},
		{http.MethodPost, "/transfer", "cross-site", "tok-abc", "tok-abc", "", false, false},
		{http.MethodPost, "/transfer", "cross-site", "tok-abc", "tok-xyz", "", false, false},
		{http.MethodPost, "/transfer", "cross-site", "", "tok-abc", "_csrf_token=tok-abc", false, false},
		{http.MethodPost, "/transfer", "cross-site", "", "tok-abc", "_csrf_token=tok-xyz", false, false},
		{http.MethodDelete, "/users/1", "cross-site", "", "tok-abc", "_csrf_token=tok-abc", false, false},
		{http.MethodPost, "/public/webhook", "cross-site", "", "", "", false, false},
		{http.MethodPost, "/publicity", "cross-site", "", "", "", false, false},
		{http.MethodPost, "/pub%6cic/webhook", "cross-site", "", "", "", false, false},
		{http.MethodPost, "/transfer?_csrf_token=tok-abc", "cross-site", "", "tok-abc", "", false, false},
		// Ciphertext shapes for the decrypt half: empty, not base64, valid
		// base64 shorter than the 12-byte GCM nonce, nonce-length noise.
		{http.MethodPost, "/transfer", "cross-site", "", "!!!not base64!!!", "", false, false},
		{http.MethodPost, "/transfer", "cross-site", "", base64.URLEncoding.EncodeToString([]byte("short")), "", false, false},
		{http.MethodPost, "/transfer", "cross-site", "", base64.URLEncoding.EncodeToString([]byte("012345678901234567890123456789")), "", false, false},
	}
	for _, s := range seeds {
		f.Add(s.method, s.path, s.site, s.header, s.cookie, s.body, s.allowSameSite, s.originOnly)
	}

	f.Fuzz(func(t *testing.T, method, path, site, headerToken, cookieToken, body string, allowSameSite, originOnly bool) {
		if _, err := http.NewRequest(method, "http://nucleus.test/", nil); err != nil {
			return // not a method a client can put on the wire
		}
		if !strings.HasPrefix(path, "/") {
			return
		}
		build := func() *http.Request {
			r, err := http.NewRequest(method, "http://nucleus.test"+path, strings.NewReader(body))
			if err != nil {
				return nil
			}
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.RemoteAddr = "192.0.2.10:4321"
			if site != "" {
				r.Header.Set("Sec-Fetch-Site", site)
			}
			if headerToken != "" {
				r.Header.Set("X-CSRF-Token", headerToken)
			}
			if cookieToken != "" {
				r.Header.Set("Cookie", "_csrf="+cookieToken)
			}
			return r
		}
		req := build()
		if req == nil {
			return
		}

		opts := CSRFOptions{
			ExemptPaths:       []string{fuzzCSRFExemptPrefix},
			EnableOriginCheck: true,
			AllowSameSite:     allowSameSite,
			OriginOnly:        originOnly,
			InsecureCookie:    true,
			Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
		mw, err := NewCSRFMiddleware(opts)
		if err != nil {
			t.Fatalf("NewCSRFMiddleware: %v", err)
		}
		rec := httptest.NewRecorder()
		mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(fuzzReachedHeader, "yes")
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(rec, req)
		reached := rec.Header().Get(fuzzReachedHeader) == "yes"

		// The server-side token: the cookie the client sent when the stdlib
		// can read it, otherwise the one the middleware generated and handed
		// back in Set-Cookie.
		twin := build()
		serverToken := getCookieToken(twin, "_csrf")
		if serverToken == "" {
			for _, c := range rec.Result().Cookies() {
				if c.Name == "_csrf" {
					serverToken = c.Value
				}
			}
		}
		// The submitted token, resolved the way the middleware resolves it:
		// the header first, then the form field — through the same
		// http.Request.FormValue, so query/body precedence and the methods
		// whose body is parsed at all are the stdlib's answer, not a guess.
		submitted := headerToken
		if submitted == "" {
			submitted = twin.FormValue("_csrf_token")
		}

		want := true
		switch {
		case strings.HasPrefix(req.URL.Path, fuzzCSRFExemptPrefix):
			// Exempt: no CSRF at all, by configuration.
		case site == "same-origin", allowSameSite && site == "same-site":
			// Layer 1 shortcut. A non-browser client can forge the header;
			// that is a documented property of the Sec-Fetch-Site design,
			// frozen by TestCSRFOriginCheckForgedSameOriginBypassesToken.
		case originOnly:
			want = false // origin-only: no token fallback
		case req.Method == http.MethodGet, req.Method == http.MethodHead, req.Method == http.MethodOptions:
			// Safe methods carry no state change.
		default:
			want = submitted != "" && submitted == serverToken
		}

		if reached != want {
			t.Fatalf("%s %s (site=%q, allowSameSite=%v, originOnly=%v, header=%q, cookie=%q, body=%q): handler reached=%v, want %v (status %d, submitted=%q, server=%q)",
				req.Method, path, site, allowSameSite, originOnly, headerToken, cookieToken, body,
				reached, want, rec.Code, submitted, serverToken)
		}

		// Round trip: a token this package issues survives the XSRF cookie.
		sealed, err := encryptToken(headerToken, fuzzCSRFKey)
		if err != nil {
			t.Fatalf("encryptToken(%q): %v", headerToken, err)
		}
		opened, err := decryptToken(sealed, fuzzCSRFKey)
		if err != nil {
			t.Fatalf("decryptToken of our own ciphertext for %q: %v", headerToken, err)
		}
		if opened != headerToken {
			t.Fatalf("XSRF token round trip: got %q, want %q", opened, headerToken)
		}
		// And a ciphertext the fuzzer made up must be REJECTED, not silently
		// opened: forging AES-GCM without the key is infeasible, so a nil
		// error here means the tag is not being checked — the shape of the
		// short-ciphertext bug that returned ("", nil).
		if got, err := decryptToken(cookieToken, fuzzCSRFKey); err == nil {
			t.Fatalf("decryptToken accepted a ciphertext it never sealed: %q -> %q", cookieToken, got)
		}
	})
}
