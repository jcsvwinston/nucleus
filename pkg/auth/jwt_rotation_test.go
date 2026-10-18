package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func mustGenRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	// 2048 bits balances test-runtime vs. realism; production deployments
	// would use 2048+ as well.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return key
}

// ---------- Multi-key construction ----------

func TestJWT_NewFromKeys_RequiresAtLeastOneKey(t *testing.T) {
	_, err := NewJWTManagerFromKeys(nil, "", time.Hour)
	if err == nil {
		t.Fatal("expected error for empty keys")
	}
}

func TestJWT_NewFromKeys_CurrentKIDMustBePresent(t *testing.T) {
	_, err := NewJWTManagerFromKeys([]SigningKey{
		{KID: "a", Algorithm: HS256, HMACSecret: []byte("aaaa")},
	}, "missing", time.Hour)
	if err == nil {
		t.Fatal("expected error when currentKID is not in the keyset")
	}
}

func TestJWT_NewFromKeys_DuplicateKidRejected(t *testing.T) {
	_, err := NewJWTManagerFromKeys([]SigningKey{
		{KID: "a", Algorithm: HS256, HMACSecret: []byte("aaaa")},
		{KID: "a", Algorithm: HS256, HMACSecret: []byte("bbbb")},
	}, "a", time.Hour)
	if err == nil {
		t.Fatal("expected error for duplicate kid")
	}
}

func TestJWT_SigningKey_ValidateRejectsAlgMaterialMismatch(t *testing.T) {
	// HS256 without HMACSecret.
	if (&SigningKey{KID: "a", Algorithm: HS256}).validate() == nil {
		t.Fatal("HS256 without HMACSecret should be rejected")
	}
	// RS256 without RSAPrivate.
	if (&SigningKey{KID: "a", Algorithm: RS256}).validate() == nil {
		t.Fatal("RS256 without RSAPrivate should be rejected")
	}
	// Empty KID.
	if (&SigningKey{KID: "", Algorithm: HS256, HMACSecret: []byte("x")}).validate() == nil {
		t.Fatal("empty KID should be rejected")
	}
	// Unsupported algorithm.
	if (&SigningKey{KID: "a", Algorithm: "ES512"}).validate() == nil {
		t.Fatal("unsupported algorithm should be rejected")
	}
}

// ---------- Generate stamps kid ----------

func TestJWT_Rotation_GenerateStampsKidIntoHeader(t *testing.T) {
	mgr, err := NewJWTManagerFromKeys([]SigningKey{
		{KID: "2026-05-13", Algorithm: HS256, HMACSecret: []byte("shared-secret-for-rotation-test")},
	}, "2026-05-13", time.Hour)
	if err != nil {
		t.Fatalf("NewJWTManagerFromKeys: %v", err)
	}

	tok, err := mgr.Generate("user-1", "alice", "admin")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Parse without verification to inspect the header.
	parsed, _, err := jwt.NewParser().ParseUnverified(tok, &Claims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	if kid, _ := parsed.Header["kid"].(string); kid != "2026-05-13" {
		t.Fatalf("expected kid 2026-05-13 in header, got %q", kid)
	}
}

func TestJWT_Legacy_GenerateOmitsKid(t *testing.T) {
	mgr := mustJWT(t, testSecret, time.Hour)
	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(tok, &Claims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	if kid, ok := parsed.Header["kid"]; ok {
		t.Fatalf("legacy single-secret manager must not stamp kid; got %v", kid)
	}
}

// ---------- Validate routes by kid ----------

func TestJWT_Rotation_ValidateLooksUpByKid(t *testing.T) {
	mgr, err := NewJWTManagerFromKeys([]SigningKey{
		{KID: "k1", Algorithm: HS256, HMACSecret: []byte("secret-for-k1-key-aaaaaaaaaaaa")},
		{KID: "k2", Algorithm: HS256, HMACSecret: []byte("secret-for-k2-key-bbbbbbbbbbbb")},
	}, "k1", time.Hour)
	if err != nil {
		t.Fatalf("NewJWTManagerFromKeys: %v", err)
	}

	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate on a current-kid token failed: %v", err)
	}
}

func TestJWT_Rotation_UnknownKidRejected(t *testing.T) {
	// Sign a token with key "rogue" using a manager that doesn't know it.
	otherMgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "rogue", Algorithm: HS256, HMACSecret: []byte("rogue-secret-aaaaaaaaaaaaaaaa")},
	}, "rogue", time.Hour)
	rogueTok, err := otherMgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("rogue Generate: %v", err)
	}

	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "known", Algorithm: HS256, HMACSecret: []byte("known-secret-aaaaaaaaaaaaaaaa")},
	}, "known", time.Hour)
	if _, err := mgr.Validate(rogueTok); err == nil {
		t.Fatal("rogue-kid token should be rejected")
	}
}

func TestJWT_Rotation_TokenWithoutKidRejectedInMultiKeyMode(t *testing.T) {
	// Hand-craft a token with no kid but signed with a key the manager
	// happens to know: still rejected because multi-key mode mandates kid.
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "k1", Algorithm: HS256, HMACSecret: []byte("known-secret-aaaaaaaaaaaaaaaa")},
	}, "k1", time.Hour)

	claims := Claims{UserID: "u", RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims) // no kid
	signed, err := tok.SignedString([]byte("known-secret-aaaaaaaaaaaaaaaa"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := mgr.Validate(signed); err == nil {
		t.Fatal("multi-key mode must reject tokens without kid")
	}
}

// ---------- Rotation flow ----------

func TestJWT_Rotation_AddNewKeyKeepsOldTokensValid(t *testing.T) {
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "v1", Algorithm: HS256, HMACSecret: []byte("v1-secret-aaaaaaaaaaaaaaaaaaaa")},
	}, "v1", time.Hour)

	oldTok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate v1: %v", err)
	}

	// Operator promotes v2.
	if err := mgr.RotateKey(SigningKey{KID: "v2", Algorithm: HS256, HMACSecret: []byte("v2-secret-aaaaaaaaaaaaaaaaaaaa")}, true); err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if mgr.CurrentKID() != "v2" {
		t.Fatalf("expected current kid v2, got %q", mgr.CurrentKID())
	}

	// New tokens are signed with v2.
	newTok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate v2: %v", err)
	}
	parsed, _, _ := jwt.NewParser().ParseUnverified(newTok, &Claims{})
	if parsed.Header["kid"] != "v2" {
		t.Fatalf("new token should be signed with v2, got kid %v", parsed.Header["kid"])
	}

	// Old tokens still validate — this is the whole point of rotation.
	if _, err := mgr.Validate(oldTok); err != nil {
		t.Fatalf("v1 token must still validate during the grace window: %v", err)
	}
}

func TestJWT_Rotation_RemoveOldKeyInvalidatesItsTokens(t *testing.T) {
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "v1", Algorithm: HS256, HMACSecret: []byte("v1-secret-aaaaaaaaaaaaaaaaaaaa")},
		{KID: "v2", Algorithm: HS256, HMACSecret: []byte("v2-secret-aaaaaaaaaaaaaaaaaaaa")},
	}, "v2", time.Hour)

	// Generate a v1-stamped token via an intermediate manager that has v1 as current.
	mgrV1Cur, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "v1", Algorithm: HS256, HMACSecret: []byte("v1-secret-aaaaaaaaaaaaaaaaaaaa")},
	}, "v1", time.Hour)
	v1Tok, _ := mgrV1Cur.Generate("u", "n", "r")

	// Until removal, v1 still validates on the multi-key mgr.
	if _, err := mgr.Validate(v1Tok); err != nil {
		t.Fatalf("expected v1 token to validate before removal: %v", err)
	}

	// Operator drops v1 after grace.
	if err := mgr.RemoveKey("v1"); err != nil {
		t.Fatalf("RemoveKey: %v", err)
	}
	if _, err := mgr.Validate(v1Tok); err == nil {
		t.Fatal("v1 token must be rejected after RemoveKey")
	}
}

func TestJWT_Rotation_CannotRemoveCurrent(t *testing.T) {
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "v1", Algorithm: HS256, HMACSecret: []byte("v1-secret-aaaaaaaaaaaaaaaaaaaa")},
	}, "v1", time.Hour)
	if err := mgr.RemoveKey("v1"); err == nil {
		t.Fatal("removing the current signing key must be rejected")
	}
}

func TestJWT_Rotation_DuplicateRotateKidRejected(t *testing.T) {
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "v1", Algorithm: HS256, HMACSecret: []byte("v1-secret-aaaaaaaaaaaaaaaaaaaa")},
	}, "v1", time.Hour)
	err := mgr.RotateKey(SigningKey{KID: "v1", Algorithm: HS256, HMACSecret: []byte("dup-aaaaaaaaaaaaaaaaaaaaaaaaaaaa")}, false)
	if err == nil {
		t.Fatal("rotating a kid that already exists must be rejected")
	}
}

// ---------- RS256 ----------

func TestJWT_RS256_SignAndValidate(t *testing.T) {
	priv := mustGenRSAKey(t)
	mgr, err := NewJWTManagerFromKeys([]SigningKey{
		{KID: "rsa-2026", Algorithm: RS256, RSAPrivate: priv},
	}, "rsa-2026", time.Hour)
	if err != nil {
		t.Fatalf("NewJWTManagerFromKeys: %v", err)
	}

	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate RS256: %v", err)
	}
	parsed, _, _ := jwt.NewParser().ParseUnverified(tok, &Claims{})
	if alg := parsed.Method.Alg(); alg != "RS256" {
		t.Fatalf("expected alg RS256, got %s", alg)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate RS256: %v", err)
	}
}

func TestJWT_Rotation_AlgMismatchRejected(t *testing.T) {
	// Manager knows kid "k1" as HS256 with secret S. Hand-craft a token that
	// claims kid=k1 but is signed RS256 — must be rejected.
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "k1", Algorithm: HS256, HMACSecret: []byte("secret-aaaaaaaaaaaaaaaaaaaaaa")},
	}, "k1", time.Hour)

	priv := mustGenRSAKey(t)
	rsaTok := jwt.NewWithClaims(jwt.SigningMethodRS256, Claims{
		UserID: "u",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	rsaTok.Header["kid"] = "k1"
	signed, err := rsaTok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign rogue RS256: %v", err)
	}

	if _, err := mgr.Validate(signed); err == nil {
		t.Fatal("kid claims HS256 but token is RS256 — must be rejected")
	}
}

// ---------- JWKS ----------

func TestJWT_JWKS_HMACKeysExcluded(t *testing.T) {
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "hmac-only", Algorithm: HS256, HMACSecret: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}, "hmac-only", time.Hour)

	set := mgr.JWKS()
	if len(set.Keys) != 0 {
		t.Fatalf("JWKS must exclude HMAC keys to avoid leaking secrets; got %d entries", len(set.Keys))
	}
}

func TestJWT_JWKS_RSAKeyShape(t *testing.T) {
	priv := mustGenRSAKey(t)
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "rsa-1", Algorithm: RS256, RSAPrivate: priv},
		{KID: "hmac-1", Algorithm: HS256, HMACSecret: []byte("hmac-secret-aaaaaaaaaaaaaaaa")},
	}, "rsa-1", time.Hour)

	set := mgr.JWKS()
	if len(set.Keys) != 1 {
		t.Fatalf("expected exactly 1 JWK entry (only the RSA key), got %d", len(set.Keys))
	}
	jwk := set.Keys[0]
	if jwk.Kid != "rsa-1" {
		t.Fatalf("kid mismatch: %s", jwk.Kid)
	}
	if jwk.Kty != "RSA" || jwk.Alg != "RS256" || jwk.Use != "sig" {
		t.Fatalf("unexpected kty/alg/use: %+v", jwk)
	}
	if jwk.N == "" || jwk.E == "" {
		t.Fatalf("missing n or e: %+v", jwk)
	}

	// n must decode to the same modulus bytes as the original public key.
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		t.Fatalf("decode n: %v", err)
	}
	if string(nBytes) != string(priv.PublicKey.N.Bytes()) {
		t.Fatal("decoded n does not match original modulus")
	}
}

func TestJWT_JWKSHandler_ServesContentTypeAndShape(t *testing.T) {
	priv := mustGenRSAKey(t)
	mgr, _ := NewJWTManagerFromKeys([]SigningKey{
		{KID: "rsa-1", Algorithm: RS256, RSAPrivate: priv},
	}, "rsa-1", time.Hour)

	srv := httptest.NewServer(mgr.JWKSHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/.well-known/jwks.json")
	if err != nil {
		t.Fatalf("GET jwks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/jwk-set+json") {
		t.Fatalf("expected application/jwk-set+json content type, got %q", ct)
	}

	var got JWKSet
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode JWKS: %v", err)
	}
	if len(got.Keys) != 1 {
		t.Fatalf("expected 1 key in JWKS, got %d", len(got.Keys))
	}
	if got.Keys[0].Kid != "rsa-1" {
		t.Fatalf("kid mismatch: %s", got.Keys[0].Kid)
	}
}

func TestJWT_Generate_NoKeysAndNoLegacy_Fails(t *testing.T) {
	// Pathological case — manager constructed bypassing the public
	// constructors via zero-value. Generate should fail loudly.
	mgr := &JWTManager{keys: map[string]*SigningKey{}, expiry: time.Hour, issuer: "test"}
	if _, err := mgr.Generate("u", "n", "r"); err == nil {
		t.Fatal("Generate with no keys configured must fail")
	}
}
