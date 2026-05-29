package auth

import (
	"net/http"
	"testing"
)

// TestNewSessionManager_SameSiteNoneForcesSecure is the FW-4 defence-in-depth
// guard: a SameSite=None cookie configured with Secure=false would be silently
// dropped by browsers, so the manager must coerce Secure=true.
func TestNewSessionManager_SameSiteNoneForcesSecure(t *testing.T) {
	sm := NewSessionManager(SessionConfig{
		SameSite: "none",
		Secure:   false,
	})

	if sm.SCS().Cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("expected SameSite=None, got %v", sm.SCS().Cookie.SameSite)
	}
	if !sm.SCS().Cookie.Secure {
		t.Fatal("expected Secure to be forced true for SameSite=None")
	}
}

// TestNewSessionManager_SameSiteNoneWithSecureUnchanged confirms the override
// is a no-op when the operator already set Secure=true.
func TestNewSessionManager_SameSiteNoneWithSecureUnchanged(t *testing.T) {
	sm := NewSessionManager(SessionConfig{
		SameSite: "none",
		Secure:   true,
	})

	if sm.SCS().Cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("expected SameSite=None, got %v", sm.SCS().Cookie.SameSite)
	}
	if !sm.SCS().Cookie.Secure {
		t.Fatal("expected Secure to remain true")
	}
}

// TestNewSessionManager_SameSiteLaxDoesNotForceSecure confirms the coercion is
// scoped to SameSite=None only — a Lax cookie with Secure=false stays insecure
// (that combination is valid and the operator's choice).
func TestNewSessionManager_SameSiteLaxDoesNotForceSecure(t *testing.T) {
	sm := NewSessionManager(SessionConfig{
		SameSite: "lax",
		Secure:   false,
	})

	if sm.SCS().Cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite=Lax, got %v", sm.SCS().Cookie.SameSite)
	}
	if sm.SCS().Cookie.Secure {
		t.Fatal("expected Secure to remain false for SameSite=Lax")
	}
}
