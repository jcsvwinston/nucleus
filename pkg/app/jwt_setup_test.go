package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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

func mustEncodePKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func mustEncodePKCS1(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

func mustRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return key
}

// ---------- buildJWTManager directly ----------

func TestBuildJWTManager_LegacySingleSecretWhenJWTKeysEmpty(t *testing.T) {
	cfg := &Config{JWTSecret: "legacy-secret", JWTExpiry: time.Hour}
	mgr, err := buildJWTManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildJWTManager: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager should not be nil")
	}
	if mgr.CurrentKID() != "" {
		t.Fatalf("legacy manager should have no current kid, got %q", mgr.CurrentKID())
	}

	// Round-trip a token to prove the manager works.
	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestBuildJWTManager_MultiKeyFromHS256EnvVar(t *testing.T) {
	t.Setenv("NUCLEUS_JWT_K1_SECRET", "k1-secret-aaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("NUCLEUS_JWT_K2_SECRET", "k2-secret-bbbbbbbbbbbbbbbbbbbbbb")

	cfg := &Config{
		JWTExpiry: time.Hour,
		JWTKeys: []JWTKeySpec{
			{KID: "k1", Algorithm: "HS256", SecretEnv: "NUCLEUS_JWT_K1_SECRET"},
			{KID: "k2", Algorithm: "HS256", SecretEnv: "NUCLEUS_JWT_K2_SECRET"},
		},
		JWTCurrentKID: "k2",
	}
	mgr, err := buildJWTManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildJWTManager: %v", err)
	}
	if mgr.CurrentKID() != "k2" {
		t.Fatalf("expected current kid k2, got %q", mgr.CurrentKID())
	}

	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestBuildJWTManager_SecretEnvAcceptsEnvPrefix confirms the secrets
// resolver chain handles both a bare env-var name and the explicit
// `env:NAME` form for secret_env / pem_env — the bare form is the
// historical behaviour and must keep working, the prefixed form is the
// new explicit syntax introduced alongside the aws-sm: scheme (ADR-005).
func TestBuildJWTManager_SecretEnvAcceptsEnvPrefix(t *testing.T) {
	t.Setenv("NUCLEUS_JWT_PREFIXED_SECRET", "prefixed-secret-aaaaaaaaaaaaaaaa")

	cfg := &Config{
		JWTExpiry: time.Hour,
		JWTKeys: []JWTKeySpec{
			{KID: "k1", Algorithm: "HS256", SecretEnv: "env:NUCLEUS_JWT_PREFIXED_SECRET"},
		},
		JWTCurrentKID: "k1",
	}
	mgr, err := buildJWTManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildJWTManager with env: prefix: %v", err)
	}
	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestBuildKeyMaterialResolver_LazyAWS verifies the AWS resolver is only
// constructed when a key actually references the aws-sm: scheme. An
// env-only keyset must not touch AWS config resolution at all.
func TestBuildKeyMaterialResolver_LazyAWS(t *testing.T) {
	// No aws-sm: reference → env-only chain, no AWS resolver.
	envOnly := []JWTKeySpec{
		{KID: "k1", Algorithm: "HS256", SecretEnv: "SOME_VAR"},
		{KID: "k2", Algorithm: "RS256", PemEnv: "SOME_PEM"},
	}
	r, err := buildKeyMaterialResolver(context.Background(), envOnly)
	if err != nil {
		t.Fatalf("buildKeyMaterialResolver (env-only): %v", err)
	}
	if r == nil {
		t.Fatal("resolver chain must never be nil")
	}
	// An aws-sm: reference against the env-only chain must error rather
	// than silently falling through — proves the AWS branch was not wired.
	if _, err := r.Resolve(context.Background(), "aws-sm:some/secret"); err == nil {
		t.Fatal("env-only chain must reject an aws-sm: reference")
	}
}

func TestBuildJWTManager_RS256FromPemPath(t *testing.T) {
	priv := mustRSA(t)
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "k1.pem")
	if err := os.WriteFile(pemPath, mustEncodePKCS8(t, priv), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}

	cfg := &Config{
		JWTExpiry: time.Hour,
		JWTKeys: []JWTKeySpec{
			{KID: "rsa-1", Algorithm: "RS256", PemPath: pemPath},
		},
		JWTCurrentKID: "rsa-1",
	}
	mgr, err := buildJWTManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildJWTManager: %v", err)
	}
	if !hasAsymmetricKey(mgr) {
		t.Fatal("manager should report at least one asymmetric key")
	}

	tok, err := mgr.Generate("u", "n", "r")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestBuildJWTManager_RS256FromPemEnv_PKCS1(t *testing.T) {
	// PKCS#1 PEM (`RSA PRIVATE KEY`) is the older format some
	// deployments still emit. The loader must accept both.
	priv := mustRSA(t)
	t.Setenv("NUCLEUS_JWT_PEM", string(mustEncodePKCS1(t, priv)))

	cfg := &Config{
		JWTExpiry: time.Hour,
		JWTKeys: []JWTKeySpec{
			{KID: "rsa-pkcs1", Algorithm: "RS256", PemEnv: "NUCLEUS_JWT_PEM"},
		},
		JWTCurrentKID: "rsa-pkcs1",
	}
	mgr, err := buildJWTManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildJWTManager: %v", err)
	}
	if mgr.CurrentKID() != "rsa-pkcs1" {
		t.Fatalf("unexpected current kid: %q", mgr.CurrentKID())
	}
}

func TestBuildJWTManager_RejectsBothPemFields(t *testing.T) {
	priv := mustRSA(t)
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "k1.pem")
	if err := os.WriteFile(pemPath, mustEncodePKCS8(t, priv), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	t.Setenv("NUCLEUS_JWT_PEM", string(mustEncodePKCS8(t, priv)))

	cfg := &Config{
		JWTKeys: []JWTKeySpec{
			{KID: "rsa-1", Algorithm: "RS256", PemPath: pemPath, PemEnv: "NUCLEUS_JWT_PEM"},
		},
		JWTCurrentKID: "rsa-1",
	}
	if _, err := buildJWTManager(context.Background(), cfg); err == nil {
		t.Fatal("expected error when both pem_path and pem_env are set")
	}
}

func TestBuildJWTManager_RejectsEmptyKID(t *testing.T) {
	t.Setenv("X", "y")
	cfg := &Config{
		JWTKeys:       []JWTKeySpec{{Algorithm: "HS256", SecretEnv: "X"}},
		JWTCurrentKID: "k-anything", // non-empty so the failure isolates to the empty kid
	}
	_, err := buildJWTManager(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when kid is missing")
	}
	if !strings.Contains(err.Error(), "kid is required") {
		t.Fatalf("error should mention kid, got %v", err)
	}
}

func TestBuildJWTManager_RejectsMissingCurrentKID(t *testing.T) {
	t.Setenv("X", "y")
	cfg := &Config{
		JWTKeys:       []JWTKeySpec{{KID: "k1", Algorithm: "HS256", SecretEnv: "X"}},
		JWTCurrentKID: "",
	}
	_, err := buildJWTManager(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when jwt_current_kid is empty")
	}
	if !strings.Contains(err.Error(), "jwt_current_kid") {
		t.Fatalf("error should mention jwt_current_kid, got %v", err)
	}
}

// TestBuildJWTManager_EmptySecretAndEmptyKeysReturnsNil pins the
// security-critical behaviour added after code review: when neither
// jwt_secret nor jwt_keys is configured, buildJWTManager returns a nil
// manager rather than auth.NewJWTManager("") which would silently
// produce a manager that signs tokens with the empty HMAC key. App.New
// records nil on App.JWT and logs a WARN.
func TestBuildJWTManager_EmptySecretAndEmptyKeysReturnsNil(t *testing.T) {
	mgr, err := buildJWTManager(context.Background(), &Config{}) // both fields zero
	if err != nil {
		t.Fatalf("buildJWTManager: %v", err)
	}
	if mgr != nil {
		t.Fatal("expected nil manager when no signing material is configured")
	}
}

// TestParseRSAPrivateKey_RejectsTrailingPEMContent guards the operator-
// mistake case where a key file and a certificate file have been
// concatenated. The loader must surface that rather than silently
// using only the first block.
func TestParseRSAPrivateKey_RejectsTrailingPEMContent(t *testing.T) {
	priv := mustRSA(t)
	combined := append(mustEncodePKCS8(t, priv), []byte("-----BEGIN CERTIFICATE-----\nMIIB...stub...\n-----END CERTIFICATE-----\n")...)
	if _, err := parseRSAPrivateKey(combined); err == nil {
		t.Fatal("expected error for PEM with trailing content")
	}
}

func TestBuildJWTManager_RejectsHS256WithoutSecretEnv(t *testing.T) {
	cfg := &Config{
		JWTKeys:       []JWTKeySpec{{KID: "k1", Algorithm: "HS256"}},
		JWTCurrentKID: "k1",
	}
	if _, err := buildJWTManager(context.Background(), cfg); err == nil {
		t.Fatal("expected error when HS256 has no secret_env")
	}
}

func TestBuildJWTManager_RejectsEmptyEnvValue(t *testing.T) {
	// Variable does not exist → empty resolution → fail loudly rather
	// than silently building a manager with an empty HMAC secret.
	cfg := &Config{
		JWTKeys:       []JWTKeySpec{{KID: "k1", Algorithm: "HS256", SecretEnv: "NUCLEUS_TEST_UNSET_VAR"}},
		JWTCurrentKID: "k1",
	}
	if _, err := buildJWTManager(context.Background(), cfg); err == nil {
		t.Fatal("expected error when secret_env resolves to empty")
	}
}

// ---------- App.New integration ----------

func TestAppNew_JWT_LegacySingleSecretByDefault(t *testing.T) {
	cfg := testAppConfig()
	cfg.JWTSecret = "legacy-secret"
	cfg.JWTExpiry = time.Hour

	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	if a.JWT == nil {
		t.Fatal("App.JWT should be non-nil")
	}
	if a.JWT.CurrentKID() != "" {
		t.Fatalf("legacy manager should have no kid, got %q", a.JWT.CurrentKID())
	}
}

func TestAppNew_JWT_MountsJWKSWhenAsymmetricKeyConfigured(t *testing.T) {
	priv := mustRSA(t)
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "k1.pem")
	if err := os.WriteFile(pemPath, mustEncodePKCS8(t, priv), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}

	cfg := testAppConfig()
	cfg.JWTExpiry = time.Hour
	cfg.JWTKeys = []JWTKeySpec{
		{KID: "rsa-2026", Algorithm: "RS256", PemPath: pemPath},
	}
	cfg.JWTCurrentKID = "rsa-2026"

	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	if a.JWT.CurrentKID() != "rsa-2026" {
		t.Fatalf("unexpected current kid: %q", a.JWT.CurrentKID())
	}

	// JWKS endpoint should respond with the public key.
	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /.well-known/jwks.json, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"kid":"rsa-2026"`) {
		t.Fatalf("expected kid in JWKS body, got %s", body)
	}
	if !strings.Contains(body, `"kty":"RSA"`) {
		t.Fatalf("expected kty in JWKS body, got %s", body)
	}
}

func TestAppNew_JWT_NoJWKSEndpointWhenHS256Only(t *testing.T) {
	t.Setenv("NUCLEUS_JWT_HMAC", "hmac-secret-aaaaaaaaaaaaaaaaaaaa")

	cfg := testAppConfig()
	cfg.JWTKeys = []JWTKeySpec{
		{KID: "k1", Algorithm: "HS256", SecretEnv: "NUCLEUS_JWT_HMAC"},
	}
	cfg.JWTCurrentKID = "k1"
	cfg.JWTExpiry = time.Hour

	a, err := New(cfg, WithOpenAuthz())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Shutdown(context.Background())

	// HS256-only manager has no public material to publish; the route
	// should be a 404 from the router rather than an empty `{"keys":[]}`.
	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	rec := httptest.NewRecorder()
	a.Router.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("HS256-only manager should not auto-mount JWKS; got 200 body=%s", rec.Body.String())
	}

	// The manager itself is still built and usable.
	if a.JWT == nil || a.JWT.CurrentKID() != "k1" {
		t.Fatalf("HS256 manager should be built, got %+v", a.JWT)
	}
}
