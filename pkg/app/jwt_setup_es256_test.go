package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustECP256(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey P-256: %v", err)
	}
	return key
}

// mustEncodeECPKCS8 encodes a P-256 key as a PKCS#8 `PRIVATE KEY` PEM —
// the modern default from `openssl genpkey`.
func mustEncodeECPKCS8(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// mustEncodeECSEC1 encodes a P-256 key as a SEC1 `EC PRIVATE KEY` PEM —
// what `openssl ecparam -genkey` emits.
func mustEncodeECSEC1(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

func TestBuildJWTManager_ES256FromPemPath_PKCS8(t *testing.T) {
	priv := mustECP256(t)
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "ec.pem")
	if err := os.WriteFile(pemPath, mustEncodeECPKCS8(t, priv), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}

	cfg := &Config{
		JWTExpiry: time.Hour,
		JWTKeys: []JWTKeySpec{
			{KID: "ec-1", Algorithm: "ES256", PemPath: pemPath},
		},
		JWTCurrentKID: "ec-1",
	}
	mgr, err := buildJWTManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildJWTManager: %v", err)
	}
	if !hasAsymmetricKey(mgr) {
		t.Fatal("ES256 manager should report at least one asymmetric key")
	}

	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestBuildJWTManager_ES256FromPemEnv_SEC1(t *testing.T) {
	priv := mustECP256(t)
	t.Setenv("NUCLEUS_JWT_EC_PEM", string(mustEncodeECSEC1(t, priv)))

	cfg := &Config{
		JWTExpiry: time.Hour,
		JWTKeys: []JWTKeySpec{
			{KID: "ec-sec1", Algorithm: "ES256", PemEnv: "NUCLEUS_JWT_EC_PEM"},
		},
		JWTCurrentKID: "ec-sec1",
	}
	mgr, err := buildJWTManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildJWTManager: %v", err)
	}
	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestParseECDSAPrivateKey_RejectsNonP256(t *testing.T) {
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(p384)
	if err != nil {
		t.Fatalf("marshal P-384: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	if _, err := parseECDSAPrivateKey(pemBytes); err == nil {
		t.Fatal("parseECDSAPrivateKey must reject a P-384 key — only P-256 is supported")
	}
}

func TestParseECDSAPrivateKey_RejectsTrailingPEMContent(t *testing.T) {
	priv := mustECP256(t)
	combined := append(mustEncodeECPKCS8(t, priv), []byte("-----BEGIN CERTIFICATE-----\nMIIB...stub...\n-----END CERTIFICATE-----\n")...)
	if _, err := parseECDSAPrivateKey(combined); err == nil {
		t.Fatal("expected error for EC PEM with trailing content")
	}
}

func TestParseECDSAPrivateKey_RejectsRSAKeyMaterial(t *testing.T) {
	// An RSA PKCS#8 PEM handed to the ES256 path must be rejected with a
	// type-mismatch error, not silently accepted.
	rsaKey := mustRSA(t)
	pemBytes := mustEncodePKCS8(t, rsaKey)
	if _, err := parseECDSAPrivateKey(pemBytes); err == nil {
		t.Fatal("parseECDSAPrivateKey must reject RSA key material")
	}
}

func TestAppNew_JWT_MountsJWKSWhenES256KeyConfigured(t *testing.T) {
	priv := mustECP256(t)
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "ec-2026.pem")
	if err := os.WriteFile(pemPath, mustEncodeECPKCS8(t, priv), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}

	cfg := testAppConfig()
	cfg.JWTExpiry = time.Hour
	cfg.JWTKeys = []JWTKeySpec{
		{KID: "ec-2026", Algorithm: "ES256", PemPath: pemPath},
	}
	cfg.JWTCurrentKID = "ec-2026"

	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	if a.JWT == nil || a.JWT.CurrentKID() != "ec-2026" {
		t.Fatalf("unexpected JWT manager state: %+v", a.JWT)
	}

	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /.well-known/jwks.json, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"kid":"ec-2026"`, `"kty":"EC"`, `"crv":"P-256"`, `"alg":"ES256"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("JWKS body missing %q: %s", want, body)
		}
	}
}
