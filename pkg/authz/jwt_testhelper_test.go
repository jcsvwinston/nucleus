package authz

import (
	"testing"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// mustJWT builds a single-secret manager for a test and fails it on a
// secret the constructor rejects.
func mustJWT(t *testing.T, secret string, expiry time.Duration, issuer ...string) *auth.JWTManager {
	t.Helper()
	m, err := auth.NewJWTManagerFromSecret(secret, expiry, issuer...)
	if err != nil {
		t.Fatalf("NewJWTManagerFromSecret: %v", err)
	}
	return m
}
