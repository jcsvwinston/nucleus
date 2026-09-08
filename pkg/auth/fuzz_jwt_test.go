package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"hash"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fuzzJWTSecret is the HS256 secret the target validates against — 34 bytes,
// over the 32-byte floor NewJWTManager panics below.
const fuzzJWTSecret = "fuzz-secret-key-at-least-32-chars!"

const fuzzJWTIssuer = "nucleus-fuzz"

// FuzzJWTValidate fuzzes the bearer token. This is the most exposed decoding
// surface in the framework: JWTManager.Middleware hands whatever follows
// "Authorization: Bearer " straight to Validate, so every byte here comes
// from an unauthenticated caller, and what Validate returns becomes the
// request's subject and role for the authorization layer.
//
// Two properties, neither of them "does not panic":
//
//  1. Acceptance implies a signature this deployment can produce. When
//     Validate returns claims, the target recomputes the HMAC over the
//     token's own header and payload with the configured secret and requires
//     it to match the token's signature segment. The check is independent of
//     the library: it fails for an "alg":"none" token, for a stripped or
//     truncated signature, for a token signed with another key, and for the
//     RS256/HS256 confusion where a public key is used as an HMAC secret.
//     The issuer and audience the manager pins are checked the same way —
//     a token minted by another deployment that shares the key must not pass
//     (NU-29).
//  2. Round trip. A token this manager generates validates back to the same
//     three claim strings. The check is skipped for input that is not valid
//     UTF-8, where the round trip is JSON's documented replacement of bad
//     bytes with U+FFFD and not a defect in this package.
//
// Seeds: the tokens the unit tests use (a valid one, one signed with another
// secret, "not.a.valid.token"), plus the forgery shapes that belong in any
// JWT corpus — alg=none with and without a trailing dot, an empty signature,
// a header claiming HS256 over an unsigned payload, and the empty string. The
// forgery shapes are committed as named corpus files in
// testdata/fuzz/FuzzJWTValidate; the ones built from a freshly minted token
// stay in f.Add, because a token with an expiry cannot be frozen in a file.
func FuzzJWTValidate(f *testing.F) {
	mgr := NewJWTManager(fuzzJWTSecret, time.Hour, fuzzJWTIssuer)

	valid, err := mgr.Generate("user-1", "alice", "admin")
	if err != nil {
		f.Fatalf("Generate: %v", err)
	}
	other := NewJWTManager("another-secret-key-at-least-32ch!", time.Hour, fuzzJWTIssuer)
	foreign, err := other.Generate("user-1", "alice", "admin")
	if err != nil {
		f.Fatalf("Generate (foreign): %v", err)
	}
	// An issuer this manager does not pin, signed with the key it does.
	elsewhere := NewJWTManager(fuzzJWTSecret, time.Hour, "someone-else")
	crossIssuer, err := elsewhere.Generate("user-1", "alice", "admin")
	if err != nil {
		f.Fatalf("Generate (cross-issuer): %v", err)
	}
	parts := strings.Split(valid, ".")

	seeds := []string{
		valid,
		foreign,
		crossIssuer,
		"not.a.valid.token",
		"",
		".",
		"..",
		parts[0] + "." + parts[1] + ".", // signature stripped
		parts[0] + "." + parts[1] + "." + parts[2][:10], // signature truncated
		// alg=none over the same payload, with and without a trailing dot.
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + ".",
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1],
		// A header that claims HS256 with no signature to back it.
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + parts[1] + ".",
		"Bearer " + valid, // the prefix the middleware is supposed to strip
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, token string) {
		claims, err := mgr.Validate(token)
		if err != nil {
			if claims != nil {
				t.Fatalf("Validate returned claims alongside an error: %+v (%v)", claims, err)
			}
		} else {
			if claims == nil {
				t.Fatal("Validate returned no claims and no error")
			}
			segments := strings.Split(token, ".")
			if len(segments) != 3 {
				t.Fatalf("accepted a token that is not a three-segment JWS: %q", token)
			}
			if !fuzzHMACVerifies(segments, fuzzJWTSecret) {
				t.Fatalf("accepted a token whose signature is not an HMAC of its own header and payload under the configured secret: %q", token)
			}
			if claims.Issuer != fuzzJWTIssuer {
				t.Fatalf("accepted a token issued by %q; the manager pins %q", claims.Issuer, fuzzJWTIssuer)
			}
		}

		// Round trip: what this manager mints, it reads back unchanged.
		if !utf8.ValidString(token) {
			return // encoding/json replaces invalid UTF-8; not this package's contract
		}
		minted, err := mgr.Generate(token, token, token)
		if err != nil {
			t.Fatalf("Generate(%q): %v", token, err)
		}
		back, err := mgr.Validate(minted)
		if err != nil {
			t.Fatalf("Validate rejected a token this manager just generated for %q: %v", token, err)
		}
		if back.UserID != token || back.Username != token || back.Role != token {
			t.Fatalf("claims round trip: got (%q, %q, %q), want %q in all three", back.UserID, back.Username, back.Role, token)
		}
	})
}

// fuzzHMACVerifies recomputes the JWS signature over "header.payload" for
// each HMAC-SHA variant the legacy path accepts (the keyfunc admits any
// *jwt.SigningMethodHMAC, not only HS256) and reports whether any of them
// matches the token's third segment.
func fuzzHMACVerifies(segments []string, secret string) bool {
	signing := segments[0] + "." + segments[1]
	for _, newHash := range []func() hash.Hash{sha256.New, sha512.New384, sha512.New} {
		mac := hmac.New(newHash, []byte(secret))
		mac.Write([]byte(signing))
		want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(want), []byte(segments[2])) {
			return true
		}
	}
	return false
}
