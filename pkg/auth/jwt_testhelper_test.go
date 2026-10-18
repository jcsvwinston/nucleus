package auth

import (
	"testing"
	"time"
)

// mustJWT builds a single-secret manager for a test and fails it on a
// secret the constructor rejects.
func mustJWT(tb testing.TB, secret string, expiry time.Duration, issuer ...string) *JWTManager {
	tb.Helper()
	m, err := NewJWTManagerFromSecret(secret, expiry, issuer...)
	if err != nil {
		tb.Fatalf("NewJWTManagerFromSecret: %v", err)
	}
	return m
}
