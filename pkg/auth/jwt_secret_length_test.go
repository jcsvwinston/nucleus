package auth

// Regression guard for NU-P2-7: the raw constructor must enforce the
// minimum HS256 secret length itself, not only pkg/app's config wiring —
// a direct NewJWTManager caller must never ship forgeable tokens.

import (
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
