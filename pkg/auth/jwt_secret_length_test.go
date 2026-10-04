package auth

// Regression guard for NU-P2-7: the raw constructor must enforce the
// minimum HS256 secret length itself, not only pkg/app's config wiring —
// a direct NewJWTManager caller must never ship forgeable tokens.

import (
	"strings"
	"testing"
	"time"
)

func TestNewJWTManager_PanicsOnShortSecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for a secret shorter than 32 bytes")
		}
	}()
	NewJWTManager("short-secret", time.Hour)
}

func TestNewJWTManager_AcceptsMinimumLengthSecret(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef" // 32 bytes
	mgr := NewJWTManager(secret, time.Hour)
	tok, err := mgr.Generate("u1", "user", "member")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := mgr.Validate(tok); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// NU-41: the error form of the single-secret constructor reports a short
// secret as an error, and builds the same manager otherwise.
func TestNewJWTManagerFromSecret_ShortSecretIsAnError(t *testing.T) {
	mgr, err := NewJWTManagerFromSecret("short-secret", time.Hour)
	if err == nil || mgr != nil {
		t.Fatalf("NewJWTManagerFromSecret(short) = %v, %v; want nil and an error", mgr, err)
	}
	if !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("error %q does not state the minimum", err)
	}
}

func TestNewJWTManagerFromSecret_BuildsAWorkingManager(t *testing.T) {
	mgr, err := NewJWTManagerFromSecret("0123456789abcdef0123456789abcdef", time.Hour, "issuer-x")
	if err != nil {
		t.Fatalf("NewJWTManagerFromSecret: %v", err)
	}
	tok, err := mgr.Generate("u1", "user", "member")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	claims, err := mgr.Validate(tok)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Issuer != "issuer-x" {
		t.Fatalf("issuer = %q", claims.Issuer)
	}
}
