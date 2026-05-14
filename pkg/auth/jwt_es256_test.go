package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func mustGenECKeyP256(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey P-256: %v", err)
	}
	return key
}

func TestJWT_ES256_SignAndValidate(t *testing.T) {
	priv := mustGenECKeyP256(t)
	mgr, err := NewJWTManagerFromKeys([]SigningKey{
		{KID: "ec-2026", Algorithm: ES256, ECDSAPrivate: priv},
	}, "ec-2026", time.Hour)
	if err != nil {
		t.Fatalf("NewJWTManagerFromKeys: %v", err)
	}

	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate ES256: %v", err)
	}
	parsed, _, _ := jwt.NewParser().ParseUnverified(tok, &Claims{})
	if alg := parsed.Method.Alg(); alg != "ES256" {
		t.Fatalf("expected alg ES256, got %s", alg)
	}
	if kid, _ := parsed.Header["kid"].(string); kid != "ec-2026" {
		t.Fatalf("expected kid ec-2026 in header, got %q", kid)
	}

	claims, err := mgr.Validate(tok)
	if err != nil {
		t.Fatalf("Validate ES256: %v", err)
	}
	if claims.UserID != "u" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestJWT_ES256_ValidateRejectsWrongKey(t *testing.T) {
	priv := mustGenECKeyP256(t)
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "ec-1", Algorithm: ES256, ECDSAPrivate: priv},
	}, "ec-1", time.Hour)

	// A token signed by a different EC key but claiming the known kid
	// must fail signature verification.
	rogue := mustGenECKeyP256(t)
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, Claims{
		UserID: "u",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	tok.Header["kid"] = "ec-1"
	signed, err := tok.SignedString(rogue)
	if err != nil {
		t.Fatalf("sign rogue ES256: %v", err)
	}

	if _, err := mgr.Validate(signed); err == nil {
		t.Fatal("token signed by a foreign EC key must be rejected")
	}
}

func TestJWT_ES256_AlgMismatchRejected(t *testing.T) {
	// Manager knows kid "k1" as ES256. A token claiming kid=k1 but signed
	// HS256 must be rejected by the alg-vs-kid check in Validate.
	priv := mustGenECKeyP256(t)
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "k1", Algorithm: ES256, ECDSAPrivate: priv},
	}, "k1", time.Hour)

	hsTok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: "u",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	hsTok.Header["kid"] = "k1"
	signed, err := hsTok.SignedString([]byte("some-hmac-secret-aaaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("sign rogue HS256: %v", err)
	}

	if _, err := mgr.Validate(signed); err == nil {
		t.Fatal("kid claims ES256 but token is HS256 — must be rejected")
	}
}

func TestJWT_SigningKey_ES256ValidateRejectsBadMaterial(t *testing.T) {
	// ES256 without ECDSAPrivate.
	if (&SigningKey{KID: "a", Algorithm: ES256}).validate() == nil {
		t.Fatal("ES256 without ECDSAPrivate should be rejected")
	}

	// ES256 with a non-P-256 curve.
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384: %v", err)
	}
	if (&SigningKey{KID: "a", Algorithm: ES256, ECDSAPrivate: p384}).validate() == nil {
		t.Fatal("ES256 with a P-384 key should be rejected — only P-256 is supported")
	}

	// Valid P-256 key passes.
	if err := (&SigningKey{KID: "a", Algorithm: ES256, ECDSAPrivate: mustGenECKeyP256(t)}).validate(); err != nil {
		t.Fatalf("valid ES256 P-256 key rejected: %v", err)
	}
}

func TestJWT_JWKS_ECKeyShape(t *testing.T) {
	priv := mustGenECKeyP256(t)
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "ec-1", Algorithm: ES256, ECDSAPrivate: priv},
		{KID: "hmac-1", Algorithm: HS256, HMACSecret: []byte("hmac-secret-aaaaaaaaaaaaaaaa")},
	}, "ec-1", time.Hour)

	set := mgr.JWKS()
	if len(set.Keys) != 1 {
		t.Fatalf("expected exactly 1 JWK entry (only the EC key), got %d", len(set.Keys))
	}
	jwk := set.Keys[0]
	if jwk.Kid != "ec-1" {
		t.Fatalf("kid mismatch: %s", jwk.Kid)
	}
	if jwk.Kty != "EC" || jwk.Alg != "ES256" || jwk.Use != "sig" {
		t.Fatalf("unexpected kty/alg/use: %+v", jwk)
	}
	if jwk.Crv != "P-256" {
		t.Fatalf("expected crv P-256, got %q", jwk.Crv)
	}
	if jwk.X == "" || jwk.Y == "" {
		t.Fatalf("missing x or y: %+v", jwk)
	}
	// An EC JWK must NOT carry RSA fields.
	if jwk.N != "" || jwk.E != "" {
		t.Fatalf("EC JWK leaked RSA fields: %+v", jwk)
	}

	// x and y must decode to exactly 32 bytes (P-256 field size) and
	// round-trip to the original public coordinates.
	xBytes, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		t.Fatalf("decode x: %v", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		t.Fatalf("decode y: %v", err)
	}
	if len(xBytes) != 32 || len(yBytes) != 32 {
		t.Fatalf("P-256 coordinates must be 32 bytes each, got x=%d y=%d", len(xBytes), len(yBytes))
	}
	if new(big.Int).SetBytes(xBytes).Cmp(priv.PublicKey.X) != 0 {
		t.Fatal("x coordinate does not round-trip to the public key")
	}
	if new(big.Int).SetBytes(yBytes).Cmp(priv.PublicKey.Y) != 0 {
		t.Fatal("y coordinate does not round-trip to the public key")
	}
}

// TestJWT_JWKS_LeftPadsShortCoordinates exercises the leftPad path: an
// EC public key whose X coordinate has a leading zero byte must still
// serialise to a full 32-byte JWK field, not a truncated one.
func TestJWT_JWKS_LeftPadsShortCoordinates(t *testing.T) {
	// Generate keys until X has a high bit clear in its top byte often
	// enough that big.Int.Bytes() returns < 32 bytes. Rather than loop,
	// just assert the invariant on a freshly generated key: leftPad must
	// always produce exactly the field size.
	for i := 0; i < 16; i++ {
		priv := mustGenECKeyP256(t)
		mgr, _ := NewJWTManagerFromKeys([]SigningKey{
			{KID: "ec", Algorithm: ES256, ECDSAPrivate: priv},
		}, "ec", time.Hour)
		jwk := mgr.JWKS().Keys[0]
		xBytes, _ := base64.RawURLEncoding.DecodeString(jwk.X)
		yBytes, _ := base64.RawURLEncoding.DecodeString(jwk.Y)
		if len(xBytes) != 32 || len(yBytes) != 32 {
			t.Fatalf("iteration %d: coordinates not padded to 32 bytes: x=%d y=%d", i, len(xBytes), len(yBytes))
		}
	}
}
