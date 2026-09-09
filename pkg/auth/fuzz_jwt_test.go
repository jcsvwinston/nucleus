package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"hash"
	"math/big"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fuzzJWTSecret is the HS256 secret the legacy manager validates against —
// 34 bytes, over the 32-byte floor NewJWTManager panics below.
const fuzzJWTSecret = "fuzz-secret-key-at-least-32-chars!"

const fuzzJWTIssuer = "nucleus-fuzz"

// The rotating manager's keyset: one kid per algorithm the package supports,
// so a token can name a kid that was registered under an algorithm its own
// header does not spell — the confusion the kid path has to refuse.
const (
	fuzzKIDHMAC   = "hmac-2026"
	fuzzKIDRSA    = "rsa-2026"
	fuzzKIDEC     = "ec-2026"
	fuzzKIDSecret = "rotating-secret-key-at-least-32ch"
)

// fuzzVerifyKey is the target's own view of one keyset entry, built from the
// same material the manager holds but never through SigningKey's methods: the
// oracle must not agree with Validate by construction.
type fuzzVerifyKey struct {
	alg    string // the exact "alg" the header has to spell
	hmac   []byte
	rsaPub *rsa.PublicKey
	ecPub  *ecdsa.PublicKey
}

// FuzzJWTValidate fuzzes the bearer token. This is the most exposed decoding
// surface in the framework: JWTManager.Middleware hands whatever follows
// "Authorization: Bearer " straight to Validate, so every byte here comes
// from an unauthenticated caller, and what Validate returns becomes the
// request's subject and role for the authorization layer.
//
// Every input is validated by two managers, because Validate's keyfunc has
// two paths and a single-secret manager never reaches the second one:
//
//   - mgr, built by NewJWTManager: the legacy path, no kid, HMAC only.
//   - rotating, built by NewJWTManagerFromKeys: the kid path, holding an
//     HS256, an RS256 and an ES256 key. A token that carries a kid is looked
//     up in that keyset, its alg is required to be the one the kid was
//     registered under, and an asymmetric entry verifies with a public key.
//
// Three properties, none of them "does not panic":
//
//  1. Legacy acceptance implies a signature this deployment can produce.
//     When mgr.Validate returns claims, the target recomputes the HMAC over
//     the token's own header and payload with the configured secret and
//     requires it to match the signature segment. It fails for "alg":"none",
//     for a stripped or truncated signature, and for a token signed with
//     another key — independently of what the JWT library decides.
//  2. Multi-key acceptance implies the header names a key in the keyset,
//     spells that key's algorithm, and carries a signature that verifies
//     under that key's material — recomputed here with hmac.Equal,
//     rsa.VerifyPKCS1v15 and ecdsa.Verify. So an unknown kid, a kid whose
//     registered algorithm disagrees with the header's alg (in both
//     directions), and the RS256/HS256 confusion — "alg":"HS256" under the
//     kid registered for RS256, signed with an HMAC over that key's public
//     half in PEM and in DER — all fail the property if they are ever
//     accepted. The pinned issuer is required on both paths: a token minted
//     by another deployment sharing the signing material must not pass
//     (NU-29).
//  3. Round trip. What either manager generates, that manager validates back
//     to the same three claim strings. Skipped for input that is not valid
//     UTF-8, where the round trip is JSON's documented replacement of bad
//     bytes with U+FFFD and not a defect in this package.
//
// Property 2 was checked against the code it guards rather than assumed to
// bite: deleting the kid-vs-alg comparison from Validate's keyfunc makes an
// HS384 token signed with the HS256 kid's own secret pass (jwt/v5 verifies
// any HMAC variant with the same []byte key), and letting an unknown kid
// fall back to the current key makes a token with kid "no-such-kid" pass.
// Both are caught here — the first by a committed corpus file, the second by
// another.
//
// Seeds: the tokens the unit tests use (a valid one, one signed with another
// secret, one from another issuer, "not.a.valid.token"), the forgery shapes
// that belong in any JWT corpus (alg=none with and without a trailing dot,
// an empty signature, a header claiming HS256 over an unsigned payload), and
// the kid-path shapes above. The ones that do not depend on a freshly minted
// token — including a header naming the RS256 kid while spelling HS256 — are
// committed as named corpus files in testdata/fuzz/FuzzJWTValidate; the rest
// stay in f.Add, because a token with an expiry and a per-run key cannot be
// frozen in a file.
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

	// The multi-key manager, and the two extra signers that mint under its
	// asymmetric kids so the seeds cover every entry in the keyset.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		f.Fatalf("rsa.GenerateKey: %v", err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	keys := []SigningKey{
		{KID: fuzzKIDHMAC, Algorithm: HS256, HMACSecret: []byte(fuzzKIDSecret)},
		{KID: fuzzKIDRSA, Algorithm: RS256, RSAPrivate: rsaKey},
		{KID: fuzzKIDEC, Algorithm: ES256, ECDSAPrivate: ecKey},
	}
	rotating, err := NewJWTManagerFromKeys(keys, fuzzKIDHMAC, time.Hour, fuzzJWTIssuer)
	if err != nil {
		f.Fatalf("NewJWTManagerFromKeys: %v", err)
	}
	rsaSigner, err := NewJWTManagerFromKeys(keys, fuzzKIDRSA, time.Hour, fuzzJWTIssuer)
	if err != nil {
		f.Fatalf("NewJWTManagerFromKeys (RS256 current): %v", err)
	}
	ecSigner, err := NewJWTManagerFromKeys(keys, fuzzKIDEC, time.Hour, fuzzJWTIssuer)
	if err != nil {
		f.Fatalf("NewJWTManagerFromKeys (ES256 current): %v", err)
	}
	// The oracle's own view of the same keyset, built from the key material
	// directly rather than from SigningKey.signingMethod/verifyMaterial.
	keyset := map[string]fuzzVerifyKey{
		fuzzKIDHMAC: {alg: "HS256", hmac: []byte(fuzzKIDSecret)},
		fuzzKIDRSA:  {alg: "RS256", rsaPub: &rsaKey.PublicKey},
		fuzzKIDEC:   {alg: "ES256", ecPub: &ecKey.PublicKey},
	}

	kidHMAC, err := rotating.Generate("user-1", "alice", "admin")
	if err != nil {
		f.Fatalf("Generate (kid HS256): %v", err)
	}
	kidRSA, err := rsaSigner.Generate("user-1", "alice", "admin")
	if err != nil {
		f.Fatalf("Generate (kid RS256): %v", err)
	}
	kidEC, err := ecSigner.Generate("user-1", "alice", "admin")
	if err != nil {
		f.Fatalf("Generate (kid ES256): %v", err)
	}
	rsaParts := strings.Split(kidRSA, ".")
	pubDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		f.Fatalf("x509.MarshalPKIXPublicKey: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	// "alg":"HS256" under the kid registered for RS256 — the header half of
	// the algorithm confusion. The signature half is an HMAC keyed with the
	// verification key itself, which is what a vulnerable keyfunc would hand
	// to the HMAC verifier.
	confusionHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT","kid":"` + fuzzKIDRSA + `"}`))
	hs384Header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS384","typ":"JWT","kid":"` + fuzzKIDHMAC + `"}`))

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

		// The kid path: one honest token per key in the keyset.
		kidHMAC,
		kidRSA,
		kidEC,
		// RS256/HS256 confusion, in both encodings of the public key.
		fuzzHMACOver(confusionHeader, rsaParts[1], pubPEM, sha256.New),
		fuzzHMACOver(confusionHeader, rsaParts[1], pubDER, sha256.New),
		// The HMAC-family swap: the kid's own secret, the wrong variant.
		// The legacy path accepts any *jwt.SigningMethodHMAC, so HS384 is a
		// legitimate signature there; under a kid it is not, because the kid
		// was registered as HS256. Nothing but the kid-vs-alg check stands
		// between this token and acceptance — jwt/v5 verifies HS384 happily
		// with the same []byte key.
		fuzzHMACOver(hs384Header, strings.Split(kidHMAC, ".")[1], []byte(fuzzKIDSecret), sha512.New384),
		// kid and alg disagreeing, in both directions.
		fuzzReheader(f, kidRSA, func(h map[string]any) { h["kid"] = fuzzKIDHMAC }),
		fuzzReheader(f, kidHMAC, func(h map[string]any) { h["alg"] = "RS256" }),
		fuzzReheader(f, kidEC, func(h map[string]any) { h["alg"] = "HS256" }),
		// A kid nothing was ever registered under, over a real signature.
		fuzzReheader(f, kidHMAC, func(h map[string]any) { h["kid"] = "no-such-kid" }),
		// alg=none wearing a kid the keyset does know.
		fuzzReheader(f, kidHMAC, func(h map[string]any) { h["alg"] = "none" }),
		// A legacy token wearing a kid: valid under mgr's secret, but the
		// kid sends it down the keyset path, where that secret is unknown.
		fuzzReheader(f, valid, func(h map[string]any) { h["kid"] = fuzzKIDHMAC }),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// Hoisted: the managers are fixed, and the round trip runs on every exec.
	roundTrip := []struct {
		name string
		mgr  *JWTManager
	}{
		{"legacy", mgr},
		{"rotating", rotating},
	}

	f.Fuzz(func(t *testing.T, token string) {
		// (1) The legacy single-secret path.
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

		// (2) The kid path: lookup, kid-vs-alg agreement, asymmetric verify.
		rotated, rerr := rotating.Validate(token)
		if rerr != nil {
			if rotated != nil {
				t.Fatalf("rotating: Validate returned claims alongside an error: %+v (%v)", rotated, rerr)
			}
		} else {
			if rotated == nil {
				t.Fatal("rotating: Validate returned no claims and no error")
			}
			segments := strings.Split(token, ".")
			if len(segments) != 3 {
				t.Fatalf("rotating: accepted a token that is not a three-segment JWS: %q", token)
			}
			if !fuzzKeysetVerifies(segments, keyset) {
				t.Fatalf("rotating: accepted a token that does not name a kid in the keyset, spell the algorithm that kid was registered under, and verify under that key: %q", token)
			}
			if rotated.Issuer != fuzzJWTIssuer {
				t.Fatalf("rotating: accepted a token issued by %q; the manager pins %q", rotated.Issuer, fuzzJWTIssuer)
			}
		}

		// (3) Round trip: what each manager mints, it reads back unchanged.
		if !utf8.ValidString(token) {
			return // encoding/json replaces invalid UTF-8; not this package's contract
		}
		for _, rt := range roundTrip {
			minted, err := rt.mgr.Generate(token, token, token)
			if err != nil {
				t.Fatalf("%s: Generate(%q): %v", rt.name, token, err)
			}
			back, err := rt.mgr.Validate(minted)
			if err != nil {
				t.Fatalf("%s: Validate rejected a token this manager just generated for %q: %v", rt.name, token, err)
			}
			if back.UserID != token || back.Username != token || back.Role != token {
				t.Fatalf("%s: claims round trip: got (%q, %q, %q), want %q in all three", rt.name, back.UserID, back.Username, back.Role, token)
			}
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

// fuzzKeysetVerifies is the oracle for the kid path. It reads the token's own
// header, requires the kid to be one the keyset holds and the alg to be
// exactly the one that kid was registered under — which is what makes an
// algorithm-confusion token fail the property — and then verifies the
// signature with the primitive that algorithm names, keyed with the material
// registered for that kid. Nothing here goes through the manager.
func fuzzKeysetVerifies(segments []string, keyset map[string]fuzzVerifyKey) bool {
	raw, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		return false
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return false
	}
	key, ok := keyset[header.Kid]
	if !ok || header.Alg != key.alg {
		return false
	}
	signing := []byte(segments[0] + "." + segments[1])
	sig, err := base64.RawURLEncoding.DecodeString(segments[2])
	if err != nil {
		return false
	}
	switch key.alg {
	case "HS256":
		mac := hmac.New(sha256.New, key.hmac)
		mac.Write(signing)
		return hmac.Equal(mac.Sum(nil), sig)
	case "RS256":
		digest := sha256.Sum256(signing)
		return rsa.VerifyPKCS1v15(key.rsaPub, crypto.SHA256, digest[:], sig) == nil
	case "ES256":
		// RFC 7518 §3.4: the signature is r||s, each the fixed 32-byte
		// big-endian coordinate for P-256, not an ASN.1 sequence.
		if len(sig) != 64 {
			return false
		}
		digest := sha256.Sum256(signing)
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		return ecdsa.Verify(key.ecPub, digest[:], r, s)
	}
	return false
}

// fuzzHMACOver signs "header.payload" with secret under the given HMAC
// variant and returns the whole token. Two forgeries are built with it: the
// algorithm confusion, where the secret is the verification public key in
// whatever encoding a vulnerable keyfunc would hand to the HMAC verifier, and
// the HMAC-family swap, where the secret is right and the variant is not.
func fuzzHMACOver(header, payload string, secret []byte, newHash func() hash.Hash) string {
	mac := hmac.New(newHash, secret)
	mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// fuzzReheader returns token with its header object mutated and re-encoded,
// payload and signature untouched — the shape of a header forgery, where the
// signature is real but describes something the header no longer says.
func fuzzReheader(tb testing.TB, token string, mutate func(map[string]any)) string {
	tb.Helper()
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		tb.Fatalf("fuzzReheader: %q is not a three-segment JWS", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(segments[0])
	if err != nil {
		tb.Fatalf("fuzzReheader: header is not base64url: %v", err)
	}
	var header map[string]any
	if err := json.Unmarshal(raw, &header); err != nil {
		tb.Fatalf("fuzzReheader: header is not JSON: %v", err)
	}
	mutate(header)
	encoded, err := json.Marshal(header)
	if err != nil {
		tb.Fatalf("fuzzReheader: re-encoding the header: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded) + "." + segments[1] + "." + segments[2]
}
